package http

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/telemetry/domain"
)

// Change 38: the dashboard asks only for its hour of error events.
func TestEventsFromKeepsOnlyTheRecentOnes(t *testing.T) {
	e := newEnv(t)
	b := domain.Batch{SchemaVersion: "1.0", Events: []domain.Event{
		{TS: clock.Add(-3 * time.Hour), Level: domain.LevelError, Message: "old"},
		{TS: clock.Add(-10 * time.Minute), Level: domain.LevelError, Message: "recent"},
		{TS: clock.Add(-5 * time.Minute), Level: domain.LevelInfo, Message: "info"},
	}}
	if _, err := e.ingest("k1", b); err != nil {
		t.Fatal(err)
	}
	from := url.QueryEscape(clock.Add(-time.Hour).Format(time.RFC3339))
	code, body := e.get(t, "/api/events?level=error&from="+from, true)
	var got struct{ Events []struct{ Message string } }
	if err := json.Unmarshal([]byte(body), &got); err != nil || code != http.StatusOK {
		t.Fatalf("events = %d %s", code, body)
	}
	if len(got.Events) != 1 || got.Events[0].Message != "recent" {
		t.Fatalf("events since an hour ago = %+v", got.Events)
	}
	if code, _ := e.get(t, "/api/events?from=yesterday", true); code != http.StatusBadRequest {
		t.Fatalf("a malformed from = %d, want 400", code)
	}
}

// Change 38: a limit keeps the newest samples, still returned oldest first.
func TestRawMetricsAtTheLimitKeepTheNewestSamples(t *testing.T) {
	e := newEnv(t)
	var ms []domain.Metric
	for i := range 10 {
		ms = append(ms, metric("cpu.usage_percent", clock.Add(time.Duration(i-10)*time.Minute), float64(i), nil))
	}
	if _, err := e.ingest("k1", batch(ms...)); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{
		"/api/metrics?host=" + e.host.ID + "&name=cpu.usage_percent&limit=3",
		"/api/metrics?name=cpu.usage_percent&step=60&from=" + url.QueryEscape(clock.Add(-time.Hour).Format(time.RFC3339)) + "&limit=3",
	} {
		code, body := e.get(t, target, true)
		var got struct{ Metrics []struct{ Value float64 } }
		if err := json.Unmarshal([]byte(body), &got); err != nil || code != http.StatusOK {
			t.Fatalf("%s = %d %s", target, code, body)
		}
		if len(got.Metrics) != 3 || got.Metrics[0].Value != 7 || got.Metrics[2].Value != 9 {
			t.Fatalf("%s = %+v, want the newest three oldest first", target, got.Metrics)
		}
	}
}

// Change 38: the checks list reads check_latest, which follows inserts and the purge.
func TestLatestChecksFollowInsertsAndDeletes(t *testing.T) {
	e := newEnv(t)
	meta := json.RawMessage(`{}`)
	b := domain.Batch{SchemaVersion: "1.0", Checks: []domain.Check{
		{Name: "disk", TS: clock.Add(-2 * time.Minute), Status: "ok", Meta: meta},
		{Name: "disk", TS: clock.Add(-1 * time.Minute), Status: "warn", Meta: meta},
	}}
	if _, err := e.ingest("k1", b); err != nil {
		t.Fatal(err)
	}
	status := func() string {
		_, body := e.get(t, "/api/checks", true)
		var got struct{ Checks []struct{ Status string } }
		if err := json.Unmarshal([]byte(body), &got); err != nil || len(got.Checks) != 1 {
			t.Fatalf("checks = %s", body)
		}
		return got.Checks[0].Status
	}
	if s := status(); s != "warn" {
		t.Fatalf("latest = %q, want warn", s)
	}
	if _, err := e.rawDB.Exec(`DELETE FROM checks WHERE ts = ?`, clock.Add(-time.Minute).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if s := status(); s != "ok" {
		t.Fatalf("after deleting the newest = %q, want the previous ok", s)
	}
}
