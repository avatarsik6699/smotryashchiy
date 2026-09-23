// Package infrastructure persists sites and pageviews in SQLite (docs/SPEC.md §4i).
package infrastructure

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/analytics/application"
	"github.com/avatarsik6699/smotryashchiy/internal/analytics/domain"
	"github.com/avatarsik6699/smotryashchiy/internal/platform/apierror"
)

// dailySaltSetting names the one persisted secret this context needs: the base for the
// daily-rotating visitor hash salt (docs/SPEC.md §4i). Stored in the shared `settings` table, the
// same mechanism as the WireGuard server key (internal/telemetry/infrastructure/enrollment.go).
const dailySaltSetting = "analytics_daily_salt_base"

// Store implements application.Repository. Timestamps are unix milliseconds in UTC.
type Store struct{ db *sql.DB }

// NewStore returns a Store over db (migrations must have run).
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

var _ application.Repository = (*Store)(nil)

// CreateSite stores a validated site. The site limit and the unique domain are enforced in one
// transaction, so concurrent creates cannot exceed the limit.
func (s *Store) CreateSite(ctx context.Context, n domain.NewSite, now time.Time) (domain.Site, error) {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return domain.Site{}, fmt.Errorf("analytics: generate site id: %w", err)
	}
	site := domain.Site{ID: hex.EncodeToString(buf), Name: n.Name, Domain: n.Domain, CreatedAt: now.UTC().Truncate(time.Millisecond)}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Site{}, fmt.Errorf("analytics: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sites`).Scan(&count); err != nil {
		return domain.Site{}, fmt.Errorf("analytics: count sites: %w", err)
	}
	if count >= domain.MaxSites {
		return domain.Site{}, apierror.Conflict(fmt.Sprintf("site limit reached (%d)", domain.MaxSites))
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO sites (id, name, domain, created_at) VALUES (?, ?, ?, ?)`,
		site.ID, site.Name, site.Domain, site.CreatedAt.UnixMilli()); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return domain.Site{}, apierror.Conflict("a site with this domain already exists")
		}
		return domain.Site{}, fmt.Errorf("analytics: create site: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.Site{}, fmt.Errorf("analytics: commit: %w", err)
	}
	return site, nil
}

// ListSites returns every site ordered by name.
func (s *Store) ListSites(ctx context.Context) ([]domain.Site, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, domain, created_at FROM sites ORDER BY name, id`)
	if err != nil {
		return nil, fmt.Errorf("analytics: list sites: %w", err)
	}
	defer rows.Close()
	sites := []domain.Site{}
	for rows.Next() {
		var site domain.Site
		var created int64
		if err := rows.Scan(&site.ID, &site.Name, &site.Domain, &created); err != nil {
			return nil, fmt.Errorf("analytics: scan site: %w", err)
		}
		site.CreatedAt = time.UnixMilli(created).UTC()
		sites = append(sites, site)
	}
	return sites, rows.Err()
}

// SiteExists looks a site up by id; the second return is false (not an error) when it is unknown —
// the public collect endpoint must never error on an unknown site (docs/SPEC.md §4i).
func (s *Store) SiteExists(ctx context.Context, id string) (domain.Site, bool, error) {
	var site domain.Site
	var created int64
	err := s.db.QueryRowContext(ctx, `SELECT id, name, domain, created_at FROM sites WHERE id = ?`, id).
		Scan(&site.ID, &site.Name, &site.Domain, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Site{}, false, nil
	}
	if err != nil {
		return domain.Site{}, false, fmt.Errorf("analytics: find site: %w", err)
	}
	site.CreatedAt = time.UnixMilli(created).UTC()
	return site, true, nil
}

// DeleteSite removes a site; its pageviews go with it (ON DELETE CASCADE).
func (s *Store) DeleteSite(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sites WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("analytics: delete site: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return apierror.NotFound("site not found")
	}
	return nil
}

