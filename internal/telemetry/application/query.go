package application

import (
	"context"
	"strconv"

	"github.com/avatarsik6699/smotryashchiy/internal/platform/apierror"
	"github.com/avatarsik6699/smotryashchiy/internal/telemetry/domain"
)

// Read limits (docs/SPEC.md §4.3).
const (
	DefaultMetricLimit = 1000
	MaxMetricLimit     = 5000
	DefaultEventLimit  = 100
	MaxEventLimit      = 500
)

// Metrics returns samples matching q. latest is incompatible with a time range.
func (s *Service) Metrics(ctx context.Context, q MetricQuery) ([]domain.MetricPoint, error) {
	if q.Latest && (q.From != nil || q.To != nil) {
		return nil, apierror.Invalid("latest cannot be combined with from/to")
	}
	if q.From != nil && q.To != nil && q.From.After(*q.To) {
		return nil, apierror.Invalid("from must not be after to")
	}
	limit, err := boundedLimit(q.Limit, DefaultMetricLimit, MaxMetricLimit)
	if err != nil {
		return nil, err
	}
	q.Limit = limit
	return s.repo.Metrics(ctx, q)
}

// Checks returns the newest Check for every (host, name).
func (s *Service) Checks(ctx context.Context, q CheckQuery) ([]domain.CheckState, error) {
	return s.repo.Checks(ctx, q)
}

// Events returns events newest first.
func (s *Service) Events(ctx context.Context, q EventQuery) ([]domain.EventEntry, error) {
	switch q.Level {
	case "", domain.LevelInfo, domain.LevelWarn, domain.LevelError, domain.LevelCritical:
	default:
		return nil, apierror.Invalid("level must be info, warn, error or critical")
	}
	limit, err := boundedLimit(q.Limit, DefaultEventLimit, MaxEventLimit)
	if err != nil {
		return nil, err
	}
	q.Limit = limit
	return s.repo.Events(ctx, q)
}

func boundedLimit(requested, def, max int) (int, error) {
	switch {
	case requested == 0:
		return def, nil
	case requested < 0 || requested > max:
		return 0, apierror.Invalid("limit must be between 1 and " + strconv.Itoa(max))
	}
	return requested, nil
}
