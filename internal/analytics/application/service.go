// Package application implements site creation, pageview ingestion and stats for site analytics
// (docs/SPEC.md §4i).
package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/analytics/domain"
)

// rollupInterval and purgeInterval mirror the telemetry maintenance job's cadence
// (internal/telemetry/application/maintenance.go).
const (
	rollupInterval = time.Hour
	purgeInterval  = time.Hour
)

// Repository is the persistence port.
type Repository interface {
	CreateSite(ctx context.Context, n domain.NewSite, now time.Time) (domain.Site, error)
	ListSites(ctx context.Context) ([]domain.Site, error)
	SiteExists(ctx context.Context, id string) (domain.Site, bool, error)
	DeleteSite(ctx context.Context, id string) error
	InsertPageview(ctx context.Context, p domain.Pageview) error
	Stats(ctx context.Context, siteID string, since time.Time) (domain.Stats, error)
	DailySaltBase(ctx context.Context) (string, error)
	RollupDay(ctx context.Context, day time.Time) error
	PurgePageviews(ctx context.Context, before time.Time, limit int) (int, error)
}

// Options tune retention; the zero value disables purging.
type Options struct {
	Retention time.Duration
}

// Service manages sites and turns beacons into stored pageviews.
type Service struct {
	repo Repository
	now  func() time.Time
	opts Options
}

// NewService wires the service.
func NewService(repo Repository, now func() time.Time, opts Options) *Service {
	return &Service{repo: repo, now: now, opts: opts}
}

// CreateSite validates and stores a site.
func (s *Service) CreateSite(ctx context.Context, n domain.NewSite) (domain.Site, error) {
	valid, err := n.Validate()
	if err != nil {
		return domain.Site{}, err
	}
	return s.repo.CreateSite(ctx, valid, s.now())
}

// Sites lists every tracked site.
func (s *Service) Sites(ctx context.Context) ([]domain.Site, error) {
	return s.repo.ListSites(ctx)
}

// DeleteSite removes a site and its pageviews.
func (s *Service) DeleteSite(ctx context.Context, id string) error {
	return s.repo.DeleteSite(ctx, id)
}

// rangeWindow maps a stats range name to how far back it looks.
var rangeWindow = map[string]time.Duration{
	"today": 24 * time.Hour,
	"7d":    7 * 24 * time.Hour,
	"30d":   30 * 24 * time.Hour,
}

// Stats returns a site's pageview summary over range ("today"|"7d"|"30d"), defaulting to "today".
func (s *Service) Stats(ctx context.Context, siteID, rng string) (domain.Stats, error) {
	window, ok := rangeWindow[rng]
	if !ok {
		window = rangeWindow["today"]
	}
	return s.repo.Stats(ctx, siteID, s.now().Add(-window))
}

// Collect validates a beacon, derives everything server-side, and stores the pageview. It never
// returns an error the caller should act on beyond "nothing was stored" (docs/SPEC.md §4i: the
// endpoint always answers 204) — the boolean reports whether a pageview was actually stored, purely
// for the caller's own logging/testing, not for the HTTP response.
func (s *Service) Collect(ctx context.Context, b domain.Beacon, clientIP, userAgent string) (bool, error) {
	b, ok := b.Normalize()
	if !ok {
		return false, nil
	}
	if isBotUA(userAgent) {
		return false, nil
	}
	site, found, err := s.repo.SiteExists(ctx, b.Site)
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}
	hash, err := s.visitorHash(ctx, site.ID, clientIP, userAgent)
	if err != nil {
		return false, err
	}
	browser, os, device := parseUserAgent(userAgent)
	err = s.repo.InsertPageview(ctx, domain.Pageview{
		SiteID:         site.ID,
		TS:             s.now(),
		Path:           b.URL,
		ReferrerDomain: referrerDomain(b.Referrer),
		VisitorHash:    hash,
		Browser:        browser,
		OS:             os,
		Device:         device,
	})
	return err == nil, err
}

// visitorHash derives a daily-rotating, salted, unlinkable-across-days visitor identity
// (docs/SPEC.md §4i): sha256(sha256(secret|UTC-day) | site | IP | UA), truncated to 16 bytes. The
// raw IP is used only for this one computation and is never stored.
func (s *Service) visitorHash(ctx context.Context, siteID, ip, ua string) (string, error) {
	base, err := s.repo.DailySaltBase(ctx)
	if err != nil {
		return "", err
	}
	day := s.now().UTC().Format("2006-01-02")
	daily := sha256.Sum256([]byte(base + "|" + day))
	visitor := sha256.Sum256([]byte(hex.EncodeToString(daily[:]) + "|" + siteID + "|" + ip + "|" + ua))
	return hex.EncodeToString(visitor[:16]), nil
}

