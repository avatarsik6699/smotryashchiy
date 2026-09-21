package http

// Contract tests for rollups, retention and the hourly read resolution (docs/SPEC.md §4.4–§4.5).

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/telemetry/application"
	"github.com/avatarsik6699/smotryashchiy/internal/telemetry/domain"
)

var hour10 = time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)

func (e *env) maintenance(now time.Time) *application.Maintenance {
	return application.NewMaintenance(e.store, 30, 396, func() time.Time { return now })
}

type rollupBody struct {
	Rollups []struct {
		TS    time.Time `json:"ts"`
		Count int64     `json:"count"`
		Min   float64   `json:"min"`
		Max   float64   `json:"max"`
		Avg   float64   `json:"avg"`
	} `json:"rollups"`
}

func (e *env) rollups(t *testing.T, query string) rollupBody {
	t.Helper()
	code, body := e.get(t, "/api/metrics?resolution=hour"+query, true)
	if code != 200 {
		t.Fatalf("rollups: %d %s", code, body)
	}
	var out rollupBody
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// normalize validates b as if received at `at`, so tests can store historical data.
func normalize(t *testing.T, b domain.Batch, at time.Time) domain.Batch {
	t.Helper()
	n, err := b.Normalize(at)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func (e *env) count(t *testing.T, table string) int {
	t.Helper()
	var n int
	if err := e.rawDB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRollupOfZeroOnlyHourKeepsCountAndZeroStats(t *testing.T) {
	e := newEnv(t)
	if _, err := e.ingest("k1", batch(
		metric("container.cpu_percent", hour10.Add(time.Minute), 0, nil),
		metric("container.cpu_percent", hour10.Add(2*time.Minute), 0, nil))); err != nil {
		t.Fatal(err)
	}
	if err := e.maintenance(hour10.Add(2 * time.Hour)).Rollup(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := e.rollups(t, "")
	if len(got.Rollups) != 1 {
		t.Fatalf("rollups = %+v", got)
	}
	r := got.Rollups[0]
	if !r.TS.Equal(hour10) || r.Count != 2 || r.Min != 0 || r.Max != 0 || r.Avg != 0 {
		t.Fatalf("zero-only hour = %+v, want count 2 and all stats 0", r)
	}
}

func TestRollupAggregatesAndOpenHourIsNotRolled(t *testing.T) {
	e := newEnv(t)
	if _, err := e.ingest("k1", batch(
		metric("cpu.usage_percent", hour10.Add(time.Minute), 10, nil),
		metric("cpu.usage_percent", hour10.Add(2*time.Minute), 30, nil),
		metric("cpu.usage_percent", hour10.Add(time.Hour+time.Minute), 99, nil))); err != nil {
		t.Fatal(err)
	}
	// "now" is inside the 11:00 hour, so only the 10:00 hour is closed.
	if err := e.maintenance(hour10.Add(time.Hour + 30*time.Minute)).Rollup(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := e.rollups(t, "")
	if len(got.Rollups) != 1 {
		t.Fatalf("rollups = %+v", got)
	}
	if r := got.Rollups[0]; r.Count != 2 || r.Min != 10 || r.Max != 30 || r.Avg != 20 {
		t.Fatalf("hour 10 = %+v", r)
	}
}

func TestRollupRerunIsIdempotentAndPicksUpLateData(t *testing.T) {
	e := newEnv(t)
	if _, err := e.ingest("k1", batch(metric("cpu.usage_percent", hour10.Add(time.Minute), 10, nil))); err != nil {
		t.Fatal(err)
	}
	now := hour10.Add(2 * time.Hour)
	m := e.maintenance(now)
	for i := 0; i < 2; i++ {
		if err := m.Rollup(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if got := e.rollups(t, ""); len(got.Rollups) != 1 || got.Rollups[0].Count != 1 {
		t.Fatalf("after re-run: %+v", got)
	}
	// A late sample for the already rolled hour arrives, then the next run absorbs it.
	if _, err := e.ingest("k2", batch(metric("cpu.usage_percent", hour10.Add(5*time.Minute), 20, nil))); err != nil {
		t.Fatal(err)
	}
	if err := e.maintenance(now.Add(time.Hour)).Rollup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := e.rollups(t, ""); len(got.Rollups) != 1 || got.Rollups[0].Count != 2 || got.Rollups[0].Avg != 15 {
		t.Fatalf("late data not absorbed: %+v", got)
	}
}

func TestRollupWithoutRawDataStoresNothingAndDistinguishesEmpty(t *testing.T) {
	e := newEnv(t)
	if err := e.maintenance(hour10).Rollup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := e.rollups(t, ""); len(got.Rollups) != 0 {
		t.Fatalf("expected no rollups, got %+v", got)
	}
}

func TestHourResolutionRejectsLatestAndUnknownResolution(t *testing.T) {
	e := newEnv(t)
	for _, target := range []string{
		"/api/metrics?resolution=hour&latest=true",
		"/api/metrics?resolution=minute",
		"/api/metrics?resolution=hour&limit=99999",
	} {
		if code, body := e.get(t, target, true); code != 400 {
			t.Fatalf("%s = %d %s, want 400", target, code, body)
		}
	}
}

func TestPurgeKeepsUnrolledRawRowsAndRemovesExpiredOnes(t *testing.T) {
	e := newEnv(t)
	now := hour10.Add(45 * 24 * time.Hour)
	old := now.Add(-40 * 24 * time.Hour) // beyond the 30-day raw TTL
	// Old data is ingested with a matching old clock, so it passes the future-timestamp rule.
	if _, err := e.store.Store(context.Background(), e.host.ID, "old", old, normalize(t, batch(metric("a.b", old, 5, nil)), old)); err != nil {
		t.Fatal(err)
	}
	m := e.maintenance(now)

	// Nothing has been rolled up yet, so the expired raw row must survive the purge.
	if err := m.Purge(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, "metrics"); n != 1 {
		t.Fatalf("raw metric purged before its hour was rolled up (rows = %d)", n)
	}

	if err := m.Rollup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.Purge(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, "metrics"); n != 0 {
		t.Fatalf("expired raw metric survived purge (rows = %d)", n)
	}
	if n := e.count(t, "ingestion_batches"); n != 0 {
		t.Fatalf("expired ingestion batch survived purge (rows = %d)", n)
	}
	// The 40-day-old hour is still inside the 13-month rollup TTL and stays readable.
	if got := e.rollups(t, ""); len(got.Rollups) != 1 || got.Rollups[0].Avg != 5 {
		t.Fatalf("rollup lost with raw data: %+v", got)
	}
}

func TestPurgeNeverTouchesFreshRawOrRecentUnrolledWindow(t *testing.T) {
	e := newEnv(t)
	if _, err := e.ingest("k1", batch(metric("a.b", clock, 1, nil))); err != nil {
		t.Fatal(err)
	}
	m := e.maintenance(clock.Add(2 * time.Hour))
	if err := m.Rollup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.Purge(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, "metrics"); n != 1 {
		t.Fatalf("fresh raw metric purged (rows = %d)", n)
	}
}

func TestRollupsExpireAfterRollupTTL(t *testing.T) {
	e := newEnv(t)
	old := hour10
	if _, err := e.store.Store(context.Background(), e.host.ID, "old", old, normalize(t, batch(metric("a.b", old, 5, nil)), old)); err != nil {
		t.Fatal(err)
	}
	if err := e.maintenance(old.Add(2 * time.Hour)).Rollup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := e.maintenance(old.Add(400 * 24 * time.Hour)).Purge(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, "metric_rollups_hourly"); n != 0 {
		t.Fatalf("rollup older than 13 months survived (rows = %d)", n)
	}
}

func TestPurgeIsBoundedPerStatement(t *testing.T) {
	e := newEnv(t)
	old := hour10
	ms := make([]domain.Metric, 0, 12)
	for i := 0; i < 12; i++ {
		ms = append(ms, metric("a.b", old.Add(time.Duration(i)*time.Second), float64(i), nil))
	}
	if _, err := e.store.Store(context.Background(), e.host.ID, "old", old, normalize(t, batch(ms...), old)); err != nil {
		t.Fatal(err)
	}
	n, err := e.store.PurgeRaw(context.Background(), old.Add(time.Hour), 5)
	if err != nil || n != 6 { // 5 metrics + the ingestion batch
		t.Fatalf("PurgeRaw = %d, %v; want 6 (bounded to 5 metrics + 1 batch)", n, err)
	}
	if left := e.count(t, "metrics"); left != 7 {
		t.Fatalf("metrics left = %d, want 7", left)
	}
}
