package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/agent/collect"
	"github.com/avatarsik6699/smotryashchiy/internal/agent/spool"
)

// Interval bounds (docs/SPEC.md §4c).
const (
	DefaultInterval = 10 * time.Second
	MinInterval     = 5 * time.Second
	MaxInterval     = 5 * time.Minute
)

// RunConfig configures the long-running agent.
type RunConfig struct {
	Interval        time.Duration
	SpoolDir        string
	MaxBatches      int   // 0 = spool default
	MaxBytes        int64 // 0 = spool default
	Collectors      []collect.Collector
	CheckCollectors []collect.CheckCollector
	EventCollectors []collect.EventCollector
	Dial            Dialer
	Now             func() time.Time
	Log             *slog.Logger
}

// ValidateInterval reports whether d is inside the allowed range.
func ValidateInterval(d time.Duration) error {
	if d < MinInterval || d > MaxInterval {
		return fmt.Errorf("agent: interval must be between %s and %s, got %s", MinInterval, MaxInterval, d)
	}
	return nil
}

// Run collects a batch every interval, writes it to the spool first and lets the sender deliver it,
// until ctx is done. Anything not yet delivered stays in the spool for the next start.
func Run(ctx context.Context, rc RunConfig) error {
	if rc.Interval <= 0 {
		return errors.New("agent: interval must be positive") // the 5s..5m operator range is enforced by ValidateInterval at the CLI
	}
	if rc.Now == nil {
		rc.Now = time.Now
	}
	if rc.Log == nil {
		rc.Log = slog.Default()
	}
	if len(rc.Collectors) == 0 {
		return errors.New("agent: no collectors configured")
	}
	sp, err := spool.Open(rc.SpoolDir, rc.MaxBatches, rc.MaxBytes)
	if err != nil {
		return err
	}
	rc.Log.Info("agent started", "interval", rc.Interval, "spool_dir", rc.SpoolDir, "queued", sp.Len())

	sender := NewSender(sp, rc.Dial, rc.Log)
	kick := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		sender.Run(ctx, kick)
	}()
	if sp.Len() > 0 {
		kick <- struct{}{} // resume delivery of what a previous run left behind
	}

	failing := map[string]bool{} // collector -> currently failing (log once per streak)
	warnOnce := func(name string, err error) {
		if !failing[name] {
			rc.Log.Warn("collector failed; its output is omitted until it recovers", "collector", name, "error", err)
		}
		failing[name] = true
	}
	recovered := func(name string) {
		if failing[name] {
			rc.Log.Info("collector recovered", "collector", name)
			failing[name] = false
		}
	}
	tick := func() {
		var samples []collect.Sample
		for _, c := range rc.Collectors {
			got, err := c.Collect()
			if err != nil {
				warnOnce(c.Name(), err)
				continue
			}
			recovered(c.Name())
			samples = append(samples, got...)
		}
		var checks []collect.Check
		for _, c := range rc.CheckCollectors {
			got, err := c.Collect()
			if err != nil {
				warnOnce(c.Name(), err)
				continue
			}
			recovered(c.Name())
			checks = append(checks, got...)
		}
		var events []collect.Event
		for _, c := range rc.EventCollectors {
			got, err := c.Collect()
			if err != nil {
				warnOnce(c.Name(), err)
				continue
			}
			recovered(c.Name())
			events = append(events, got...)
		}
		batch, skipped, err := BuildBatch(rc.Now(), samples, checks, events)
		if err != nil {
			rc.Log.Error("could not encode batch", "error", err)
			return
		}
		if skipped > 0 {
			rc.Log.Warn("dropped non-finite samples", "count", skipped)
		}
		if batch == nil {
			return
		}
		dropped, err := sp.Add(NewKey(), batch)
		if err != nil {
			rc.Log.Error("could not spool batch", "error", err)
			return
		}
		if dropped > 0 {
			rc.Log.Warn("spool full; dropped the oldest batches", "dropped", dropped)
		}
		select {
		case kick <- struct{}{}:
		default: // a kick is already pending
		}
	}

	tick()
	ticker := time.NewTicker(rc.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			<-done
			rc.Log.Info("agent stopped", "queued", sp.Len())
			return nil
		case <-ticker.C:
			tick()
		}
	}
}
