package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

const readyTimeout = 2 * time.Second

// RegisterHealth mounts GET /healthz (liveness, empty 200) and GET /health/ready (readiness
// reporting the release; 503 when ping fails).
func RegisterHealth(mux *http.ServeMux, release string, ping func(context.Context) error) {
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
		defer cancel()
		status, code := "ok", http.StatusOK
		if err := ping(ctx); err != nil {
			status, code = "unavailable", http.StatusServiceUnavailable
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": status, "release": release})
	})
}