// InsertPageview stores one observation.
func (s *Store) InsertPageview(ctx context.Context, p domain.Pageview) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO pageviews (site_id, ts, path, referrer_domain, visitor_hash, browser, os, device) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		p.SiteID, p.TS.UnixMilli(), p.Path, p.ReferrerDomain, p.VisitorHash, p.Browser, p.OS, p.Device)
	if err != nil {
		return fmt.Errorf("analytics: insert pageview: %w", err)
	}
	return nil
}

// Stats aggregates raw pageviews since the given time (docs/SPEC.md §4i: all supported ranges fit
// inside the default raw retention, so this reads raw rows, not the daily rollups).
func (s *Store) Stats(ctx context.Context, siteID string, since time.Time) (domain.Stats, error) {
	var stats domain.Stats
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COUNT(DISTINCT visitor_hash) FROM pageviews WHERE site_id = ? AND ts >= ?`,
		siteID, since.UnixMilli()).Scan(&stats.Pageviews, &stats.Visitors); err != nil {
		return domain.Stats{}, fmt.Errorf("analytics: stats summary: %w", err)
	}
	pages, err := s.db.QueryContext(ctx,
		`SELECT path, COUNT(*) AS c FROM pageviews WHERE site_id = ? AND ts >= ? GROUP BY path ORDER BY c DESC, path LIMIT 10`,
		siteID, since.UnixMilli())
	if err != nil {
		return domain.Stats{}, fmt.Errorf("analytics: top pages: %w", err)
	}
	defer pages.Close()
	stats.TopPages = []domain.PathCount{}
	for pages.Next() {
		var pc domain.PathCount
		if err := pages.Scan(&pc.Path, &pc.Count); err != nil {
			return domain.Stats{}, fmt.Errorf("analytics: scan top page: %w", err)
		}
		stats.TopPages = append(stats.TopPages, pc)
	}
	if err := pages.Err(); err != nil {
		return domain.Stats{}, err
	}
	refs, err := s.db.QueryContext(ctx,
		`SELECT referrer_domain, COUNT(*) AS c FROM pageviews WHERE site_id = ? AND ts >= ? AND referrer_domain != '' GROUP BY referrer_domain ORDER BY c DESC, referrer_domain LIMIT 10`,
		siteID, since.UnixMilli())
	if err != nil {
		return domain.Stats{}, fmt.Errorf("analytics: top referrers: %w", err)
	}
	defer refs.Close()
	stats.TopReferrers = []domain.DomainCount{}
	for refs.Next() {
		var dc domain.DomainCount
		if err := refs.Scan(&dc.Domain, &dc.Count); err != nil {
			return domain.Stats{}, fmt.Errorf("analytics: scan top referrer: %w", err)
		}
		stats.TopReferrers = append(stats.TopReferrers, dc)
	}
	return stats, refs.Err()
}

// DailySaltBase returns the server's analytics salt secret, generating and storing one on first
// use (docs/SPEC.md §4i) — the same race-safe pattern as the WireGuard server key.
func (s *Store) DailySaltBase(ctx context.Context) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, dailySaltSetting).Scan(&value)
	if err == nil {
		return value, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("analytics: read salt: %w", err)
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("analytics: generate salt: %w", err)
	}
	secret := hex.EncodeToString(buf)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO NOTHING`, dailySaltSetting, secret); err != nil {
		return "", fmt.Errorf("analytics: store salt: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, dailySaltSetting).Scan(&value); err != nil {
		return "", fmt.Errorf("analytics: read salt: %w", err)
	}
	return value, nil
}

// PurgePageviews deletes up to limit pageviews older than before (bounded so the single writer is
// never held long).
func (s *Store) PurgePageviews(ctx context.Context, before time.Time, limit int) (int, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM pageviews WHERE id IN (SELECT id FROM pageviews WHERE ts < ? LIMIT ?)`,
		before.UnixMilli(), limit)
	if err != nil {
		return 0, fmt.Errorf("analytics: purge pageviews: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