// Rollup aggregates yesterday's (and, defensively, the last 2 days') raw pageviews into
// pageview_rollups_daily, mirroring the metric rollup job's idempotent re-aggregation pattern
// (docs/SPEC.md §4.4). Today is never rolled up: it is still accumulating.
func (s *Service) Rollup(ctx context.Context) error {
	today := s.now().UTC().Truncate(24 * time.Hour)
	for i := 1; i <= 2; i++ {
		if err := s.repo.RollupDay(ctx, today.Add(-time.Duration(i)*24*time.Hour)); err != nil {
			return err
		}
	}
	return nil
}

// Purge deletes pageviews older than the retention window, in bounded batches.
func (s *Service) Purge(ctx context.Context) error {
	if s.opts.Retention <= 0 {
		return nil
	}
	before := s.now().Add(-s.opts.Retention)
	for {
		n, err := s.repo.PurgePageviews(ctx, before, 5000)
		if err != nil || n == 0 {
			return err
		}
	}
}

// Run executes rollup then purge immediately and then hourly until ctx is done. Failures are
// logged and retried at the next tick; they never stop the server (mirrors
// internal/telemetry/application/maintenance.go's Run).
func (s *Service) Run(ctx context.Context) {
	s.runOnce(ctx, "rollup", s.Rollup)
	s.runOnce(ctx, "purge", s.Purge)
	rollupTick := time.NewTicker(rollupInterval)
	purgeTick := time.NewTicker(purgeInterval)
	defer rollupTick.Stop()
	defer purgeTick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-rollupTick.C:
			s.runOnce(ctx, "rollup", s.Rollup)
		case <-purgeTick.C:
			s.runOnce(ctx, "purge", s.Purge)
		}
	}
}

func (s *Service) runOnce(ctx context.Context, name string, job func(context.Context) error) {
	if err := job(ctx); err != nil && ctx.Err() == nil {
		slog.Error("analytics maintenance job failed", "job", name, "err", err)
	}
}

func referrerDomain(referrer string) string {
	if referrer == "" {
		return ""
	}
	u, err := url.Parse(referrer)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	return u.Hostname()
}

// botPatterns is a small, deliberately conservative known-bot User-Agent list: defense in depth
// beyond "the browser ran the JS at all" (some crawlers, e.g. headless Chrome, do run JS).
var botPatterns = []string{
	"bot", "spider", "crawl", "slurp", "facebookexternalhit", "embedly", "quora link preview",
	"whatsapp", "google-inspectiontool", "bingpreview", "headlesschrome", "phantomjs", "pingdom",
	"uptimerobot", "curl/", "wget/", "python-requests", "go-http-client",
}

func isBotUA(ua string) bool {
	ual := strings.ToLower(ua)
	for _, p := range botPatterns {
		if strings.Contains(ual, p) {
			return true
		}
	}
	return false
}

// parseUserAgent derives a coarse browser/OS/device breakdown without a third-party dependency —
// good enough for a summary view, not a precise device database.
func parseUserAgent(ua string) (browser, os, device string) {
	ual := strings.ToLower(ua)
	switch {
	case strings.Contains(ual, "edg/"):
		browser = "Edge"
	case strings.Contains(ual, "opr/") || strings.Contains(ual, "opera"):
		browser = "Opera"
	case strings.Contains(ual, "firefox/"):
		browser = "Firefox"
	case strings.Contains(ual, "chrome/"):
		browser = "Chrome"
	case strings.Contains(ual, "safari/"):
		browser = "Safari"
	default:
		browser = "Other"
	}
	switch {
	case strings.Contains(ual, "windows"):
		os = "Windows"
	case strings.Contains(ual, "iphone"), strings.Contains(ual, "ipad"), strings.Contains(ual, "ios"):
		// must precede "mac os x": an iPhone/iPad UA reads "like Mac OS X" as part of its own token.
		os = "iOS"
	case strings.Contains(ual, "mac os x"), strings.Contains(ual, "macintosh"):
		os = "macOS"
	case strings.Contains(ual, "android"):
		os = "Android"
	case strings.Contains(ual, "linux"):
		os = "Linux"
	default:
		os = "Other"
	}
	switch {
	case strings.Contains(ual, "ipad"), strings.Contains(ual, "tablet"):
		device = "tablet"
	case strings.Contains(ual, "mobi"), strings.Contains(ual, "iphone"), strings.Contains(ual, "android"):
		device = "mobile"
	default:
		device = "desktop"
	}
	return browser, os, device
}
