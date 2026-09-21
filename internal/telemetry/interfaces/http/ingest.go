package http

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/platform/apierror"
	"github.com/avatarsik6699/smotryashchiy/internal/platform/ratelimit"
	"github.com/avatarsik6699/smotryashchiy/internal/telemetry/application"
	"github.com/avatarsik6699/smotryashchiy/internal/telemetry/domain"
)

const (
	maxIngestBody = 1 << 20 // 1 MiB
	// A host may send a burst of 20 batches, then 10 per second.
	ingestEvery = 100 * time.Millisecond
	ingestBurst = 20
)

// HostResolver maps a tunnel source address to the enrolled host that owns it.
type HostResolver interface {
	HostForTunnelIP(ctx context.Context, ip netip.Addr) (hostID string, ok bool, err error)
}

// IngestHandlers serves POST /api/ingest. It must be mounted only on the tunnel listener: the host
// is derived from the connection's tunnel source address (WireGuard cryptokey routing guarantees a
// peer can only source its own address), never from anything the client sends.
type IngestHandlers struct {
	service *application.Service
	hosts   HostResolver
	limiter *ratelimit.Limiter
}

// NewIngestHandlers wires IngestHandlers to the ingest service and host resolver.
func NewIngestHandlers(svc *application.Service, hosts HostResolver) *IngestHandlers {
	return &IngestHandlers{service: svc, hosts: hosts, limiter: ratelimit.New(ingestEvery, ingestBurst)}
}

// Register mounts POST /api/ingest on mux.
func (h *IngestHandlers) Register(mux *http.ServeMux) { mux.HandleFunc("POST /api/ingest", h.ingest) }

type countsJSON struct {
	Metrics int `json:"metrics"`
	Checks  int `json:"checks"`
	Events  int `json:"events"`
}

type ingestResponse struct {
	Accepted   countsJSON `json:"accepted"`
	Duplicates countsJSON `json:"duplicates"`
	Replayed   bool       `json:"replayed"`
}

func (h *IngestHandlers) ingest(w http.ResponseWriter, r *http.Request) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		apierror.Write(w, apierror.Unauthorized("unknown tunnel peer"))
		return
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		apierror.Write(w, apierror.Unauthorized("unknown tunnel peer"))
		return
	}
	hostID, ok, err := h.hosts.HostForTunnelIP(r.Context(), ip.Unmap())
	if err != nil {
		apierror.Write(w, err)
		return
	}
	if !ok {
		apierror.Write(w, apierror.Unauthorized("unknown tunnel peer"))
		return
	}
	if !h.limiter.Allow(hostID) {
		w.Header().Set("Retry-After", "1")
		writeStatus(w, http.StatusTooManyRequests, "too many batches")
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxIngestBody))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeStatus(w, http.StatusRequestEntityTooLarge, "batch exceeds 1 MiB")
			return
		}
		apierror.Write(w, apierror.Invalid("could not read request body"))
		return
	}
	batch, err := domain.DecodeBatch(raw)
	if err != nil {
		apierror.Write(w, err)
		return
	}
	result, err := h.service.Ingest(r.Context(), hostID, r.Header.Get("Idempotency-Key"), batch)
	if err != nil {
		apierror.Write(w, err)
		return
	}
	writeJSON(w, ingestResponse{
		Accepted:   countsJSON{Metrics: len(result.Accepted.Metrics), Checks: len(result.Accepted.Checks), Events: len(result.Accepted.Events)},
		Duplicates: countsJSON{Metrics: result.Duplicates.Metrics, Checks: result.Duplicates.Checks, Events: result.Duplicates.Events},
		Replayed:   result.Replayed,
	})
}
