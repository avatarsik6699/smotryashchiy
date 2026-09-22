// Package http exposes the site analytics API (docs/SPEC.md §4i). Session gating for /api/sites*
// is applied by the auth middleware around the whole mux, same as every other /api/ route; the
// exceptions (POST /api/collect, GET /track.js) are registered elsewhere (collect.go, track.go)
// and are public by design.
package http

import (
	"encoding/json"
	"net/http"

	"github.com/avatarsik6699/smotryashchiy/internal/analytics/application"
	"github.com/avatarsik6699/smotryashchiy/internal/analytics/domain"
	"github.com/avatarsik6699/smotryashchiy/internal/platform/apierror"
)

const maxBody = 8192

// Handlers serves the site management/stats routes over an application.Service.
type Handlers struct{ service *application.Service }

// NewHandlers wires Handlers to svc.
func NewHandlers(svc *application.Service) *Handlers { return &Handlers{service: svc} }

// Register mounts the session-gated routes on mux.
func (h *Handlers) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/sites", h.list)
	mux.HandleFunc("POST /api/sites", h.create)
	mux.HandleFunc("DELETE /api/sites/{id}", h.remove)
	mux.HandleFunc("GET /api/sites/{id}/stats", h.stats)
}

func (h *Handlers) list(w http.ResponseWriter, r *http.Request) {
	sites, err := h.service.Sites(r.Context())
	if err != nil {
		apierror.Write(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sites": sites})
}

func (h *Handlers) create(w http.ResponseWriter, r *http.Request) {
	var req domain.NewSite
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(&req); err != nil {
		apierror.Write(w, apierror.Invalid("malformed request body"))
		return
	}
	site, err := h.service.CreateSite(r.Context(), req)
	if err != nil {
		apierror.Write(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, site)
}

func (h *Handlers) remove(w http.ResponseWriter, r *http.Request) {
	if err := h.service.DeleteSite(r.Context(), r.PathValue("id")); err != nil {
		apierror.Write(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) stats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.service.Stats(r.Context(), r.PathValue("id"), r.URL.Query().Get("range"))
	if err != nil {
		apierror.Write(w, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
