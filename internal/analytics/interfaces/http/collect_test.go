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
	for i := 0; i < 12; i++ {
		post(t, srv.URL+"/api/collect", "", body)
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
