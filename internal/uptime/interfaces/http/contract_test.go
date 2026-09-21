package http

// Contract tests of the uptime prober end to end: real SQLite, the scheduler probing real local
// servers, the session-gated API and the live stream (docs/SPEC.md §4e).

import (
	"context"
	"database/sql"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	authapp "github.com/avatarsik6699/smotryashchiy/internal/auth/application"
	authinfra "github.com/avatarsik6699/smotryashchiy/internal/auth/infrastructure"
	authhttp "github.com/avatarsik6699/smotryashchiy/internal/auth/interfaces/http"
	"github.com/avatarsik6699/smotryashchiy/internal/platform/db"
	telemetryapp "github.com/avatarsik6699/smotryashchiy/internal/telemetry/application"
	telemetryhttp "github.com/avatarsik6699/smotryashchiy/internal/telemetry/interfaces/http"
	"github.com/avatarsik6699/smotryashchiy/internal/uptime/application"
	"github.com/avatarsik6699/smotryashchiy/internal/uptime/domain"
	"github.com/avatarsik6699/smotryashchiy/internal/uptime/infrastructure"
)

type hubPublisher struct{ hub *telemetryapp.Hub }

func (p hubPublisher) PublishResult(r domain.Result) {
	p.hub.Publish([]telemetryapp.Message{{Type: telemetryapp.TypeUptime, TargetID: r.TargetID, Payload: r}})
}

type env struct {
	db      *sql.DB
	store   *infrastructure.Store
	svc     *application.Service
	hub     *telemetryapp.Hub
	handler http.Handler
	cookie  *http.Cookie
	srv     *httptest.Server
	stop    context.CancelFunc
	done    chan struct{}
}

type envOptions struct {
	interval  time.Duration
	first     time.Duration
	retention time.Duration
	seed      func(*infrastructure.Store)
	checker   func(*application.Checker)
}

func newEnv(t *testing.T, o envOptions) *env {
	t.Helper()
	if o.interval == 0 {
		o.interval = 80 * time.Millisecond
	}
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "u.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Migrate(sqlDB); err != nil {
		t.Fatal(err)
	}
	store := infrastructure.NewStore(sqlDB)
	if o.seed != nil {
		o.seed(store)
	}
	hub := telemetryapp.NewHub()
	checker := application.NewChecker()
	checker.Timeout = time.Second
	if o.checker != nil {
		o.checker(checker)
	}
	svc := application.NewService(store, checker, hubPublisher{hub}, time.Now, application.Options{
		IntervalFor:   func(domain.Target) time.Duration { return o.interval },
		FirstRunDelay: func(domain.Target) time.Duration { return o.first },
		Retention:     o.retention,
	})

	auth := authapp.NewService(authinfra.NewSettingsStore(sqlDB))
	const password = "correct-horse-battery-staple-1"
	if err := auth.SetAdminPassword(context.Background(), password); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	authhttp.NewHandlers(auth, authhttp.Options{}).Register(mux)
	NewHandlers(svc).Register(mux)
	telemetryhttp.NewStreamHandlers(hub).Register(mux)
	handler := authhttp.RequireSession(auth)(mux)
	login := httptest.NewRecorder()
	handler.ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"password":"`+password+`"}`)))
	if login.Code != http.StatusNoContent {
		t.Fatalf("login = %d", login.Code)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); svc.Run(ctx) }()
	e := &env{db: sqlDB, store: store, svc: svc, hub: hub, handler: handler, cookie: login.Result().Cookies()[0], stop: cancel, done: done}
	e.srv = httptest.NewServer(handler)
	t.Cleanup(func() {
		e.srv.Close()
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("scheduler did not stop")
		}
	})
	return e
}

func (e *env) do(t *testing.T, method, target, body string, authed bool) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if authed {
		req.AddCookie(e.cookie)
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

type resultJSON struct {
	OK            bool       `json:"ok"`
	LatencyMS     *int64     `json:"latency_ms"`
	StatusCode    *int       `json:"status_code"`
	Error         string     `json:"error"`
	CertExpiresAt *time.Time `json:"cert_expires_at"`
}

type targetJSON struct {
	ID              string           `json:"id"`
	Name            string           `json:"name"`
	Kind            string           `json:"kind"`
	Target          string           `json:"target"`
	IntervalSeconds int              `json:"interval_seconds"`
	Last            *resultJSON      `json:"last"`
	Latency         [][]*json.Number `json:"latency"`
}

func (e *env) create(t *testing.T, name, kind, target string) targetJSON {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"name": name, "kind": kind, "target": target})
	code, resp := e.do(t, http.MethodPost, "/api/uptime", string(body), true)
	if code != http.StatusCreated {
		t.Fatalf("create %s: %d %s", name, code, resp)
	}
	var out targetJSON
	if err := json.Unmarshal([]byte(resp), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func (e *env) targets(t *testing.T) []targetJSON {
	t.Helper()
	code, body := e.do(t, http.MethodGet, "/api/uptime", "", true)
	if code != 200 {
		t.Fatalf("list: %d %s", code, body)
	}
	var out struct {
		Targets []targetJSON `json:"targets"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	return out.Targets
}

