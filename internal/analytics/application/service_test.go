package application

import (
	"context"
	"testing"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/analytics/domain"
)

type fakeRepo struct {
	sites     map[string]domain.Site
	pageviews []domain.Pageview
	salt      string
	since     time.Time // the last Stats call's lower bound
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{sites: map[string]domain.Site{}, salt: "test-salt-base"}
}

func (f *fakeRepo) CreateSite(_ context.Context, n domain.NewSite, now time.Time) (domain.Site, error) {
	s := domain.Site{ID: n.Domain, Name: n.Name, Domain: n.Domain, CreatedAt: now}
	f.sites[s.ID] = s
	return s, nil
}
func (f *fakeRepo) ListSites(context.Context) ([]domain.Site, error) {
	out := make([]domain.Site, 0, len(f.sites))
	for _, s := range f.sites {
		out = append(out, s)
	}
	return out, nil
}
func (f *fakeRepo) SiteExists(_ context.Context, id string) (domain.Site, bool, error) {
	s, ok := f.sites[id]
	return s, ok, nil
}
func (f *fakeRepo) DeleteSite(_ context.Context, id string) error { delete(f.sites, id); return nil }
func (f *fakeRepo) InsertPageview(_ context.Context, p domain.Pageview) error {
	f.pageviews = append(f.pageviews, p)
	return nil
}
func (f *fakeRepo) Stats(_ context.Context, _ string, since time.Time) (domain.Stats, error) {
	f.since = since
	return domain.Stats{}, nil
}
func (f *fakeRepo) DailySaltBase(context.Context) (string, error)               { return f.salt, nil }
func (f *fakeRepo) PurgePageviews(context.Context, time.Time, int) (int, error) { return 0, nil }

func TestCollectStoresAValidBeacon(t *testing.T) {
	repo := newFakeRepo()
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	repo.sites["infraege.ru"] = domain.Site{ID: "infraege.ru", Name: "Infraege", Domain: "infraege.ru", CreatedAt: now}
	svc := NewService(repo, func() time.Time { return now }, Options{})

	stored, err := svc.Collect(context.Background(), domain.Beacon{Site: "infraege.ru", URL: "/docs", Referrer: "https://google.com/search?q=x"}, "203.0.113.5", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120.0", "infraege.ru")
	if err != nil || !stored {
		t.Fatalf("Collect: stored=%v err=%v", stored, err)
	}
	if len(repo.pageviews) != 1 {
		t.Fatalf("got %d pageviews, want 1", len(repo.pageviews))
	}
	pv := repo.pageviews[0]
	if pv.Path != "/docs" || pv.ReferrerDomain != "google.com" || pv.Browser != "Chrome" || pv.OS != "Windows" || pv.Device != "desktop" {
		t.Fatalf("pageview = %+v", pv)
	}
	if pv.VisitorHash == "" {
		t.Fatal("want a non-empty visitor hash")
	}
}

func TestCollectDropsUnknownSiteWithoutStoringOrErroring(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, time.Now, Options{})
	stored, err := svc.Collect(context.Background(), domain.Beacon{Site: "no-such-site", URL: "/"}, "1.2.3.4", "Mozilla/5.0", "no-such-site")
	if err != nil {
		t.Fatalf("Collect returned an error for an unknown site: %v", err)
	}
	if stored {
		t.Fatal("want nothing stored for an unknown site")
	}
	if len(repo.pageviews) != 0 {
		t.Fatalf("got %d pageviews, want 0", len(repo.pageviews))
	}
}

func TestCollectDropsKnownBotUserAgents(t *testing.T) {
	repo := newFakeRepo()
	repo.sites["s"] = domain.Site{ID: "s", Domain: "s"}
	svc := NewService(repo, time.Now, Options{})
	bots := []string{
		"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
		"curl/8.5.0",
		"python-requests/2.31.0",
		"Mozilla/5.0 (compatible; AhrefsBot/7.0)",
		// PageSpeed Insights / Lighthouse on the production origin (docs/SPEC.md §4i).
		"Mozilla/5.0 (Linux; Android 11; moto g power (2022)) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/137.0.0.0 Mobile Safari/537.36 Chrome-Lighthouse",
	}
	for _, ua := range bots {
		stored, err := svc.Collect(context.Background(), domain.Beacon{Site: "s", URL: "/"}, "1.2.3.4", ua, "s")
		if err != nil {
			t.Fatalf("Collect(%q): %v", ua, err)
		}
		if stored {
			t.Fatalf("Collect(%q): want a bot User-Agent dropped", ua)
		}
	}
}

