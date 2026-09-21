// Package http exposes the uptime target API (docs/SPEC.md §4e). Session gating is applied by the auth
// middleware around the whole mux.
package http

import (
	"encoding/json"
	"net/http"

	"github.com/avatarsik6699/smotryashchiy/internal/platform/apierror"
	"github.com/avatarsik6699/smotryashchiy/internal/uptime/application"
	"github.com/avatarsik6699/smotryashchiy/internal/uptime/domain"
)

const maxBody = 8192

// Handlers serves the uptime routes over an application.Service.
type Handlers struct{ service *application.Service }

// NewHandlers wires Handlers to svc.
func NewHandlers(svc *application.Service) *Handlers { return &Handlers{service: svc} }

// Register mounts the routes on mux.
func (h *Handlers) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/uptime", h.list)
	mux.HandleFunc("POST /api/uptime", h.create)
	mux.HandleFunc("DELETE /api/uptime/{id}", h.remove)
}

func (h *Handlers) list(w http.ResponseWriter, r *http.Request) {
	targets, err := h.service.Targets(r.Context())
	if err != nil {
		apierror.Write(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"targets": targets})
}

func (h *Handlers) create(w http.ResponseWriter, r *http.Request) {
	var req domain.NewTarget
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(&req); err != nil {
		apierror.Write(w, apierror.Invalid("malformed request body"))
		return
	}
	t, err := h.service.Create(r.Context(), req)
	if err != nil {
		apierror.Write(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, application.TargetView{Target: t, Latency: []domain.LatencyPoint{}})
}

func (h *Handlers) remove(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Delete(r.Context(), r.PathValue("id")); err != nil {
		apierror.Write(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
