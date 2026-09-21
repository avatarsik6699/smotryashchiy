package http

import (
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/platform/apierror"
	"github.com/avatarsik6699/smotryashchiy/internal/platform/ratelimit"
	"github.com/avatarsik6699/smotryashchiy/internal/telemetry/application"
)

const (
	maxEnrollBody = 4096
	// enrollEvery/enrollBurst allow a burst of 10 attempts per address, then one per 6 seconds.
	enrollEvery = 6 * time.Second
	enrollBurst = 10
)

// EnrollHandlers serves the public agent enrollment endpoint (docs/SPEC.md §4b). The route is
// exempt from session gating (see auth's public paths); the one-time secret is the credential.
type EnrollHandlers struct {
	service *application.EnrollmentService
	limiter *ratelimit.Limiter
}

// NewEnrollHandlers wires EnrollHandlers to svc.
func NewEnrollHandlers(svc *application.EnrollmentService) *EnrollHandlers {
	return &EnrollHandlers{service: svc, limiter: ratelimit.New(enrollEvery, enrollBurst)}
}

// Register mounts POST /api/enroll on mux.
func (h *EnrollHandlers) Register(mux *http.ServeMux) { mux.HandleFunc("POST /api/enroll", h.enroll) }

type enrollRequest struct {
	Secret    string `json:"secret"`
	PublicKey string `json:"public_key"`
}

type enrollResponse struct {
	HostID          string `json:"host_id"`
	TunnelIP        string `json:"tunnel_ip"`
	ServerPublicKey string `json:"server_public_key"`
	ServerEndpoint  string `json:"server_endpoint"`
	ServerTunnelIP  string `json:"server_tunnel_ip"`
}

func (h *EnrollHandlers) enroll(w http.ResponseWriter, r *http.Request) {
	client, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		client = r.RemoteAddr
	}
	if !h.limiter.Allow(client) {
		w.Header().Set("Retry-After", strconv.Itoa(int(enrollEvery/time.Second)))
		writeStatus(w, http.StatusTooManyRequests, "too many enrollment attempts")
		return
	}
	var req enrollRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxEnrollBody)).Decode(&req); err != nil {
		apierror.Write(w, apierror.Invalid("malformed request body"))
		return
	}
	result, err := h.service.Enroll(r.Context(), req.Secret, req.PublicKey)
	if err != nil {
		apierror.Write(w, err)
		return
	}
	writeJSON(w, enrollResponse{
		HostID:          result.HostID,
		TunnelIP:        result.TunnelIP.String(),
		ServerPublicKey: result.Server.PublicKey,
		ServerEndpoint:  result.Server.Endpoint,
		ServerTunnelIP:  result.Server.TunnelIP.String(),
	})
}

func writeStatus(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
