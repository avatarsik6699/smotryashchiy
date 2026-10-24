package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// script writes an executable shell script and returns its path.
func script(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "operator-command")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func operator(path string, timeout time.Duration) *Operator {
	return &Operator{Path: path, Run: ExecRunner, Timeout: timeout, Interval: time.Hour, Log: slog.New(slog.DiscardHandler)}
}

func TestOperatorExecutesAllowListedKindWithArgsOnStdinAndCleanEnv(t *testing.T) {
	t.Setenv("SECRET_FROM_AGENT_ENV", "leak")
	path := script(t, `kind="$1"; args=$(cat); printf '{"kind":"%s","args":%s,"secret":"%s","argc":%d}' "$kind" "$args" "$SECRET_FROM_AGENT_ENV" "$#"`)
	report := operator(path, 5*time.Second).Execute(context.Background(), Command{ID: "1", Kind: "warn", Args: json.RawMessage(`{"user":"u","reason":"x; rm -rf /"}`)})
	if report.Status != "done" {
		t.Fatalf("report = %+v", report)
	}
	var out struct {
		Kind   string         `json:"kind"`
		Args   map[string]any `json:"args"`
		Secret string         `json:"secret"`
		Argc   int            `json:"argc"`
	}
	if err := json.Unmarshal(report.Result, &out); err != nil {
		t.Fatal(err)
	}
	if out.Kind != "warn" || out.Args["reason"] != "x; rm -rf /" || out.Argc != 1 {
		t.Fatalf("executable saw %+v", out)
	}
	if out.Secret != "" {
		t.Fatal("the executable must run with a clean environment")
	}
}

func TestOperatorRefusesKindsOutsideItsOwnAllowList(t *testing.T) {
	path := script(t, `echo '{"ran":true}'`)
	for _, kind := range []string{"drop_tables", "warn; id", "", "WARN"} {
		report := operator(path, time.Second).Execute(context.Background(), Command{Kind: kind, Args: json.RawMessage(`{}`)})
		if report.Status != "failed" || !strings.Contains(report.Error, "not allowed") {
			t.Fatalf("kind %q: %+v", kind, report)
		}
	}
	report := operator(path, time.Second).Execute(context.Background(), Command{Kind: "warn", Args: json.RawMessage(`["x"]`)})
	if report.Status != "failed" {
		t.Fatalf("non-object args: %+v", report)
	}
}

func TestOperatorReportsRefusalsTimeoutsAndOversizedOutput(t *testing.T) {
	refusal := script(t, `echo '{"error":"invalid arguments"}'; echo 'details on stderr' >&2; exit 2`)
	report := operator(refusal, time.Second).Execute(context.Background(), Command{Kind: "warn", Args: json.RawMessage(`{}`)})
	if report.Status != "failed" || report.Error != "invalid arguments" {
		t.Fatalf("refusal: %+v", report)
	}
	crash := script(t, `echo 'traceback line' >&2; exit 1`)
	report = operator(crash, time.Second).Execute(context.Background(), Command{Kind: "warn", Args: json.RawMessage(`{}`)})
	if report.Status != "failed" || report.Error != "traceback line" {
		t.Fatalf("crash: %+v", report)
	}
	slow := script(t, `sleep 10`)
	started := time.Now()
	report = operator(slow, 300*time.Millisecond).Execute(context.Background(), Command{Kind: "warn", Args: json.RawMessage(`{}`)})
	if report.Status != "failed" || !strings.Contains(report.Error, "timed out") || time.Since(started) > 8*time.Second {
		t.Fatalf("timeout: %+v after %s", report, time.Since(started))
	}
	huge := script(t, `printf '{"x":"'; head -c 70000 /dev/zero | tr '\0' 'a'; printf '"}'`)
	report = operator(huge, 5*time.Second).Execute(context.Background(), Command{Kind: "account_show", Args: json.RawMessage(`{}`)})
	if report.Status != "failed" || !strings.Contains(report.Error, "64 KiB") {
		t.Fatalf("oversized: %+v", report)
	}
	text := script(t, `echo 'not json'`)
	report = operator(text, time.Second).Execute(context.Background(), Command{Kind: "account_show", Args: json.RawMessage(`{}`)})
	if report.Status != "failed" {
		t.Fatalf("not json: %+v", report)
	}
}
