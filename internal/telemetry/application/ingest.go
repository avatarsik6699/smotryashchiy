package application

import (
	"context"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/platform/apierror"
	"github.com/avatarsik6699/smotryashchiy/internal/telemetry/domain"
)

// Service implements ingestion and querying over a Repository.
type Service struct {
	repo Repository
	now  func() time.Time
}

// NewService wires a Service to its Repository using the system clock.
func NewService(repo Repository) *Service { return NewServiceWithClock(repo, time.Now) }

// NewServiceWithClock is NewService with an injectable clock for deterministic tests.
func NewServiceWithClock(repo Repository, now func() time.Time) *Service {
	return &Service{repo: repo, now: now}
}

// Ingest validates batch and stores it atomically for hostID. A batch with any invalid record is
// rejected as a whole. Re-sending an idempotencyKey is a no-op reported as Replayed; records that
// overlap earlier data are stored once and reported as Duplicates, and Accepted contains only the
// genuinely new records so downstream alert evaluation never re-fires on replays.
func (s *Service) Ingest(ctx context.Context, hostID, idempotencyKey string, batch domain.Batch) (StoreResult, error) {
	if hostID == "" {
		return StoreResult{}, apierror.Invalid("host is required")
	}
	if idempotencyKey == "" || len(idempotencyKey) > domain.MaxIdempotencyKey {
		return StoreResult{}, apierror.Invalid("idempotency key must contain 1..128 bytes")
	}
	receivedAt := s.now()
	normalized, err := batch.Normalize(receivedAt)
	if err != nil {
		return StoreResult{}, err
	}
	return s.repo.Store(ctx, hostID, idempotencyKey, receivedAt, normalized)
}
