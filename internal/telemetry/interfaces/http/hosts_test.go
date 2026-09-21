package http

// Contract tests for the UI host API and metric downsampling (docs/SPEC.md §4d, §4.3).

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/telemetry/application"
	"github.com/avatarsik6699/smotryashchiy/internal/telemetry/domain"
)

func (e *env) post(t *testing.T, target, body string, authed bool) (int, string, http.Header) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	if authed {
		req.AddCookie(e.cookie)
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String(), rec.Header()
}

type hostList struct {
	Hosts []struct {
		ID         string     `json:"id"`
		Name       string     `json:"name"`
		CreatedAt  time.Time  `json:"created_at"`
		LastSeenAt *time.Time `json:"last_seen_at"`
	} `json:"hosts"`
}

func TestHostsListIsSortedAndNeverSeenStaysNull(t *testing.T) {
	e := newEnv(t) // creates "web-1"
	for _, name := range []string{"zeta", "alpha"} {
		if _, _, _, err := e.enroll.CreateHost(context.Background(), name); err != nil {
			t.Fatal(err)
		}
	}
	code, body := e.get(t, "/api/hosts", true)
	if code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	var out hostList
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, h := range out.Hosts {
		names = append(names, h.Name)
		if h.LastSeenAt != nil {
			t.Fatalf("%s: last_seen_at must be null before the first batch", h.Name)
		}
	}
	if strings.Join(names, ",") != "alpha,web-1,zeta" {
		t.Fatalf("order = %v, want by name", names)
	}
	if !strings.Contains(body, `"last_seen_at":null`) {
		t.Fatalf("null must be explicit, not omitted: %s", body)
	}
}

func TestHostsListShowsLastSeenAfterAnIngest(t *testing.T) {
	e := newEnv(t)
	if _, err := e.ingest("k1", batch(metric("a.b", clock, 0, nil))); err != nil {
		t.Fatal(err)
	}
	_, body := e.get(t, "/api/hosts", true)
	var out hostList
	_ = json.Unmarshal([]byte(body), &out)
	if len(out.Hosts) != 1 || out.Hosts[0].LastSeenAt == nil || !out.Hosts[0].LastSeenAt.Equal(clock) {
		t.Fatalf("hosts = %s; want last_seen_at = receipt time", body)
	}
}

func TestHostsRequireSession(t *testing.T) {
	e := newEnv(t)
	if code, _ := e.get(t, "/api/hosts", false); code != 401 {
		t.Fatalf("GET /api/hosts without session = %d", code)
	}
	if code, _, _ := e.post(t, "/api/hosts", `{"name":"x"}`, false); code != 401 {
		t.Fatalf("POST /api/hosts without session = %d", code)
	}
}

type created struct {
	Host struct {
		ID         string     `json:"id"`
		Name       string     `json:"name"`
		LastSeenAt *time.Time `json:"last_seen_at"`
	} `json:"host"`
	ServerURL string    `json:"server_url"`
	Secret    string    `json:"secret"`
	ExpiresAt time.Time `json:"expires_at"`
}

func TestCreateHostReturnsOneTimeSecretThatEnrollsAnAgent(t *testing.T) {
	e := newEnv(t)
	code, body, hdr := e.post(t, "/api/hosts", `{"name":"vps-9"}`, true)
	if code != http.StatusCreated {
		t.Fatalf("%d %s", code, body)
	}
	if hdr.Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q; a response with a secret must not be cached", hdr.Get("Cache-Control"))
	}
	var c created
	if err := json.Unmarshal([]byte(body), &c); err != nil {
		t.Fatal(err)
	}
	if c.Host.Name != "vps-9" || c.Host.ID == "" || c.Host.LastSeenAt != nil || c.Secret == "" || !c.ExpiresAt.Equal(clock.Add(application.EnrollmentTTL)) {
		t.Fatalf("response = %+v", c)
	}
	if c.ServerURL != "http://example.com" { // httptest requests carry Host: example.com
		t.Fatalf("server_url = %q, want the request-derived origin", c.ServerURL)
	}
	// The secret is stored only as its hash...
	var stored string
	if err := e.rawDB.QueryRow(`SELECT secret_hash FROM host_enrollments WHERE host_id = ?`, c.Host.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == c.Secret || stored != application.HashSecret(c.Secret) {
		t.Fatalf("stored = %q, want only the SHA-256 of the secret", stored)
	}
	// ...and it works exactly once.
	if code, _ := postEnroll(t, e, c.Secret, newPublicKey(t)); code != 200 {
		t.Fatalf("enrollment with the UI-created secret = %d", code)
	}
	if code, _ := postEnroll(t, e, c.Secret, newPublicKey(t)); code != 401 {
		t.Fatalf("second use = %d, want 401", code)
	}
}

func TestCreateHostValidationAndConflict(t *testing.T) {
	e := newEnv(t)
	for name, tc := range map[string]struct {
		body string
		want int
	}{
		"blank name":     {`{"name":"  "}`, 400},
		"missing name":   {`{}`, 400},
		"too long":       {`{"name":"` + strings.Repeat("x", 81) + `"}`, 400},
		"malformed body": {`{`, 400},
		"duplicate":      {`{"name":"web-1"}`, 409}, // newEnv already registered web-1
	} {
		if code, body, _ := e.post(t, "/api/hosts", tc.body, true); code != tc.want {
			t.Errorf("%s: %d %s, want %d", name, code, body, tc.want)
		}
	}
	if code, _, _ := e.post(t, "/api/hosts", string(bytes.Repeat([]byte("a"), maxHostBody+10)), true); code != 400 {
		t.Errorf("oversized body = %d, want 400", code)
	}
}

