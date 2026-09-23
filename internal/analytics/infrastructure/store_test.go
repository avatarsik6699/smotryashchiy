package infrastructure

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/analytics/domain"
	"github.com/avatarsik6699/smotryashchiy/internal/platform/db"
)

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
	if err := s.InsertPageview(ctx, domain.Pageview{SiteID: site.ID, TS: now, Path: "/", VisitorHash: "h1"}); err != nil {
		t.Fatalf("InsertPageview: %v", err)
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
		if err := s.InsertPageview(ctx, r); err != nil {
			t.Fatalf("InsertPageview: %v", err)
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
		if err := s.InsertPageview(ctx, domain.Pageview{SiteID: site.ID, TS: ts, Path: "/x", VisitorHash: hash}); err != nil {
			t.Fatalf("InsertPageview: %v", err)
		}
	}
	n, err := s.PurgePageviews(ctx, day.Add(24*time.Hour), 100)
	if err != nil {
		t.Fatalf("PurgePageviews: %v", err)
	}
	if n != 3 {
		t.Fatalf("purged %d pageviews, want 3", n)
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
