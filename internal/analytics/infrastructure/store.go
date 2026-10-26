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
	if n.Timezone == "" {
		n.Timezone = domain.DefaultTimezone
	}
	site := domain.Site{ID: hex.EncodeToString(buf), Name: n.Name, Domain: n.Domain, Timezone: n.Timezone, CreatedAt: now.UTC().Truncate(time.Millisecond)}
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
	if _, err := tx.ExecContext(ctx, `INSERT INTO sites (id, name, domain, timezone, created_at) VALUES (?, ?, ?, ?, ?)`,
		site.ID, site.Name, site.Domain, site.Timezone, site.CreatedAt.UnixMilli()); err != nil {
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
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, domain, timezone, created_at FROM sites ORDER BY name, id`)
	if err != nil {
		return nil, fmt.Errorf("analytics: list sites: %w", err)
	}
	defer rows.Close()
	sites := []domain.Site{}
	for rows.Next() {
		var site domain.Site
		var created int64
		if err := rows.Scan(&site.ID, &site.Name, &site.Domain, &site.Timezone, &created); err != nil {
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
	err := s.db.QueryRowContext(ctx, `SELECT id, name, domain, timezone, created_at FROM sites WHERE id = ?`, id).
		Scan(&site.ID, &site.Name, &site.Domain, &site.Timezone, &created)
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

// RecordPageview stores one observation inside its visit (Change 35, docs/SPEC.md §4i): the
// visitor's latest visit continues while its last pageview is less than domain.VisitIdle old;
// otherwise v opens a new visit at this pageview.
func (s *Store) RecordPageview(ctx context.Context, p domain.Pageview, v domain.Visit) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("analytics: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	ts := p.TS.UnixMilli()
	var visitID string
	err = tx.QueryRowContext(ctx,
		`SELECT id FROM visits WHERE site_id = ? AND visitor_hash = ? AND last_at > ? ORDER BY last_at DESC LIMIT 1`,
		p.SiteID, p.VisitorHash, ts-domain.VisitIdle.Milliseconds()).Scan(&visitID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		buf := make([]byte, 12)
		if _, err := rand.Read(buf); err != nil {
			return fmt.Errorf("analytics: generate visit id: %w", err)
		}
		visitID = hex.EncodeToString(buf)
		if _, err := tx.ExecContext(ctx, `INSERT INTO visits (id, site_id, visitor_hash, started_at, last_at,
			entry_path, exit_path, pageviews, hostname, referrer_domain, source, channel,
			utm_source, utm_medium, utm_campaign, utm_content, utm_term, paid, country, region,
			browser, browser_version, os, os_version, device, screen)
			VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			visitID, p.SiteID, p.VisitorHash, ts, ts, p.Path, p.Path, p.Hostname, p.ReferrerDomain,
			v.Source, v.Channel, v.UTM.Source, v.UTM.Medium, v.UTM.Campaign, v.UTM.Content, v.UTM.Term,
			v.Paid, v.Country, v.Region, p.Browser, v.BrowserVersion, p.OS, v.OSVersion, p.Device,
			v.Screen); err != nil {
			return fmt.Errorf("analytics: open visit: %w", err)
		}
	case err != nil:
		return fmt.Errorf("analytics: find visit: %w", err)
	default:
		if _, err := tx.ExecContext(ctx,
			`UPDATE visits SET last_at = ?, exit_path = ?, pageviews = pageviews + 1 WHERE id = ?`,
			ts, p.Path, visitID); err != nil {
			return fmt.Errorf("analytics: continue visit: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO pageviews (site_id, ts, path, referrer_domain, visitor_hash, browser, os, device, visit_id, hostname) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.SiteID, ts, p.Path, p.ReferrerDomain, p.VisitorHash, p.Browser, p.OS, p.Device, visitID, p.Hostname); err != nil {
		return fmt.Errorf("analytics: insert pageview: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("analytics: commit pageview: %w", err)
	}
	return nil
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

// PurgePageviews deletes up to limit pageviews older than before, then up to limit visits whose
// last pageview is older (Change 35; their remaining pageviews go with them). Bounded so the single
// writer is never held long; the count is both together, 0 once nothing is left.
func (s *Store) PurgePageviews(ctx context.Context, before time.Time, limit int) (int, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM pageviews WHERE id IN (SELECT id FROM pageviews WHERE ts < ? LIMIT ?)`,
		before.UnixMilli(), limit)
	if err != nil {
		return 0, fmt.Errorf("analytics: purge pageviews: %w", err)
	}
	pageviews, _ := res.RowsAffected()
	res, err = s.db.ExecContext(ctx,
		`DELETE FROM visits WHERE id IN (SELECT id FROM visits WHERE last_at < ? LIMIT ?)`,
		before.UnixMilli(), limit)
	if err != nil {
		return 0, fmt.Errorf("analytics: purge visits: %w", err)
	}
	visits, _ := res.RowsAffected()
	return int(pageviews + visits), nil
}
