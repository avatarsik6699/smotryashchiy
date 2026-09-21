// Package infrastructure persists uptime targets and results in SQLite.
package infrastructure

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/platform/apierror"
	"github.com/avatarsik6699/smotryashchiy/internal/uptime/application"
	"github.com/avatarsik6699/smotryashchiy/internal/uptime/domain"
)

// Store implements application.Repository. Timestamps are unix milliseconds in UTC.
type Store struct{ db *sql.DB }

// NewStore returns a Store over db (migrations must have run).
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

var _ application.Repository = (*Store)(nil)

// Create stores a validated definition. The target limit and the unique name are enforced in one
// transaction, so concurrent creates cannot exceed the limit.
func (s *Store) Create(ctx context.Context, n domain.NewTarget, now time.Time) (domain.Target, error) {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return domain.Target{}, fmt.Errorf("uptime: generate target id: %w", err)
	}
	t := domain.Target{
		ID: hex.EncodeToString(buf), Name: n.Name, Kind: domain.Kind(n.Kind), Address: n.Address,
		IntervalSeconds: n.IntervalSeconds, CreatedAt: now.UTC().Truncate(time.Millisecond),
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Target{}, fmt.Errorf("uptime: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM uptime_targets`).Scan(&count); err != nil {
		return domain.Target{}, fmt.Errorf("uptime: count targets: %w", err)
	}
	if count >= domain.MaxTargets {
		return domain.Target{}, apierror.Conflict(fmt.Sprintf("target limit reached (%d)", domain.MaxTargets))
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO uptime_targets (id, name, kind, address, interval_seconds, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		t.ID, t.Name, string(t.Kind), t.Address, t.IntervalSeconds, t.CreatedAt.UnixMilli()); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return domain.Target{}, apierror.Conflict("a target with this name already exists")
		}
		return domain.Target{}, fmt.Errorf("uptime: create target: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.Target{}, fmt.Errorf("uptime: commit: %w", err)
	}
	return t, nil
}

// List returns every target ordered by name.
func (s *Store) List(ctx context.Context) ([]domain.Target, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, kind, address, interval_seconds, created_at FROM uptime_targets ORDER BY name, id`)
	if err != nil {
		return nil, fmt.Errorf("uptime: list targets: %w", err)
	}
	defer rows.Close()
	targets := []domain.Target{}
	for rows.Next() {
		var (
			t       domain.Target
			kind    string
			created int64
		)
		if err := rows.Scan(&t.ID, &t.Name, &kind, &t.Address, &t.IntervalSeconds, &created); err != nil {
			return nil, fmt.Errorf("uptime: scan target: %w", err)
		}
		t.Kind, t.CreatedAt = domain.Kind(kind), time.UnixMilli(created).UTC()
		targets = append(targets, t)
	}
	return targets, rows.Err()
}

// Delete removes a target; its results go with it (ON DELETE CASCADE).
func (s *Store) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM uptime_targets WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("uptime: delete target: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return apierror.NotFound("target not found")
	}
	return nil
}

// InsertResult stores one observation. A result whose target was deleted meanwhile is dropped silently
// by the caller (the foreign key rejects it).
func (s *Store) InsertResult(ctx context.Context, r domain.Result) error {
	var cert any
	if r.CertExpiresAt != nil {
		cert = r.CertExpiresAt.UnixMilli()
	}
	var errText any
	if r.Error != "" {
		errText = r.Error
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO uptime_results (target_id, ts, ok, latency_ms, status_code, error, cert_expires_at) VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT DO NOTHING`,
		r.TargetID, r.TS.UnixMilli(), boolInt(r.OK), r.LatencyMS, r.StatusCode, errText, cert)
	if err != nil {
		return fmt.Errorf("uptime: insert result: %w", err)
	}
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Latest returns the newest result of every target that has one.
func (s *Store) Latest(ctx context.Context) (map[string]domain.Result, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT r.target_id, r.ts, r.ok, r.latency_ms, r.status_code, r.error, r.cert_expires_at
		 FROM uptime_results r
		 JOIN (SELECT target_id, MAX(ts) AS ts FROM uptime_results GROUP BY target_id) l
		   ON r.target_id = l.target_id AND r.ts = l.ts`)
	if err != nil {
		return nil, fmt.Errorf("uptime: latest results: %w", err)
	}
	defer rows.Close()
	out := map[string]domain.Result{}
	for rows.Next() {
		var (
			r       domain.Result
			ts      int64
			ok      int
			latency sql.NullInt64
			status  sql.NullInt64
			errText sql.NullString
			cert    sql.NullInt64
		)
		if err := rows.Scan(&r.TargetID, &ts, &ok, &latency, &status, &errText, &cert); err != nil {
			return nil, fmt.Errorf("uptime: scan result: %w", err)
		}
		r.TS, r.OK = time.UnixMilli(ts).UTC(), ok == 1
		if latency.Valid {
			v := latency.Int64
			r.LatencyMS = &v
		}
		if status.Valid {
			v := int(status.Int64)
			r.StatusCode = &v
		}
		r.Error = errText.String
		if cert.Valid {
			t := time.UnixMilli(cert.Int64).UTC()
			r.CertExpiresAt = &t
		}
		out[r.TargetID] = r
	}
	return out, rows.Err()
}

// History returns the latency samples since the given time per target, oldest first; a failed check
// is a point with a nil latency so gaps stay visible.
func (s *Store) History(ctx context.Context, since time.Time) (map[string][]domain.LatencyPoint, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT target_id, ts, latency_ms FROM uptime_results WHERE ts >= ? ORDER BY target_id, ts`, since.UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("uptime: history: %w", err)
	}
	defer rows.Close()
	out := map[string][]domain.LatencyPoint{}
	for rows.Next() {
		var (
			id      string
			p       domain.LatencyPoint
			latency sql.NullInt64
		)
		if err := rows.Scan(&id, &p.TS, &latency); err != nil {
			return nil, fmt.Errorf("uptime: scan history: %w", err)
		}
		if latency.Valid {
			v := latency.Int64
			p.Latency = &v
		}
		out[id] = append(out[id], p)
	}
	return out, rows.Err()
}

// PurgeResults deletes up to limit results older than before (bounded so the single writer is never held long).
func (s *Store) PurgeResults(ctx context.Context, before time.Time, limit int) (int, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM uptime_results WHERE (target_id, ts) IN (SELECT target_id, ts FROM uptime_results WHERE ts < ? LIMIT ?)`,
		before.UnixMilli(), limit)
	if err != nil {
		return 0, fmt.Errorf("uptime: purge results: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
