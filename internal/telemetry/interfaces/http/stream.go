package http

import (
	"context"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/avatarsik6699/smotryashchiy/internal/platform/apierror"
	"github.com/avatarsik6699/smotryashchiy/internal/telemetry/application"
)

const (
	pingInterval = 30 * time.Second
	writeTimeout = 10 * time.Second
)

// StreamHandlers serves the live WebSocket stream (docs/SPEC.md §4.6). Session gating is applied by
// the auth middleware around the whole mux; the library rejects cross-origin upgrades.
type StreamHandlers struct {
	hub          *application.Hub
	pingInterval time.Duration
}

// NewStreamHandlers wires StreamHandlers to hub.
func NewStreamHandlers(hub *application.Hub) *StreamHandlers {
	return &StreamHandlers{hub: hub, pingInterval: pingInterval}
}

// Register mounts GET /api/stream on mux.
func (h *StreamHandlers) Register(mux *http.ServeMux) { mux.HandleFunc("GET /api/stream", h.stream) }

// streamFrame is the server-to-client JSON frame: {"type","host_id","record"}.
type streamFrame struct {
	Type   string `json:"type"`
	HostID string `json:"host_id"`
	Record any    `json:"record"`
}

func (h *StreamHandlers) stream(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	typ := q.Get("type")
	switch typ {
	case "", application.TypeMetric, application.TypeCheck, application.TypeEvent:
	default:
		apierror.Write(w, apierror.Invalid("type must be metric, check or event"))
		return
	}
	// Default AcceptOptions enforce that a present Origin header matches the request host.
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return // Accept has already written the HTTP error response
	}
	defer conn.CloseNow()

	sub := h.hub.Subscribe(q.Get("host"), typ)
	defer h.hub.Unsubscribe(sub)

	// The stream is server-to-client only: CloseRead discards client data frames, answers pings and
	// cancels ctx when the client goes away.
	ctx := conn.CloseRead(r.Context())
	ping := time.NewTicker(h.pingInterval)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-sub.C:
			if !ok {
				if sub.Dropped() {
					_ = conn.Close(websocket.StatusPolicyViolation, "client too slow")
				} else {
					_ = conn.Close(websocket.StatusGoingAway, "server shutting down")
				}
				return
			}
			if err := writeFrame(ctx, conn, msg); err != nil {
				return
			}
		case <-ping.C:
			pingCtx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := conn.Ping(pingCtx)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

func writeFrame(ctx context.Context, conn *websocket.Conn, msg application.Message) error {
	frame := streamFrame{Type: msg.Type, HostID: msg.HostID}
	switch {
	case msg.Metric != nil:
		m := msg.Metric
		frame.Record = metricJSON{Host: msg.HostID, Name: m.Name, TS: m.TS, Value: m.Value, Labels: m.Labels}
	case msg.Check != nil:
		c := msg.Check
		frame.Record = checkJSON{Host: msg.HostID, Name: c.Name, TS: c.TS, Status: c.Status, Meta: c.Meta}
	case msg.Event != nil:
		e := msg.Event
		frame.Record = eventJSON{Host: msg.HostID, TS: e.TS, Level: e.Level, Message: e.Message, Labels: e.Labels}
	default:
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return wsjson.Write(ctx, conn, frame)
}
