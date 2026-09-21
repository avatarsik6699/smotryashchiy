// Package infrastructure persists telemetry in SQLite.
package infrastructure

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/platform/apierror"
	"github.com/avatarsik6699/smotryashchiy/internal/telemetry/application"
	"github.com/avatarsik6699/smotryashchiy/internal/telemetry/domain"
)

// maxCheckRows bounds the newest-check-per-name listing.
const maxCheckRows = 1000

// Store implements application.Repository. Timestamps are unix milliseconds in UTC.
type Store struct{ db *sql.DB }

// NewStore returns a Store over db (migrations must have run).
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

var _ application.Repository = (*Store)(nil)

// CreateHost registers a host identity. Enrollment in Stage 2 builds on this.
func (s *Store) CreateHost(ctx context.Context, name string, now time.Time) (domain.Host, error) {
	if strings.TrimSpace(name) == "" || len(name) > 80 {
		return domain.Host{}, apierror.Invalid("host name must contain 1..80 bytes")
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return domain.Host{}, fmt.Errorf("telemetry: generate host id: %w", err)
	}
	host := domain.Host{ID: hex.EncodeToString(buf), Name: name, CreatedAt: now.UTC().Truncate(time.Millisecond)}
	_, err := s.db.ExecContext(ctx, `INSERT INTO hosts (id, name, created_at) VALUES (?, ?, ?)`,
		host.ID, host.Name, host.CreatedAt.UnixMilli())
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return domain.Host{}, apierror.Conflict("host name already exists")
		}
		return domain.Host{}, fmt.Errorf("telemetry: create host: %w", err)
	}
	return host, nil
}

// Host returns one host or a NotFound error.
func (s *Store) Host(ctx context.Context, id string) (domain.Host, error) {
	var (
		host     domain.Host
		created  int64
		lastSeen sql.NullInt64
	)
	err := s.db.QueryRowContext(ctx, `SELECT id, name, created_at, last_seen_at FROM hosts WHERE id = ?`, id).
		Scan(&host.ID, &host.Name, &created, &lastSeen)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Host{}, apierror.NotFound("host not found")
	}
	if err != nil {
		return domain.Host{}, fmt.Errorf("telemetry: get host: %w", err)
	}
	host.CreatedAt = time.UnixMilli(created).UTC()
	if lastSeen.Valid {
		t := time.UnixMilli(lastSeen.Int64).UTC()
		host.LastSeenAt = &t
	}
	return host, nil
}

