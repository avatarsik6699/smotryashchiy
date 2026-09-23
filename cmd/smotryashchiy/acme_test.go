package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avatarsik6699/smotryashchiy/internal/platform/config"
)

func TestRedirectToHTTPSStripsThePortAndKeepsThePath(t *testing.T) {
	h := redirectToHTTPS()
	for host, want := range map[string]string{
		"monitor.example.com":      "https://monitor.example.com/hosts?x=1",
		"monitor.example.com:8080": "https://monitor.example.com/hosts?x=1",
		"127.0.0.1:8080":           "https://127.0.0.1/hosts?x=1",
	} {
		req := httptest.NewRequest(http.MethodGet, "http://"+host+"/hosts?x=1", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != want {
			t.Errorf("host %q: code=%d location=%q, want 301 to %q", host, rec.Code, rec.Header().Get("Location"), want)
		}
		if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
			t.Errorf("host %q: plain-HTTP redirect sent HSTS %q; only the TLS listener may", host, got)
		}
	}
}

func TestACMECAPicksTheRightEndpoint(t *testing.T) {
	if acmeCA(false) == acmeCA(true) {
		t.Fatal("production and staging must be different CA endpoints")
	}
}

// setupACME's network calls (a real ACME exchange) are exercised manually against a local Pebble
// server, not in unit tests (docs/changes/10-acme-deploy-runbook.md Implementation Notes). This
// covers the part of setupACME that runs before any network call: it must fail fast, before ever
// touching the ACME directory, on an ACMEHTTPAddr it cannot use as AltHTTPPort.
func TestSetupACMERejectsAnUnusableACMEHTTPAddrBeforeAnyNetworkCall(t *testing.T) {
	for _, addr := range []string{"no-port", "", ":not-a-number"} {
		cfg := config.Config{TLSDomain: "monitor.example.com", ACMEHTTPAddr: addr, DBPath: t.TempDir() + "/s.db"}
		_, err := setupACME(context.Background(), cfg, nil)
		if err == nil || !strings.Contains(err.Error(), "ACMEHTTPAddr") && !strings.Contains(err.Error(), "port") {
			t.Errorf("ACMEHTTPAddr=%q: err = %v, want a local validation error naming the address", addr, err)
		}
	}
}
