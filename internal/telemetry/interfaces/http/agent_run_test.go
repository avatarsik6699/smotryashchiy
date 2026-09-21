package http

// End-to-end tests of the agent run loop against the real in-process server: collection, spool,
// outage/recovery, restart resume and permanent rejection (docs/SPEC.md §4c).

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/agent"
	"github.com/avatarsik6699/smotryashchiy/internal/agent/collect"
)

// counter is a collector whose value is the tick number (the first tick is a measured 0).
type counter struct{ n atomic.Int64 }

func (*counter) Name() string { return "counter" }
func (c *counter) Collect() ([]collect.Sample, error) {
	return []collect.Sample{{Name: "test.tick", Value: float64(c.n.Add(1) - 1)}}, nil
}

// silent collects nothing, so a run with it only drains what earlier runs left in the spool.
type silent struct{}

func (silent) Name() string                       { return "silent" }
func (silent) Collect() ([]collect.Sample, error) { return nil, nil }

type flakyUplink struct {
	agent.Uplink
	down *atomic.Bool
}

func (f flakyUplink) Send(ctx context.Context, key string, batch []byte) (agent.Result, error) {
	if f.down.Load() {
		return agent.Result{}, errors.New("simulated outage")
	}
	return f.Uplink.Send(ctx, key, batch)
}

type runHandle struct {
	cancel context.CancelFunc
	done   chan error
}

func (h runHandle) stop(t *testing.T) {
	t.Helper()
	h.cancel()
	select {
	case err := <-h.done:
		if err != nil {
			t.Fatalf("agent.Run: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("agent did not stop")
	}
}

// startAgent runs the agent with a virtual clock (clock + n ms per tick) so batches are always
// inside the server's accepted window; badFirst makes the first batch 1 h in the future.
func (e *env) startAgent(t *testing.T, cfg agent.Config, spoolDir string, c collect.Collector, down *atomic.Bool, badFirst bool) runHandle {
	t.Helper()
	var ticks atomic.Int64
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- agent.Run(ctx, agent.RunConfig{
			Interval:   30 * time.Millisecond,
			SpoolDir:   spoolDir,
			Collectors: []collect.Collector{c},
			Now: func() time.Time {
				n := ticks.Add(1)
				if badFirst && n == 1 {
					return clock.Add(time.Hour)
				}
				return clock.Add(time.Duration(n) * time.Millisecond)
			},
			Dial: func(ctx context.Context) (agent.Uplink, error) {
				if down.Load() {
					return nil, errors.New("simulated outage: no route")
				}
				s, err := agent.Dial(ctx, cfg)
				if err != nil {
					return nil, err
				}
				return flakyUplink{Uplink: s, down: down}, nil
			},
			Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		})
	}()
	return runHandle{cancel: cancel, done: done}
}

func spoolFiles(t *testing.T, dir string) int {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	n := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			n++
		}
	}
	return n
}

type seriesBody struct {
	Metrics []struct {
		TS    time.Time `json:"ts"`
		Value float64   `json:"value"`
	} `json:"metrics"`
}

