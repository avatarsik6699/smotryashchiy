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
	if n := countRows(t, s, "pageviews"); n != 0 {
		t.Fatalf("%d pageviews survived site deletion", n)
	}
	if err := s.DeleteSite(ctx, site.ID); err == nil {
		t.Fatal("want a not-found error deleting an already-deleted site")
	}
}

func TestRawRowsCountTheWindowByPage(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now()
	site, err := s.CreateSite(ctx, domain.NewSite{Name: "A", Domain: "a.example"}, now)
	if err != nil {
		t.Fatalf("CreateSite: %v", err)
	}
	rows := []domain.Pageview{
		{SiteID: site.ID, TS: now, Path: "/home", VisitorHash: "v1"},
		{SiteID: site.ID, TS: now, Path: "/home", VisitorHash: "v2"},
		{SiteID: site.ID, TS: now, Path: "/about", VisitorHash: "v1"},
		{SiteID: site.ID, TS: now.Add(-48 * time.Hour), Path: "/old", VisitorHash: "v3"}, // outside the window below
	}
	for _, r := range rows {
		if err := s.RecordPageview(ctx, r, direct); err != nil {
			t.Fatalf("RecordPageview: %v", err)
		}
	}
	from, to := now.Add(-24*time.Hour), now.Add(time.Minute)
	if got := total(t, s, site.ID, from, to); got.Pageviews != 3 || got.Visitors != 2 || got.Visits != 2 {
		t.Fatalf("total = %+v, want 3 pageviews of 2 visitors in 2 visits", got)
	}
	pages, err := s.RawRows(ctx, site.ID, domain.DimensionPage, from, to, nil)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]domain.Counts{}
	for _, r := range pages {
		byPath[r.Value] = r.Counts
	}
	if byPath["/home"].Pageviews != 2 || byPath["/home"].Visitors != 2 || byPath["/about"].Visits != 1 || len(byPath) != 2 {
		t.Fatalf("pages = %+v", byPath)
	}
}

// total is the raw total of [from, to).
func total(t *testing.T, s *Store, siteID string, from, to time.Time) domain.Counts {
	t.Helper()
	rows, err := s.RawRows(context.Background(), siteID, "", from, to, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		return domain.Counts{}
	}
	return rows[0].Counts
}

func countRows(t *testing.T, s *Store, table string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
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
	if n := countRows(t, s, "pageviews") + countRows(t, s, "visits"); n != 0 {
		t.Fatalf("%d raw rows survived the purge", n)
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
	// two of three visits have one page; the durations are 600 + 0 + 0 s
	got := total(t, s, site.ID, start, start.Add(time.Hour))
	if want := (domain.Counts{Visitors: 2, Visits: 3, Pageviews: 4, Bounces: 2, DurationMS: 600_000}); got != want {
		t.Fatalf("total = %+v, want %+v", got, want)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT visitor_hash, entry_path, exit_path, pageviews, source, channel, country,
		region, screen, utm_source, browser_version, os_version FROM visits ORDER BY started_at`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var visits []string
	for rows.Next() {
		var hash, entry, exit, source, channel, country, region, screen, utm, bv, ov string
		var n int
		if err := rows.Scan(&hash, &entry, &exit, &n, &source, &channel, &country, &region, &screen, &utm, &bv, &ov); err != nil {
			t.Fatal(err)
		}
		visits = append(visits, fmt.Sprintf("%s %s→%s %d %s/%s %s/%s %s %s %s/%s", hash, entry, exit, n, source, channel, country, region, screen, utm, bv, ov))
	}
	want := []string{
		"a /ege→/ege/16 2 Яндекс/organic_search RU/Moscow mobile vk 131/14",
		"b /→/ 1 /direct /   /",
		"a /courses→/courses 1 /direct /   /",
	}
	if strings.Join(visits, "\n") != strings.Join(want, "\n") {
		t.Fatalf("visits:\n%s\nwant:\n%s", strings.Join(visits, "\n"), strings.Join(want, "\n"))
	}
	var orphans int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM pageviews WHERE visit_id IS NULL`).Scan(&orphans); err != nil || orphans != 0 {
		t.Fatalf("pageviews without a visit = %d, %v", orphans, err)
	}
}

// Pageviews stored before Change 35 have no visit. The query API counts visits, so they stay out of
// it (the dashboard starts with the release of Change 35) until the raw purge removes them.
func TestPageviewsWithoutAVisitStayOutOfQueries(t *testing.T) {
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
	if got := total(t, s, site.ID, now.Add(-time.Hour), now.Add(time.Hour)); got != (domain.Counts{}) {
		t.Fatalf("total = %+v, want nothing", got)
	}
	if n, err := s.PurgePageviews(ctx, now.Add(time.Hour), 10); err != nil || n != 1 {
		t.Fatalf("purge = %d (%v), want the old pageview gone", n, err)
	}
	if site.Timezone != domain.DefaultTimezone {
		t.Fatalf("timezone = %q, want the default", site.Timezone)
	}
}
