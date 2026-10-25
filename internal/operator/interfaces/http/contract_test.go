package http

// Contract tests of the operator console (docs/SPEC.md §4j) end to end: real SQLite, the
// session-gated API with its step-up check, and an enrolled agent claiming and answering commands
// through the real in-process WireGuard tunnel.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/agent"
	"github.com/avatarsik6699/smotryashchiy/internal/agent/collect"
	authapp "github.com/avatarsik6699/smotryashchiy/internal/auth/application"
	authinfra "github.com/avatarsik6699/smotryashchiy/internal/auth/infrastructure"
	authhttp "github.com/avatarsik6699/smotryashchiy/internal/auth/interfaces/http"
	"github.com/avatarsik6699/smotryashchiy/internal/operator/application"
	"github.com/avatarsik6699/smotryashchiy/internal/operator/domain"
	"github.com/avatarsik6699/smotryashchiy/internal/operator/infrastructure"
	"github.com/avatarsik6699/smotryashchiy/internal/platform/db"
	telemetryapp "github.com/avatarsik6699/smotryashchiy/internal/telemetry/application"
	telemetryinfra "github.com/avatarsik6699/smotryashchiy/internal/telemetry/infrastructure"
	telemetryhttp "github.com/avatarsik6699/smotryashchiy/internal/telemetry/interfaces/http"
	"github.com/avatarsik6699/smotryashchiy/internal/transport"
)

const password = "correct-horse-battery-staple-1"

type env struct {
	db      *sql.DB
	store   *infrastructure.Store
	svc     *application.Service
	enroll  *telemetryapp.EnrollmentService
	handler http.Handler
	public  *httptest.Server
	cookie  *http.Cookie
}

