// Package application implements site creation, pageview ingestion and queries for site analytics
// (docs/SPEC.md §4i).
package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/analytics/domain"
)

// purgeInterval mirrors the telemetry maintenance job's cadence
// (internal/telemetry/application/maintenance.go).
const purgeInterval = time.Hour

// Repository is the persistence port.
type Repository interface {
	CreateSite(ctx context.Context, n domain.NewSite, now time.Time) (domain.Site, error)
	ListSites(ctx context.Context) ([]domain.Site, error)
	SiteExists(ctx context.Context, id string) (domain.Site, bool, error)
	DeleteSite(ctx context.Context, id string) error
	// RecordPageview stores a pageview within its visit (Change 35): the latest visit of the same
	// site and visitor hash whose last pageview is less than domain.VisitIdle old, else a new visit
	// described by v. One transaction, so a crash never leaves a pageview without its visit.
	RecordPageview(ctx context.Context, p domain.Pageview, v domain.Visit) error
	DailySaltBase(ctx context.Context) (string, error)
	PurgePageviews(ctx context.Context, before time.Time, limit int) (int, error)
}

// Geo places a visitor by IP (Change 35): an ISO country code and a region name, "" when unknown.
// The IP is used for this lookup only and never stored.
type Geo interface {
	Locate(ip string) (country, region string)
}

type noGeo struct{}

func (noGeo) Locate(string) (string, string) { return "", "" }

// Options tune retention and geography; the zero value disables purging and places nobody.
type Options struct {
	Retention time.Duration
	Geo       Geo
}

// Collect outcomes counted since the server started (docs/SPEC.md §4i, Change 26): the public
// endpoint always answers 204, so these are the only place a drop or a storage error shows.
const (
	OutcomeStored         = "stored"
	OutcomeRateLimited    = "rate_limited"
	OutcomeMalformed      = "malformed"
	OutcomeInvalid        = "invalid"
	OutcomeBot            = "bot"
	OutcomeUnknownSite    = "unknown_site"
	OutcomeOriginMismatch = "origin_mismatch"
	OutcomeError          = "error"
)

// Service manages sites and turns beacons into stored pageviews.
type Service struct {
	repo Repository
	now  func() time.Time
	opts Options

	countsMu sync.Mutex
	counts   map[string]int64

	// Change 38: every beacon needs the site list and the salt base. Both are cached: the salt
	// base never changes once stored, the list changes only through CreateSite and DeleteSite
	// (which reset it) and is reloaded after sitesTTL in any case.
	cacheMu  sync.Mutex
	sites    []domain.Site
	sitesAt  time.Time
	saltBase string
}

// sitesTTL bounds how long the cached site list is trusted.
const sitesTTL = time.Minute

// NewService wires the service.
func NewService(repo Repository, now func() time.Time, opts Options) *Service {
	if opts.Geo == nil {
		opts.Geo = noGeo{}
	}
	return &Service{repo: repo, now: now, opts: opts, counts: map[string]int64{}}
}

// CountCollect records one collect outcome.
func (s *Service) CountCollect(outcome string) {
	s.countsMu.Lock()
	s.counts[outcome]++
	s.countsMu.Unlock()
}

// CollectCounts returns every outcome since start, zero for outcomes not seen yet.
func (s *Service) CollectCounts() map[string]int64 {
	s.countsMu.Lock()
	defer s.countsMu.Unlock()
	out := map[string]int64{}
	for _, o := range []string{OutcomeStored, OutcomeRateLimited, OutcomeMalformed, OutcomeInvalid,
		OutcomeBot, OutcomeUnknownSite, OutcomeOriginMismatch, OutcomeError} {
		out[o] = s.counts[o]
	}
	return out
}

// CreateSite validates and stores a site.
func (s *Service) CreateSite(ctx context.Context, n domain.NewSite) (domain.Site, error) {
	valid, err := n.Validate()
	if err != nil {
		return domain.Site{}, err
	}
	defer s.resetSites()
	return s.repo.CreateSite(ctx, valid, s.now())
}

