package http

import (
	"net/http"

	"github.com/avatarsik6699/smotryashchiy/internal/auth/application"
	"github.com/avatarsik6699/smotryashchiy/internal/auth/domain"
	"github.com/avatarsik6699/smotryashchiy/internal/platform/apierror"
)

// publicPaths lists the only routes exempt from session gating: login itself and health checks.
var publicPaths = map[string]bool{
	"/api/auth/login": true,
	"/api/enroll":     true,
	"/healthz":        true,
	"/health/ready":   true,
}

// RequireSession rejects any request outside publicPaths that lacks a valid session cookie.
func RequireSession(svc *application.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if publicPaths[r.URL.Path] {
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