// Store applies batch in one transaction: idempotency record, insert-on-conflict-do-nothing per record
// (the primary key is the record identity; never OR IGNORE, which also swallows CHECK failures) and the host's last_seen_at. Anything failing rolls it all
// back, so a batch is never half-stored.
func (s *Store) Store(ctx context.Context, hostID, key string, receivedAt time.Time, batch domain.Batch) (application.StoreResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return application.StoreResult{}, fmt.Errorf("telemetry: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM hosts WHERE id = ?`, hostID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return application.StoreResult{}, apierror.NotFound("host not found")
	} else if err != nil {
		return application.StoreResult{}, fmt.Errorf("telemetry: check host: %w", err)
	}

	claimed, err := tx.ExecContext(ctx,
		`INSERT INTO ingestion_batches (host_id, idempotency_key, received_at, record_count)
		 VALUES (?, ?, ?, ?) ON CONFLICT DO NOTHING`, hostID, key, receivedAt.UnixMilli(), batch.Len())
	if err != nil {
		return application.StoreResult{}, fmt.Errorf("telemetry: record batch: %w", err)
	}
	if n, _ := claimed.RowsAffected(); n == 0 {
		return application.StoreResult{Replayed: true}, nil
	}

	result := application.StoreResult{Accepted: domain.Batch{SchemaVersion: batch.SchemaVersion}}
	for _, m := range batch.Metrics {
		inserted, err := insertIgnore(ctx, tx,
			`INSERT INTO metrics (host_id, name, labels_json, ts, value, schema_version) VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
			hostID, m.Name, domain.CanonicalLabels(m.Labels), m.TS.UnixMilli(), m.Value, batch.SchemaVersion)
		if err != nil {
			return application.StoreResult{}, err
		}
		if inserted {
			result.Accepted.Metrics = append(result.Accepted.Metrics, m)
		} else {
			result.Duplicates.Metrics++
		}
	}
	for _, c := range batch.Checks {
		inserted, err := insertIgnore(ctx, tx,
			`INSERT INTO checks (host_id, name, ts, status, meta_json, schema_version) VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
			hostID, c.Name, c.TS.UnixMilli(), c.Status, string(c.Meta), batch.SchemaVersion)
		if err != nil {
			return application.StoreResult{}, err
		}
		if inserted {
			result.Accepted.Checks = append(result.Accepted.Checks, c)
		} else {
			result.Duplicates.Checks++
		}
	}
	for _, e := range batch.Events {
		inserted, err := insertIgnore(ctx, tx,
			`INSERT INTO events (host_id, ts, level, message, labels_json, schema_version) VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
			hostID, e.TS.UnixMilli(), e.Level, e.Message, domain.CanonicalLabels(e.Labels), batch.SchemaVersion)
		if err != nil {
			return application.StoreResult{}, err
		}
		if inserted {
			result.Accepted.Events = append(result.Accepted.Events, e)
		} else {
			result.Duplicates.Events++
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE hosts SET last_seen_at = ? WHERE id = ?`, receivedAt.UnixMilli(), hostID); err != nil {
		return application.StoreResult{}, fmt.Errorf("telemetry: update last_seen_at: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return application.StoreResult{}, fmt.Errorf("telemetry: commit: %w", err)
	}
	return result, nil
}

func insertIgnore(ctx context.Context, tx *sql.Tx, query string, args ...any) (bool, error) {
	res, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return false, fmt.Errorf("telemetry: insert record: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("telemetry: rows affected: %w", err)
	}
	return n == 1, nil
}

// filter builds an AND-joined WHERE clause from optional equality and range conditions.
type filter struct {
	conds []string
	args  []any
}

func (f *filter) add(cond string, arg any) {
	f.conds = append(f.conds, cond)
	f.args = append(f.args, arg)
}

func (f *filter) where() string {
	if len(f.conds) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(f.conds, " AND ")
}

// Metrics returns samples oldest first, or the newest sample per series when q.Latest is set.
func (s *Store) Metrics(ctx context.Context, q application.MetricQuery) ([]domain.MetricPoint, error) {
	var f filter
	if q.HostID != "" {
		f.add("host_id = ?", q.HostID)
	}
	if q.Name != "" {
		f.add("name = ?", q.Name)
	}
	var query string
	if q.Step > 0 {
		if q.From != nil {
			f.add("ts >= ?", q.From.UnixMilli())
		}
		if q.To != nil {
			f.add("ts <= ?", q.To.UnixMilli())
		}
		// Newest sample per Step-second bucket per series; ts is part of the primary key, so the
		// join back on (series, ts) selects exactly one row per bucket.
		query = `SELECT m.host_id, m.name, m.labels_json, m.ts, m.value FROM metrics m
			JOIN (SELECT host_id, name, labels_json, MAX(ts) AS ts FROM metrics` + f.where() + `
			      GROUP BY host_id, name, labels_json, ts / ?) l
			  ON m.host_id = l.host_id AND m.name = l.name AND m.labels_json = l.labels_json AND m.ts = l.ts
			ORDER BY m.ts, m.host_id, m.name, m.labels_json LIMIT ?`
		f.args = append(f.args, int64(q.Step)*1000)
	} else if q.Latest {
		query = `SELECT m.host_id, m.name, m.labels_json, m.ts, m.value FROM metrics m
			JOIN (SELECT host_id, name, labels_json, MAX(ts) AS ts FROM metrics` + f.where() + `
			      GROUP BY host_id, name, labels_json) l
			  ON m.host_id = l.host_id AND m.name = l.name AND m.labels_json = l.labels_json AND m.ts = l.ts
			ORDER BY m.host_id, m.name, m.labels_json LIMIT ?`
	} else {
		if q.From != nil {
			f.add("ts >= ?", q.From.UnixMilli())
		}
		if q.To != nil {
			f.add("ts <= ?", q.To.UnixMilli())
		}
		query = `SELECT host_id, name, labels_json, ts, value FROM metrics` + f.where() +
			` ORDER BY ts, host_id, name, labels_json LIMIT ?`
	}
	rows, err := s.db.QueryContext(ctx, query, append(f.args, q.Limit)...)
	if err != nil {
		return nil, fmt.Errorf("telemetry: query metrics: %w", err)
	}
	defer rows.Close()
	points := []domain.MetricPoint{}
	for rows.Next() {
		var (
			p      domain.MetricPoint
			labels string
			ts     int64
		)
		if err := rows.Scan(&p.HostID, &p.Name, &labels, &ts, &p.Value); err != nil {
			return nil, fmt.Errorf("telemetry: scan metric: %w", err)
		}
		p.TS, p.Labels = time.UnixMilli(ts).UTC(), domain.ParseLabels(labels)
		points = append(points, p)
	}
	return points, rows.Err()
}

// Hosts lists every registered host ordered by name.
func (s *Store) Hosts(ctx context.Context) ([]domain.Host, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, created_at, last_seen_at FROM hosts ORDER BY name, id`)
	if err != nil {
		return nil, fmt.Errorf("telemetry: list hosts: %w", err)
	}
	defer rows.Close()
	hosts := []domain.Host{}
	for rows.Next() {
		var (
			h        domain.Host
			created  int64
			lastSeen sql.NullInt64
		)
		if err := rows.Scan(&h.ID, &h.Name, &created, &lastSeen); err != nil {
			return nil, fmt.Errorf("telemetry: scan host: %w", err)
		}
		h.CreatedAt = time.UnixMilli(created).UTC()
		if lastSeen.Valid {
			t := time.UnixMilli(lastSeen.Int64).UTC()
			h.LastSeenAt = &t
		}
		hosts = append(hosts, h)
	}
	return hosts, rows.Err()
}

// Checks returns the newest Check for every (host, name).
func (s *Store) Checks(ctx context.Context, q application.CheckQuery) ([]domain.CheckState, error) {
	var f filter
	if q.HostID != "" {
		f.add("host_id = ?", q.HostID)
	}
	if q.Name != "" {
		f.add("name = ?", q.Name)
	}
	query := `SELECT c.host_id, c.name, c.ts, c.status, c.meta_json FROM checks c
		JOIN (SELECT host_id, name, MAX(ts) AS ts FROM checks` + f.where() + ` GROUP BY host_id, name) l
		  ON c.host_id = l.host_id AND c.name = l.name AND c.ts = l.ts
		ORDER BY c.host_id, c.name LIMIT ?`
	rows, err := s.db.QueryContext(ctx, query, append(f.args, maxCheckRows)...)
	if err != nil {
		return nil, fmt.Errorf("telemetry: query checks: %w", err)
	}
	defer rows.Close()
	checks := []domain.CheckState{}
	for rows.Next() {
		var (
			c    domain.CheckState
			ts   int64
			meta string
		)
		if err := rows.Scan(&c.HostID, &c.Name, &ts, &c.Status, &meta); err != nil {
			return nil, fmt.Errorf("telemetry: scan check: %w", err)
		}
		c.TS, c.Meta = time.UnixMilli(ts).UTC(), json.RawMessage(meta)
		checks = append(checks, c)
	}
	return checks, rows.Err()
}

// Events returns events newest first.
func (s *Store) Events(ctx context.Context, q application.EventQuery) ([]domain.EventEntry, error) {
	var f filter
	if q.HostID != "" {
		f.add("host_id = ?", q.HostID)
	}
	if q.Level != "" {
		f.add("level = ?", q.Level)
	}
	query := `SELECT host_id, ts, level, message, labels_json FROM events` + f.where() +
		` ORDER BY ts DESC, host_id, level, message LIMIT ?`
	rows, err := s.db.QueryContext(ctx, query, append(f.args, q.Limit)...)
	if err != nil {
		return nil, fmt.Errorf("telemetry: query events: %w", err)
	}
	defer rows.Close()
	events := []domain.EventEntry{}
	for rows.Next() {
		var (
			e      domain.EventEntry
			ts     int64
			labels string
		)
		if err := rows.Scan(&e.HostID, &ts, &e.Level, &e.Message, &labels); err != nil {
			return nil, fmt.Errorf("telemetry: scan event: %w", err)
		}
		e.TS, e.Labels = time.UnixMilli(ts).UTC(), domain.ParseLabels(labels)
		events = append(events, e)
	}
	return events, rows.Err()
}
