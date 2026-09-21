package http

// Contract tests for the WebSocket stream (docs/SPEC.md §4.6).

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/avatarsik6699/smotryashchiy/internal/telemetry/application"
	"github.com/avatarsik6699/smotryashchiy/internal/telemetry/domain"
)

func (e *env) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(e.handler)
	t.Cleanup(srv.Close)
	return srv
}

func (e *env) dial(t *testing.T, srv *httptest.Server, query string, headers http.Header) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	t.Cleanup(cancel)
	if headers == nil {
		headers = http.Header{}
	}
	if headers.Get("Cookie") == "" {
		headers.Set("Cookie", e.cookie.String())
	}
	return websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/stream"+query, &websocket.DialOptions{HTTPHeader: headers})
}

func readFrame(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var frame map[string]any
	if err := wsjson.Read(ctx, conn, &frame); err != nil {
		t.Fatalf("read frame: %v", err)
	}
	return frame
}

func TestStreamPublishesOnlyNewlyAcceptedRecords(t *testing.T) {
	e := newEnv(t)
	srv := e.server(t)
	conn, _, err := e.dial(t, srv, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()

	// Wait until the handler has subscribed, then ingest.
	waitFor(t, func() bool { return subscriberCount(e.hub) == 1 })
	b := domain.Batch{SchemaVersion: "1.0",
		Metrics: []domain.Metric{metric("container.cpu_percent", clock, 0, map[string]string{"container": "idle"})},
		Checks:  []domain.Check{{Name: "disk.root", TS: clock, Status: "ok", Meta: json.RawMessage("{}")}},
		Events:  []domain.Event{{TS: clock, Level: "warn", Message: "ban", Labels: map[string]string{"jail": "sshd"}}}}
	if _, err := e.ingest("k1", b); err != nil {
		t.Fatal(err)
	}

	types := map[string]bool{}
	for i := 0; i < 3; i++ {
		f := readFrame(t, conn)
		typ := f["type"].(string)
		types[typ] = true
		if f["host_id"] != e.host.ID {
			t.Fatalf("frame host_id = %v", f["host_id"])
		}
		if typ == "metric" {
			if rec := f["record"].(map[string]any); rec["value"] != float64(0) {
				t.Fatalf("zero metric must be published as 0, got %v", rec["value"])
			}
		}
	}
	if !types["metric"] || !types["check"] || !types["event"] {
		t.Fatalf("frame types = %v", types)
	}

	// A replay (same key) and an overlap (new key, same records) must publish nothing; the marker
	// batch afterwards must be the very next frame.
	if _, err := e.ingest("k1", b); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ingest("k2", b); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ingest("k3", batch(metric("marker.value", clock, 7, nil))); err != nil {
		t.Fatal(err)
	}
	f := readFrame(t, conn)
	if rec := f["record"].(map[string]any); rec["name"] != "marker.value" {
		t.Fatalf("duplicate or replayed record was published: %v", f)
	}
}

func TestStreamFiltersByHostAndType(t *testing.T) {
	e := newEnv(t)
	srv := e.server(t)
	conn, _, err := e.dial(t, srv, "?type=check&host="+e.host.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	waitFor(t, func() bool { return subscriberCount(e.hub) == 1 })
	if _, err := e.ingest("k1", domain.Batch{SchemaVersion: "1.0",
		Metrics: []domain.Metric{metric("a.b", clock, 1, nil)},
		Checks:  []domain.Check{{Name: "c.d", TS: clock, Status: "warn", Meta: json.RawMessage("{}")}}}); err != nil {
		t.Fatal(err)
	}
	if f := readFrame(t, conn); f["type"] != "check" {
		t.Fatalf("filter let through %v", f)
	}
}

func TestStreamRejectsUnauthenticatedAndCrossOriginUpgrades(t *testing.T) {
	e := newEnv(t)
	srv := e.server(t)

	noCookie := http.Header{"Cookie": {"x=y"}}
	if _, resp, err := e.dial(t, srv, "", noCookie); err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated upgrade: err=%v resp=%v, want 401", err, resp)
	}
	cross := http.Header{"Origin": {"https://evil.example"}}
	if _, resp, err := e.dial(t, srv, "", cross); err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin upgrade: err=%v resp=%v, want 403", err, resp)
	}
	same := http.Header{"Origin": {srv.URL}}
	conn, _, err := e.dial(t, srv, "", same)
	if err != nil {
		t.Fatalf("same-origin upgrade rejected: %v", err)
	}
	conn.CloseNow()
	if code, _ := e.get(t, "/api/stream?type=bogus", true); code != http.StatusBadRequest {
		t.Fatalf("bad type filter = %d, want 400", code)
	}
}

func TestSlowStreamClientCannotStallIngest(t *testing.T) {
	e := newEnv(t)
	srv := e.server(t)
	conn, _, err := e.dial(t, srv, "", nil) // never reads: it is the slow client
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	waitFor(t, func() bool { return subscriberCount(e.hub) == 1 })

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 20; i++ {
			ms := make([]domain.Metric, 0, 50)
			for j := 0; j < 50; j++ {
				ms = append(ms, metric("a.b", clock.Add(-time.Duration(i*50+j)*time.Millisecond), 1, nil))
			}
			if _, err := e.ingest("k"+string(rune('a'+i%26))+strings.Repeat("x", i/26), batch(ms...)); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("ingest stalled behind a slow stream client")
	}
	// Whether the socket buffers overflow enough to drop the client is kernel-dependent; the drop
	// itself is asserted deterministically in application.TestHubDropsSlowSubscriberWithoutBlocking.
}

func TestHubCloseDisconnectsStreamClients(t *testing.T) {
	e := newEnv(t)
	srv := e.server(t)
	conn, _, err := e.dial(t, srv, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	waitFor(t, func() bool { return subscriberCount(e.hub) == 1 })
	e.hub.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _, err = conn.Read(ctx)
	if websocket.CloseStatus(err) != websocket.StatusGoingAway {
		t.Fatalf("read error = %v, want close status going-away", err)
	}
}

func subscriberCount(h *application.Hub) int { return h.SubscriberCount() }

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached in time")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
