package http

// Contract tests: the KNOWN_GOTCHAS lessons inherited from sre-kit, exercised end to end through
// real SQLite, the ingest service and the read API.

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	authapp "github.com/avatarsik6699/smotryashchiy/internal/auth/application"
	authinfra "github.com/avatarsik6699/smotryashchiy/internal/auth/infrastructure"
	authhttp "github.com/avatarsik6699/smotryashchiy/internal/auth/interfaces/http"
	"github.com/avatarsik6699/smotryashchiy/internal/platform/apierror"
	"github.com/avatarsik6699/smotryashchiy/internal/platform/db"
	"github.com/avatarsik6699/smotryashchiy/internal/telemetry/application"
	"github.com/avatarsik6699/smotryashchiy/internal/telemetry/domain"
	"github.com/avatarsik6699/smotryashchiy/internal/telemetry/infrastructure"
)

var clock = time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

type env struct {
	rawDB   *sql.DB
	store   *infrastructure.Store
	svc     *application.Service
	hub     *application.Hub
	handler http.Handler
	host    domain.Host
	cookie  *http.Cookie
}

func newEnv(t *testing.T) *env {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Migrate(sqlDB); err != nil {
		t.Fatal(err)
	}
	store := infrastructure.NewStore(sqlDB)
	hub := application.NewHub()
	svc := application.NewServiceWithClock(store, func() time.Time { return clock }).WithPublisher(hub)
	host, err := store.CreateHost(context.Background(), "web-1", clock)
	if err != nil {
		t.Fatal(err)
	}

	auth := authapp.NewService(authinfra.NewSettingsStore(sqlDB))
	const password = "correct-horse-battery-staple-1"
	if err := auth.SetAdminPassword(context.Background(), password); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	authhttp.NewHandlers(auth, authhttp.Options{}).Register(mux)
	NewHandlers(svc).Register(mux)
	NewStreamHandlers(hub).Register(mux)
	handler := authhttp.RequireSession(auth)(mux)

	login := httptest.NewRecorder()
	handler.ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"password":"`+password+`"}`)))
	if login.Code != http.StatusNoContent {
		t.Fatalf("login = %d", login.Code)
	}
	return &env{rawDB: sqlDB, store: store, svc: svc, hub: hub, handler: handler, host: host, cookie: login.Result().Cookies()[0]}
}

func (e *env) get(t *testing.T, target string, authed bool) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if authed {
		req.AddCookie(e.cookie)
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func (e *env) ingest(key string, b domain.Batch) (application.StoreResult, error) {
	return e.svc.Ingest(context.Background(), e.host.ID, key, b)
}

func metric(name string, ts time.Time, v float64, labels map[string]string) domain.Metric {
	return domain.Metric{Name: name, TS: ts, Value: v, Labels: labels}
}

func batch(ms ...domain.Metric) domain.Batch {
	return domain.Batch{SchemaVersion: "1.0", Metrics: ms}
}

func TestZeroMetricRoundTripsThroughReadAPI(t *testing.T) {
	e := newEnv(t)
	if _, err := e.ingest("k1", batch(metric("container.cpu_percent", clock, 0, map[string]string{"container": "idle"}))); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"/api/metrics", "/api/metrics?latest=true"} {
		code, body := e.get(t, target, true)
		if code != 200 || !strings.Contains(body, `"value":0`) {
			t.Fatalf("%s: %d %s", target, code, body)
		}
	}
}

func TestFutureTimestampRejectsWholeBatchAtomically(t *testing.T) {
	e := newEnv(t)
	bad := batch(metric("a.b", clock, 1, nil), metric("a.c", clock.Add(time.Hour), 2, nil))
	_, err := e.ingest("k1", bad)
	if apiErr, ok := err.(*apierror.Error); !ok || apiErr.Kind != apierror.KindInvalid {
		t.Fatalf("err = %v", err)
	}
	if _, body := e.get(t, "/api/metrics", true); !strings.Contains(body, `"metrics":[]`) {
		t.Fatalf("partial data stored: %s", body)
	}
	host, _ := e.store.Host(context.Background(), e.host.ID)
	if host.LastSeenAt != nil {
		t.Fatal("rejected batch must not mark the host as seen")
	}
	// The rejected batch did not consume its idempotency key.
	res, err := e.ingest("k1", batch(metric("a.b", clock, 1, nil)))
	if err != nil || res.Replayed || len(res.Accepted.Metrics) != 1 {
		t.Fatalf("retry after rejection: %+v %v", res, err)
	}
}

func TestStoreFailureMidBatchRollsBackEverything(t *testing.T) {
	e := newEnv(t)
	// Bypass Normalize to make the check insert violate the table constraint after metrics stored.
	corrupt := domain.Batch{
		SchemaVersion: "1.0",
		Metrics:       []domain.Metric{metric("a.b", clock, 1, nil)},
		Checks:        []domain.Check{{Name: "c.d", TS: clock, Status: "bogus", Meta: json.RawMessage("{}")}},
	}
	if _, err := e.store.Store(context.Background(), e.host.ID, "k1", clock, corrupt); err == nil {
		t.Fatal("expected constraint failure")
	}
	if _, body := e.get(t, "/api/metrics", true); !strings.Contains(body, `"metrics":[]`) {
		t.Fatalf("metric survived a failed batch: %s", body)
	}
	res, err := e.ingest("k1", batch(metric("a.b", clock, 1, nil)))
	if err != nil || res.Replayed {
		t.Fatalf("key consumed by a failed batch: %+v %v", res, err)
	}
}

func TestReplayByKeyIsNoOp(t *testing.T) {
	e := newEnv(t)
	b := batch(metric("a.b", clock, 1, nil))
	first, err := e.ingest("k1", b)
	if err != nil || first.Replayed || len(first.Accepted.Metrics) != 1 {
		t.Fatalf("first: %+v %v", first, err)
	}
	second, err := e.ingest("k1", b)
	if err != nil || !second.Replayed || second.Accepted.Len() != 0 {
		t.Fatalf("replay must be a no-op: %+v %v", second, err)
	}
	if _, body := e.get(t, "/api/metrics", true); strings.Count(body, `"name":"a.b"`) != 1 {
		t.Fatalf("stored more than once: %s", body)
	}
}

func TestOverlapByContentIsStoredOnceAndReportedAsDuplicate(t *testing.T) {
	e := newEnv(t)
	labels := map[string]string{"b": "2", "a": "1"}
	if _, err := e.ingest("k1", batch(metric("a.b", clock, 1, labels))); err != nil {
		t.Fatal(err)
	}
	overlap := batch(
		metric("a.b", clock, 1, map[string]string{"a": "1", "b": "2"}), // same identity, other label order
		metric("a.b", clock.Add(time.Second), 2, labels),               // genuinely new
		metric("a.b", clock.Add(time.Second), 2, labels),               // repeated inside the batch
	)
	res, err := e.ingest("k2", overlap)
	if err != nil {
		t.Fatal(err)
	}
	if res.Replayed || len(res.Accepted.Metrics) != 1 || res.Duplicates.Metrics != 2 {
		t.Fatalf("accepted=%d duplicates=%d replayed=%v", len(res.Accepted.Metrics), res.Duplicates.Metrics, res.Replayed)
	}
	if !res.Accepted.Metrics[0].TS.Equal(clock.Add(time.Second)) {
		t.Fatal("Accepted must contain only the new record")
	}
}

func TestUnknownHostIsNotFound(t *testing.T) {
	e := newEnv(t)
	_, err := e.svc.Ingest(context.Background(), "no-such-host", "k1", batch(metric("a.b", clock, 1, nil)))
	if apiErr, ok := err.(*apierror.Error); !ok || apiErr.Kind != apierror.KindNotFound {
		t.Fatalf("err = %v", err)
	}
}

// Unknown must stay unknown: no data is an empty list, never a zero.
func TestNoDataIsDistinguishableFromZero(t *testing.T) {
	e := newEnv(t)
	for path, key := range map[string]string{"/api/metrics": "metrics", "/api/checks": "checks", "/api/events": "events"} {
		code, body := e.get(t, path, true)
		if code != 200 || !strings.Contains(body, `"`+key+`":[]`) {
			t.Fatalf("%s: %d %s", path, code, body)
		}
	}
	host, _ := e.store.Host(context.Background(), e.host.ID)
	if host.LastSeenAt != nil {
		t.Fatal("a host that never reported must have no last_seen_at")
	}
	// An empty batch is a valid heartbeat and marks the host seen at receipt time.
	if _, err := e.ingest("hb", domain.Batch{SchemaVersion: "1.0"}); err != nil {
		t.Fatal(err)
	}
	host, _ = e.store.Host(context.Background(), e.host.ID)
	if host.LastSeenAt == nil || !host.LastSeenAt.Equal(clock) {
		t.Fatalf("last_seen_at = %v, want %v", host.LastSeenAt, clock)
	}
}

func TestLastSeenUsesReceiptTimeNotProducerTime(t *testing.T) {
	e := newEnv(t)
	old := clock.Add(-48 * time.Hour)
	if _, err := e.ingest("k1", batch(metric("a.b", old, 1, nil))); err != nil {
		t.Fatal(err)
	}
	host, _ := e.store.Host(context.Background(), e.host.ID)
	if host.LastSeenAt == nil || !host.LastSeenAt.Equal(clock) {
		t.Fatalf("last_seen_at = %v, want receipt time %v", host.LastSeenAt, clock)
	}
}

func TestMetricRangeLatestAndLimits(t *testing.T) {
	e := newEnv(t)
	var ms []domain.Metric
	for i := 0; i < 5; i++ {
		ms = append(ms, metric("cpu.usage_percent", clock.Add(-time.Duration(4-i)*time.Minute), float64(i), map[string]string{"core": "0"}))
	}
	ms = append(ms, metric("cpu.usage_percent", clock, 9, map[string]string{"core": "1"}))
	if _, err := e.ingest("k1", batch(ms...)); err != nil {
		t.Fatal(err)
	}
	type resp struct {
		Metrics []struct {
			Value  float64           `json:"value"`
			Labels map[string]string `json:"labels"`
			TS     time.Time         `json:"ts"`
		} `json:"metrics"`
	}
	decode := func(target string) resp {
		code, body := e.get(t, target, true)
		if code != 200 {
			t.Fatalf("%s = %d %s", target, code, body)
		}
		var r resp
		if err := json.Unmarshal([]byte(body), &r); err != nil {
			t.Fatal(err)
		}
		return r
	}
	if r := decode("/api/metrics?name=cpu.usage_percent"); len(r.Metrics) != 6 || r.Metrics[0].Value != 0 {
		t.Fatalf("ascending order/all: %+v", r.Metrics)
	}
	from := clock.Add(-2 * time.Minute).Format(time.RFC3339)
	to := clock.Add(-time.Minute).Format(time.RFC3339)
	if r := decode("/api/metrics?from=" + from + "&to=" + to + "&limit=10"); len(r.Metrics) != 2 {
		t.Fatalf("range: %+v", r.Metrics)
	}
	if r := decode("/api/metrics?limit=3"); len(r.Metrics) != 3 {
		t.Fatalf("limit: %+v", r.Metrics)
	}
	latest := decode("/api/metrics?latest=true")
	if len(latest.Metrics) != 2 { // newest point per (name, labels): core 0 and core 1
		t.Fatalf("latest per series: %+v", latest.Metrics)
	}
	for _, m := range latest.Metrics {
		if m.Labels["core"] == "0" && m.Value != 4 {
			t.Fatalf("latest core 0 = %v, want 4", m.Value)
		}
	}
	if r := decode("/api/metrics?host=" + e.host.ID + "x"); len(r.Metrics) != 0 {
		t.Fatal("unknown host filter must return nothing")
	}
}

func TestMetricQueryValidation(t *testing.T) {
	e := newEnv(t)
	from := clock.Format(time.RFC3339)
	for _, target := range []string{
		"/api/metrics?latest=true&from=" + from,
		"/api/metrics?latest=maybe",
		"/api/metrics?from=yesterday",
		"/api/metrics?limit=5001",
		"/api/metrics?limit=-1",
		"/api/metrics?limit=abc",
		"/api/metrics?from=" + clock.Add(time.Hour).Format(time.RFC3339) + "&to=" + from,
		"/api/events?limit=501",
		"/api/events?level=debug",
	} {
		if code, body := e.get(t, target, true); code != http.StatusBadRequest {
			t.Errorf("%s = %d %s", target, code, body)
		}
	}
}

func TestChecksReturnNewestPerName(t *testing.T) {
	e := newEnv(t)
	b := domain.Batch{SchemaVersion: "1.0", Checks: []domain.Check{
		{Name: "disk.root", TS: clock.Add(-time.Minute), Status: domain.StatusOK},
		{Name: "disk.root", TS: clock, Status: domain.StatusCritical, Meta: json.RawMessage(`{"used":97}`)},
		{Name: "tls.expiry", TS: clock, Status: domain.StatusWarn},
	}}
	if _, err := e.ingest("k1", b); err != nil {
		t.Fatal(err)
	}
	_, body := e.get(t, "/api/checks", true)
	var r struct {
		Checks []struct{ Name, Status string } `json:"checks"`
	}
	if err := json.Unmarshal([]byte(body), &r); err != nil || len(r.Checks) != 2 {
		t.Fatalf("%v %s", err, body)
	}
	if r.Checks[0].Name != "disk.root" || r.Checks[0].Status != "critical" || !strings.Contains(body, `"used":97`) {
		t.Fatalf("newest check per name: %s", body)
	}
	if _, body := e.get(t, "/api/checks?name=tls.expiry", true); strings.Contains(body, "disk.root") {
		t.Fatalf("name filter ignored: %s", body)
	}
}

func TestEventsNewestFirstWithLevelFilter(t *testing.T) {
	e := newEnv(t)
	b := domain.Batch{SchemaVersion: "1.0", Events: []domain.Event{
		{TS: clock.Add(-2 * time.Minute), Level: domain.LevelInfo, Message: "first"},
		{TS: clock, Level: domain.LevelError, Message: "third", Labels: map[string]string{"jail": "sshd"}},
		{TS: clock.Add(-time.Minute), Level: domain.LevelWarn, Message: "second"},
	}}
	if _, err := e.ingest("k1", b); err != nil {
		t.Fatal(err)
	}
	_, body := e.get(t, "/api/events", true)
	if strings.Index(body, "third") > strings.Index(body, "second") || strings.Index(body, "second") > strings.Index(body, "first") {
		t.Fatalf("not newest first: %s", body)
	}
	if _, body := e.get(t, "/api/events?level=error&limit=1", true); !strings.Contains(body, "third") || strings.Contains(body, "second") {
		t.Fatalf("level filter: %s", body)
	}
}

func TestReadEndpointsRequireSession(t *testing.T) {
	e := newEnv(t)
	for _, path := range []string{"/api/metrics", "/api/checks", "/api/events"} {
		if code, _ := e.get(t, path, false); code != http.StatusUnauthorized {
			t.Errorf("%s without session = %d", path, code)
		}
	}
}
