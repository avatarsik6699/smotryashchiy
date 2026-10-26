// Package http exposes the site analytics API (docs/SPEC.md §4i). Session gating for /api/sites*
// is applied by the auth middleware around the whole mux, same as every other /api/ route; the
// exceptions (POST /api/collect, GET /track.js) are registered elsewhere (collect.go, track.go)
// and are public by design.
package http

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/avatarsik6699/smotryashchiy/internal/analytics/application"
	"github.com/avatarsik6699/smotryashchiy/internal/analytics/domain"
	"github.com/avatarsik6699/smotryashchiy/internal/platform/apierror"
)

const maxBody = 8192

// maxQueryBody bounds a query request: filters with long values are the large part.
const maxQueryBody = 64 << 10

// Handlers serves the site management, stats and query routes.
type Handlers struct {
	service *application.Service
	queries *application.Queries
}

// NewHandlers wires Handlers to the site service and the query service.
func NewHandlers(svc *application.Service, queries *application.Queries) *Handlers {
	return &Handlers{service: svc, queries: queries}
}

// Register mounts the session-gated routes on mux.
func (h *Handlers) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/sites", h.list)
	mux.HandleFunc("POST /api/sites", h.create)
	mux.HandleFunc("DELETE /api/sites/{id}", h.remove)
	mux.HandleFunc("GET /api/sites/{id}/stats", h.stats)
	mux.HandleFunc("POST /api/sites/{id}/query", h.query)
	mux.HandleFunc("GET /api/sites/{id}/realtime", h.realtime)
	mux.HandleFunc("GET /api/collect/stats", h.collectStats)
}

// collectStats reports collect outcomes since the server started (Change 26).
func (h *Handlers) collectStats(w http.ResponseWriter, _ *http.Request) {
	apierror.JSON(w, http.StatusOK, map[string]any{"outcomes": h.service.CollectCounts()})
}

func (h *Handlers) list(w http.ResponseWriter, r *http.Request) {
	sites, err := h.service.Sites(r.Context())
	if err != nil {
		apierror.Write(w, err)
		return
	}
	apierror.JSON(w, http.StatusOK, map[string]any{"sites": sites})
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
	apierror.JSON(w, http.StatusCreated, site)
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
	apierror.JSON(w, http.StatusOK, stats)
}

// query answers the query API (Change 36) as JSON, or as CSV with "format":"csv".
func (h *Handlers) query(w http.ResponseWriter, r *http.Request) {
	var req domain.Query
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxQueryBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		apierror.Write(w, apierror.Invalid("malformed query: "+err.Error()))
		return
	}
	result, err := h.queries.Query(r.Context(), r.PathValue("id"), req)
	if err != nil {
		apierror.Write(w, err)
		return
	}
	if req.Format != "csv" {
		apierror.JSON(w, http.StatusOK, result)
		return
	}
	metrics := req.Metrics
	if len(metrics) == 0 {
		metrics = []string{domain.MetricVisitors}
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="analytics-`+result.DateRange[0]+`-`+result.DateRange[1]+`.csv"`)
	w.WriteHeader(http.StatusOK)
	_ = writeCSV(w, req.Dimension, metrics, result)
}

// writeCSV writes one line per row: the value, the metrics, then the comparison's.
func writeCSV(w http.ResponseWriter, dimension string, metrics []string, result domain.QueryResult) error {
	out := csv.NewWriter(w)
	if dimension == "" {
		dimension = "total"
	}
	header := []string{dimension}
	header = append(header, metrics...)
	_, timeline := domain.IsTimeDimension(dimension)
	if result.CompareRange != nil {
		if timeline {
			header = append(header, "compare_"+dimension)
		}
		for _, m := range metrics {
			header = append(header, "compare_"+m)
		}
	}
	if err := out.Write(header); err != nil {
		return err
	}
	for _, row := range result.Rows {
		line := []string{csvCell(row.Value)}
		for _, m := range metrics {
			line = append(line, strconv.FormatFloat(row.Metrics[m], 'f', -1, 64))
		}
		if result.CompareRange != nil {
			if timeline {
				line = append(line, csvCell(row.CompareValue))
			}
			for _, m := range metrics {
				if row.Compare == nil {
					line = append(line, "")
					continue
				}
				line = append(line, strconv.FormatFloat(row.Compare[m], 'f', -1, 64))
			}
		}
		if err := out.Write(line); err != nil {
			return err
		}
	}
	out.Flush()
	return out.Error()
}

// csvCell defuses formula injection: a spreadsheet runs a cell that starts with = + - @ (or a tab or
// carriage return before one), so such a cell gets a leading apostrophe.
func csvCell(v string) string {
	if v != "" && strings.ContainsRune("=+-@\t\r", rune(v[0])) {
		return "'" + v
	}
	return v
}

// realtime counts visitors with a pageview in the last five minutes.
func (h *Handlers) realtime(w http.ResponseWriter, r *http.Request) {
	n, err := h.queries.Realtime(r.Context(), r.PathValue("id"))
	if err != nil {
		apierror.Write(w, err)
		return
	}
	apierror.JSON(w, http.StatusOK, map[string]int{"visitors": n})
}
