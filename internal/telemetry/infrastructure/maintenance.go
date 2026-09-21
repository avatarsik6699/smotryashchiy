package infrastructure

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/telemetry/application"
	"github.com/avatarsik6699/smotryashchiy/internal/telemetry/domain"
)

var (
	_ application.MaintenanceRepository = (*Store)(nil)
)

// RolledThrough returns the instant before which every hour has been aggregated.
func (s *Store) RolledThrough(ctx context.Context) (time.Time, bool, error) {
	var ms int64
	err := s.db.QueryRowContext(ctx, `SELECT rolled_through FROM rollup_state WHERE id = 1`).Scan(&ms)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("telemetry: read rollup state: %w", err)
	}
	return time.UnixMilli(ms).UTC(), true, nil
}

// SetRolledThrough records that every hour before t has been aggregated.
func (s *Store) SetRolledThrough(ctx context.Context, t time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO rollup_state (id, rolled_through) VALUES (1, ?)
		 ON CONFLICT(id) DO UPDATE SET rolled_through = excluded.rolled_through`, t.UnixMilli())
	if err != nil {
		return fmt.Errorf("telemetry: write rollup state: %w", err)
	}
	return nil
}

// EarliestMetric returns the timestamp of the oldest raw metric.
func (s *Store) EarliestMetric(ctx context.Context) (time.Time, bool, error) {
	var ms sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT MIN(ts) FROM metrics`).Scan(&ms); err != nil {
		return time.Time{}, false, fmt.Errorf("telemetry: earliest metric: %w", err)
	}
	if !ms.Valid {
		return time.Time{}, false, nil
	}
	return time.UnixMilli(ms.Int64).UTC(), true, nil
}

// RollupHour re-aggregates one hour of raw metrics, overwriting the previous aggregate.
func (s *Store) RollupHour(ctx context.Context, hour time.Time) error {
	start := hour.UTC().Truncate(time.Hour)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO metric_rollups_hourly (host_id, name, labels_json, hour_ts, count, min, max, sum)
		 SELECT host_id, name, labels_json, ?1, COUNT(*), MIN(value), MAX(value), SUM(value)
		 FROM metrics WHERE ts >= ?1 AND ts < ?2 GROUP BY host_id, name, labels_json
		 ON CONFLICT (host_id, name, labels_json, hour_ts) DO UPDATE SET
		   count = excluded.count, min = excluded.min, max = excluded.max, sum = excluded.sum`,
		start.UnixMilli(), start.Add(time.Hour).UnixMilli())
	if err != nil {
		return fmt.Errorf("telemetry: rollup hour: %w", err)
	}
	return nil
}

// purgeRawStatements delete at most ?2 rows older than ?1 by primary key, so each statement is a
// short, bounded write.
var purgeRawStatements = []string{
	`DELETE FROM metrics WHERE (host_id, name, labels_json, ts) IN
	   (SELECT host_id, name, labels_json, ts FROM metrics WHERE ts < ?1 LIMIT ?2)`,
	`DELETE FROM checks WHERE (host_id, name, ts) IN
	   (SELECT host_id, name, ts FROM checks WHERE ts < ?1 LIMIT ?2)`,
	`DELETE FROM events WHERE (host_id, ts, level, message, labels_json) IN
	   (SELECT host_id, ts, level, message, labels_json FROM events WHERE ts < ?1 LIMIT ?2)`,
	`DELETE FROM ingestion_batches WHERE (host_id, idempotency_key) IN
	   (SELECT host_id, idempotency_key FROM ingestion_batches WHERE received_at < ?1 LIMIT ?2)`,
}

// PurgeRaw deletes up to limit rows per table older than before.
func (s *Store) PurgeRaw(ctx context.Context, before time.Time, limit int) (int, error) {
	total := 0
	for _, stmt := range purgeRawStatements {
		res, err := s.db.ExecContext(ctx, stmt, before.UnixMilli(), limit)
		if err != nil {
			return total, fmt.Errorf("telemetry: purge raw: %w", err)
		}
		n, _ := res.RowsAffected()
		total += int(n)
	}
	return total, nil
}

// PurgeRollups deletes up to limit rollups whose hour starts before before.
func (s *Store) PurgeRollups(ctx context.Context, before time.Time, limit int) (int, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM metric_rollups_hourly WHERE (host_id, name, labels_json, hour_ts) IN
		   (SELECT host_id, name, labels_json, hour_ts FROM metric_rollups_hourly WHERE hour_ts < ?1 LIMIT ?2)`,
		before.UnixMilli(), limit)
	if err != nil {
		return 0, fmt.Errorf("telemetry: purge rollups: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// MetricRollups returns hourly aggregates oldest first; From/To bound the hour start.
func (s *Store) MetricRollups(ctx context.Context, q application.MetricQuery) ([]domain.RollupPoint, error) {
	var f filter
	if q.HostID != "" {
		f.add("host_id = ?", q.HostID)
	}
	if q.Name != "" {
		f.add("name = ?", q.Name)
	}
	if q.From != nil {
		f.add("hour_ts >= ?", q.From.UnixMilli())
	}
	if q.To != nil {
		f.add("hour_ts <= ?", q.To.UnixMilli())
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT host_id, name, labels_json, hour_ts, count, min, max, sum FROM metric_rollups_hourly`+f.where()+
			` ORDER BY hour_ts, host_id, name, labels_json LIMIT ?`, append(f.args, q.Limit)...)
	if err != nil {
		return nil, fmt.Errorf("telemetry: query rollups: %w", err)
	}
	defer rows.Close()
	points := []domain.RollupPoint{}
	for rows.Next() {
		var (
			p      domain.RollupPoint
			labels string
			ts     int64
			sum    float64
		)
		if err := rows.Scan(&p.HostID, &p.Name, &labels, &ts, &p.Count, &p.Min, &p.Max, &sum); err != nil {
			return nil, fmt.Errorf("telemetry: scan rollup: %w", err)
		}
		p.TS, p.Labels, p.Avg = time.UnixMilli(ts).UTC(), domain.ParseLabels(labels), sum/float64(p.Count)
		points = append(points, p)
	}
	return points, rows.Err()
}
