package application

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

const (
	// rerollWindow is how far back each rollup run re-aggregates, so data that arrived late for an
	// already-rolled hour is still picked up (docs/SPEC.md §4.4).
	rerollWindow = 24 * time.Hour
	// purgeBatch bounds every delete statement so the single SQLite writer is never held long.
	purgeBatch = 5000

	rollupInterval = time.Hour
	purgeInterval  = 24 * time.Hour
)

// Maintenance runs the rollup and retention jobs (docs/SPEC.md §4.4–§4.5).
type Maintenance struct {
	repo      MaintenanceRepository
	now       func() time.Time
	rawTTL    time.Duration
	rollupTTL time.Duration
}

// NewMaintenance builds the jobs with the given TTLs in days and an injectable clock.
func NewMaintenance(repo MaintenanceRepository, rawDays, rollupDays int, now func() time.Time) *Maintenance {
	day := 24 * time.Hour
	return &Maintenance{repo: repo, now: now, rawTTL: time.Duration(rawDays) * day, rollupTTL: time.Duration(rollupDays) * day}
}

// Rollup aggregates every closed hour that is not yet rolled up, plus the last 24 hours again to
// absorb late data. It is idempotent: re-running it changes nothing when no data changed.
func (m *Maintenance) Rollup(ctx context.Context) error {
	current := m.now().UTC().Truncate(time.Hour)
	earliest, hasRaw, err := m.repo.EarliestMetric(ctx)
	if err != nil {
		return fmt.Errorf("telemetry: rollup earliest metric: %w", err)
	}
	through, rolled, err := m.repo.RolledThrough(ctx)
	if err != nil {
		return fmt.Errorf("telemetry: rollup state: %w", err)
	}
	if hasRaw {
		from := earliest.UTC().Truncate(time.Hour)
		if rolled && through.Add(-rerollWindow).After(from) {
			from = through.Add(-rerollWindow)
		}
		for hour := from; hour.Before(current); hour = hour.Add(time.Hour) {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := m.repo.RollupHour(ctx, hour); err != nil {
				return fmt.Errorf("telemetry: rollup hour %s: %w", hour.Format(time.RFC3339), err)
			}
		}
	}
	if rolled && !current.After(through) {
		return nil // never move the marker backwards (clock skew)
	}
	return m.repo.SetRolledThrough(ctx, current)
}

// Purge deletes expired raw data and rollups in bounded batches. Raw rows are only deleted once
// their hour is rolled up, and never inside the re-aggregation window, so a rollup can always be
// recomputed from complete raw data. Until the first rollup has completed nothing raw is purged.
func (m *Maintenance) Purge(ctx context.Context) error {
	now := m.now().UTC()
	through, rolled, err := m.repo.RolledThrough(ctx)
	if err != nil {
		return fmt.Errorf("telemetry: purge state: %w", err)
	}
	if rolled {
		rawCutoff := now.Add(-m.rawTTL)
		if safe := through.Add(-rerollWindow); rawCutoff.After(safe) {
			rawCutoff = safe
		}
		if err := drain(ctx, func() (int, error) { return m.repo.PurgeRaw(ctx, rawCutoff, purgeBatch) }); err != nil {
			return fmt.Errorf("telemetry: purge raw: %w", err)
		}
	}
	rollupCutoff := now.Add(-m.rollupTTL).Truncate(time.Hour)
	if err := drain(ctx, func() (int, error) { return m.repo.PurgeRollups(ctx, rollupCutoff, purgeBatch) }); err != nil {
		return fmt.Errorf("telemetry: purge rollups: %w", err)
	}
	return nil
}

func drain(ctx context.Context, step func() (int, error)) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := step()
		if err != nil {
			return err
		}
		if n == 0 {
			return nil
		}
	}
}

// Run executes rollup then purge immediately and then on their intervals until ctx is done.
// Failures are logged and retried at the next tick; they never stop the server.
func (m *Maintenance) Run(ctx context.Context) {
	m.runOnce(ctx, "rollup", m.Rollup)
	m.runOnce(ctx, "purge", m.Purge)
	rollupTick := time.NewTicker(rollupInterval)
	purgeTick := time.NewTicker(purgeInterval)
	defer rollupTick.Stop()
	defer purgeTick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-rollupTick.C:
			m.runOnce(ctx, "rollup", m.Rollup)
		case <-purgeTick.C:
			m.runOnce(ctx, "purge", m.Purge)
		}
	}
}

func (m *Maintenance) runOnce(ctx context.Context, name string, job func(context.Context) error) {
	if err := job(ctx); err != nil && ctx.Err() == nil {
		slog.Error("telemetry maintenance job failed", "job", name, "err", err)
	}
}