func waitUntil(t *testing.T, limit time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func find(ts []targetJSON, name string) *targetJSON {
	for i := range ts {
		if ts[i].Name == name {
			return &ts[i]
		}
	}
	return nil
}

func TestUpAndDownTargetsAreProbedAndReportedHonestly(t *testing.T) {
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	defer good.Close()
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	closedAddr := ln.Addr().String()
	ln.Close()

	e := newEnv(t, envOptions{})
	e.create(t, "site", "http", good.URL)
	e.create(t, "dead", "tcp", closedAddr)

	waitUntil(t, 5*time.Second, "both targets to have a result", func() bool {
		ts := e.targets(t)
		s, d := find(ts, "site"), find(ts, "dead")
		return s != nil && s.Last != nil && d != nil && d.Last != nil
	})
	ts := e.targets(t)
	s, d := find(ts, "site"), find(ts, "dead")
	if !s.Last.OK || s.Last.LatencyMS == nil || s.Last.StatusCode == nil || *s.Last.StatusCode != 200 || s.Last.Error != "" {
		t.Fatalf("site = %+v", s.Last)
	}
	if d.Last.OK || d.Last.LatencyMS != nil || d.Last.Error == "" {
		t.Fatalf("a dead target must have no latency (not 0) and an error text: %+v", d.Last)
	}
	// A failed check appears in the history as a null point, so the gap stays visible.
	waitUntil(t, 3*time.Second, "history points", func() bool { d = find(e.targets(t), "dead"); return len(d.Latency) >= 1 })
	if d.Latency[0][1] != nil {
		t.Fatalf("failed check history point = %v, want [ts,null]", d.Latency[0])
	}
}

func TestATargetThatRecoversFlipsFromDownToUpWithoutRestart(t *testing.T) {
	var broken atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if broken.Load() {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()
	e := newEnv(t, envOptions{})
	e.create(t, "flaky", "http", srv.URL)
	state := func() *resultJSON { return find(e.targets(t), "flaky").Last }
	waitUntil(t, 5*time.Second, "UP", func() bool { l := state(); return l != nil && l.OK })
	broken.Store(true)
	waitUntil(t, 5*time.Second, "DOWN", func() bool { l := state(); return l != nil && !l.OK })
	if l := state(); l.Error != "unexpected status 500" || l.StatusCode == nil || *l.StatusCode != 500 {
		t.Fatalf("down result = %+v", l)
	}
	broken.Store(false)
	waitUntil(t, 5*time.Second, "UP again", func() bool { l := state(); return l != nil && l.OK })
}

func TestValidationDuplicatesAndTheTargetLimit(t *testing.T) {
	e := newEnv(t, envOptions{interval: time.Hour, first: time.Hour})
	for name, body := range map[string]string{
		"blank name":     `{"name":" ","kind":"tcp","target":"a:1"}`,
		"bad kind":       `{"name":"x","kind":"icmp","target":"a:1"}`,
		"bad url":        `{"name":"x","kind":"http","target":"example.com"}`,
		"credentials":    `{"name":"x","kind":"http","target":"https://u:p@example.com"}`,
		"bad hostport":   `{"name":"x","kind":"tcp","target":"nohost"}`,
		"short interval": `{"name":"x","kind":"tcp","target":"a:1","interval_seconds":5}`,
		"malformed":      `{`,
	} {
		if code, resp := e.do(t, http.MethodPost, "/api/uptime", body, true); code != 400 {
			t.Errorf("%s: %d %s, want 400", name, code, resp)
		}
	}
	e.create(t, "dup", "tcp", "a.example:1")
	if code, _ := e.do(t, http.MethodPost, "/api/uptime", `{"name":"dup","kind":"tcp","target":"b.example:1"}`, true); code != 409 {
		t.Errorf("duplicate name = %d, want 409", code)
	}
	for i := 1; i < domain.MaxTargets; i++ {
		e.create(t, "t"+strings.Repeat("x", i), "tcp", "a.example:1")
	}
	if code, resp := e.do(t, http.MethodPost, "/api/uptime", `{"name":"one-too-many","kind":"tcp","target":"a.example:1"}`, true); code != 409 || !strings.Contains(resp, "limit") {
		t.Errorf("over the limit = %d %s, want 409 naming the limit", code, resp)
	}
}

func TestDeleteStopsProbingAndRemovesResults(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hits.Add(1) }))
	defer srv.Close()
	e := newEnv(t, envOptions{})
	created := e.create(t, "gone-soon", "http", srv.URL)
	waitUntil(t, 5*time.Second, "a few probes", func() bool { return hits.Load() >= 3 })

	if code, _ := e.do(t, http.MethodDelete, "/api/uptime/"+created.ID, "", true); code != http.StatusNoContent {
		t.Fatalf("delete = %d", code)
	}
	time.Sleep(200 * time.Millisecond) // let an in-flight probe finish
	settled := hits.Load()
	time.Sleep(500 * time.Millisecond)
	if hits.Load() != settled {
		t.Fatalf("probing continued after delete: %d -> %d", settled, hits.Load())
	}
	var n int
	_ = e.db.QueryRow(`SELECT COUNT(*) FROM uptime_results`).Scan(&n)
	if n != 0 || len(e.targets(t)) != 0 {
		t.Fatalf("results left after delete: %d rows, %d targets", n, len(e.targets(t)))
	}
	if code, _ := e.do(t, http.MethodDelete, "/api/uptime/"+created.ID, "", true); code != http.StatusNotFound {
		t.Fatalf("second delete = %d, want 404", code)
	}
}

