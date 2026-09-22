package collect

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

// Journald tails the systemd journal continuously (`journalctl -f -o json`, started once and kept
// running for the agent's lifetime) and forwards new lines as events (docs/SPEC.md §4h). It
// disables itself (Collect returns an error) when journalctl is unavailable, e.g. a non-systemd
// host, or once the subprocess has exited.
type Journald struct {
	mu     sync.Mutex
	buf    []Event
	failed error
}

// NewJournald starts tailing immediately; ctx bounds the subprocess's lifetime (it is killed when
// ctx is done, matching the agent run loop's own shutdown).
func NewJournald(ctx context.Context) *Journald {
	j := &Journald{}
	j.start(ctx)
	return j
}

func (*Journald) Name() string { return "journald" }

func (j *Journald) start(ctx context.Context) {
	cmd := exec.CommandContext(ctx, "journalctl", "-f", "-o", "json", "--since", "now", "--no-pager")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		j.setFailed(err)
		return
	}
	if err := cmd.Start(); err != nil {
		j.setFailed(err)
		return
	}
	go j.readLoop(stdout)
	go func() {
		if err := cmd.Wait(); err != nil && ctx.Err() == nil {
			j.setFailed(err) // an unexpected exit; a context-cancel shutdown is not a failure
		} else if ctx.Err() == nil {
			j.setFailed(errors.New("journald: journalctl exited"))
		}
	}()
}

func (j *Journald) setFailed(err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.failed == nil {
		j.failed = err
	}
}

type journaldLine struct {
	Message  string `json:"MESSAGE"`
	Priority string `json:"PRIORITY"`
	Unit     string `json:"_SYSTEMD_UNIT"`
	RealTime string `json:"__REALTIME_TIMESTAMP"` // microseconds since epoch
}

func (j *Journald) readLoop(stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		var line journaldLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			continue // one malformed line must not stop the tail
		}
		if isRoutineHealthCheck(line.Message) {
			continue // docs/SPEC.md §4h: routine 2xx health-check polling is not forwarded
		}
		ts := time.Now()
		if us, err := strconv.ParseInt(line.RealTime, 10, 64); err == nil {
			ts = time.UnixMicro(us)
		}
		labels := map[string]string{}
		if line.Unit != "" {
			labels["unit"] = line.Unit
		}
		ev := Event{TS: ts, Level: journaldLevel(line.Priority), Message: truncateMessage(line.Message), Labels: labels}
		j.mu.Lock()
		j.buf = append(j.buf, ev)
		j.mu.Unlock()
	}
	if err := scanner.Err(); err != nil {
		j.setFailed(err)
	}
}

// journaldLevel maps a syslog priority (0=emerg..7=debug) to the wire event levels (docs/SPEC.md
// §4.1). An unparsable priority defaults to info rather than being dropped.
func journaldLevel(priority string) string {
	n, err := strconv.Atoi(priority)
	if err != nil {
		return "info"
	}
	switch {
	case n <= 3: // emerg, alert, crit, err
		return "error"
	case n == 4: // warning
		return "warn"
	default: // notice, info, debug
		return "info"
	}
}

// maxEventMessage mirrors the server's validation cap (docs/SPEC.md §4.1); truncating here avoids
// sending a whole batch that the server would reject over one oversized line.
const maxEventMessage = 2048

func truncateMessage(s string) string {
	if len(s) <= maxEventMessage {
		return s
	}
	return s[:maxEventMessage]
}

// Collect drains whatever the tail goroutine has buffered since the last call. An empty result is
// not an error: quiet logs are the common case.
func (j *Journald) Collect() ([]Event, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.failed != nil {
		return nil, j.failed
	}
	if len(j.buf) == 0 {
		return nil, nil
	}
	out := j.buf
	j.buf = nil
	return out, nil
}
