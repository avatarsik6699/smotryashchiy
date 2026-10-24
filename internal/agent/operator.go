package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// Operator command channel limits (docs/SPEC.md §4j).
const (
	OperatorPollInterval = 15 * time.Second
	OperatorTimeout      = 60 * time.Second
	maxOperatorOutput    = 64 << 10
	maxOperatorError     = 2 << 10
)

// OperatorKinds is the agent's own copy of the allow-list: a kind missing here is refused even if
// the server queued it.
var OperatorKinds = map[string]bool{
	"warn": true, "suspend": true, "terminate": true, "lift": true,
	"refund_execute": true, "account_show": true,
}

// Command is one operator command the server handed to this host.
type Command struct {
	ID   string          `json:"id"`
	Kind string          `json:"kind"`
	Args json.RawMessage `json:"args"`
}

// CommandReport is the host's answer to one command.
type CommandReport struct {
	Status string          `json:"status"` // done | failed
	Result json.RawMessage `json:"result"`
	Error  string          `json:"error,omitempty"`
}

// CommandLink is the part of a tunnel session the operator poller uses; *Session implements it.
type CommandLink interface {
	Commands(ctx context.Context) ([]Command, error)
	Report(ctx context.Context, id string, r CommandReport) error
}

// Commands claims this host's queued operator commands.
func (s *Session) Commands(ctx context.Context) ([]Command, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.base+"/agent/commands", nil)
	if err != nil {
		return nil, fmt.Errorf("agent: build commands request: %w", err)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("agent: commands request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, &StatusError{Code: resp.StatusCode, Body: strings.TrimSpace(string(raw))}
	}
	var out struct {
		Commands []Command `json:"commands"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("agent: decode commands: %w", err)
	}
	return out.Commands, nil
}

// Report sends the answer to one command.
func (s *Session) Report(ctx context.Context, id string, r CommandReport) error {
	body, err := json.Marshal(r)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.base+"/agent/commands/"+id+"/result", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("agent: build result request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("agent: result request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusNoContent {
		return &StatusError{Code: resp.StatusCode, Body: strings.TrimSpace(string(raw))}
	}
	return nil
}

// Runner executes the host's operator executable: argv only, the args JSON on stdin.
type Runner func(ctx context.Context, path, kind string, stdin []byte) (stdout, stderr []byte, err error)

// ExecRunner runs path with a clean environment and no shell.
func ExecRunner(ctx context.Context, path, kind string, stdin []byte) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, path, kind)
	cmd.Env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C.UTF-8"}
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout, stderr limitedBuffer
	stdout.max, stderr.max = maxOperatorOutput+1, 1<<20
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.WaitDelay = 5 * time.Second
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

// Operator polls for commands and answers them through the shared tunnel session.
type Operator struct {
	Path     string
	Link     func(ctx context.Context) (Uplink, error)
	Run      Runner
	Interval time.Duration
	Timeout  time.Duration
	Log      *slog.Logger
}

// Loop polls every Interval until ctx is done; failures are logged and retried on the next tick.
func (o *Operator) Loop(ctx context.Context) {
	ticker := time.NewTicker(o.Interval)
	defer ticker.Stop()
	for {
		if err := o.Poll(ctx); err != nil && ctx.Err() == nil {
			o.Log.Warn("operator command poll failed; will retry", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Poll claims the queued commands once and answers each of them.
func (o *Operator) Poll(ctx context.Context) error {
	up, err := o.Link(ctx)
	if err != nil {
		return err
	}
	link, ok := up.(CommandLink)
	if !ok {
		return errors.New("agent: the tunnel session cannot carry operator commands")
	}
	cmds, err := link.Commands(ctx)
	if err != nil {
		return err
	}
	for _, c := range cmds {
		report := o.Execute(ctx, c)
		o.Log.Info("operator command finished", "id", c.ID, "kind", c.Kind, "status", report.Status)
		if err := link.Report(ctx, c.ID, report); err != nil {
			return fmt.Errorf("agent: report command %s: %w", c.ID, err)
		}
	}
	return nil
}

// Execute runs one command and turns its outcome into a report; it never returns an error.
func (o *Operator) Execute(ctx context.Context, c Command) CommandReport {
	if !OperatorKinds[c.Kind] {
		return CommandReport{Status: "failed", Error: "kind not allowed on this host"}
	}
	args := bytes.TrimSpace(c.Args)
	if len(args) == 0 || args[0] != '{' || !json.Valid(args) {
		return CommandReport{Status: "failed", Error: "args must be a JSON object"}
	}
	runCtx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()
	stdout, stderr, err := o.Run(runCtx, o.Path, c.Kind, args)
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return CommandReport{Status: "failed", Error: fmt.Sprintf("timed out after %s", o.Timeout)}
	}
	out := bytes.TrimSpace(stdout)
	object := len(out) > 0 && len(out) <= maxOperatorOutput && out[0] == '{' && json.Valid(out)
	if err != nil {
		message := tail(stderr)
		if object {
			var refusal struct {
				Error string `json:"error"`
			}
			if json.Unmarshal(out, &refusal) == nil && refusal.Error != "" {
				message = refusal.Error
			}
		}
		if message == "" {
			message = err.Error()
		}
		return CommandReport{Status: "failed", Error: message}
	}
	if !object {
		return CommandReport{Status: "failed", Error: "the executable did not print one JSON object of at most 64 KiB"}
	}
	return CommandReport{Status: "done", Result: json.RawMessage(out)}
}

// tail keeps the last maxOperatorError bytes of text, cut on a valid UTF-8 boundary.
func tail(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > maxOperatorError {
		s = s[len(s)-maxOperatorError:]
	}
	return strings.ToValidUTF8(s, "")
}

// limitedBuffer keeps at most max bytes and silently drops the rest, so a runaway executable
// cannot exhaust the agent's memory.
type limitedBuffer struct {
	bytes.Buffer
	max int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := b.max - b.Len(); room > 0 {
		if len(p) > room {
			b.Buffer.Write(p[:room])
		} else {
			b.Buffer.Write(p)
		}
	}
	return len(p), nil
}
