package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The 2026-09-23 production audit's reproduction (docs/SPEC.md §4d): a page on another subdomain
// of the same site rides the Lax session cookie with a text/plain POST, which needs no preflight.
func TestCrossOriginWriteGuard(t *testing.T) {
	h, _ := newServer(t, Options{})
	session := login(h, password, "192.0.2.1:1").Result().Cookies()[0]
	const host = "sre.example.test"
	for _, c := range []struct {
		name    string
		path    string
		headers []string
		want    int
	}{
		{"same-site subdomain, text/plain (audit repro)", "/api/private",
			[]string{"Origin", "https://evil.example.test", "Content-Type", "text/plain"}, http.StatusForbidden},
		{"same-site subdomain, JSON", "/api/private",
			[]string{"Origin", "https://evil.example.test"}, http.StatusForbidden},
		{"Sec-Fetch-Site same-site wins over a matching Origin", "/api/private",
			[]string{"Sec-Fetch-Site", "same-site", "Origin", "https://" + host}, http.StatusForbidden},
		{"Sec-Fetch-Site cross-site", "/api/private",
			[]string{"Sec-Fetch-Site", "cross-site"}, http.StatusForbidden},
		{"null Origin", "/api/private", []string{"Origin", "null"}, http.StatusForbidden},
		{"own origin, JSON", "/api/private", []string{"Origin", "https://" + host}, http.StatusCreated},
		{"Sec-Fetch-Site same-origin, JSON", "/api/private", []string{"Sec-Fetch-Site", "same-origin"}, http.StatusCreated},
		{"own origin, text/plain", "/api/private",
			[]string{"Origin", "https://" + host, "Content-Type", "text/plain"}, http.StatusUnsupportedMediaType},
		{"no browser headers (CLI), JSON", "/api/private", nil, http.StatusCreated},
		{"login from a foreign origin (login CSRF)", "/api/auth/login",
			[]string{"Origin", "https://evil.example.test"}, http.StatusForbidden},
		{"collect is exempt", "/api/collect",
			[]string{"Origin", "https://tracked.example", "Content-Type", "text/plain"}, http.StatusNoContent},
	} {
		t.Run(c.name, func(t *testing.T) {
			req := newPost(c.path, `{"password":"`+password+`"}`, c.headers...)
			req.Host = host
			req.AddCookie(session)
			rec := serve(h, req)
			if rec.Code != c.want {
				t.Fatalf("code = %d, want %d (body %s)", rec.Code, c.want, rec.Body.String())
			}
		})
	}
}

// newPost builds a JSON POST (what the SPA sends); headers may override Content-Type.
func newPost(path, body string, headers ...string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	setHeaders(req, headers)
	return req
}

func newGet(path string, headers ...string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	setHeaders(req, headers)
	return req
}

func setHeaders(req *http.Request, headers []string) {
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
}

func serve(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestCrossOriginGuardCoversLogoutAndLeavesReadsAlone(t *testing.T) {
	h, _ := newServer(t, Options{})
	session := login(h, password, "192.0.2.1:1").Result().Cookies()[0]

	logout := newPost("/api/auth/logout", "", "Origin", "https://evil.example.test")
	logout.AddCookie(session)
	if rec := serve(h, logout); rec.Code != http.StatusForbidden {
		t.Fatalf("foreign logout = %d, want 403", rec.Code)
	}
	read := newGet("/api/private", "Sec-Fetch-Site", "cross-site")
	read.AddCookie(session)
	if rec := serve(h, read); rec.Code != http.StatusOK {
		t.Fatalf("cross-site GET = %d, want 200 (reads are not guarded)", rec.Code)
	}
}