func TestEveryRouteRequiresASession(t *testing.T) {
	e := newEnv(t, envOptions{interval: time.Hour, first: time.Hour})
	for _, c := range []struct{ method, path string }{{"GET", "/api/uptime"}, {"POST", "/api/uptime"}, {"DELETE", "/api/uptime/abc"}} {
		if code, _ := e.do(t, c.method, c.path, `{}`, false); code != http.StatusUnauthorized {
			t.Errorf("%s %s without session = %d", c.method, c.path, code)
		}
	}
}

func TestResultsAreStreamedAndFilteredByType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	e := newEnv(t, envOptions{})

	dial := func(query string) *websocket.Conn {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(e.srv.URL, "http")+"/api/stream"+query, &websocket.DialOptions{HTTPHeader: http.Header{"Cookie": {e.cookie.String()}}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.CloseNow() })
		return conn
	}
	uptimeConn, metricConn, hostConn := dial("?type=uptime"), dial("?type=metric"), dial("?host=some-host")
	waitUntil(t, 3*time.Second, "subscribers", func() bool { return e.hub.SubscriberCount() == 3 })

	created := e.create(t, "streamed", "http", srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var frame struct {
		Type     string     `json:"type"`
		TargetID string     `json:"target_id"`
		HostID   *string    `json:"host_id"`
		Record   resultJSON `json:"record"`
	}
	if err := wsjson.Read(ctx, uptimeConn, &frame); err != nil {
		t.Fatalf("no uptime frame: %v", err)
	}
	if frame.Type != "uptime" || frame.TargetID != created.ID || frame.HostID != nil || !frame.Record.OK {
		t.Fatalf("frame = %+v", frame)
	}
	// Subscriptions for metrics or for one host must never receive uptime results.
	for name, conn := range map[string]*websocket.Conn{"type=metric": metricConn, "host filter": hostConn} {
		rctx, rcancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
		var junk map[string]any
		if err := wsjson.Read(rctx, conn, &junk); err == nil {
			t.Errorf("%s received %v", name, junk)
		}
		rcancel()
	}
}

func TestResultsOlderThanTheRetentionAreDeletedAndFreshOnesKept(t *testing.T) {
	var targetID string
	e := newEnv(t, envOptions{
		interval: time.Hour, first: time.Hour, retention: time.Hour,
		seed: func(s *infrastructure.Store) {
			created, err := s.Create(context.Background(), domain.NewTarget{Name: "seeded", Kind: "tcp", Address: "a.example:1", IntervalSeconds: 60}, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			targetID = created.ID
			for _, age := range []time.Duration{3 * time.Hour, 2 * time.Hour, 10 * time.Minute} {
				if err := s.InsertResult(context.Background(), domain.Result{TargetID: created.ID, TS: time.Now().Add(-age).UTC(), OK: true}); err != nil {
					t.Fatal(err)
				}
			}
		},
	})
	waitUntil(t, 3*time.Second, "old results to be purged", func() bool {
		var n int
		_ = e.db.QueryRow(`SELECT COUNT(*) FROM uptime_results WHERE target_id = ?`, targetID).Scan(&n)
		return n == 1
	})
	seeded := find(e.targets(t), "seeded")
	if seeded == nil || seeded.Last == nil || !seeded.Last.OK {
		t.Fatalf("the fresh result must survive: %+v", seeded)
	}
}

func TestNoMoreThanEightChecksRunAtOnce(t *testing.T) {
	var current, peak atomic.Int64
	var mu sync.Mutex
	e := newEnv(t, envOptions{
		interval: 30 * time.Millisecond,
		checker: func(c *application.Checker) {
			c.Dial = func(ctx context.Context, _, _ string) (net.Conn, error) {
				n := current.Add(1)
				mu.Lock()
				if n > peak.Load() {
					peak.Store(n)
				}
				mu.Unlock()
				select {
				case <-time.After(60 * time.Millisecond):
				case <-ctx.Done():
				}
				current.Add(-1)
				return nil, net.ErrClosed
			}
		},
	})
	for i := 0; i < 20; i++ {
		e.create(t, "slow"+strings.Repeat("x", i), "tcp", "a.example:1")
	}
	time.Sleep(700 * time.Millisecond)
	if p := peak.Load(); p > 8 || p < 2 {
		t.Fatalf("peak concurrent checks = %d, want between 2 and 8", p)
	}
}
