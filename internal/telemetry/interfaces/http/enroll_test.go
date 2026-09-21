package http

// End-to-end contract tests for enrollment, the WireGuard tunnel and the tunnel-only ingest
// endpoint (docs/SPEC.md §4b): admin host -> agent enroll -> push through the tunnel -> read API
// and live stream.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/agent"
	"github.com/avatarsik6699/smotryashchiy/internal/platform/apierror"
	"github.com/avatarsik6699/smotryashchiy/internal/telemetry/application"
	"github.com/avatarsik6699/smotryashchiy/internal/transport"
)

// enrolled creates a host, enrolls an agent against the real HTTP handler and returns its config.
func (e *env) enrolled(t *testing.T, name string) (agent.Config, string) {
	t.Helper()
	_, secret, _, err := e.enroll.CreateHost(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	srv := e.server(t)
	path := filepath.Join(t.TempDir(), "agent.json")
	cfg, err := agent.Enroll(context.Background(), srv.URL, secret, path)
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	return cfg, path
}

// handshakeSpacing keeps consecutive fresh tunnels of the same key apart: wireguard-go rounds
// handshake timestamps to ~16.7 ms and rate-limits initiations to one per 20 ms, so a second
// handshake from the same peer inside that window is dropped and only retried after 5 s.
const handshakeSpacing = 40 * time.Millisecond

func push(cfg agent.Config, batch []byte, key string) (agent.Result, error) {
	time.Sleep(handshakeSpacing)
	return agent.Push(context.Background(), cfg, batch, key)
}

func batchJSON(ts time.Time, name string, value float64) []byte {
	raw, _ := json.Marshal(map[string]any{
		"schema_version": "1.0",
		"metrics":        []map[string]any{{"name": name, "ts": ts.Format(time.RFC3339), "value": value, "labels": map[string]string{"core": "0"}}},
	})
	return raw
}

func TestEndToEndEnrollPushReadAndStream(t *testing.T) {
	e := newEnv(t)
	cfg, path := e.enrolled(t, "vps-1")

	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("agent config mode = %v, err = %v; want 0600", info.Mode().Perm(), err)
	}
	if cfg.TunnelIP != "10.99.0.2" || cfg.ServerTunnelIP != "10.99.0.1" || cfg.HostID == "" {
		t.Fatalf("enrollment = %+v", cfg)
	}
	loaded, err := agent.LoadConfig(path)
	if err != nil || loaded != cfg {
		t.Fatalf("config round trip: %+v %v", loaded, err)
	}

	stream, _, err := e.dial(t, e.server(t), "?host="+cfg.HostID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.CloseNow()
	waitFor(t, func() bool { return subscriberCount(e.hub) == 1 })

	res, err := push(cfg, batchJSON(clock, "container.cpu_percent", 0), "key-1")
	if err != nil {
		t.Fatalf("push through tunnel: %v", err)
	}
	if res.Accepted.Metrics != 1 || res.Replayed {
		t.Fatalf("result = %+v", res)
	}

	// The zero value is stored, attributed to the enrolled host and readable through the API.
	code, body := e.get(t, "/api/metrics?host="+cfg.HostID, true)
	if code != 200 || !strings.Contains(body, `"value":0`) || !strings.Contains(body, cfg.HostID) {
		t.Fatalf("read API: %d %s", code, body)
	}
	if f := readFrame(t, stream); f["type"] != "metric" || f["host_id"] != cfg.HostID {
		t.Fatalf("stream frame = %v", f)
	}
	host, _ := e.store.Host(context.Background(), cfg.HostID)
	if host.LastSeenAt == nil {
		t.Fatal("ingest must advance last_seen_at")
	}
}

func TestIngestReplayByKeyAndOverlapByContent(t *testing.T) {
	e := newEnv(t)
	cfg, _ := e.enrolled(t, "vps-1")
	b := batchJSON(clock, "cpu.usage_percent", 7)

	first, err := push(cfg, b, "k1")
	if err != nil || first.Accepted.Metrics != 1 {
		t.Fatalf("first push: %+v %v", first, err)
	}
	replay, err := push(cfg, b, "k1")
	if err != nil || !replay.Replayed || replay.Accepted.Metrics != 0 {
		t.Fatalf("replay: %+v %v", replay, err)
	}
	overlap, err := push(cfg, b, "k2")
	if err != nil || overlap.Replayed || overlap.Accepted.Metrics != 0 || overlap.Duplicates.Metrics != 1 {
		t.Fatalf("overlap: %+v %v", overlap, err)
	}
	if _, body := e.get(t, "/api/metrics?host="+cfg.HostID, true); strings.Count(body, `"cpu.usage_percent"`) != 1 {
		t.Fatalf("record stored more than once: %s", body)
	}
}

func TestIngestRejectsInvalidBatchesWithoutStoring(t *testing.T) {
	e := newEnv(t)
	cfg, _ := e.enrolled(t, "vps-1")
	future := batchJSON(clock.Add(time.Hour), "cpu.usage_percent", 1)
	if _, err := push(cfg, future, "k1"); err == nil || !strings.Contains(err.Error(), "(400)") {
		t.Fatalf("future timestamp: err = %v, want 400", err)
	}
	if _, err := push(cfg, []byte(`{"schema_version":"1.0","metrics":[{"name":"a.b","ts":"2026-09-19T12:00:00Z"}]}`), "k2"); err == nil || !strings.Contains(err.Error(), "(400)") {
		t.Fatalf("missing value must be rejected as absent, not zero: err = %v", err)
	}
	if _, err := push(cfg, []byte(`not json`), "k3"); err == nil || !strings.Contains(err.Error(), "(400)") {
		t.Fatalf("malformed body: err = %v", err)
	}
	if _, body := e.get(t, "/api/metrics", true); !strings.Contains(body, `"metrics":[]`) {
		t.Fatalf("rejected batch stored data: %s", body)
	}
}

func TestIngestIsNotServedOnThePublicListener(t *testing.T) {
	e := newEnv(t)
	req := httptest.NewRequest(http.MethodPost, "/api/ingest", bytes.NewReader(batchJSON(clock, "a.b", 1)))
	req.Header.Set("Idempotency-Key", "k1")
	req.AddCookie(e.cookie) // even an authenticated operator session must not reach ingest here
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("public listener answered ingest with %d, want 404", rec.Code)
	}
	if _, body := e.get(t, "/api/metrics", true); !strings.Contains(body, `"metrics":[]`) {
		t.Fatal("public listener stored data")
	}
}

