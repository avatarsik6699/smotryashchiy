// Package application holds the telemetry use-cases: ingestion and querying.
package application

import (
	"context"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/telemetry/domain"
)

// Counts is a per-kind record tally.
type Counts struct{ Metrics, Checks, Events int }

// StoreResult reports what a stored batch changed.
type StoreResult struct {
	// Replayed is true when the Idempotency-Key was already applied; nothing was written.
	Replayed bool
	// Accepted holds only the records that were new (already normalized).
	Accepted domain.Batch
	// Duplicates counts records skipped because an identical record already existed.
	Duplicates Counts
}

// MetricQuery selects metric samples. Zero-valued optional fields mean "no filter".
type MetricQuery struct {
	HostID string
	Name   string
	From   *time.Time
	To     *time.Time
	Limit  int
	Latest bool
	// Step, when > 0, keeps only the newest sample of every Step-second bucket per series.
	Step int
}

// Resolution selects raw samples or hourly rollups for a metric query.
const (
	ResolutionRaw  = "raw"
	ResolutionHour = "hour"
)

// EventQuery selects events, newest first.
type EventQuery struct {
	HostID string
	Level  string
	Limit  int
}

// CheckQuery selects the newest Check per host and name.
type CheckQuery struct {
	HostID string
	Name   string
}

// Repository is the persistence port. Store must apply a whole batch atomically.
type Repository interface {
	Store(ctx context.Context, hostID, idempotencyKey string, receivedAt time.Time, batch domain.Batch) (StoreResult, error)
	Metrics(ctx context.Context, q MetricQuery) ([]domain.MetricPoint, error)
	Checks(ctx context.Context, q CheckQuery) ([]domain.CheckState, error)
	Events(ctx context.Context, q EventQuery) ([]domain.EventEntry, error)
	// Hosts lists every registered host ordered by name.
	Hosts(ctx context.Context) ([]domain.Host, error)
	// MetricRollups returns hourly aggregates, oldest first; From/To bound the hour start.
	MetricRollups(ctx context.Context, q MetricQuery) ([]domain.RollupPoint, error)
}

// MaintenanceRepository is the persistence port for the rollup and retention jobs.
type MaintenanceRepository interface {
	// RolledThrough returns the instant before which every hour has been aggregated; ok is false
	// until the first rollup run has completed.
	RolledThrough(ctx context.Context) (t time.Time, ok bool, err error)
	SetRolledThrough(ctx context.Context, t time.Time) error
	// EarliestMetric returns the timestamp of the oldest raw metric; ok is false when none exist.
	EarliestMetric(ctx context.Context) (t time.Time, ok bool, err error)
	// RollupHour re-aggregates the raw metrics of the hour starting at hour, overwriting any
	// previous aggregate. It is idempotent and a no-op for an hour without raw metrics.
	RollupHour(ctx context.Context, hour time.Time) error
	// PurgeRaw deletes up to limit rows per table (metrics, checks, events, ingestion batches)
	// older than before and returns the total number deleted.
	PurgeRaw(ctx context.Context, before time.Time, limit int) (int, error)
	// PurgeRollups deletes up to limit rollups whose hour starts before before.
	PurgeRollups(ctx context.Context, before time.Time, limit int) (int, error)
}
