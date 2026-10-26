package infrastructure

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/analytics/domain"
	"github.com/avatarsik6699/smotryashchiy/internal/platform/db"
)

// direct is the visit description the older tests do not care about.
var direct = domain.Visit{Channel: "direct"}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "analytics.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Migrate(sqlDB); err != nil {
		t.Fatal(err)
	}
	return NewStore(sqlDB)
}

func TestCreateSiteEnforcesUniqueDomainAndLimit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now()
	if _, err := s.CreateSite(ctx, domain.NewSite{Name: "A", Domain: "a.example"}, now); err != nil {
		t.Fatalf("CreateSite: %v", err)
	}
	if _, err := s.CreateSite(ctx, domain.NewSite{Name: "A2", Domain: "a.example"}, now); err == nil {
		t.Fatal("want a conflict error for a duplicate domain")
	}
	for i := 1; i < domain.MaxSites; i++ {
		if _, err := s.CreateSite(ctx, domain.NewSite{Name: "N", Domain: time.Now().Format("150405.000000000") + ".example"}, now); err != nil {
			t.Fatalf("CreateSite #%d: %v", i, err)
		}
	}
	if _, err := s.CreateSite(ctx, domain.NewSite{Name: "over", Domain: "over.example"}, now); err == nil {
		t.Fatal("want a conflict error once the site limit is reached")
	}
}

func TestSiteExistsAndDeleteCascadesPageviews(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now()
	site, err := s.CreateSite(ctx, domain.NewSite{Name: "A", Domain: "a.example"}, now)
	if err != nil {
		t.Fatalf("CreateSite: %v", err)
	}
	if _, found, err := s.SiteExists(ctx, "no-such-id"); err != nil || found {
		t.Fatalf("SiteExists(unknown) = found=%v err=%v", found, err)
	}
	if got, found, err := s.SiteExists(ctx, site.ID); err != nil || !found || got.Domain != "a.example" {
		t.Fatalf("SiteExists(%q) = %+v found=%v err=%v", site.ID, got, found, err)
	}
	if err := s.RecordPageview(ctx, domain.Pageview{SiteID: site.ID, TS: now, Path: "/", VisitorHash: "h1"}, direct); err != nil {
		t.Fatalf("RecordPageview: %v", err)
	}
	if err := s.DeleteSite(ctx, site.ID); err != nil {
		t.Fatalf("DeleteSite: %v", err)
	}
	stats, err := s.Stats(ctx, site.ID, now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("Stats after delete: %v", err)
	}
	if stats.Pageviews != 0 {
		t.Fatalf("pageviews survived site deletion: %+v", stats)
	}
	if err := s.DeleteSite(ctx, site.ID); err == nil {
		t.Fatal("want a not-found error deleting an already-deleted site")
	}
}

func TestStatsAggregatesPageviewsAndTopLists(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now()
	site, err := s.CreateSite(ctx, domain.NewSite{Name: "A", Domain: "a.example"}, now)
	if err != nil {
		t.Fatalf("CreateSite: %v", err)
	}
	rows := []domain.Pageview{
		{SiteID: site.ID, TS: now, Path: "/home", ReferrerDomain: "google.com", VisitorHash: "v1"},
		{SiteID: site.ID, TS: now, Path: "/home", ReferrerDomain: "google.com", VisitorHash: "v2"},
		{SiteID: site.ID, TS: now, Path: "/about", ReferrerDomain: "", VisitorHash: "v1"},
		{SiteID: site.ID, TS: now.Add(-48 * time.Hour), Path: "/old", VisitorHash: "v3"}, // outside the 24h window below
	}
	for _, r := range rows {
		if err := s.RecordPageview(ctx, r, direct); err != nil {
			t.Fatalf("RecordPageview: %v", err)
		}
	}
	stats, err := s.Stats(ctx, site.ID, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Pageviews != 3 {
		t.Fatalf("Pageviews = %d, want 3 (the old row must be excluded)", stats.Pageviews)
	}
	if stats.Visitors != 2 {
		t.Fatalf("Visitors = %d, want 2 distinct visitor hashes", stats.Visitors)
	}
	if len(stats.TopPages) == 0 || stats.TopPages[0].Path != "/home" || stats.TopPages[0].Count != 2 {
		t.Fatalf("TopPages = %+v", stats.TopPages)
	}
	if len(stats.TopReferrers) != 1 || stats.TopReferrers[0].Domain != "google.com" || stats.TopReferrers[0].Count != 2 {
		t.Fatalf("TopReferrers = %+v (empty referrer must be excluded)", stats.TopReferrers)
	}
}

func TestDailySaltBaseIsGeneratedOnceAndStable(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	a, err := s.DailySaltBase(ctx)
	if err != nil || a == "" {
		t.Fatalf("DailySaltBase: %q, %v", a, err)
	}
	b, err := s.DailySaltBase(ctx)
	if err != nil || a != b {
		t.Fatalf("DailySaltBase not stable: %q vs %q, err=%v", a, b, err)
	}
}

func TestPurgeRemovesOldPageviews(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	site, err := s.CreateSite(ctx, domain.NewSite{Name: "A", Domain: "a.example"}, time.Now())
	if err != nil {
		t.Fatalf("CreateSite: %v", err)
	}
	day := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	for i, ts := range []time.Time{day.Add(time.Hour), day.Add(2 * time.Hour), day.Add(3 * time.Hour)} {
		hash := "v1"
		if i == 2 {
			hash = "v2"
		}
		if err := s.RecordPageview(ctx, domain.Pageview{SiteID: site.ID, TS: ts, Path: "/x", VisitorHash: hash}, direct); err != nil {
			t.Fatalf("RecordPageview: %v", err)
		}
	}
	// three pageviews in three visits (an hour apart is past the 30-minute idle limit)
	n, err := s.PurgePageviews(ctx, day.Add(24*time.Hour), 100)
	if err != nil {
		t.Fatalf("PurgePageviews: %v", err)
	}
	if n != 6 {
		t.Fatalf("purged %d rows, want 3 pageviews + 3 visits", n)
	}
	if n, err := s.PurgePageviews(ctx, day.Add(24*time.Hour), 100); err != nil || n != 0 {
		t.Fatalf("second purge = %d, %v; want nothing left", n, err)
	}
	stats, err := s.Stats(ctx, site.ID, day.Add(-time.Hour))
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Pageviews != 0 {
		t.Fatalf("raw pageviews survived purge: %+v", stats)
	}
}

// Change 20 dropped the daily pageview rollups (docs/SPEC.md §4i): a migrated database has no such
// table, so nothing can quietly start writing to it again.
func TestMigratedDatabaseHasNoPageviewRollups(t *testing.T) {
	s := newTestStore(t)
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'pageview_rollups_daily'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("pageview_rollups_daily still exists after migrating")
	}
}

