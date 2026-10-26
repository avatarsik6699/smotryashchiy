package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/analytics/application"
	"github.com/avatarsik6699/smotryashchiy/internal/analytics/domain"
	"github.com/avatarsik6699/smotryashchiy/internal/platform/apierror"
)

// seedMonth stores 30 site days of visits ending the day before now: on day i, i%4+1 visitors, each
// with one or two visits of one to three pageviews from a channel that varies by visitor.
func seedMonth(t *testing.T, s *Store, site domain.Site, now time.Time) {
	t.Helper()
	loc := site.Location()
	channels := []string{"direct", "organic_search", "referral"}
	pages := []string{"/", "/pricing", "/docs/start"}
	for i := 30; i >= 1; i-- {
		dayStart := domain.DayOf(now, loc).AddDays(-i).Start(loc)
		for v := 0; v <= i%4; v++ {
			hash := fmt.Sprintf("h-%d-%d", i, v)
			for visit := 0; visit <= v%2; visit++ {
				start := dayStart.Add(time.Duration(9+v) * time.Hour).Add(time.Duration(visit) * 2 * time.Hour)
				for p := 0; p <= (i+v+visit)%3; p++ {
					err := s.RecordPageview(context.Background(), domain.Pageview{
						SiteID: site.ID, TS: start.Add(time.Duration(p) * time.Minute), Path: pages[(p+v)%3],
						VisitorHash: hash, Browser: "Chrome", OS: "Windows", Device: "desktop",
					}, domain.Visit{Channel: channels[v%3], Country: "RU"})
					if err != nil {
						t.Fatal(err)
					}
				}
			}
		}
	}
}