func TestIngestRejectsAuthorizedTunnelPeerWithoutHost(t *testing.T) {
	e := newEnv(t)
	private, public, err := transport.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	ip := netip.MustParseAddr("10.99.0.50")
	if err := e.tunnel.AddPeer(public, ip); err != nil { // in the tunnel, but no host row owns it
		t.Fatal(err)
	}
	cfg := agent.Config{HostID: "ghost", PrivateKey: private, TunnelIP: ip.String(),
		ServerPublicKey: e.tunnel.PublicKey(), ServerEndpoint: fmtEndpoint(e), ServerTunnelIP: "10.99.0.1"}
	if _, err := push(cfg, batchJSON(clock, "a.b", 1), "k1"); err == nil || !strings.Contains(err.Error(), "(401)") {
		t.Fatalf("err = %v, want 401 for a tunnel peer no host owns", err)
	}
}

func TestHostIdentityComesFromTunnelAddressNotFromRequest(t *testing.T) {
	e := newEnv(t)
	a, _ := e.enrolled(t, "host-a")
	b, _ := e.enrolled(t, "host-b")
	if _, err := push(a, batchJSON(clock, "a.only", 1), "ka"); err != nil {
		t.Fatal(err)
	}
	if _, err := push(b, batchJSON(clock, "b.only", 2), "kb"); err != nil {
		t.Fatal(err)
	}
	_, bodyA := e.get(t, "/api/metrics?host="+a.HostID, true)
	_, bodyB := e.get(t, "/api/metrics?host="+b.HostID, true)
	if !strings.Contains(bodyA, "a.only") || strings.Contains(bodyA, "b.only") || !strings.Contains(bodyB, "b.only") || strings.Contains(bodyB, "a.only") {
		t.Fatalf("records attributed to the wrong host:\nA=%s\nB=%s", bodyA, bodyB)
	}
	if a.TunnelIP == b.TunnelIP {
		t.Fatal("hosts must get distinct tunnel addresses")
	}
}

