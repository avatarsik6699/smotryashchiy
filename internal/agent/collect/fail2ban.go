package collect

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultFail2banLog is fail2ban's standard log path on Debian/Ubuntu.
const DefaultFail2banLog = "/var/log/fail2ban.log"

// fail2banLineRe matches fail2ban's own ban/unban log lines, e.g.
// `2026-09-22 10:00:00,000 fail2ban.actions [1234]: NOTICE [sshd] Ban 203.0.113.7`.
var fail2banLineRe = regexp.MustCompile(`NOTICE\s+\[([\w.-]+)\]\s+(Ban|Unban)\s+(\S+)`)

// Fail2banEvents tails fail2ban's own log (`tail -F`, so it survives log rotation) for ban/unban
// lines and forwards them as events (docs/SPEC.md §4h). It disables itself (Collect returns an
// error) when the log is absent/unreadable, e.g. fail2ban is not installed.
type Fail2banEvents struct {
	mu     sync.Mutex
	buf    []Event
	failed error
}

// NewFail2banEvents starts tailing immediately; ctx bounds the subprocess's lifetime. logPath ""
// defaults to DefaultFail2banLog.
func NewFail2banEvents(ctx context.Context, logPath string) *Fail2banEvents {
	if logPath == "" {
		logPath = DefaultFail2banLog
	}
	f := &Fail2banEvents{}
	f.start(ctx, logPath)
	return f
}

func (*Fail2banEvents) Name() string { return "fail2ban_events" }

func (f *Fail2banEvents) start(ctx context.Context, logPath string) {
	cmd := exec.CommandContext(ctx, "tail", "-F", "-n0", logPath)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		f.setFailed(err)
		return
	}
	if err := cmd.Start(); err != nil {
		f.setFailed(err)
		return
	}
	go f.readLoop(stdout)
	go func() {
		if err := cmd.Wait(); err != nil && ctx.Err() == nil {
			f.setFailed(err)
		} else if ctx.Err() == nil {
			f.setFailed(errors.New("fail2ban: tail exited"))
		}
	}()
}

func (f *Fail2banEvents) setFailed(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failed == nil {
		f.failed = err
	}
}

func (f *Fail2banEvents) readLoop(stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		m := fail2banLineRe.FindStringSubmatch(scanner.Text())
		if m == nil {
			continue
		}
		jail, action, ip := m[1], m[2], m[3]
		level := "warn"
		if action == "Unban" {
			level = "info"
		}
		ev := Event{Level: level, Message: action + " " + ip, Labels: map[string]string{"jail": jail}}
		f.mu.Lock()
		f.buf = append(f.buf, ev)
		f.mu.Unlock()
	}
	if err := scanner.Err(); err != nil {
		f.setFailed(err)
	}
}

// Collect drains whatever the tail goroutine has buffered since the last call. No new bans/unbans
// is not an error.
func (f *Fail2banEvents) Collect() ([]Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failed != nil {
		return nil, f.failed
	}
	if len(f.buf) == 0 {
		return nil, nil
	}
	out := f.buf
	f.buf = nil
	return out, nil
}

// Fail2banStatus reports each jail's currently-banned count as a check (docs/SPEC.md §4h), via
// `fail2ban-client status`/`status <jail>` — stable, well-documented text output — rather than
// parsing fail2ban's own jail-config cascade. It disables itself when fail2ban-client is
// unavailable or its control socket is unreachable (e.g. insufficient privilege).
type Fail2banStatus struct{}

// NewFail2banStatus builds the collector; it probes fail2ban-client lazily on each Collect.
func NewFail2banStatus() *Fail2banStatus { return &Fail2banStatus{} }

func (*Fail2banStatus) Name() string { return "fail2ban_status" }

var (
	jailListRe    = regexp.MustCompile(`Jail list:\s*(.*)`)
	bannedCountRe = regexp.MustCompile(`Currently banned:\s*(\d+)`)
)

// checkNameSafe maps an arbitrary jail name into the server's name charset
// (^[a-z][a-z0-9_.]{0,127}$, docs/SPEC.md §4.1): fail2ban jail names commonly contain hyphens
// (e.g. "nginx-limit-req"), which that regex forbids. The original jail name is kept in Meta.
func checkNameSafe(jail string) string {
	jail = strings.ToLower(jail)
	var b strings.Builder
	for _, r := range jail {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	safe := b.String()
	if safe == "" || (safe[0] < 'a' || safe[0] > 'z') {
		safe = "j_" + safe
	}
	return safe
}

func (*Fail2banStatus) Collect() ([]Check, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "fail2ban-client", "status").Output()
	if err != nil {
		return nil, fmt.Errorf("fail2ban: status: %w", err)
	}
	m := jailListRe.FindStringSubmatch(string(out))
	if m == nil {
		return nil, errors.New("fail2ban: could not parse jail list")
	}
	var checks []Check
	for _, jail := range strings.Split(m[1], ",") {
		jail = strings.TrimSpace(jail)
		if jail == "" {
			continue
		}
		jctx, jcancel := context.WithTimeout(context.Background(), 5*time.Second)
		jout, jerr := exec.CommandContext(jctx, "fail2ban-client", "status", jail).Output()
		jcancel()
		if jerr != nil {
			continue // one jail failing to report doesn't drop the others
		}
		banned := 0
		if bm := bannedCountRe.FindStringSubmatch(string(jout)); bm != nil {
			banned, _ = strconv.Atoi(bm[1])
		}
		checks = append(checks, Check{
			Name:   "fail2ban.jail." + checkNameSafe(jail),
			Status: "ok",
			Meta:   map[string]any{"currently_banned": banned, "jail": jail},
		})
	}
	return checks, nil
}
