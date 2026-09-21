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
}

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
}