// A tracked site's own local, CI and Lighthouse runs load the snippet from localhost/127.x; only
// beacons from the site's own origin count (docs/SPEC.md §4i).
func TestCollectStoresOnlyBeaconsFromTheSitesOwnOrigin(t *testing.T) {
	const ua = "Mozilla/5.0 (Linux; Android 11) AppleWebKit/537.36 Chrome/120.0 Mobile Safari/537.36"
	cases := []struct {
		origin string
		stored bool
	}{
		{"infraege.ru", true},
		{"INFRAEGE.RU", true},
		{"www.infraege.ru", true},
		{"localhost", false},
		{"127.0.0.2", false},
		{"evil.example", false},
		{"sub.infraege.ru", false},
		{"infraege.ru.evil.example", false},
		{"", false},
	}
	for _, c := range cases {
		t.Run(c.origin, func(t *testing.T) {
			repo := newFakeRepo()
			repo.sites["id1"] = domain.Site{ID: "id1", Domain: "infraege.ru"}
			svc := NewService(repo, time.Now, Options{})
			stored, err := svc.Collect(context.Background(), domain.Beacon{Site: "id1", URL: "/"}, "1.2.3.4", ua, c.origin)
			if err != nil {
				t.Fatal(err)
			}
			if stored != c.stored || len(repo.pageviews) != map[bool]int{true: 1, false: 0}[c.stored] {
				t.Fatalf("origin %q: stored=%v pageviews=%d, want stored=%v", c.origin, stored, len(repo.pageviews), c.stored)
			}
		})
	}
}

func TestVisitorHashRotatesDailyAndIsStableWithinADay(t *testing.T) {
	repo := newFakeRepo()
	day1 := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)
	day1Later := time.Date(2026, 9, 22, 23, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 9, 23, 0, 0, 1, 0, time.UTC)

	hashAt := func(now time.Time) string {
		svc := NewService(repo, func() time.Time { return now }, Options{})
		h, err := svc.visitorHash(context.Background(), "site1", "203.0.113.5", "UA")
		if err != nil {
			t.Fatalf("visitorHash: %v", err)
		}
		return h
	}
	h1, h1b, h2 := hashAt(day1), hashAt(day1Later), hashAt(day2)
	if h1 != h1b {
		t.Fatalf("hash changed within the same UTC day: %q vs %q", h1, h1b)
	}
	if h1 == h2 {
		t.Fatal("hash did not rotate across a UTC day boundary")
	}
}

