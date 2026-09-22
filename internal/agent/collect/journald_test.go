package collect

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// fakeJournalctl points PATH at a tiny script standing in for journalctl -f -o json: it prints
// the given lines once, then blocks (like a real -f tail with no more activity) until killed.
func fakeJournalctl(t *testing.T, lines []string) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("journalctl fake requires a POSIX shell")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\n"
	for _, l := range lines {
		script += "printf '%s\\n' '" + l + "'\n"
	}
	script += "sleep 60\n"
	path := filepath.Join(dir, "journalctl")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}

func TestJournaldCollectDrainsBufferedLinesWithLevelsAndUnit(t *testing.T) {
	fakeJournalctl(t, []string{
		`{"MESSAGE":"disk nearly full","PRIORITY":"4","_SYSTEMD_UNIT":"smotryashchiy.service","__REALTIME_TIMESTAMP":"1758537600000000"}`,
		`{"MESSAGE":"started ok","PRIORITY":"6","_SYSTEMD_UNIT":"smotryashchiy.service","__REALTIME_TIMESTAMP":"1758537601000000"}`,
	})
	j := NewJournald(t.Context())

	var events []Event
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, err := j.Collect()
		if err != nil {
			t.Fatalf("Collect: %v", err)
		}
		events = append(events, got...)
		if len(events) >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2: %+v", len(events), events)
	}
	if events[0].Level != "warn" || events[0].Message != "disk nearly full" || events[0].Labels["unit"] != "smotryashchiy.service" {
		t.Fatalf("event[0] = %+v", events[0])
	}
	if events[1].Level != "info" {
		t.Fatalf("event[1].Level = %q, want info", events[1].Level)
	}
	if events[0].TS.IsZero() || !events[0].TS.Before(events[1].TS) {
		t.Fatalf("timestamps not parsed/ordered: %v, %v", events[0].TS, events[1].TS)
	}
}

func TestJournaldCollectDropsRoutineHealthCheckLines(t *testing.T) {
	fakeJournalctl(t, []string{
		`{"MESSAGE":"INFO: 127.0.0.1:1 - \"GET /health/ready HTTP/1.1\" 200 OK","PRIORITY":"6","_SYSTEMD_UNIT":"docker.service","__REALTIME_TIMESTAMP":"1758537600000000"}`,
		`{"MESSAGE":"real event","PRIORITY":"6","_SYSTEMD_UNIT":"docker.service","__REALTIME_TIMESTAMP":"1758537601000000"}`,
	})
	j := NewJournald(t.Context())

	var events []Event
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, err := j.Collect()
		if err != nil {
			t.Fatalf("Collect: %v", err)
		}
		events = append(events, got...)
		if len(events) >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(events) != 1 || events[0].Message != "real event" {
		t.Fatalf("got %+v, want only the non-health-check event", events)
	}
}

func TestJournaldLevelMapping(t *testing.T) {
	cases := map[string]string{"0": "error", "3": "error", "4": "warn", "5": "info", "6": "info", "7": "info", "garbage": "info"}
	for priority, want := range cases {
		if got := journaldLevel(priority); got != want {
			t.Errorf("journaldLevel(%q) = %q, want %q", priority, got, want)
		}
	}
}

func TestJournaldCollectFailsWhenJournalctlIsMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // empty PATH: journalctl cannot be found
	j := NewJournald(t.Context())
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := j.Collect(); err != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("want Collect to eventually report an error when journalctl is missing")
}