// Change 35: a hand-counted day. Visitor a: /ege, /ege/16 ten minutes later (one visit of two
// pages, 10 min). Visitor b: one page (a bounce). Visitor a again 40 minutes after its last page:
// a new visit. Visits, bounces, duration, entry and exit pages follow.
func TestRecordPageviewGroupsVisits(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	start := time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)
	site, err := s.CreateSite(ctx, domain.NewSite{Name: "K", Domain: "kodumka.ru", Timezone: "Europe/Moscow"}, start)
	if err != nil {
		t.Fatal(err)
	}
	first := domain.Visit{Source: "Яндекс", Channel: "organic_search", Country: "RU", Region: "Moscow", Screen: "mobile",
		UTM: domain.UTM{Source: "vk"}, BrowserVersion: "131", OSVersion: "14"}
	steps := []struct {
		at   time.Duration
		hash string
		path string
		v    domain.Visit
	}{
		{0, "a", "/ege", first},
		{10 * time.Minute, "a", "/ege/16", direct}, // continues: the description of the visit stays
		{12 * time.Minute, "b", "/", direct},
		{50 * time.Minute, "a", "/courses", direct}, // 40 min after a's last page: a new visit
	}
	for _, st := range steps {
		if err := s.RecordPageview(ctx, domain.Pageview{SiteID: site.ID, TS: start.Add(st.at), Path: st.path,
			VisitorHash: st.hash, Hostname: "kodumka.ru"}, st.v); err != nil {
			t.Fatal(err)
		}
	}
	stats, err := s.Stats(ctx, site.ID, start)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Pageviews != 4 || stats.Visitors != 2 || stats.Visits != 3 {
		t.Fatalf("pageviews/visitors/visits = %d/%d/%d, want 4/2/3", stats.Pageviews, stats.Visitors, stats.Visits)
	}
	// two of three visits have one page; the mean duration is (600 + 0 + 0) / 3 s
	if stats.BounceRate < 0.666 || stats.BounceRate > 0.667 || stats.VisitDurationSeconds != 200 {
		t.Fatalf("bounce %.3f duration %d, want 0.667 and 200", stats.BounceRate, stats.VisitDurationSeconds)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT visitor_hash, entry_path, exit_path, pageviews, source, channel, country,
		region, screen, utm_source, browser_version, os_version FROM visits ORDER BY started_at`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var hash, entry, exit, source, channel, country, region, screen, utm, bv, ov string
		var n int
		if err := rows.Scan(&hash, &entry, &exit, &n, &source, &channel, &country, &region, &screen, &utm, &bv, &ov); err != nil {
			t.Fatal(err)
		}
		got = append(got, fmt.Sprintf("%s %s→%s %d %s/%s %s/%s %s %s %s/%s", hash, entry, exit, n, source, channel, country, region, screen, utm, bv, ov))
	}
	want := []string{
		"a /ege→/ege/16 2 Яндекс/organic_search RU/Moscow mobile vk 131/14",
		"b /→/ 1 /direct /   /",
		"a /courses→/courses 1 /direct /   /",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("visits:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	var orphans int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM pageviews WHERE visit_id IS NULL`).Scan(&orphans); err != nil || orphans != 0 {
		t.Fatalf("pageviews without a visit = %d, %v", orphans, err)
	}
}

// Pageviews stored before Change 35 have no visit: they still count as pageviews and visitors.
func TestPageviewsWithoutAVisitStillCount(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)
	site, err := s.CreateSite(ctx, domain.NewSite{Name: "K", Domain: "kodumka.ru"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO pageviews (site_id, ts, path, visitor_hash) VALUES (?, ?, '/', 'old')`, site.ID, now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	stats, err := s.Stats(ctx, site.ID, now.Add(-time.Hour))
	if err != nil || stats.Pageviews != 1 || stats.Visitors != 1 || stats.Visits != 0 {
		t.Fatalf("stats = %+v, %v", stats, err)
	}
	if site.Timezone != domain.DefaultTimezone {
		t.Fatalf("timezone = %q, want the default", site.Timezone)
	}
}
