// Package http exposes the operator console (docs/SPEC.md §4j): the session-gated operator API and
// the tunnel-only routes the host's agent uses to claim and answer commands.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"strconv"

	"github.com/avatarsik6699/smotryashchiy/internal/operator/application"
	"github.com/avatarsik6699/smotryashchiy/internal/operator/domain"
	"github.com/avatarsik6699/smotryashchiy/internal/platform/apierror"
	"github.com/avatarsik6699/smotryashchiy/internal/platform/httpserver"
)

const maxBody = 16 << 10

// Guard is what the operator API needs from auth: the step-up password check and the client
// address for the audit log.
type Guard interface {
	StepUp(r *http.Request, password string) error
	ClientIP(r *http.Request) string
}

// Handlers serves the session-gated operator API; the auth middleware wraps the whole mux.
type Handlers struct {
	service *application.Service
	guard   Guard
}

// NewHandlers wires Handlers.
func NewHandlers(svc *application.Service, guard Guard) *Handlers {
	return &Handlers{service: svc, guard: guard}
}

// Register mounts the routes on mux.
func (h *Handlers) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/operator/commands", h.queue)
	mux.HandleFunc("GET /api/operator/commands", h.commands)
	mux.HandleFunc("GET /api/operator/commands/{id}", h.command)
	mux.HandleFunc("GET /api/operator/incidents", h.incidents)
	mux.HandleFunc("POST /api/operator/incidents", h.createIncident)
	mux.HandleFunc("PATCH /api/operator/incidents/{id}", h.updateIncident)
	mux.HandleFunc("DELETE /api/operator/incidents/{id}", h.deleteIncident)
	mux.HandleFunc("GET /api/audit", h.audit)
}

func (h *Handlers) queue(w http.ResponseWriter, r *http.Request) {
	var req domain.NewCommand
	if err := decode(w, r, &req); err != nil {
		apierror.Write(w, err)
		return
	}
	client := h.guard.ClientIP(r)
	if req.Kind.NeedsStepUp() {
		if err := h.guard.StepUp(r, req.Password); err != nil {
			h.service.Refused(r.Context(), req, client, "step-up refused")
			apierror.Write(w, err)
			return
		}
	}
	req.Password = ""
	c, err := h.service.Queue(r.Context(), req, client)
	if err != nil {
		apierror.Write(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"command": c})
}

func (h *Handlers) commands(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	list, err := h.service.Commands(r.Context(), limit)
	if err != nil {
		apierror.Write(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"commands": list})
}

func (h *Handlers) command(w http.ResponseWriter, r *http.Request) {
	c, err := h.service.Command(r.Context(), r.PathValue("id"))
	if err != nil {
		apierror.Write(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"command": c})
}

func (h *Handlers) incidents(w http.ResponseWriter, r *http.Request) {
	list, err := h.service.Incidents(r.Context())
	if err != nil {
		apierror.Write(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"incidents": list})
}

func (h *Handlers) createIncident(w http.ResponseWriter, r *http.Request) {
	var in domain.IncidentInput
	if err := decode(w, r, &in); err != nil {
		apierror.Write(w, err)
		return
	}
	inc, err := h.service.CreateIncident(r.Context(), in, h.guard.ClientIP(r))
	if err != nil {
		apierror.Write(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"incident": inc})
}

func (h *Handlers) updateIncident(w http.ResponseWriter, r *http.Request) {
	var in domain.IncidentInput
	if err := decode(w, r, &in); err != nil {
		apierror.Write(w, err)
		return
	}
	inc, err := h.service.UpdateIncident(r.Context(), r.PathValue("id"), in, h.guard.ClientIP(r))
	if err != nil {
		apierror.Write(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"incident": inc})
}

func (h *Handlers) deleteIncident(w http.ResponseWriter, r *http.Request) {
	if err := h.service.DeleteIncident(r.Context(), r.PathValue("id"), h.guard.ClientIP(r)); err != nil {
		apierror.Write(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) audit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	before, _ := strconv.ParseInt(q.Get("before"), 10, 64)
	entries, err := h.service.Audit(r.Context(), before, limit)
	if err != nil {
		apierror.Write(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

// HostResolver maps a tunnel source address to the enrolled host that owns it.
type HostResolver interface {
	HostForTunnelIP(ctx context.Context, ip netip.Addr) (hostID string, ok bool, err error)
}

// AgentHandlers serves the tunnel-only command routes. Like ingest, the host comes from the
// WireGuard source address, never from anything the agent sends.
type AgentHandlers struct {
	service *application.Service
	hosts   HostResolver
}

// NewAgentHandlers wires AgentHandlers.
func NewAgentHandlers(svc *application.Service, hosts HostResolver) *AgentHandlers {
	return &AgentHandlers{service: svc, hosts: hosts}
}

// Register mounts the agent routes; mount them only on the tunnel mux.
func (h *AgentHandlers) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /agent/commands", h.claim)
	mux.HandleFunc("POST /agent/commands/{id}/result", h.result)
}

func (h *AgentHandlers) host(r *http.Request) (string, error) {
	ip, err := httpserver.TunnelPeer(r)
	if err != nil {
		return "", err
	}
	hostID, ok, err := h.hosts.HostForTunnelIP(r.Context(), ip)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", apierror.Unauthorized("unknown tunnel peer")
	}
	return hostID, nil
}

func (h *AgentHandlers) claim(w http.ResponseWriter, r *http.Request) {
	hostID, err := h.host(r)
	if err != nil {
		apierror.Write(w, err)
		return
	}
	claimed, err := h.service.Claim(r.Context(), hostID)
	if err != nil {
		apierror.Write(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"commands": claimed})
}

func (h *AgentHandlers) result(w http.ResponseWriter, r *http.Request) {
	hostID, err := h.host(r)
	if err != nil {
		apierror.Write(w, err)
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, domain.MaxResultBody))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "result exceeds 80 KiB"})
			return
		}
		apierror.Write(w, apierror.Invalid("could not read request body"))
		return
	}
	var report domain.Report
	if err := json.Unmarshal(raw, &report); err != nil {
		apierror.Write(w, apierror.Invalid("malformed result"))
		return
	}
	if _, err := h.service.Complete(r.Context(), hostID, r.PathValue("id"), report); err != nil {
		apierror.Write(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decode(w http.ResponseWriter, r *http.Request, into any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return apierror.Invalid("malformed request body")
	}
	return nil
}

// writeJSON marks operator responses no-store: they carry account emails and addresses.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Cache-Control", "no-store")
	apierror.JSON(w, status, body)
}