func (e *env) series(t *testing.T, host string) seriesBody {
	t.Helper()
	code, body := e.get(t, "/api/metrics?name=test.tick&limit=5000&host="+host, true)
	if code != 200 {
		t.Fatalf("read API: %d %s", code, body)
	}
	var out seriesBody
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestAgentRunDeliversMeasuredZeroAndLaterTicks(t *testing.T) {
	e := newEnv(t)
	cfg, _ := e.enrolled(t, "vps-1")
	c, down := &counter{}, &atomic.Bool{}
	h := e.startAgent(t, cfg, filepath.Join(t.TempDir(), "spool"), c, down, false)
	waitFor(t, func() bool { return len(e.series(t, cfg.HostID).Metrics) >= 3 })
	h.stop(t)
	s := e.series(t, cfg.HostID)
	if s.Metrics[0].Value != 0 {
		t.Fatalf("first measured value = %v, want the measured zero to be stored", s.Metrics[0].Value)
	}
}

func TestOutageThenRecoveryDeliversEveryBatchExactlyOnceInOrder(t *testing.T) {
	e := newEnv(t)
	cfg, _ := e.enrolled(t, "vps-1")
	spoolDir := filepath.Join(t.TempDir(), "spool")
	c, down := &counter{}, &atomic.Bool{}
	down.Store(true)
	h := e.startAgent(t, cfg, spoolDir, c, down, false)

	waitFor(t, func() bool { return spoolFiles(t, spoolDir) >= 8 })
	if n := len(e.series(t, cfg.HostID).Metrics); n != 0 {
		t.Fatalf("%d batches reached the server during the outage", n)
	}
	down.Store(false)
	// The sender is in exponential backoff (1 s, 2 s, 4 s ...) when the outage ends.
	waitForWithin(t, 20*time.Second, func() bool { return len(e.series(t, cfg.HostID).Metrics) >= 8 })
	h.stop(t)

	// After stopping, deliver whatever was still queued by resuming with a fresh run and no new data.
	produced := int(c.n.Load())
	h2 := e.startAgent(t, cfg, spoolDir, silent{}, down, false)
	waitForWithin(t, 20*time.Second, func() bool { return spoolFiles(t, spoolDir) == 0 && len(e.series(t, cfg.HostID).Metrics) >= produced })
	h2.stop(t)

	s := e.series(t, cfg.HostID)
	seen := map[float64]bool{}
	prev := time.Time{}
	for _, m := range s.Metrics {
		if m.Value < float64(produced) { // values 0..produced-1 come from the first run's counter
			if seen[m.Value] {
				t.Fatalf("value %v stored twice", m.Value)
			}
			seen[m.Value] = true
		}
		if m.TS.Before(prev) {
			t.Fatal("read API must order by ts")
		}
		prev = m.TS
	}
	for i := 0; i < produced; i++ {
		if !seen[float64(i)] {
			t.Fatalf("tick %d was lost across the outage (%d produced, %d stored)", i, produced, len(seen))
		}
	}
}

func TestRestartResumesTheSpoolLeftByAPreviousRun(t *testing.T) {
	e := newEnv(t)
	cfg, _ := e.enrolled(t, "vps-1")
	spoolDir := filepath.Join(t.TempDir(), "spool")
	down := &atomic.Bool{}
	down.Store(true)
	first := e.startAgent(t, cfg, spoolDir, &counter{}, down, false)
	waitFor(t, func() bool { return spoolFiles(t, spoolDir) >= 3 })
	first.stop(t) // process "crashes" with undelivered data on disk
	left := spoolFiles(t, spoolDir)
	if left < 3 {
		t.Fatalf("spool holds %d batches, want the undelivered ones kept", left)
	}
	down.Store(false)
	second := e.startAgent(t, cfg, spoolDir, &counter{}, down, false)
	waitFor(t, func() bool { return len(e.series(t, cfg.HostID).Metrics) >= left })
	second.stop(t)
}

func TestPermanentlyRejectedBatchIsDroppedAndDoesNotBlockLaterOnes(t *testing.T) {
	e := newEnv(t)
	cfg, _ := e.enrolled(t, "vps-1")
	spoolDir := filepath.Join(t.TempDir(), "spool")
	h := e.startAgent(t, cfg, spoolDir, &counter{}, &atomic.Bool{}, true) // tick 1 is from the future
	waitFor(t, func() bool { return len(e.series(t, cfg.HostID).Metrics) >= 3 })
	h.stop(t)
	for _, m := range e.series(t, cfg.HostID).Metrics {
		if m.Value == 0 {
			t.Fatal("the future-dated batch (tick 0) must have been rejected by the server, not stored")
		}
	}
	if n := spoolFiles(t, spoolDir); n > 3 {
		t.Fatalf("%d batches still queued; the rejected batch must not block the queue", n)
	}
}

func waitForWithin(t *testing.T, limit time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("condition not reached within %s", limit)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
