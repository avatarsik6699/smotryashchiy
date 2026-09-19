package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func serve(t *testing.T, ping func(context.Context) error, path string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	RegisterHealth(mux, "rel-1", ping)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestHealthzIsEmpty200(t *testing.T) {
	rec := serve(t, func(context.Context) error { return errors.New("db down") }, "/healthz")
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Fatalf("code = %d body = %q", rec.Code, rec.Body.String())
	}
}

func TestReadyReportsReleaseWhenHealthy(t *testing.T) {
	rec := serve(t, func(context.Context) error { return nil }, "/health/ready")
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || body["status"] != "ok" || body["release"] != "rel-1" {
		t.Fatalf("code = %d body = %v", rec.Code, body)
	}
}

func TestReadyIs503WhenDatabaseBroken(t *testing.T) {
	rec := serve(t, func(context.Context) error { return errors.New("db down") }, "/health/ready")
	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusServiceUnavailable || body["status"] != "unavailable" || body["release"] != "rel-1" {
		t.Fatalf("code = %d body = %v", rec.Code, body)
	}
}