// Sites lists every tracked site.
func (s *Service) Sites(ctx context.Context) ([]domain.Site, error) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if s.sites == nil || s.now().Sub(s.sitesAt) >= sitesTTL || s.now().Before(s.sitesAt) {
		sites, err := s.repo.ListSites(ctx)
		if err != nil {
			return nil, err
		}
		s.sites, s.sitesAt = sites, s.now()
	}
	return append([]domain.Site(nil), s.sites...), nil
}

// site finds a tracked site in the cached list.
func (s *Service) site(ctx context.Context, id string) (domain.Site, bool, error) {
	sites, err := s.Sites(ctx)
	if err != nil {
		return domain.Site{}, false, err
	}
	for _, site := range sites {
		if site.ID == id {
			return site, true, nil
		}
	}
	return domain.Site{}, false, nil
}

func (s *Service) resetSites() {
	s.cacheMu.Lock()
	s.sites = nil
	s.cacheMu.Unlock()
}

// DeleteSite removes a site and its pageviews.
func (s *Service) DeleteSite(ctx context.Context, id string) error {
	defer s.resetSites()
	return s.repo.DeleteSite(ctx, id)
}

// Collect validates a beacon, derives everything server-side, and stores the pageview. It never
// returns an error the caller should act on beyond "nothing was stored" (docs/SPEC.md §4i: the
// endpoint always answers 204) — the boolean reports whether a pageview was actually stored, purely
// for the caller's own logging/testing, not for the HTTP response.
// originHost is the request Origin's hostname; a beacon from any other origin (the site's own
// localhost/CI/Lighthouse runs, a missing Origin) is dropped.
func (s *Service) Collect(ctx context.Context, b domain.Beacon, clientIP, userAgent, originHost string) (bool, error) {
	stored, outcome, err := s.collect(ctx, b, clientIP, userAgent, originHost)
	s.CountCollect(outcome)
	return stored, err
}

func (s *Service) collect(ctx context.Context, b domain.Beacon, clientIP, userAgent, originHost string) (bool, string, error) {
	b, ok := b.Normalize()
	if !ok {
		return false, OutcomeInvalid, nil
	}
	if isBotUA(userAgent) {
		return false, OutcomeBot, nil
	}
	site, found, err := s.site(ctx, b.Site)
	if err != nil {
		return false, OutcomeError, err
	}
	if !found {
		return false, OutcomeUnknownSite, nil
	}
	if !OriginMatchesSite(originHost, site.Domain) {
		return false, OutcomeOriginMismatch, nil
	}
	now := s.now()
	hash, err := s.visitorHash(ctx, site, clientIP, userAgent, now)
	if err != nil {
		return false, OutcomeError, err
	}
	browser, os, device := parseUserAgent(userAgent)
	referrer := externalReferrer(b.Referrer, site.Domain)
	source, channel := classify(referrer, b.UTM, b.Paid)
	country, region := s.opts.Geo.Locate(clientIP)
	browserMajor, osVersion := uaVersions(userAgent, browser, os)
	// the page's own host when it belongs to the site, else the Origin it came from
	hostname := strings.ToLower(originHost)
	if OriginMatchesSite(b.Hostname, site.Domain) {
		hostname = b.Hostname
	}
	err = s.repo.RecordPageview(ctx, domain.Pageview{
		SiteID:         site.ID,
		TS:             now,
		Path:           pathOnly(b.URL),
		ReferrerDomain: referrer,
		VisitorHash:    hash,
		Browser:        browser,
		OS:             os,
		Device:         device,
		Hostname:       hostname,
	}, domain.Visit{
		Source:         source,
		Channel:        channel,
		UTM:            b.UTM,
		Paid:           b.Paid,
		Country:        country,
		Region:         region,
		BrowserVersion: browserMajor,
		OSVersion:      osVersion,
		Screen:         domain.ScreenGroup(b.Width),
	})
	if err != nil {
		return false, OutcomeError, err
	}
	return true, OutcomeStored, nil
}

