package http

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/analytics/application"
	"github.com/avatarsik6699/smotryashchiy/internal/analytics/domain"
	"github.com/avatarsik6699/smotryashchiy/internal/analytics/infrastructure"
	"github.com/avatarsik6699/smotryashchiy/internal/platform/db"
)

func newTestServer(t *testing.T) (*httptest.Server, *application.Service) {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "collect.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Migrate(sqlDB); err != nil {
		t.Fatal(err)
	}
	svc := application.NewService(infrastructure.NewStore(sqlDB), time.Now, application.Options{})
	mux := http.NewServeMux()
	NewCollectHandlers(svc, nil).Register(mux)
	NewHandlers(svc).Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, svc
}

func post(t *testing.T, url, origin string, body []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/120.0")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestCollectEndToEndStoresAPageviewAndAnswers204(t *testing.T) {
	srv, svc := newTestServer(t)
	site, err := svc.CreateSite(t.Context(), domain.NewSite{Name: "A", Domain: "a.example"})
	if err != nil {
		t.Fatalf("CreateSite: %v", err)
	}

	resp := post(t, srv.URL+"/api/collect", "https://a.example", []byte(`{"site":"`+site.ID+`","url":"/docs"}`))
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://a.example" {
		t.Fatalf("CORS header = %q, want the matching origin echoed back", got)
	}

	stats, err := svc.Stats(t.Context(), site.ID, "today")
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Pageviews != 1 {
		t.Fatalf("Pageviews = %d, want 1", stats.Pageviews)
	}
}

func TestCollectAnswers204ForAnUnknownSiteAndStoresNothing(t *testing.T) {
	srv, _ := newTestServer(t)
	resp := post(t, srv.URL+"/api/collect", "", []byte(`{"site":"no-such-site","url":"/"}`))
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (never leak that the site doesn't exist)", resp.StatusCode)
	}
}

func TestCollectOmitsCORSHeaderForAnUnregisteredOrigin(t *testing.T) {
	srv, svc := newTestServer(t)
	site, err := svc.CreateSite(t.Context(), domain.NewSite{Name: "A", Domain: "a.example"})
	if err != nil {
		t.Fatalf("CreateSite: %v", err)
	}
	resp := post(t, srv.URL+"/api/collect", "https://evil.example", []byte(`{"site":"`+site.ID+`","url":"/"}`))
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("CORS header = %q, want empty for an unregistered origin", got)
	}
}

// The tracked site's own dev/CI/Lighthouse runs post from localhost; only its own origin counts
// (docs/SPEC.md §4i). Every case still answers 204.
func TestCollectStoresOnlyFromTheSitesOwnOrigin(t *testing.T) {
	for _, c := range []struct {
		origin string
		want   int
	}{
		{"https://a.example", 1},
		{"https://www.a.example", 1},
		{"http://localhost:3000", 0},
		{"http://127.0.0.2:3200", 0},
		{"https://evil.example", 0},
		{"null", 0},
		{"", 0},
	} {
		t.Run(c.origin, func(t *testing.T) {
			srv, svc := newTestServer(t)
			site, err := svc.CreateSite(t.Context(), domain.NewSite{Name: "A", Domain: "a.example"})
			if err != nil {
				t.Fatalf("CreateSite: %v", err)
			}
			resp := post(t, srv.URL+"/api/collect", c.origin, []byte(`{"site":"`+site.ID+`","url":"/"}`))
			if resp.StatusCode != http.StatusNoContent {
				t.Fatalf("status = %d, want 204", resp.StatusCode)
			}
			stats, err := svc.Stats(t.Context(), site.ID, "today")
			if err != nil {
				t.Fatalf("Stats: %v", err)
			}
			if stats.Pageviews != c.want {
				t.Fatalf("origin %q: Pageviews = %d, want %d", c.origin, stats.Pageviews, c.want)
			}
		})
	}
}

func TestCollectRateLimitsPerSourceIP(t *testing.T) {
	srv, svc := newTestServer(t)
	site, err := svc.CreateSite(t.Context(), domain.NewSite{Name: "A", Domain: "a.example"})
	if err != nil {
		t.Fatalf("CreateSite: %v", err)
	}
	body := []byte(`{"site":"` + site.ID + `","url":"/"}`)
	// The burst (5, see collectRateLimit) plus a few more: httptest always presents the same
	// loopback source IP, so extra requests beyond the burst must be dropped (still 204, but not
	// stored — the endpoint never reveals rate limiting in its response).
	for range 12 {
		post(t, srv.URL+"/api/collect", "https://a.example", body)
	}
	stats, err := svc.Stats(t.Context(), site.ID, "today")
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Pageviews >= 12 {
		t.Fatalf("Pageviews = %d, want fewer than 12 (rate limit should have dropped some)", stats.Pageviews)
	}
	if stats.Pageviews == 0 {
		t.Fatal("want at least the burst allowance stored")
	}
}

func TestCollectCORSAcceptsTheWWWVariantLikeStorage(t *testing.T) {
	srv, svc := newTestServer(t)
	site, err := svc.CreateSite(t.Context(), domain.NewSite{Name: "A", Domain: "a.example"})
	if err != nil {
		t.Fatalf("CreateSite: %v", err)
	}
	resp := post(t, srv.URL+"/api/collect", "https://www.a.example", []byte(`{"site":"`+site.ID+`","url":"/"}`))
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://www.a.example" {
		t.Fatalf("CORS header = %q, want the www. origin echoed back", got)
	}
}

// The rate limit is checked before any database work (docs/SPEC.md §4i): a limited request never
// reaches the CORS site lookup, so it carries no CORS header even from a registered origin.
func TestCollectRateLimitRunsBeforeTheCORSLookup(t *testing.T) {
	srv, svc := newTestServer(t)
	site, err := svc.CreateSite(t.Context(), domain.NewSite{Name: "A", Domain: "a.example"})
	if err != nil {
		t.Fatalf("CreateSite: %v", err)
	}
	body := []byte(`{"site":"` + site.ID + `","url":"/"}`)
	var last *http.Response
	for range collectRateLimit.burst + 1 {
		last = post(t, srv.URL+"/api/collect", "https://a.example", body)
	}
	if last.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", last.StatusCode)
	}
	if got := last.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("limited request got CORS header %q: the site lookup ran before the limit", got)
	}
}
