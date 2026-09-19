package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"

	"github.com/avatarsik6699/smotryashchiy/internal/auth/application"
	authinfra "github.com/avatarsik6699/smotryashchiy/internal/auth/infrastructure"
	"github.com/avatarsik6699/smotryashchiy/internal/platform/db"
	"github.com/avatarsik6699/smotryashchiy/internal/platform/httpserver"
)

const password = "correct-horse-battery-staple-1"

func newServer(t *testing.T, opts Options) (http.Handler, *application.Service) {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Migrate(sqlDB); err != nil {
		t.Fatal(err)
	}
	svc := application.NewService(authinfra.NewSettingsStore(sqlDB))
	if err := svc.SetAdminPassword(context.Background(), password); err != nil {
		t.Fatal(err)
	}
	srv := httpserver.New(":0")
	httpserver.RegisterHealth(srv.Mux, "rel", sqlDB.PingContext)
	NewHandlers(svc, opts).Register(srv.Mux)
	srv.Mux.HandleFunc("GET /api/private", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	var h http.Handler = srv.Mux
	h = RequireSession(svc)(h)
	return h, svc
}

func post(h http.Handler, path, body, remote string, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.RemoteAddr = remote
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func login(h http.Handler, pw, remote string) *httptest.ResponseRecorder {
	return post(h, "/api/auth/login", `{"password":"`+pw+`"}`, remote)
}

func TestLoginSetsHardenedCookieAndUnlocksPrivateRoutes(t *testing.T) {
	h, _ := newServer(t, Options{SecureCookies: true})
	rec := login(h, password, "192.0.2.1:1000")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("code = %d", rec.Code)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %v", cookies)
	}
	c := cookies[0]
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.Name != "session" {
		t.Fatalf("cookie attributes: %+v", c)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/private", nil)
	req.AddCookie(c)
	out := httptest.NewRecorder()
	h.ServeHTTP(out, req)
	if out.Code != http.StatusOK {
		t.Fatalf("private with session = %d", out.Code)
	}
}

func TestCookieNotSecureInDevelopment(t *testing.T) {
	h, _ := newServer(t, Options{})
	rec := login(h, password, "192.0.2.1:1000")
	if c := rec.Result().Cookies()[0]; c.Secure {
		t.Fatal("development cookie must not be Secure over plain HTTP")
	}
}

func TestPrivateRoutesRequireSessionButHealthDoesNot(t *testing.T) {
	h, _ := newServer(t, Options{})
	for path, want := range map[string]int{"/api/private": 401, "/healthz": 200, "/health/ready": 200} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != want {
			t.Errorf("%s = %d, want %d", path, rec.Code, want)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/api/private", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "forged"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("forged session = %d", rec.Code)
	}
}

func TestWrongPasswordAndMalformedBody(t *testing.T) {
	h, _ := newServer(t, Options{})
	if rec := login(h, "nope", "192.0.2.1:1"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password = %d", rec.Code)
	}
	if rec := post(h, "/api/auth/login", "not json", "192.0.2.1:1"); rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed = %d", rec.Code)
	}
}

func TestLoginRateLimitPerClient(t *testing.T) {
	h, _ := newServer(t, Options{})
	for i := 0; i < loginFailureLimit; i++ {
		if rec := login(h, "bad", "192.0.2.9:1"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d", i, rec.Code)
		}
	}
	rec := login(h, password, "192.0.2.9:1") // even the right password is refused while limited
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("limited = %d retry-after = %q", rec.Code, rec.Header().Get("Retry-After"))
	}
	if rec := login(h, password, "192.0.2.10:1"); rec.Code != http.StatusNoContent {
		t.Fatalf("other client = %d", rec.Code)
	}
}

func TestForwardedAddressOnlyTrustedFromProxy(t *testing.T) {
	proxy := netip.MustParsePrefix("10.0.0.2/32")
	h, _ := newServer(t, Options{TrustedProxyCIDRs: []netip.Prefix{proxy}})
	// Failures forwarded by the trusted proxy are attributed to the forwarded client, not the proxy.
	for i := 0; i < loginFailureLimit; i++ {
		post(h, "/api/auth/login", `{"password":"bad"}`, "10.0.0.2:5", "X-Forwarded-For", "203.0.113.7")
	}
	if rec := post(h, "/api/auth/login", `{"password":"`+password+`"}`, "10.0.0.2:5", "X-Forwarded-For", "203.0.113.7"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("forwarded client not limited: %d", rec.Code)
	}
	if rec := post(h, "/api/auth/login", `{"password":"`+password+`"}`, "10.0.0.2:5", "X-Forwarded-For", "203.0.113.8"); rec.Code != http.StatusNoContent {
		t.Fatalf("different forwarded client = %d", rec.Code)
	}
	// An untrusted peer cannot spoof its identity via X-Forwarded-For.
	for i := 0; i < loginFailureLimit; i++ {
		post(h, "/api/auth/login", `{"password":"bad"}`, "198.51.100.1:5", "X-Forwarded-For", "203.0.113.99")
	}
	if rec := post(h, "/api/auth/login", `{"password":"bad"}`, "198.51.100.1:5", "X-Forwarded-For", "203.0.113.100"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("spoofed header bypassed limiter: %d", rec.Code)
	}
}

func TestLogoutInvalidatesSessionAndClearsCookie(t *testing.T) {
	h, _ := newServer(t, Options{})
	session := login(h, password, "192.0.2.1:1").Result().Cookies()[0]
	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.AddCookie(session)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout = %d", rec.Code)
	}
	if c := rec.Result().Cookies()[0]; c.Value != "" || c.MaxAge >= 0 {
		t.Fatalf("cookie not cleared: %+v", c)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/private", nil)
	req.AddCookie(session)
	out := httptest.NewRecorder()
	h.ServeHTTP(out, req)
	if out.Code != http.StatusUnauthorized {
		t.Fatalf("session after logout = %d", out.Code)
	}
}
