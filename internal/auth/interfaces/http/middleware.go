package http

import (
	"net/http"
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
}

// isAPIPath reports whether path belongs to the API namespace.
func isAPIPath(path string) bool { return path == "/api" || strings.HasPrefix(path, "/api/") }

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
