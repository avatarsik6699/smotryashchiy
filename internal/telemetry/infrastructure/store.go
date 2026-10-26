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
	// Change 38: each kind's INSERT is prepared once per batch, not parsed again per record.
	insertMetric, err := prepared(ctx, tx, len(batch.Metrics),
		`INSERT INTO metrics (host_id, name, labels_json, ts, value, schema_version) VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`)
	if err != nil {
		return application.StoreResult{}, err
	}
	insertCheck, err := prepared(ctx, tx, len(batch.Checks),
		`INSERT INTO checks (host_id, name, ts, status, meta_json, schema_version) VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`)
	if err != nil {
		return application.StoreResult{}, err
	}
	insertEvent, err := prepared(ctx, tx, len(batch.Events),
		`INSERT INTO events (host_id, ts, level, message, labels_json, schema_version) VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`)
	if err != nil {
		return application.StoreResult{}, err
	}
	for _, m := range batch.Metrics {
		inserted, err := insertIgnore(ctx, insertMetric,
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
		inserted, err := insertIgnore(ctx, insertCheck,
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
		inserted, err := insertIgnore(ctx, insertEvent,
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
	// Change 27: a document replaces the host's previous one of its kind. The spool delivers
	// oldest first, so a later batch always carries the newer document.
	for _, d := range batch.Documents {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO documents (host_id, kind, received_at, body) VALUES (?, ?, ?, ?)
			 ON CONFLICT (host_id, kind) DO UPDATE SET received_at = excluded.received_at, body = excluded.body`,
			hostID, d.Kind, receivedAt.UnixMilli(), string(d.Body)); err != nil {
			return application.StoreResult{}, fmt.Errorf("telemetry: store document: %w", err)
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

// prepared prepares query on tx when the batch has records for it; the statement closes with tx.
func prepared(ctx context.Context, tx *sql.Tx, records int, query string) (*sql.Stmt, error) {
	if records == 0 {
		return nil, nil
	}
	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("telemetry: prepare insert: %w", err)
	}
	return stmt, nil
}

func insertIgnore(ctx context.Context, stmt *sql.Stmt, args ...any) (bool, error) {
	res, err := stmt.ExecContext(ctx, args...)
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
		stepSource := "metrics"
		switch {
		case q.HostID == "" && q.Name != "":
			// Change 38: one series range per host through metrics_series_ts instead of every
			// metric of the window through metrics_ts; the host list is short.
			f.conds = append(f.conds, "host_id IN (SELECT id FROM hosts)")
		case q.HostID == "" && (q.From != nil || q.To != nil):
			// Without a host or name, SQLite otherwise scans every raw series through
			// metrics_series_ts, even when the caller requests only the last hour.
			stepSource += " INDEXED BY metrics_ts"
		}
		if q.From != nil {
			f.add("ts >= ?", q.From.UnixMilli())
		}
		if q.To != nil {
			f.add("ts <= ?", q.To.UnixMilli())
		}
		// Newest sample per Step-second bucket per series; ts is part of the primary key, so the
		// join back on (series, ts) selects exactly one row per bucket.
		// The limit keeps the newest buckets (Change 38); the outer query turns them oldest first.
		query = `SELECT * FROM (SELECT m.host_id, m.name, m.labels_json, m.ts, m.value FROM metrics m
			JOIN (SELECT host_id, name, labels_json, MAX(ts) AS ts FROM ` + stepSource + f.where() + `
			      GROUP BY host_id, name, labels_json, ts / ?) l
			  ON m.host_id = l.host_id AND m.name = l.name AND m.labels_json = l.labels_json AND m.ts = l.ts
			ORDER BY m.ts DESC, m.host_id, m.name, m.labels_json LIMIT ?)
			ORDER BY ts, host_id, name, labels_json`
		f.args = append(f.args, int64(q.Step)*1000)
	} else if q.Latest {
		query = `SELECT host_id, name, labels_json, ts, value FROM metric_latest` + f.where() +
			` ORDER BY host_id, name, labels_json LIMIT ?`
	} else {
		if q.From != nil {
			f.add("ts >= ?", q.From.UnixMilli())
		}
		if q.To != nil {
			f.add("ts <= ?", q.To.UnixMilli())
		}
		// The limit keeps the newest samples (Change 38: an hour of many interfaces used to lose
		// its end); the outer query turns them oldest first.
		query = `SELECT * FROM (SELECT host_id, name, labels_json, ts, value FROM metrics` + f.where() +
			` ORDER BY ts DESC, host_id, name, labels_json LIMIT ?) ORDER BY ts, host_id, name, labels_json`
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
	// check_latest is kept by triggers (migration 0013, Change 38).
	query := `SELECT host_id, name, ts, status, meta_json FROM check_latest` + f.where() +
		` ORDER BY host_id, name LIMIT ?`
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
	if q.From != nil {
		f.add("ts >= ?", q.From.UnixMilli())
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

// Documents returns the latest document of kind from every host, by host name.
func (s *Store) Documents(ctx context.Context, kind string) ([]domain.HostDocument, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT d.host_id, h.name, d.kind, d.received_at, d.body
		 FROM documents AS d JOIN hosts AS h ON h.id = d.host_id
		 WHERE d.kind = ? ORDER BY h.name`, kind)
	if err != nil {
		return nil, fmt.Errorf("telemetry: query documents: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []domain.HostDocument{}
	for rows.Next() {
		var d domain.HostDocument
		var received int64
		var body string
		if err := rows.Scan(&d.HostID, &d.HostName, &d.Kind, &received, &body); err != nil {
			return nil, fmt.Errorf("telemetry: scan document: %w", err)
		}
		d.ReceivedAt = time.UnixMilli(received).UTC()
		d.Body = json.RawMessage(body)
		out = append(out, d)
	}
	return out, rows.Err()
}