// OriginMatchesSite reports whether an Origin hostname is the site's domain or www. plus it,
// case-insensitively (docs/SPEC.md §4i).
func OriginMatchesSite(originHost, siteDomain string) bool {
	host := strings.ToLower(originHost)
	d := strings.ToLower(siteDomain)
	return host != "" && (host == d || host == "www."+d)
}

// visitorHash derives a daily-rotating, salted, unlinkable-across-days visitor identity
// (docs/SPEC.md §4i): sha256(sha256(secret|site-day) | site | IP | UA), truncated to 16 bytes. The
// day is the site's local date (Change 35), so a visitor's day and the report's day agree. The
// raw IP is used only for this one computation and is never stored.
func (s *Service) visitorHash(ctx context.Context, site domain.Site, ip, ua string, now time.Time) (string, error) {
	base, err := s.dailySaltBase(ctx)
	if err != nil {
		return "", err
	}
	day := now.In(site.Location()).Format("2006-01-02")
	daily := sha256.Sum256([]byte(base + "|" + day))
	visitor := sha256.Sum256([]byte(hex.EncodeToString(daily[:]) + "|" + site.ID + "|" + ip + "|" + ua))
	return hex.EncodeToString(visitor[:16]), nil
}

// dailySaltBase is the stored salt secret, read once (it never changes after it is generated).
func (s *Service) dailySaltBase(ctx context.Context) (string, error) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if s.saltBase == "" {
		base, err := s.repo.DailySaltBase(ctx)
		if err != nil {
			return "", err
		}
		s.saltBase = base
	}
	return s.saltBase, nil
}

// Purge deletes pageviews and visits older than the retention window, in bounded batches.
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

// Run executes the purge immediately and then hourly until ctx is done. Failures are logged and
// retried at the next tick; they never stop the server (mirrors
// internal/telemetry/application/maintenance.go's Run). The daily aggregates have their own job
// (Queries.Run, Change 36).
func (s *Service) Run(ctx context.Context) {
	s.runOnce(ctx, "purge", s.Purge)
	purgeTick := time.NewTicker(purgeInterval)
	defer purgeTick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
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

// pathOnly keeps a beacon URL's pathname: the query string and fragment can carry tokens or
// e-mail addresses and split one page into many (docs/SPEC.md §4i). An older snippet still sends
// them, so they are dropped here, not only in track.js.
func pathOnly(u string) string {
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		u = u[:i]
	}
	if u == "" {
		return "/"
	}
	return u
}

// externalReferrer is the referrer's hostname, or "" when there is none or it is the site itself:
// navigation inside the site is not a referral (docs/SPEC.md §4i).
func externalReferrer(referrer, siteDomain string) string {
	host := referrerDomain(referrer)
	if OriginMatchesSite(host, siteDomain) {
		return ""
	}
	return host
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
	// Lighthouse and PageSpeed Insights load the real production origin with a mobile-Chrome UA plus
	// this token; the origin rule (Change 18) only drops their local runs (docs/SPEC.md §4i).
	"chrome-lighthouse",
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
// good enough for a summary view, not a precise device database. Browser tokens are checked
// most-specific first: each browser also carries the tokens of the engines after it (Yandex
// Browser says Chrome/ and Safari/, Chrome on iOS says Safari/) (docs/SPEC.md §4i).
func parseUserAgent(ua string) (browser, os, device string) {
	ual := strings.ToLower(ua)
	switch {
	case strings.Contains(ual, "yabrowser/"):
		browser = "Yandex Browser"
	case strings.Contains(ual, "edg/"), strings.Contains(ual, "edgios/"), strings.Contains(ual, "edga/"):
		browser = "Edge"
	case strings.Contains(ual, "opr/") || strings.Contains(ual, "opera"):
		browser = "Opera"
	case strings.Contains(ual, "firefox/"), strings.Contains(ual, "fxios/"):
		browser = "Firefox"
	case strings.Contains(ual, "chrome/"), strings.Contains(ual, "crios/"):
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