func newEnv(t *testing.T) *env {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Migrate(sqlDB); err != nil {
		t.Fatal(err)
	}
	telemetryStore := telemetryinfra.NewStore(sqlDB)
	auth := authapp.NewService(authinfra.NewSettingsStore(sqlDB))
	if err := auth.SetAdminPassword(context.Background(), password); err != nil {
		t.Fatal(err)
	}
	key, err := telemetryStore.ServerKey(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	tunnel, err := transport.NewServer(key, 0, netip.MustParseAddr("10.99.0.1"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tunnel.Close)
	enroll := telemetryapp.NewEnrollmentService(telemetryStore, tunnel, netip.MustParsePrefix("10.99.0.0/16"),
		telemetryapp.ServerInfo{PublicKey: tunnel.PublicKey(), Endpoint: fmt.Sprintf("127.0.0.1:%d", tunnel.UDPPort()), TunnelIP: tunnel.TunnelIP()},
		time.Now)
	store := infrastructure.NewStore(sqlDB)
	svc := application.NewService(store, time.Now, infrastructure.NewID)

	mux := http.NewServeMux()
	authHandlers := authhttp.NewHandlers(auth, authhttp.Options{})
	authHandlers.Register(mux)
	telemetryhttp.NewEnrollHandlers(enroll, nil).Register(mux)
	NewHandlers(svc, authHandlers).Register(mux)
	handler := authhttp.RequireSession(auth)(mux)

	tunnelLn, err := tunnel.Listen(transport.IngestPort)
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(context.Background())
	t.Cleanup(stop)
	ingest := telemetryhttp.NewIngestHandlers(telemetryapp.NewService(telemetryStore), enroll)
	go func() { _ = telemetryhttp.ServeTunnel(ctx, tunnelLn, ingest, NewAgentHandlers(svc, enroll)) }()

	login := httptest.NewRecorder()
	handler.ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"password":"`+password+`"}`)))
	if login.Code != http.StatusNoContent {
		t.Fatalf("login = %d", login.Code)
	}
	public := httptest.NewServer(handler)
	t.Cleanup(public.Close)
	return &env{db: sqlDB, store: store, svc: svc, enroll: enroll, handler: handler, public: public, cookie: login.Result().Cookies()[0]}
}

func (e *env) do(t *testing.T, method, target, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.AddCookie(e.cookie)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// enrolled creates a host and enrolls an agent for it through the real enrollment endpoint.
func (e *env) enrolled(t *testing.T, name string) (agent.Config, string) {
	t.Helper()
	host, secret, _, err := e.enroll.CreateHost(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := agent.Enroll(context.Background(), e.public.URL, secret, filepath.Join(t.TempDir(), "agent.json"))
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	return cfg, host.ID
}

func (e *env) queue(t *testing.T, body string) (int, domain.Command) {
	t.Helper()
	code, out := e.do(t, http.MethodPost, "/api/operator/commands", body)
	var parsed struct {
		Command domain.Command `json:"command"`
	}
	_ = json.Unmarshal([]byte(out), &parsed)
	return code, parsed.Command
}

func (e *env) audit(t *testing.T) []domain.AuditEntry {
	t.Helper()
	code, body := e.do(t, http.MethodGet, "/api/audit?limit=200", "")
	if code != http.StatusOK {
		t.Fatalf("audit = %d %s", code, body)
	}
	var out struct {
		Entries []domain.AuditEntry `json:"entries"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	return out.Entries
}

const user = "0b6f2c1e-5d4a-4c3b-9a8f-7e6d5c4b3a21"

func TestQueueValidatesKindArgsReasonAndStepUp(t *testing.T) {
	e := newEnv(t)
	_, hostID := e.enrolled(t, "kodumka")
	for _, body := range []string{
		`{"host_id":"` + hostID + `","kind":"drop_tables","args":{}}`,
		`{"host_id":"` + hostID + `","kind":"warn","args":{"user":"` + user + `"}}`,
		`{"host_id":"` + hostID + `","kind":"warn","args":{"user":"` + user + `","shell":"rm"},"reason":"x"}`,
		`{"host_id":"` + hostID + `","kind":"warn","args":{"user":"not-a-uuid"},"reason":"x"}`,
		`{"host_id":"` + hostID + `","kind":"warn","args":{"user":"` + user + `","email":"a@b.c"},"reason":"x"}`,
		`{"host_id":"` + hostID + `","kind":"suspend","args":{"user":"` + user + `","days":31},"reason":"x"}`,
		`{"host_id":"` + hostID + `","kind":"warn","args":{"user":"` + user + `"},"reason":"line\nbreak"}`,
		`{"host_id":"` + hostID + `","kind":"lift","args":{"user":"` + user + `"},"reason":"x"}`,
		`{"host_id":"unknown","kind":"account_show","args":{"email":"a@b.c"}}`,
	} {
		if code, _ := e.queue(t, body); code != http.StatusBadRequest {
			t.Fatalf("%s → %d, want 400", body, code)
		}
	}
	// a destructive command needs the admin password again
	terminate := `{"host_id":"` + hostID + `","kind":"terminate","args":{"user":"` + user + `"},"reason":"повтор"`
	if code, _ := e.queue(t, terminate+`}`); code != http.StatusForbidden {
		t.Fatalf("terminate without password = %d", code)
	}
	if code, _ := e.queue(t, terminate+`,"password":"wrong-password-wrong-password"}`); code != http.StatusForbidden {
		t.Fatalf("terminate with a wrong password = %d", code)
	}
	code, c := e.queue(t, terminate+`,"password":"`+password+`"}`)
	if code != http.StatusAccepted || c.Status != domain.StatusQueued {
		t.Fatalf("terminate with the password = %d %+v", code, c)
	}
	var args map[string]any
	_ = json.Unmarshal(c.Args, &args)
	if args["reason"] != "повтор" || args["user"] != user {
		t.Fatalf("host args = %s", c.Args)
	}
	entries := e.audit(t)
	actions := map[string]int{}
	for _, entry := range entries {
		actions[entry.Action]++
		if strings.Contains(string(entry.Detail), password) {
			t.Fatal("the audit log must never hold the password")
		}
	}
	if actions["command.refused"] != 2 || actions["command.queue"] != 1 {
		t.Fatalf("audit actions = %v", actions)
	}
}

// fakeRunner records what the agent ran and answers with a fixed JSON object.
type fakeRunner struct {
	mu   sync.Mutex
	runs []string
}

func (f *fakeRunner) run(_ context.Context, path, kind string, stdin []byte) ([]byte, []byte, error) {
	f.mu.Lock()
	f.runs = append(f.runs, path+" "+kind+" "+string(stdin))
	f.mu.Unlock()
	return []byte(`{"kind":"` + kind + `","ok":true}`), nil, nil
}

func poller(t *testing.T, cfg agent.Config, runner *fakeRunner) (*agent.Operator, func()) {
	t.Helper()
	time.Sleep(40 * time.Millisecond) // handshake spacing between fresh tunnels of one key
	session, err := agent.Dial(context.Background(), cfg)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	op := &agent.Operator{
		Path: "/opt/site/operator-command.sh", Run: runner.run, Timeout: 5 * time.Second, Interval: time.Hour,
		Link: func(context.Context) (agent.Uplink, error) { return session, nil },
		Log:  slog.New(slog.DiscardHandler),
	}
	return op, session.Close
}

func TestAgentClaimsAndAnswersOnlyItsOwnCommandsThroughTheTunnel(t *testing.T) {
	e := newEnv(t)
	cfgA, hostA := e.enrolled(t, "kodumka")
	cfgB, _ := e.enrolled(t, "other")
	code, c := e.queue(t, `{"host_id":"`+hostA+`","kind":"warn","args":{"email":"student@example.com"},"reason":"входы из трёх городов"}`)
	if code != http.StatusAccepted {
		t.Fatalf("queue = %d", code)
	}

	// another host neither receives nor can close it
	runnerB := &fakeRunner{}
	opB, closeB := poller(t, cfgB, runnerB)
	defer closeB()
	if err := opB.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(runnerB.runs) != 0 {
		t.Fatalf("host B ran %v", runnerB.runs)
	}

	runnerA := &fakeRunner{}
	opA, closeA := poller(t, cfgA, runnerA)
	defer closeA()
	if err := opA.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(runnerA.runs) != 1 || !strings.HasPrefix(runnerA.runs[0], "/opt/site/operator-command.sh warn {") ||
		!strings.Contains(runnerA.runs[0], `"reason":"входы из трёх городов"`) {
		t.Fatalf("host A ran %v", runnerA.runs)
	}
	code, body := e.do(t, http.MethodGet, "/api/operator/commands/"+c.ID, "")
	var got struct {
		Command domain.Command `json:"command"`
	}
	_ = json.Unmarshal([]byte(body), &got)
	if code != http.StatusOK || got.Command.Status != domain.StatusDone || !strings.Contains(string(got.Command.Result), `"ok":true`) {
		t.Fatalf("command after the answer = %d %s", code, body)
	}
	// a second poll claims nothing: a command runs once
	if err := opA.Poll(context.Background()); err != nil || len(runnerA.runs) != 1 {
		t.Fatalf("second poll: %v %v", err, runnerA.runs)
	}
	// host B cannot close host A's command, even a running one
	_, _ = e.queue(t, `{"host_id":"`+hostA+`","kind":"account_show","args":{"email":"student@example.com"}}`)
	claimed, err := e.svc.Claim(context.Background(), hostA)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim: %v %v", claimed, err)
	}
	sessionB, err := agent.Dial(context.Background(), cfgB)
	if err == nil {
		defer sessionB.Close()
		err = sessionB.Report(context.Background(), claimed[0].ID, agent.CommandReport{Status: "done", Result: json.RawMessage(`{}`)})
	}
	var status *agent.StatusError
	if err == nil || !errors.As(err, &status) || status.Code != http.StatusNotFound {
		t.Fatalf("host B closing host A's command: %v", err)
	}
	actions := map[string]int{}
	for _, entry := range e.audit(t) {
		actions[entry.Action]++
	}
	if actions["command.queue"] != 2 || actions["command.result"] != 1 {
		t.Fatalf("audit actions = %v", actions)
	}
}

