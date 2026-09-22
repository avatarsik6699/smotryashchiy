package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/avatarsik6699/smotryashchiy/internal/platform/db"
)

func TestVersionPrintsReleaseAndMigrationCount(t *testing.T) {
	old := release
	release = "abc123"
	defer func() { release = old }()
	var out bytes.Buffer
	if err := runVersion(&out); err != nil {
		t.Fatal(err)
	}
	want := "smotryashchiy release=abc123 migrations="
	if !strings.HasPrefix(out.String(), want) || db.MigrationCount() < 5 {
		t.Fatalf("output %q, migrations %d", out.String(), db.MigrationCount())
	}
}

func TestHealthURLMapsWildcardsToLoopback(t *testing.T) {
	for in, want := range map[string]string{
		":8080":          "http://127.0.0.1:8080/health/ready",
		"0.0.0.0:9000":   "http://127.0.0.1:9000/health/ready",
		"[::]:9000":      "http://127.0.0.1:9000/health/ready",
		"10.0.0.5:8080":  "http://10.0.0.5:8080/health/ready",
		"localhost:8081": "http://localhost:8081/health/ready",
	} {
		got, err := healthURL(in)
		if err != nil || got != want {
			t.Errorf("healthURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := healthURL("no-port"); err == nil {
		t.Error("an address without a port must be rejected")
	}
}

func serveHealth(t *testing.T, status int) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health/ready" {
			t.Errorf("probed %s, want /health/ready", r.URL.Path)
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://")
}

func TestHealthcheckSucceedsOnlyOn200(t *testing.T) {
	t.Setenv("SMOTRYASHCHIY_ADDR", serveHealth(t, 200))
	var out bytes.Buffer
	if err := runHealthcheck(&out); err != nil || strings.TrimSpace(out.String()) != "ok" {
		t.Fatalf("200: err = %v, out = %q", err, out.String())
	}
	t.Setenv("SMOTRYASHCHIY_ADDR", serveHealth(t, 503))
	if err := runHealthcheck(&bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("503: err = %v, want a not-ready error naming the status", err)
	}
}

func TestHealthcheckFailsWhenNothingListens(t *testing.T) {
	// Close a server first so its port is really closed.
	srv := httptest.NewServer(http.NotFoundHandler())
	dead := strings.TrimPrefix(srv.URL, "http://")
	srv.Close()
	t.Setenv("SMOTRYASHCHIY_ADDR", dead)
	if err := runHealthcheck(&bytes.Buffer{}); err == nil {
		t.Fatal("a closed port must fail the healthcheck")
	}
}

func TestHealthcheckIgnoresUnrelatedProductionSettings(t *testing.T) {
	// The container healthcheck runs with the production environment; it must not re-validate it.
	t.Setenv("SMOTRYASHCHIY_PRODUCTION", "true")
	t.Setenv("SMOTRYASHCHIY_ADDR", serveHealth(t, 200))
	if err := runHealthcheck(&bytes.Buffer{}); err != nil {
		t.Fatalf("healthcheck failed on an incomplete production config: %v", err)
	}
}

func TestBackupAndRestoreCommandsRoundTripThroughTheRealDatabase(t *testing.T) {
	dir := t.TempDir()
	dbPath := dir + "/data/smotryashchiy.db"
	t.Setenv("SMOTRYASHCHIY_DB_PATH", dbPath)
	t.Setenv("SMOTRYASHCHIY_PRODUCTION", "")
	// Create the database the way the server does, with a host that has a one-time enrollment secret.
	if err := runHostCreateForTest(t); err != nil {
		t.Fatal(err)
	}
	bundle := dir + "/b.tar.gz"
	var out bytes.Buffer
	if err := runAdmin([]string{"backup", "--out", bundle}, nil, &out); err != nil {
		t.Fatalf("backup: %v", err)
	}
	if !strings.Contains(out.String(), "keep it private") {
		t.Fatalf("the secrets warning is missing: %q", out.String())
	}
	if err := runAdmin([]string{"backup", "--out", bundle}, nil, &out); err == nil {
		t.Fatal("a second backup to the same file must be refused")
	}
	out.Reset()
	restoreTo := dir + "/restored/smotryashchiy.db"
	t.Setenv("SMOTRYASHCHIY_DB_PATH", restoreTo)
	if err := runAdmin([]string{"restore", "--from", bundle}, nil, &out); err != nil || !strings.Contains(out.String(), "start the server again") {
		t.Fatalf("restore: %v %q", err, out.String())
	}
	if err := runAdmin([]string{"restore", "--from", bundle}, nil, &out); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("restoring over an existing database without --force: %v", err)
	}
	for _, bad := range [][]string{{"backup"}, {"backup", "--out"}, {"restore"}, {"restore", "--from", bundle, "extra"}} {
		if err := runAdmin(bad, nil, &out); err == nil {
			t.Errorf("%v must be a usage error", bad)
		}
	}
}

func runHostCreateForTest(t *testing.T) error {
	t.Helper()
	return runAdmin([]string{"host", "create", "--name", "vps-1"}, nil, &bytes.Buffer{})
}

func TestRestoreReadsTheBundleFromStdin(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SMOTRYASHCHIY_DB_PATH", dir+"/data/smotryashchiy.db")
	t.Setenv("SMOTRYASHCHIY_PRODUCTION", "")
	if err := runHostCreateForTest(t); err != nil {
		t.Fatal(err)
	}
	bundle := dir + "/b.tar.gz"
	if err := runAdmin([]string{"backup", "--out", bundle}, nil, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(bundle)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SMOTRYASHCHIY_DB_PATH", dir+"/piped/smotryashchiy.db")
	var out bytes.Buffer
	if err := runAdmin([]string{"restore", "--from", "-"}, bytes.NewReader(raw), &out); err != nil || !strings.Contains(out.String(), "restored") {
		t.Fatalf("restore from stdin: %v %q", err, out.String())
	}
	if err := runAdmin([]string{"restore", "--from", "-", "--force"}, strings.NewReader("not a bundle"), &out); err == nil {
		t.Fatal("garbage on stdin must be refused")
	}
}