func TestReferrerDomainExtractsHostnameOnly(t *testing.T) {
	cases := map[string]string{
		"":          "",
		"not a url": "",
		"https://google.com/search?q=secret+terms": "google.com",
		"https://sub.example.com/x":                "sub.example.com",
	}
	for in, want := range cases {
		if got := referrerDomain(in); got != want {
			t.Errorf("referrerDomain(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseUserAgent(t *testing.T) {
	cases := []struct {
		ua                  string
		browser, os, device string
	}{
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36", "Chrome", "Windows", "desktop"},
		{"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15", "Safari", "macOS", "desktop"},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1", "Safari", "iOS", "mobile"},
		{"Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1", "Safari", "iOS", "tablet"},
		{"Mozilla/5.0 (Linux; Android 13) AppleWebKit/537.36 Chrome/120.0 Mobile Safari/537.36", "Chrome", "Android", "mobile"},
		{"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Firefox/120.0", "Firefox", "Linux", "desktop"},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/134.0.0.0 YaBrowser/25.4.0.0 Safari/537.36", "Yandex Browser", "Windows", "desktop"},
		{"Mozilla/5.0 (Linux; Android 14; SM-A546B) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/134.0.0.0 YaBrowser/25.4.1.100 Mobile Safari/537.36", "Yandex Browser", "Android", "mobile"},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 18_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/131.0.6778.73 Mobile/15E148 Safari/604.1", "Chrome", "iOS", "mobile"},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 18_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) FxiOS/133.0 Mobile/15E148 Safari/605.1.15", "Firefox", "iOS", "mobile"},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 18_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.1 Mobile/15E148 Safari/604.1 EdgiOS/131.0.2903.68", "Edge", "iOS", "mobile"},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36 Edg/131.0.0.0", "Edge", "Windows", "desktop"},
	}
	for _, c := range cases {
		browser, os, device := parseUserAgent(c.ua)
		if browser != c.browser || os != c.os || device != c.device {
			t.Errorf("parseUserAgent(%q) = (%q,%q,%q), want (%q,%q,%q)", c.ua, browser, os, device, c.browser, c.os, c.device)
		}
	}
}

// The stored path is the pathname only, also when an old snippet still sends a query or fragment
// (docs/SPEC.md §4i); the site's own domain is not a referrer.
func TestCollectStoresPathnameOnlyAndOnlyExternalReferrers(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		url, referrer, wantPath, wantRef string
	}{
		{"/?_=1790150776977", "", "/", ""},
		{"/reset?token=secret&email=a@b.c", "", "/reset", ""},
		{"/docs#intro", "", "/docs", ""},
		{"?x=1", "", "/", ""},
		{"/courses", "https://infraege.ru/", "/courses", ""},
		{"/courses", "https://WWW.infraege.ru/ege", "/courses", ""},
		{"/courses", "https://yandex.ru/search/?text=ege", "/courses", "yandex.ru"},
		{"/courses", "https://sub.infraege.ru/", "/courses", "sub.infraege.ru"},
	} {
		repo := newFakeRepo()
		repo.sites["s"] = domain.Site{ID: "s", Domain: "infraege.ru"}
		svc := NewService(repo, func() time.Time { return now }, Options{})
		_, err := svc.Collect(context.Background(), domain.Beacon{Site: "s", URL: c.url, Referrer: c.referrer},
			"203.0.113.5", "Mozilla/5.0 (Windows NT 10.0) Chrome/120.0", "infraege.ru")
		if err != nil || len(repo.pageviews) != 1 {
			t.Fatalf("%q: err=%v stored=%d", c.url, err, len(repo.pageviews))
		}
		if pv := repo.pageviews[0]; pv.Path != c.wantPath || pv.ReferrerDomain != c.wantRef {
			t.Errorf("url %q referrer %q: path=%q ref=%q, want %q %q", c.url, c.referrer, pv.Path, pv.ReferrerDomain, c.wantPath, c.wantRef)
		}
	}
}

// Ranges are UTC calendar days, today included (docs/SPEC.md §4i), including right around midnight.
func TestStatsRangesAreUTCCalendarDays(t *testing.T) {
	for _, c := range []struct {
		now   time.Time
		rng   string
		since time.Time
	}{
		{time.Date(2026, 9, 23, 0, 1, 0, 0, time.UTC), "today", time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)},
		{time.Date(2026, 9, 22, 23, 59, 0, 0, time.UTC), "today", time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)},
		{time.Date(2026, 9, 23, 3, 0, 0, 0, time.FixedZone("MSK", 3*3600)), "today", time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)},
		{time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC), "", time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)},
		{time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC), "7d", time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)},
		{time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC), "30d", time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)},
	} {
		repo := newFakeRepo()
		svc := NewService(repo, func() time.Time { return c.now }, Options{})
		if _, err := svc.Stats(context.Background(), "s", c.rng); err != nil {
			t.Fatal(err)
		}
		if !repo.since.Equal(c.since) {
			t.Errorf("now %s range %q: since %s, want %s", c.now, c.rng, repo.since.UTC(), c.since)
		}
	}
}