func TestExpiryRetentionAndAppendOnlyAudit(t *testing.T) {
	e := newEnv(t)
	_, hostID := e.enrolled(t, "kodumka")
	ctx := context.Background()
	now := time.Now().UTC()
	stale := domain.Command{ID: "stale-running", HostID: hostID, Kind: domain.KindAccountShow, Args: json.RawMessage(`{"email":"a@b.c"}`), Status: domain.StatusQueued, CreatedAt: now.Add(-7 * time.Minute)}
	if err := e.store.InsertCommand(ctx, stale); err != nil {
		t.Fatal(err)
	}
	if _, err := e.store.Claim(ctx, hostID, now.Add(-6*time.Minute), 5); err != nil {
		t.Fatal(err)
	}
	old := domain.Command{ID: "old-queued", HostID: hostID, Kind: domain.KindAccountShow, Args: json.RawMessage(`{"email":"a@b.c"}`), Status: domain.StatusQueued, CreatedAt: now.Add(-11 * time.Minute)}
	if err := e.store.InsertCommand(ctx, old); err != nil {
		t.Fatal(err)
	}
	if n, err := e.store.Expire(ctx, now); err != nil || n != 2 {
		t.Fatalf("expire = %d %v", n, err)
	}
	if c, _ := e.store.Command(ctx, "old-queued"); c.Status != domain.StatusExpired {
		t.Fatalf("old-queued = %+v", c)
	}
	if c, _ := e.store.Command(ctx, "stale-running"); c.Status != domain.StatusFailed || c.Error != "no result" {
		t.Fatalf("stale-running = %+v", c)
	}
	// results that may name people are cleared after 30 days; the row stays
	if _, err := e.db.Exec(`UPDATE operator_commands SET result_json = '{"email":"a@b.c"}', finished_at = ? WHERE id = 'stale-running'`, now.Add(-31*24*time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	e.svc.Maintain(ctx)
	if c, _ := e.store.Command(ctx, "stale-running"); c.Result != nil || c.Error != "" {
		t.Fatalf("result not cleared: %+v", c)
	}

	// the audit log refuses updates and early deletes; the retention job removes only old rows
	if err := e.store.AppendAudit(ctx, domain.AuditEntry{At: now, Action: "test.recent"}); err != nil {
		t.Fatal(err)
	}
	if err := e.store.AppendAudit(ctx, domain.AuditEntry{At: now.Add(-4 * 365 * 24 * time.Hour), Action: "test.ancient"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(`UPDATE audit_log SET outcome = 'forged'`); err == nil {
		t.Fatal("audit rows must not be updatable")
	}
	if _, err := e.db.Exec(`DELETE FROM audit_log WHERE action = 'test.recent'`); err == nil {
		t.Fatal("recent audit rows must not be deletable")
	}
	e.svc.Maintain(ctx)
	var ancient, recent int
	_ = e.db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action = 'test.ancient'`).Scan(&ancient)
	_ = e.db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action = 'test.recent'`).Scan(&recent)
	if ancient != 0 || recent != 1 {
		t.Fatalf("retention left ancient=%d recent=%d", ancient, recent)
	}
}

func TestIncidentsAreRecordedEditedAndAudited(t *testing.T) {
	e := newEnv(t)
	detected := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second).Format(time.RFC3339)
	code, body := e.do(t, http.MethodPost, "/api/operator/incidents",
		`{"title":"Утечка выгрузки","kind":"leak","detected_at":"`+detected+`","scope":"emails","people_affected":12}`)
	if code != http.StatusCreated {
		t.Fatalf("create = %d %s", code, body)
	}
	var created struct {
		Incident domain.Incident `json:"incident"`
	}
	_ = json.Unmarshal([]byte(body), &created)
	if created.Incident.Kind != domain.IncidentLeak || created.Incident.PeopleAffected == nil || *created.Incident.PeopleAffected != 12 {
		t.Fatalf("created = %+v", created.Incident)
	}
	if code, _ := e.do(t, http.MethodPost, "/api/operator/incidents", `{"title":"","kind":"leak"}`); code != http.StatusBadRequest {
		t.Fatalf("empty title = %d", code)
	}
	primary := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	code, body = e.do(t, http.MethodPatch, "/api/operator/incidents/"+created.Incident.ID,
		`{"title":"Утечка выгрузки","kind":"leak","detected_at":"`+detected+`","rkn_primary_at":"`+primary+`"}`)
	if code != http.StatusOK || !strings.Contains(body, `"rkn_primary_at":"`) {
		t.Fatalf("update = %d %s", code, body)
	}
	if code, _ := e.do(t, http.MethodDelete, "/api/operator/incidents/"+created.Incident.ID, ""); code != http.StatusNoContent {
		t.Fatalf("delete = %d", code)
	}
	if code, body := e.do(t, http.MethodGet, "/api/operator/incidents", ""); code != http.StatusOK || !strings.Contains(body, `"incidents":[]`) {
		t.Fatalf("list = %d %s", code, body)
	}
	actions := map[string]int{}
	for _, entry := range e.audit(t) {
		actions[entry.Action]++
	}
	if actions["incident.create"] != 1 || actions["incident.update"] != 1 || actions["incident.delete"] != 1 {
		t.Fatalf("audit actions = %v", actions)
	}
}

// quiet is a collector with nothing to report, so the run loop only carries operator traffic.
type quiet struct{}

func (quiet) Name() string                       { return "quiet" }
func (quiet) Collect() ([]collect.Sample, error) { return nil, nil }

func TestAgentRunLoopAnswersCommandsOverItsOneTunnel(t *testing.T) {
	e := newEnv(t)
	cfg, hostID := e.enrolled(t, "kodumka")
	code, c := e.queue(t, `{"host_id":"`+hostID+`","kind":"lift","args":{"user":"`+user+`","compensate":true}}`)
	if code != http.StatusAccepted {
		t.Fatalf("queue = %d", code)
	}
	runner := &fakeRunner{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	time.Sleep(40 * time.Millisecond)
	go func() {
		done <- agent.Run(ctx, agent.RunConfig{
			Interval:         time.Second,
			SpoolDir:         t.TempDir(),
			Collectors:       []collect.Collector{quiet{}},
			OperatorCommand:  "/opt/site/operator-command.sh",
			OperatorRunner:   runner.run,
			OperatorInterval: 50 * time.Millisecond,
			Dial:             func(ctx context.Context) (agent.Uplink, error) { return agent.Dial(ctx, cfg) },
			Log:              slog.New(slog.DiscardHandler),
		})
	}()
	deadline := time.Now().Add(15 * time.Second)
	for {
		got, err := e.svc.Command(context.Background(), c.ID)
		if err == nil && got.Status == domain.StatusDone {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("command never finished: %+v %v", got, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("agent did not stop")
	}
	if len(runner.runs) != 1 || !strings.Contains(runner.runs[0], `"compensate":true`) {
		t.Fatalf("runs = %v", runner.runs)
	}
}
