package http

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/platform/apierror"
	"github.com/avatarsik6699/smotryashchiy/internal/telemetry/application"
	"github.com/avatarsik6699/smotryashchiy/internal/telemetry/domain"
)

const maxHostBody = 4096

// HostsHandlers serves the host registry for the UI (docs/SPEC.md §4d): list and create. Session
// gating is applied by the auth middleware around the whole mux.
type HostsHandlers struct {
	service *application.Service
	enroll  *application.EnrollmentService
	// publicURL is the configured server URL agents use to enroll; empty derives it per request.
	publicURL     string
	secureCookies bool
}

// NewHostsHandlers wires HostsHandlers. publicURL may be empty; secureCookies selects https for the
// request-derived fallback when TLS is terminated by a proxy.
func NewHostsHandlers(svc *application.Service, enroll *application.EnrollmentService, publicURL string, secureCookies bool) *HostsHandlers {
	return &HostsHandlers{service: svc, enroll: enroll, publicURL: publicURL, secureCookies: secureCookies}
}

// Register mounts GET and POST /api/hosts on mux.
func (h *HostsHandlers) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/hosts", h.list)
	mux.HandleFunc("POST /api/hosts", h.create)
}

// hostJSON never omits last_seen_at: null is the explicit "never seen" state.
type hostJSON struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastSeenAt *time.Time `json:"last_seen_at"`
}

func toHostJSON(h domain.Host) hostJSON {
	return hostJSON{ID: h.ID, Name: h.Name, CreatedAt: h.CreatedAt, LastSeenAt: h.LastSeenAt}
}

func (h *HostsHandlers) list(w http.ResponseWriter, r *http.Request) {
	hosts, err := h.service.Hosts(r.Context())
	if err != nil {
		apierror.Write(w, err)
		return
	}
	items := make([]hostJSON, 0, len(hosts))
	for _, host := range hosts {
		items = append(items, toHostJSON(host))
	}
	writeJSON(w, map[string]any{"hosts": items})
}

type createHostRequest struct {
	Name string `json:"name"`
}

type createHostResponse struct {
	Host      hostJSON  `json:"host"`
	ServerURL string    `json:"server_url"`
	Secret    string    `json:"secret"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (h *HostsHandlers) create(w http.ResponseWriter, r *http.Request) {
	var req createHostRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxHostBody)).Decode(&req); err != nil {
		apierror.Write(w, apierror.Invalid("malformed request body"))
		return
	}
	host, secret, expires, err := h.enroll.CreateHost(r.Context(), req.Name)
	if err != nil {
		apierror.Write(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store") // the response carries a one-time secret
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(createHostResponse{
		Host:      toHostJSON(host),
		ServerURL: h.serverURL(r),
		Secret:    secret,
		ExpiresAt: expires,
	})
}

// serverURL is what an agent should pass to `agent enroll --server`: the configured public URL, else
// the address this request arrived on.
func (h *HostsHandlers) serverURL(r *http.Request) string {
	if h.publicURL != "" {
		return h.publicURL
	}
	scheme := "http"
	if r.TLS != nil || h.secureCookies {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}