func TestServerURLPrefersConfiguredPublicURLAndHonoursSecureCookies(t *testing.T) {
	e := newEnv(t)
	configured := NewHostsHandlers(e.svc, e.enroll, "https://monitor.example.com", false)
	mux := http.NewServeMux()
	configured.Register(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/hosts", strings.NewReader(`{"name":"h1"}`))
	req.Host = "internal:8080"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var c created
	_ = json.Unmarshal(rec.Body.Bytes(), &c)
	if c.ServerURL != "https://monitor.example.com" {
		t.Fatalf("server_url = %q, want the configured public URL", c.ServerURL)
	}

	secure := NewHostsHandlers(e.svc, e.enroll, "", true)
	mux2 := http.NewServeMux()
	secure.Register(mux2)
	req2 := httptest.NewRequest(http.MethodPost, "/api/hosts", strings.NewReader(`{"name":"h2"}`))
	req2.Host = "monitor.example.com"
	rec2 := httptest.NewRecorder()
	mux2.ServeHTTP(rec2, req2)
	_ = json.Unmarshal(rec2.Body.Bytes(), &c)
	if c.ServerURL != "https://monitor.example.com" {
		t.Fatalf("derived server_url = %q, want https when cookies are Secure", c.ServerURL)
	}
}

// --- step downsampling ---

func stepPoints(t *testing.T, e *env, query string) []struct {
	TS    time.Time `json:"ts"`
	Value float64   `json:"value"`
} {
	t.Helper()
	code, body := e.get(t, "/api/metrics?name=s.m&"+query, true)
	if code != 200 {
		t.Fatalf("%s: %d %s", query, code, body)
	}
	var out struct {
		Metrics []struct {
			TS    time.Time `json:"ts"`
			Value float64   `json:"value"`
		} `json:"metrics"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	return out.Metrics
}

func TestStepKeepsTheNewestSampleOfEveryBucketPerSeries(t *testing.T) {
	e := newEnv(t)
	base := clock.Add(-10 * time.Minute).Truncate(time.Minute) // aligned to a 60 s bucket edge
	var ms []domain.Metric
	for i := 0; i < 30; i++ { // one sample every 10 s for 5 minutes, value = i
		ms = append(ms, metric("s.m", base.Add(time.Duration(i)*10*time.Second), float64(i), nil))
	}
	// A second series must be bucketed independently.
	ms = append(ms, metric("s.m", base.Add(15*time.Second), 500, map[string]string{"core": "1"}))
	if _, err := e.ingest("k1", batch(ms...)); err != nil {
		t.Fatal(err)
	}
	got := stepPoints(t, e, "step=60&limit=5000")
	var main []float64
	for _, p := range got {
		if p.Value != 500 {
			main = append(main, p.Value)
		}
	}
	// 30 samples, 6 per minute: the newest of each minute are i = 5, 11, 17, 23, 29.
	want := []float64{5, 11, 17, 23, 29}
	if len(main) != len(want) {
		t.Fatalf("step=60 kept %v, want %v", main, want)
	}
	for i := range want {
		if main[i] != want[i] {
			t.Fatalf("step=60 kept %v, want %v", main, want)
		}
	}
	if len(got) != len(want)+1 {
		t.Fatalf("got %d points, want %d (the other series contributes its own single bucket)", len(got), len(want)+1)
	}
	for i := 1; i < len(got); i++ {
		if got[i].TS.Before(got[i-1].TS) {
			t.Fatal("downsampled points must stay ordered by ts")
		}
	}
	if raw := stepPoints(t, e, "limit=5000"); len(raw) != 31 {
		t.Fatalf("without step all %d samples must be returned, got %d", 31, len(raw))
	}
}

func TestStepKeepsAMeasuredZeroAndHonoursTheRange(t *testing.T) {
	e := newEnv(t)
	base := clock.Add(-10 * time.Minute).Truncate(time.Minute)
	if _, err := e.ingest("k1", batch(
		metric("s.m", base, 0, nil),
		metric("s.m", base.Add(2*time.Minute), 7, nil),
		metric("s.m", base.Add(4*time.Minute), 9, nil))); err != nil {
		t.Fatal(err)
	}
	got := stepPoints(t, e, "step=60&from="+base.Format(time.RFC3339)+"&to="+base.Add(3*time.Minute).Format(time.RFC3339))
	if len(got) != 2 || got[0].Value != 0 || got[1].Value != 7 {
		t.Fatalf("got %+v; want the measured zero and 7 inside the range only", got)
	}
}

func TestStepValidation(t *testing.T) {
	e := newEnv(t)
	for _, q := range []string{
		"step=9", "step=3601", "step=-5", "step=abc",
		"step=60&latest=true",
		"step=60&resolution=hour",
	} {
		if code, body := e.get(t, "/api/metrics?"+q, true); code != 400 {
			t.Errorf("%s = %d %s, want 400", q, code, body)
		}
	}
	for _, q := range []string{"step=10", "step=3600", "step=60"} {
		if code, body := e.get(t, "/api/metrics?"+q, true); code != 200 {
			t.Errorf("%s = %d %s, want 200", q, code, body)
		}
	}
}
