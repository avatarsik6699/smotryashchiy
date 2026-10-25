package http

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/analytics/application"
	"github.com/avatarsik6699/smotryashchiy/internal/analytics/domain"
	"github.com/avatarsik6699/smotryashchiy/internal/platform/ratelimit"
)

const maxCollectBody = 4096

// collectRateLimit: one beacon per second sustained, 5-burst per source IP — enough for a
// legitimate visitor's SPA route changes, bounding abuse of the one write path with no auth
// (docs/SPEC.md §4i).
var collectRateLimit = struct {
	every time.Duration
	burst int
}{time.Second, 5}

// CollectHandlers serves the public, unauthenticated pageview beacon endpoint. It is registered
// outside auth's session gating (see internal/auth/interfaces/http/middleware.go's
// publicAPIPaths) — anonymous visitor browsers, not an operator session, call this.
type CollectHandlers struct {
	service        *application.Service
	limiter        *ratelimit.Limiter
	trustedProxies []netip.Prefix
}

// NewCollectHandlers wires CollectHandlers to svc. trustedProxies mirrors the login limiter's own
// (config.TrustedProxyCIDRs): only those peers may supply X-Forwarded-For.
func NewCollectHandlers(svc *application.Service, trustedProxies []netip.Prefix) *CollectHandlers {
	return &CollectHandlers{service: svc, limiter: ratelimit.New(collectRateLimit.every, collectRateLimit.burst), trustedProxies: trustedProxies}
}

// Register mounts the public collect route on mux.
func (h *CollectHandlers) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/collect", h.collect)
}

// collect always answers 204, regardless of outcome (docs/SPEC.md §4i): a rate-limited request, an
// unknown site, a bot User-Agent or a malformed body must be indistinguishable from a successfully
// stored pageview to anything probing this endpoint.
func (h *CollectHandlers) collect(w http.ResponseWriter, r *http.Request) {
	ip := h.clientIP(r)
	// The limit comes first: a limited request must cost no database work (docs/SPEC.md §4i).
	if !h.limiter.Allow(ratelimit.ClientKey(ip)) {
		h.service.CountCollect(application.OutcomeRateLimited)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	h.setCORS(w, r)
	var b domain.Beacon
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxCollectBody)).Decode(&b); err != nil {
		h.service.CountCollect(application.OutcomeMalformed)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if _, err := h.service.Collect(r.Context(), b, ip, r.UserAgent(), originHostname(r)); err != nil {
		// The visitor's IP and User-Agent never reach the log (docs/SPEC.md §4i).
		slog.Error("analytics collect failed", "err", err)
	}
	w.WriteHeader(http.StatusNoContent)
}

// setCORS allows the response to be read cross-origin only when the request's Origin is a
// registered site's domain or www. plus it (the same rule that decides storage) — not a security boundary (a non-browser client ignores CORS
// entirely), just keeps a legitimate embed's fetch fallback console-clean.
func (h *CollectHandlers) setCORS(w http.ResponseWriter, r *http.Request) {
	host := originHostname(r)
	if host == "" {
		return
	}
	sites, err := h.service.Sites(r.Context())
	if err != nil {
		return
	}
	for _, site := range sites {
		if application.OriginMatchesSite(host, site.Domain) {
			w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
			w.Header().Set("Vary", "Origin")
			return
		}
	}
}

// originHostname is the request Origin's hostname, or "" when absent, "null" or unparsable.
func originHostname(r *http.Request) string {
	u, err := url.Parse(r.Header.Get("Origin"))
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func (h *CollectHandlers) clientIP(r *http.Request) string {
	return ratelimit.ClientIP(r, h.trustedProxies)
}
