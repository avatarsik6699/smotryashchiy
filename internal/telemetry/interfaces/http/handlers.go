// Package http exposes the read-only telemetry API (docs/SPEC.md §4.3). Session gating is applied
// by the auth middleware around the whole mux.
package http

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/platform/apierror"
	"github.com/avatarsik6699/smotryashchiy/internal/telemetry/application"
)

// Handlers serves the read endpoints over an application.Service.
type Handlers struct{ service *application.Service }

// NewHandlers wires Handlers to svc.
func NewHandlers(svc *application.Service) *Handlers { return &Handlers{service: svc} }

// Register mounts the routes on mux.
func (h *Handlers) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/metrics", h.metrics)
	mux.HandleFunc("GET /api/checks", h.checks)
	mux.HandleFunc("GET /api/events", h.events)
}

// Response items are explicit structs (never omitempty) so a zero value is always serialized.
type metricJSON struct {
	Host   string            `json:"host"`
	Name   string            `json:"name"`
	TS     time.Time         `json:"ts"`
	Value  float64           `json:"value"`
	Labels map[string]string `json:"labels"`
}

type checkJSON struct {
	Host   string          `json:"host"`
	Name   string          `json:"name"`
	TS     time.Time       `json:"ts"`
	Status string          `json:"status"`
	Meta   json.RawMessage `json:"meta"`
}

type eventJSON struct {
	Host    string            `json:"host"`
	TS      time.Time         `json:"ts"`
	Level   string            `json:"level"`
	Message string            `json:"message"`
	Labels  map[string]string `json:"labels"`
}

type rollupJSON struct {
	Host   string            `json:"host"`
	Name   string            `json:"name"`
	TS     time.Time         `json:"ts"`
	Count  int64             `json:"count"`
	Min    float64           `json:"min"`
	Max    float64           `json:"max"`
	Avg    float64           `json:"avg"`
	Labels map[string]string `json:"labels"`
}

func (h *Handlers) metrics(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	resolution := q.Get("resolution")
	switch resolution {
	case "", application.ResolutionRaw, application.ResolutionHour:
	default:
		apierror.Write(w, apierror.Invalid("resolution must be raw or hour"))
		return
	}
	query := application.MetricQuery{HostID: q.Get("host"), Name: q.Get("name")}
	var err error
	if query.From, err = parseTime(q, "from"); err != nil {
		apierror.Write(w, err)
		return
	}
	if query.To, err = parseTime(q, "to"); err != nil {
		apierror.Write(w, err)
		return
	}
	if query.Limit, err = parseInt(q, "limit"); err != nil {
		apierror.Write(w, err)
		return
	}
	if query.Latest, err = parseBool(q, "latest"); err != nil {
		apierror.Write(w, err)
		return
	}
	if resolution == application.ResolutionHour {
		rollups, err := h.service.MetricRollups(r.Context(), query)
		if err != nil {
			apierror.Write(w, err)
			return
		}
		items := make([]rollupJSON, 0, len(rollups))
		for _, p := range rollups {
			items = append(items, rollupJSON{Host: p.HostID, Name: p.Name, TS: p.TS, Count: p.Count, Min: p.Min, Max: p.Max, Avg: p.Avg, Labels: p.Labels})
		}
		writeJSON(w, map[string]any{"rollups": items})
		return
	}
	points, err := h.service.Metrics(r.Context(), query)
	if err != nil {
		apierror.Write(w, err)
		return
	}
	items := make([]metricJSON, 0, len(points))
	for _, p := range points {
		items = append(items, metricJSON{Host: p.HostID, Name: p.Name, TS: p.TS, Value: p.Value, Labels: p.Labels})
	}
	writeJSON(w, map[string]any{"metrics": items})
}

func (h *Handlers) checks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	states, err := h.service.Checks(r.Context(), application.CheckQuery{HostID: q.Get("host"), Name: q.Get("name")})
	if err != nil {
		apierror.Write(w, err)
		return
	}
	items := make([]checkJSON, 0, len(states))
	for _, c := range states {
		items = append(items, checkJSON{Host: c.HostID, Name: c.Name, TS: c.TS, Status: c.Status, Meta: c.Meta})
	}
	writeJSON(w, map[string]any{"checks": items})
}

func (h *Handlers) events(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, err := parseInt(q, "limit")
	if err != nil {
		apierror.Write(w, err)
		return
	}
	entries, err := h.service.Events(r.Context(), application.EventQuery{HostID: q.Get("host"), Level: q.Get("level"), Limit: limit})
	if err != nil {
		apierror.Write(w, err)
		return
	}
	items := make([]eventJSON, 0, len(entries))
	for _, e := range entries {
		items = append(items, eventJSON{Host: e.HostID, TS: e.TS, Level: e.Level, Message: e.Message, Labels: e.Labels})
	}
	writeJSON(w, map[string]any{"events": items})
}

func parseTime(q url.Values, key string) (*time.Time, error) {
	raw := q.Get(key)
	if raw == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return nil, apierror.Invalid(key + " must be an RFC 3339 timestamp")
	}
	t = t.UTC()
	return &t, nil
}

func parseInt(q url.Values, key string) (int, error) {
	raw := q.Get(key)
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, apierror.Invalid(key + " must be an integer")
	}
	return n, nil
}

func parseBool(q url.Values, key string) (bool, error) {
	raw := q.Get(key)
	if raw == "" {
		return false, nil
	}
	b, err := strconv.ParseBool(raw)
	if err != nil {
		return false, apierror.Invalid(key + " must be a boolean")
	}
	return b, nil
}

func writeJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}
