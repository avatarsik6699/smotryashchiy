package http

import (
	"encoding/json"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/avatarsik6699/smotryashchiy/internal/auth/application"
	"github.com/avatarsik6699/smotryashchiy/internal/auth/domain"
	"github.com/avatarsik6699/smotryashchiy/internal/platform/apierror"
)

// publicAPIPaths are the only API routes exempt from session gating: login itself and agent
// enrollment (its one-time secret is the credential).
var publicAPIPaths = map[string]bool{
	"/api/auth/login": true,
	"/api/enroll":     true,
	// Answers 200 either way (see Handlers.sessionState) so a logged-out page load is not a 401.
	"/api/auth/session": true,
	// Anonymous visitor browsers call this, not an operator session (docs/SPEC.md §4i); it is its
	// own line of defense (rate limit, unknown-site silently dropped), not session-gated.
	"/api/collect": true,
}

// isAPIPath reports whether path belongs to the API namespace.
func isAPIPath(path string) bool { return path == "/api" || strings.HasPrefix(path, "/api/") }

// crossOriginExempt are the unsafe-method API routes a foreign page may call: the analytics beacon
// (tracked sites post it cross-origin by design) and agent enrollment (a CLI, not a browser).
var crossOriginExempt = map[string]bool{
	"/api/collect": true,
	"/api/enroll":  true,
}

// GuardCrossOriginWrites refuses browser writes to the API from any other origin (docs/SPEC.md
// §4d). SameSite=Lax still sends the session cookie on a POST from a same-site page — any
// subdomain of the UI's registrable domain — and a text/plain body needs no CORS preflight, so the
// cookie alone does not prove the operator's own page sent the request. The origin comparison is
// by host, like the WebSocket upgrade's (coder/websocket): TLS may end at a trusted proxy, so the
// scheme the server sees is not the browser's.
func GuardCrossOriginWrites() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isAPIPath(r.URL.Path) || isSafeMethod(r.Method) || crossOriginExempt[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}
			if !sameOriginRequest(r) {
				writeError(w, http.StatusForbidden, "cross-origin request refused")
				return
			}
			if hasBody(r) && !isJSON(r.Header.Get("Content-Type")) {
				writeError(w, http.StatusUnsupportedMediaType, "request body must be application/json")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func isSafeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

// sameOriginRequest trusts Sec-Fetch-Site when the browser sends it, and otherwise compares a
// present Origin with the request's host. A request with neither header is not a browser's and
// cannot carry the operator's cookie, so it passes.
func sameOriginRequest(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
		return site == "same-origin" || site == "none"
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false // includes "null"
	}
	return strings.EqualFold(u.Host, r.Host)
}

func hasBody(r *http.Request) bool {
	return r.ContentLength > 0 || r.ContentLength == -1
}

func isJSON(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && mediaType == "application/json"
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// RequireSession gates the API namespace: every /api/ route needs a valid session cookie except
// publicAPIPaths. Everything outside /api/ is public by design: the embedded static SPA (it holds no
// secrets and must load to show the login form) and the health probes (docs/SPEC.md §4d).
func RequireSession(svc *application.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isAPIPath(r.URL.Path) || publicAPIPaths[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}
			cookie, err := r.Cookie(sessionCookieName)
			if err != nil {
				apierror.Write(w, domain.ErrSessionInvalid)
				return
			}
			if err := svc.ValidateSession(cookie.Value); err != nil {
				apierror.Write(w, err)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
