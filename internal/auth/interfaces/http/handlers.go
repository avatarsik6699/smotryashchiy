// Package http exposes POST /api/auth/login, POST /api/auth/logout and the session-required
// middleware (docs/SPEC.md §3: every endpoint except login and health requires a session).
package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/auth/application"
	"github.com/avatarsik6699/smotryashchiy/internal/auth/domain"
	"github.com/avatarsik6699/smotryashchiy/internal/platform/apierror"
)

// sessionCookieName is the HttpOnly cookie the session token travels in.
const sessionCookieName = "session"

// Handlers exposes the auth HTTP surface bound to an application.Service.
type Handlers struct {
	service       *application.Service
	secureCookies bool
	loginLimiter  *loginLimiter
}

// Options define the production trust boundary without changing development defaults.
type Options struct {
	SecureCookies     bool
	TrustedProxyCIDRs []netip.Prefix
}

// NewHandlers wires Handlers to svc.
func NewHandlers(svc *application.Service, opts Options) *Handlers {
	return &Handlers{
		service:       svc,
		secureCookies: opts.SecureCookies,
		loginLimiter:  newLoginLimiter(opts.TrustedProxyCIDRs),
	}
}

// Register mounts the auth routes on mux.
func (h *Handlers) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/auth/login", h.login)
	mux.HandleFunc("POST /api/auth/logout", h.logout)
	mux.HandleFunc("GET /api/auth/session", h.sessionState)
}

type loginRequest struct {
	Password string `json:"password"`
}

func (h *Handlers) login(w http.ResponseWriter, r *http.Request) {
	clientIP := h.loginLimiter.clientIP(r)
	if retryAfter, limited := h.loginLimiter.retryAfter(clientIP); limited {
		seconds := int(retryAfter.Round(time.Second).Seconds())
		if seconds < 1 {
			seconds = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "too many login attempts"})
		return
	}
	var req loginRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		apierror.Write(w, apierror.Invalid("malformed request body"))
		return
	}
	session, err := h.service.Login(r.Context(), req.Password)
	if err != nil {
		if errors.Is(err, domain.ErrInvalidCredentials) {
			h.loginLimiter.failure(clientIP)
		}
		apierror.Write(w, err)
		return
	}
	h.loginLimiter.success(clientIP)
	h.setCookie(w, r, session.Token, session.ExpiresAt)
	w.WriteHeader(http.StatusNoContent)
}

// sessionState reports whether the request carries a valid session. It answers 200 in both cases:
// "not logged in" is an answer here, not an error, so browsers do not log a failed request.
func (h *Handlers) sessionState(w http.ResponseWriter, r *http.Request) {
	authenticated := false
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		authenticated = h.service.ValidateSession(cookie.Value) == nil
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]bool{"authenticated": authenticated})
}

func (h *Handlers) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		h.service.Logout(cookie.Value)
	}
	h.setCookie(w, r, "", time.Unix(0, 0))
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) setCookie(w http.ResponseWriter, r *http.Request, value string, expires time.Time) {
	cookie := &http.Cookie{
		Name:     sessionCookieName,
		Value:    value,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   h.secureCookies || r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	}
	if value == "" {
		cookie.MaxAge = -1
	}
	http.SetCookie(w, cookie)
}
