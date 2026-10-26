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
	visits    []domain.Visit // the visit each pageview was recorded with
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
func (f *fakeRepo) RecordPageview(_ context.Context, p domain.Pageview, v domain.Visit) error {
	f.pageviews = append(f.pageviews, p)
	f.visits = append(f.visits, v)
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

// Change 35: the salt turns over at the site's local midnight — 21:00 UTC for Europe/Moscow.
func TestVisitorHashRotatesAtTheSitesMidnight(t *testing.T) {
	repo := newFakeRepo()
	site := domain.Site{ID: "site1", Timezone: "Europe/Moscow"}
	hashAt := func(now time.Time) string {
		svc := NewService(repo, func() time.Time { return now }, Options{})
		h, err := svc.visitorHash(context.Background(), site, "203.0.113.5", "UA", now)
		if err != nil {
			t.Fatalf("visitorHash: %v", err)
		}
		return h
	}
	morning := hashAt(time.Date(2026, 9, 22, 0, 30, 0, 0, time.UTC))       // 03:30 MSK, 22 Sep
	evening := hashAt(time.Date(2026, 9, 22, 20, 59, 0, 0, time.UTC))      // 23:59 MSK, 22 Sep
	nextMoscowDay := hashAt(time.Date(2026, 9, 22, 21, 0, 1, 0, time.UTC)) // 00:00 MSK, 23 Sep
	if morning != evening {
		t.Fatalf("hash changed within one Moscow day: %q vs %q", morning, evening)
	}
	if evening == nextMoscowDay {
		t.Fatal("hash did not rotate at Moscow midnight")
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

type fakeGeo struct{ ip string }

func (g *fakeGeo) Locate(ip string) (string, string) {
	g.ip = ip
	return "RU", "Kirov Oblast"
}

// Change 35: a v2 beacon's campaign, screen and hostname, the server-side geography and versions
// reach the visit; the visitor's IP only reaches the geography lookup.
func TestCollectDescribesTheVisitFromAV2Beacon(t *testing.T) {
	repo := newFakeRepo()
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	repo.sites["s"] = domain.Site{ID: "s", Domain: "kodumka.ru", Timezone: "Europe/Moscow"}
	geo := &fakeGeo{}
	svc := NewService(repo, func() time.Time { return now }, Options{Geo: geo})
	const ua = "Mozilla/5.0 (Linux; Android 14; SM-A546B) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/134.0.0.0 YaBrowser/25.4.1.100 Mobile Safari/537.36"
	b := domain.Beacon{Site: "s", URL: "/ege", Referrer: "https://vk.com", V: 2,
		UTM: domain.UTM{Source: "VK", Medium: "social", Campaign: "ege 16 октябрь"}, Width: 390, Hostname: "www.kodumka.ru"}
	if stored, err := svc.Collect(context.Background(), b, "203.0.113.5", ua, "www.kodumka.ru"); err != nil || !stored {
		t.Fatalf("stored=%v err=%v", stored, err)
	}
	pv, v := repo.pageviews[0], repo.visits[0]
	if pv.Hostname != "www.kodumka.ru" || geo.ip != "203.0.113.5" {
		t.Fatalf("hostname %q, geo saw %q", pv.Hostname, geo.ip)
	}
	want := domain.Visit{Source: "VK", Channel: ChannelSocial,
		UTM:     domain.UTM{Source: "vk", Medium: "social", Campaign: domain.InvalidUTM},
		Country: "RU", Region: "Kirov Oblast", BrowserVersion: "25", OSVersion: "14", Screen: "mobile"}
	if v != want {
		t.Fatalf("visit = %+v\nwant    %+v", v, want)
	}
	// a hostname outside the site falls back to the Origin the beacon came from
	b.Hostname = "evil.test"
	if _, err := svc.Collect(context.Background(), b, "203.0.113.5", ua, "kodumka.ru"); err != nil {
		t.Fatal(err)
	}
	if got := repo.pageviews[1].Hostname; got != "kodumka.ru" {
		t.Fatalf("foreign hostname kept: %q", got)
	}
}

func TestUAVersions(t *testing.T) {
	cases := []struct{ ua, browser, os, wantBrowser, wantOS string }{
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36 Edg/131.0.0.0", "Edge", "Windows", "131", "10/11"},
		{"Mozilla/5.0 (Windows NT 6.1; Win64; x64) AppleWebKit/537.36 Chrome/109.0 Safari/537.36", "Chrome", "Windows", "109", "7"},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 18_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.1 Mobile/15E148 Safari/604.1", "Safari", "iOS", "18", "18"},
		{"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15", "Safari", "macOS", "17", "10.15"},
		{"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Firefox/120.0", "Firefox", "Linux", "120", ""},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 18_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) FxiOS/133.0 Mobile/15E148 Safari/605.1.15", "Firefox", "iOS", "133", "18"},
		{"Mozilla/5.0 something unknown", "Other", "Other", "", ""},
	}
	for _, c := range cases {
		b, o := uaVersions(c.ua, c.browser, c.os)
		if b != c.wantBrowser || o != c.wantOS {
			t.Errorf("uaVersions(%q) = %q, %q; want %q, %q", c.ua, b, o, c.wantBrowser, c.wantOS)
		}
	}
}