func fmtEndpoint(e *env) string { return fmt.Sprintf("127.0.0.1:%d", e.tunnel.UDPPort()) }

func postEnroll(t *testing.T, e *env, secret, publicKey string) (int, string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"secret": secret, "public_key": publicKey})
	req := httptest.NewRequest(http.MethodPost, "/api/enroll", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func newPublicKey(t *testing.T) string {
	t.Helper()
	_, pub, err := transport.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	return pub
}

func TestEnrollmentSecretIsSingleUseAndFailuresLookIdentical(t *testing.T) {
	e := newEnv(t)
	_, secret, _, err := e.enroll.CreateHost(context.Background(), "vps-1")
	if err != nil {
		t.Fatal(err)
	}
	if code, body := postEnroll(t, e, secret, newPublicKey(t)); code != 200 {
		t.Fatalf("first enrollment: %d %s", code, body)
	}
	usedCode, usedBody := postEnroll(t, e, secret, newPublicKey(t))
	wrongCode, wrongBody := postEnroll(t, e, "not-the-secret", newPublicKey(t))
	if usedCode != 401 || wrongCode != 401 || usedBody != wrongBody {
		t.Fatalf("used = %d %q, wrong = %d %q; want identical 401 responses", usedCode, usedBody, wrongCode, wrongBody)
	}

	_, expiring, _, err := e.enroll.CreateHost(context.Background(), "vps-2")
	if err != nil {
		t.Fatal(err)
	}
	e.enrollNow = clock.Add(application.EnrollmentTTL + time.Second)
	if code, body := postEnroll(t, e, expiring, newPublicKey(t)); code != 401 || body != wrongBody {
		t.Fatalf("expired secret: %d %q, want the same 401", code, body)
	}
}

func TestEnrollmentStoresOnlyTheSecretHash(t *testing.T) {
	e := newEnv(t)
	_, secret, _, err := e.enroll.CreateHost(context.Background(), "vps-1")
	if err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := e.rawDB.QueryRow(`SELECT secret_hash FROM host_enrollments`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == secret || stored != application.HashSecret(secret) || len(stored) != 64 {
		t.Fatalf("stored = %q, want the SHA-256 hex of the secret", stored)
	}
}

func TestEnrollmentDuplicatePublicKeyIs409AndKeepsSecretUsable(t *testing.T) {
	e := newEnv(t)
	_, first, _, _ := e.enroll.CreateHost(context.Background(), "vps-1")
	_, second, _, _ := e.enroll.CreateHost(context.Background(), "vps-2")
	key := newPublicKey(t)
	if code, body := postEnroll(t, e, first, key); code != 200 {
		t.Fatalf("first: %d %s", code, body)
	}
	if code, _ := postEnroll(t, e, second, key); code != 409 {
		t.Fatalf("duplicate key = %d, want 409", code)
	}
	if code, body := postEnroll(t, e, second, newPublicKey(t)); code != 200 {
		t.Fatalf("secret must stay usable after a 409, got %d %s", code, body)
	}
}

func TestEnrollmentValidatesInputAndDuplicateHostName(t *testing.T) {
	e := newEnv(t)
	if code, _ := postEnroll(t, e, "x", "not-a-key"); code != 400 {
		t.Fatalf("bad public key = %d, want 400", code)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/enroll", strings.NewReader("{"))
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatalf("malformed body = %d, want 400", rec.Code)
	}
	if _, _, _, err := e.enroll.CreateHost(context.Background(), "dup"); err != nil {
		t.Fatal(err)
	}
	_, _, _, err := e.enroll.CreateHost(context.Background(), "dup")
	var apiErr *apierror.Error
	if !errors.As(err, &apiErr) || apiErr.Kind != apierror.KindConflict {
		t.Fatalf("duplicate host name err = %v, want conflict", err)
	}
	if _, _, _, err := e.enroll.CreateHost(context.Background(), "  "); err == nil {
		t.Fatal("blank host name must be rejected")
	}
}

func TestEnrollmentIsRateLimitedPerAddress(t *testing.T) {
	e := newEnv(t)
	last := 0
	for i := 0; i < enrollBurst+1; i++ {
		last, _ = postEnroll(t, e, "guess", newPublicKey(t))
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("attempt %d = %d, want 429", enrollBurst+1, last)
	}
}

type stubResolver struct {
	hostID string
	ok     bool
}

func (s stubResolver) HostForTunnelIP(context.Context, netip.Addr) (string, bool, error) {
	return s.hostID, s.ok, nil
}

func TestIngestHandlerLimitsBodySizeAndRate(t *testing.T) {
	e := newEnv(t)
	h := NewIngestHandlers(e.svc, stubResolver{hostID: e.host.ID, ok: true})
	mux := http.NewServeMux()
	h.Register(mux)
	post := func(body []byte) int {
		req := httptest.NewRequest(http.MethodPost, "/api/ingest", bytes.NewReader(body))
		req.RemoteAddr = "10.99.0.2:40000"
		req.Header.Set("Idempotency-Key", "k")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := post(bytes.Repeat([]byte("a"), maxIngestBody+1)); code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize body = %d, want 413", code)
	}
	limited := false
	for i := 0; i < ingestBurst+5; i++ {
		if post([]byte("{}")) == http.StatusTooManyRequests {
			limited = true
		}
	}
	if !limited {
		t.Fatal("expected 429 once a host exceeds its batch rate")
	}
}

type recordingAuthorizer struct{ added map[string]netip.Addr }

func (r *recordingAuthorizer) AddPeer(key string, ip netip.Addr) error {
	r.added[key] = ip
	return nil
}

func TestRestorePeersReplaysStoredPeersIntoTheTunnel(t *testing.T) {
	e := newEnv(t)
	cfgA, _ := e.enrolled(t, "host-a")
	cfgB, _ := e.enrolled(t, "host-b")
	rec := &recordingAuthorizer{added: map[string]netip.Addr{}}
	svc := application.NewEnrollmentService(e.store, rec, netip.MustParsePrefix("10.99.0.0/16"), application.ServerInfo{}, func() time.Time { return clock })
	n, err := svc.RestorePeers(context.Background())
	if err != nil || n != 2 || len(rec.added) != 2 {
		t.Fatalf("restored %d peers (%v), err = %v; want 2", n, rec.added, err)
	}
	for _, ip := range rec.added {
		if ip.String() != cfgA.TunnelIP && ip.String() != cfgB.TunnelIP {
			t.Fatalf("unexpected restored address %s", ip)
		}
	}
}

func TestServerKeyIsGeneratedOnceAndStable(t *testing.T) {
	e := newEnv(t)
	first, err := e.store.ServerKey(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, _ := e.store.ServerKey(context.Background())
	if first == "" || first != second {
		t.Fatalf("server key changed between calls: %q vs %q", first, second)
	}
}

func TestAgentEnrollRefusesToOverwriteExistingConfig(t *testing.T) {
	e := newEnv(t)
	_, path := e.enrolled(t, "vps-1")
	before, _ := os.ReadFile(path)
	_, secret, _, _ := e.enroll.CreateHost(context.Background(), "vps-2")
	if _, err := agent.Enroll(context.Background(), e.server(t).URL, secret, path); err == nil {
		t.Fatal("enrolling over an existing config would destroy its private key")
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
		t.Fatal("existing agent config was modified")
	}
}