func newQuerySite(t *testing.T, s *Store, tz string) domain.Site {
	t.Helper()
	site, err := s.CreateSite(context.Background(), domain.NewSite{Name: "A", Domain: "a.example", Timezone: tz}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return site
}

func mustQuery(t *testing.T, q *application.Queries, siteID string, query domain.Query) domain.QueryResult {
	t.Helper()
	res, err := q.Query(context.Background(), siteID, query)
	if err != nil {
		t.Fatalf("Query(%+v): %v", query, err)
	}
	return res
}

var allMetrics = []string{"visitors", "visits", "pageviews", "views_per_visit", "bounce_rate", "visit_duration"}

func TestRawAndAggregatePathsCountAlike(t *testing.T) {
	s := newTestStore(t)
	site := newQuerySite(t, s, "Europe/Moscow")
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	seedMonth(t, s, site, now)
	clock := func() time.Time { return now }
	raw := application.NewQueries(s, clock, 0)
	if err := raw.Rollup(context.Background()); err != nil {
		t.Fatal(err)
	}
	// a 1-day retention leaves only today raw: the month reads the aggregates
	agg := application.NewQueries(s, clock, 24*time.Hour)
	month := []string{"2026-09-26", "2026-10-25"}
	for _, dim := range []string{"", "page", "entry_page", "exit_page", "channel", "country", "time:day", "time:week", "time:month"} {
		q := domain.Query{Metrics: allMetrics, Dimension: dim, DateRange: month}
		a, b := mustQuery(t, raw, site.ID, q), mustQuery(t, agg, site.ID, q)
		if a.Source != application.SourceRaw || b.Source != application.SourceAggregates {
			t.Fatalf("%q sources = %s/%s", dim, a.Source, b.Source)
		}
		if !reflect.DeepEqual(a.Rows, b.Rows) {
			t.Errorf("dimension %q: raw %v\naggregates %v", dim, a.Rows, b.Rows)
		}
	}
	total := mustQuery(t, raw, site.ID, domain.Query{Metrics: []string{"visitors", "pageviews"}, DateRange: month})
	if total.Rows[0].Metrics["visitors"] == 0 || total.Rows[0].Metrics["pageviews"] <= total.Rows[0].Metrics["visitors"] {
		t.Fatalf("synthetic month total = %v", total.Rows[0].Metrics)
	}
	days := mustQuery(t, raw, site.ID, domain.Query{Dimension: "time:day", DateRange: month})
	if len(days.Rows) != 30 || days.Rows[0].Value != "2026-09-26" {
		t.Fatalf("day buckets = %d, first %q", len(days.Rows), days.Rows[0].Value)
	}
}

func TestRangeAcrossTheRawBoundarySumsBothParts(t *testing.T) {
	s := newTestStore(t)
	site := newQuerySite(t, s, "Europe/Moscow")
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	seedMonth(t, s, site, now)
	clock := func() time.Time { return now }
	full := application.NewQueries(s, clock, 0)
	if err := full.Rollup(context.Background()); err != nil {
		t.Fatal(err)
	}
	q := domain.Query{Metrics: allMetrics, Dimension: "channel", DateRange: []string{"2026-09-26", "2026-10-25"}}
	want := mustQuery(t, full, site.ID, q)
	// the 10-day purge drops the older raw rows; their days answer from the aggregates
	if _, err := s.PurgePageviews(context.Background(), now.Add(-10*24*time.Hour), 1<<20); err != nil {
		t.Fatal(err)
	}
	got := mustQuery(t, application.NewQueries(s, clock, 10*24*time.Hour), site.ID, q)
	if got.Source != application.SourceAggregates {
		t.Fatalf("source = %s", got.Source)
	}
	if !reflect.DeepEqual(got.Rows, want.Rows) {
		t.Fatalf("after the purge %v\nbefore %v", got.Rows, want.Rows)
	}
}

func TestFiltersByVisitAndPageDimensions(t *testing.T) {
	s := newTestStore(t)
	site := newQuerySite(t, s, "UTC")
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()
	record := func(hash, path, channel string, at time.Time) {
		t.Helper()
		if err := s.RecordPageview(ctx, domain.Pageview{SiteID: site.ID, TS: at, Path: path, VisitorHash: hash},
			domain.Visit{Channel: channel}); err != nil {
			t.Fatal(err)
		}
	}
	base := time.Date(2026, 10, 26, 8, 0, 0, 0, time.UTC)
	record("a", "/", "direct", base)
	record("a", "/pricing", "direct", base.Add(time.Minute))
	record("b", "/docs", "referral", base)
	record("c", "/pricing", "referral", base)
	q := application.NewQueries(s, func() time.Time { return now }, 90*24*time.Hour)

	byChannel := mustQuery(t, q, site.ID, domain.Query{Metrics: []string{"visits"}, Dimension: "channel",
		Filters: []domain.Filter{{Dimension: "page", Op: "is", Values: []string{"/pricing"}}}})
	if got := rowsOf(byChannel, "visits"); !reflect.DeepEqual(got, map[string]float64{"direct": 1, "referral": 1}) {
		t.Fatalf("visits with /pricing by channel = %v", got)
	}
	byPage := mustQuery(t, q, site.ID, domain.Query{Metrics: []string{"pageviews"}, Dimension: "page",
		Filters: []domain.Filter{{Dimension: "channel", Op: "is_not", Values: []string{"direct"}}}})
	if got := rowsOf(byPage, "pageviews"); !reflect.DeepEqual(got, map[string]float64{"/docs": 1, "/pricing": 1}) {
		t.Fatalf("non-direct pageviews by page = %v", got)
	}
	contains := mustQuery(t, q, site.ID, domain.Query{Metrics: []string{"visitors"},
		Filters: []domain.Filter{{Dimension: "page", Op: "contains", Values: []string{"pri%"}}}})
	if contains.Rows[0].Metrics["visitors"] != 0 {
		t.Fatalf("a %% in contains must match literally, got %v", contains.Rows[0].Metrics)
	}
	contains = mustQuery(t, q, site.ID, domain.Query{Metrics: []string{"visitors"},
		Filters: []domain.Filter{{Dimension: "page", Op: "contains", Values: []string{"pric"}}}})
	if contains.Rows[0].Metrics["visitors"] != 2 {
		t.Fatalf("visitors who saw a page containing pric = %v", contains.Rows[0].Metrics)
	}
}

func rowsOf(res domain.QueryResult, metric string) map[string]float64 {
	out := map[string]float64{}
	for _, r := range res.Rows {
		out[r.Value] = r.Metrics[metric]
	}
	return out
}

func TestHourBucketsFollowTheSiteClock(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for i, c := range []struct {
		tz, day string
		hours   int
	}{
		{"Europe/Moscow", "2026-03-08", 24},
		{"America/New_York", "2026-03-08", 23},
		{"America/New_York", "2026-11-01", 25},
	} {
		site, err := s.CreateSite(ctx, domain.NewSite{Name: c.tz, Domain: fmt.Sprintf("s%d.example", i), Timezone: c.tz}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		now := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
		q := application.NewQueries(s, func() time.Time { return now }, 400*24*time.Hour)
		res := mustQuery(t, q, site.ID, domain.Query{Dimension: "time:hour", DateRange: []string{c.day, c.day}})
		if len(res.Rows) != c.hours {
			t.Errorf("%s %s: %d hour buckets, want %d", c.tz, c.day, len(res.Rows), c.hours)
		}
		seen := map[string]bool{}
		for _, r := range res.Rows {
			if seen[r.Value] {
				t.Errorf("%s %s: repeated hour label %s", c.tz, c.day, r.Value)
			}
			seen[r.Value] = true
		}
		days := mustQuery(t, q, site.ID, domain.Query{Dimension: "time:week", DateRange: []string{"2026-10-28", "2026-11-10"}})
		labels := []string{}
		for _, r := range days.Rows {
			labels = append(labels, r.Value)
		}
		if want := []string{"2026-10-28", "2026-11-02", "2026-11-09"}; !reflect.DeepEqual(labels, want) {
			t.Errorf("%s week labels = %v, want %v", c.tz, labels, want)
		}
	}
}

func TestComparisonPeriods(t *testing.T) {
	s := newTestStore(t)
	site := newQuerySite(t, s, "Europe/Moscow")
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	seedMonth(t, s, site, now)
	q := application.NewQueries(s, func() time.Time { return now }, 0)

	res := mustQuery(t, q, site.ID, domain.Query{Metrics: []string{"visits"}, Period: "7d", Compare: "previous"})
	if res.DateRange != [2]string{"2026-10-20", "2026-10-26"} || *res.CompareRange != [2]string{"2026-10-13", "2026-10-19"} {
		t.Fatalf("ranges = %v / %v", res.DateRange, *res.CompareRange)
	}
	prev := mustQuery(t, q, site.ID, domain.Query{Metrics: []string{"visits"}, DateRange: []string{"2026-10-13", "2026-10-19"}})
	if res.Rows[0].Compare["visits"] != prev.Rows[0].Metrics["visits"] {
		t.Fatalf("compare = %v, want %v", res.Rows[0].Compare, prev.Rows[0].Metrics)
	}
	series := mustQuery(t, q, site.ID, domain.Query{Dimension: "time:day", Period: "7d", Compare: "previous"})
	if series.Rows[0].CompareValue != "2026-10-13" || len(series.Rows) != 7 {
		t.Fatalf("series compare = %+v", series.Rows[0])
	}
	year := mustQuery(t, q, site.ID, domain.Query{Period: "month", Compare: "year_ago"})
	if *year.CompareRange != [2]string{"2025-10-01", "2025-10-26"} || year.Rows[0].Compare["visitors"] != 0 {
		t.Fatalf("year ago = %v %v", *year.CompareRange, year.Rows[0].Compare)
	}
	for period, want := range map[string][2]string{
		"today": {"2026-10-26", "2026-10-26"}, "yesterday": {"2026-10-25", "2026-10-25"},
		"30d": {"2026-09-27", "2026-10-26"}, "last_month": {"2026-09-01", "2026-09-30"},
		"12mo": {"2025-11-01", "2026-10-26"},
	} {
		if got := mustQuery(t, q, site.ID, domain.Query{Period: period}).DateRange; got != want {
			t.Errorf("%s = %v, want %v", period, got, want)
		}
	}
}

func TestQueryValidation(t *testing.T) {
	s := newTestStore(t)
	site := newQuerySite(t, s, "UTC")
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	q := application.NewQueries(s, func() time.Time { return now }, 90*24*time.Hour)
	for name, query := range map[string]domain.Query{
		"unknown metric":        {Metrics: []string{"revenue"}},
		"unknown dimension":     {Dimension: "city"},
		"bad interval":          {Dimension: "time:minute"},
		"bad op":                {Filters: []domain.Filter{{Dimension: "page", Op: "like", Values: []string{"/"}}}},
		"order by other metric": {Metrics: []string{"visits"}, OrderBy: "pageviews"},
		"limit":                 {Limit: 1001},
		"bad period":            {Period: "week"},
		"reversed range":        {DateRange: []string{"2026-10-02", "2026-10-01"}},
		"too long":              {DateRange: []string{"2024-01-01", "2026-10-01"}},
		"hour over days":        {Dimension: "time:hour", Period: "7d"},
		"filters on aggregates": {Period: "12mo", Filters: []domain.Filter{{Dimension: "page", Op: "is", Values: []string{"/"}}}},
		"compare":               {Compare: "last_week"},
	} {
		_, err := q.Query(context.Background(), site.ID, query)
		var apiErr *apierror.Error
		if !errors.As(err, &apiErr) || apiErr.Kind != apierror.KindInvalid {
			t.Errorf("%s: err = %v, want invalid", name, err)
		}
	}
	if _, err := q.Query(context.Background(), "nope", domain.Query{}); err == nil {
		t.Error("unknown site must fail")
	}
}

func TestRollupIsIdempotentAndPurgesOldDays(t *testing.T) {
	s := newTestStore(t)
	site := newQuerySite(t, s, "Europe/Moscow")
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	seedMonth(t, s, site, now)
	ctx := context.Background()
	if _, err := s.db.Exec(`INSERT INTO analytics_daily VALUES (?, '2024-09-25', '', '', 1, 1, 1, 1, 0)`, site.ID); err != nil {
		t.Fatal(err)
	}
	q := application.NewQueries(s, func() time.Time { return now }, 0)
	sums := func() [3]int64 {
		var out [3]int64
		if err := s.db.QueryRow(`SELECT COUNT(*), SUM(visits), SUM(pageviews) FROM analytics_daily`).Scan(&out[0], &out[1], &out[2]); err != nil {
			t.Fatal(err)
		}
		return out
	}
	if err := q.Rollup(ctx); err != nil {
		t.Fatal(err)
	}
	first := sums()
	if err := q.Rollup(ctx); err != nil {
		t.Fatal(err)
	}
	if again := sums(); again != first {
		t.Fatalf("rerun changed the aggregates: %v -> %v", first, again)
	}
	var old int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM analytics_daily WHERE day < '2024-10-01'`).Scan(&old); err != nil || old != 0 {
		t.Fatalf("days older than 25 months = %d (%v)", old, err)
	}
	var days int
	if err := s.db.QueryRow(`SELECT COUNT(DISTINCT day) FROM analytics_daily`).Scan(&days); err != nil || days != 31 {
		t.Fatalf("aggregated days = %d (%v), want the 30 seeded days and today", days, err)
	}
}

func TestRealtimeCountsTheLastFiveMinutes(t *testing.T) {
	s := newTestStore(t)
	site := newQuerySite(t, s, "UTC")
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	for i, ago := range []time.Duration{time.Minute, 4 * time.Minute, 6 * time.Minute} {
		if err := s.RecordPageview(context.Background(), domain.Pageview{SiteID: site.ID, TS: now.Add(-ago), Path: "/",
			VisitorHash: fmt.Sprint("v", i)}, direct); err != nil {
			t.Fatal(err)
		}
	}
	n, err := application.NewQueries(s, func() time.Time { return now }, 0).Realtime(context.Background(), site.ID)
	if err != nil || n != 2 {
		t.Fatalf("realtime = %d (%v), want 2", n, err)
	}
}
