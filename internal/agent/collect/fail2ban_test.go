package collect

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
	"time"
)

// checkNameRe mirrors the server's own name validation (docs/SPEC.md §4.1,
// internal/telemetry/domain/validate.go's nameRe) so this test fails if a sanitized name would be
// rejected by the real server.
var checkNameRe = regexp.MustCompile(`^[a-z][a-z0-9_.]{0,127}$`)

func writeScript(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestFail2banEventsCollectParsesBanAndUnban(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("fake tail requires a POSIX shell")
	}
	dir := t.TempDir()
	writeScript(t, dir, "tail", "#!/bin/sh\n"+
		`printf '%s\n' '2026-09-22 10:00:00,000 fail2ban.actions [1]: NOTICE  [sshd] Ban 203.0.113.7'`+"\n"+
		`printf '%s\n' '2026-09-22 10:05:00,000 fail2ban.actions [1]: NOTICE  [sshd] Unban 203.0.113.7'`+"\n"+
		"sleep 60\n")
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	f := NewFail2banEvents(t.Context(), "/dev/null")
	var events []Event
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, err := f.Collect()
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
	if events[0].Level != "warn" || events[0].Message != "Ban 203.0.113.7" || events[0].Labels["jail"] != "sshd" {
		t.Fatalf("event[0] = %+v", events[0])
	}
	if events[1].Level != "info" || events[1].Message != "Unban 203.0.113.7" {
		t.Fatalf("event[1] = %+v", events[1])
	}
}

func TestFail2banEventsCollectFailsWhenTailIsMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	f := NewFail2banEvents(t.Context(), "/dev/null")
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := f.Collect(); err != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("want Collect to eventually report an error when tail is missing")
}

func TestFail2banStatusCollectParsesJailsAndBannedCount(t *testing.T) {
	dir := t.TempDir()
	backtick := "`"
	script := "#!/bin/sh\n" +
		"if [ \"$#\" -eq 1 ]; then\n" +
		"  printf 'Status\\n|- Number of jail:\\t2\\n" + backtick + "- Jail list:\\tsshd, nginx-botsearch\\n'\n" +
		"elif [ \"$2\" = \"sshd\" ]; then\n" +
		"  printf 'Status for the jail: sshd\\n|- Filter\\n" + backtick + "- Actions\\n   |- Currently banned:\\t2\\n   " + backtick + "- Banned IP list:\\t1.2.3.4 5.6.7.8\\n'\n" +
		"else\n" +
		"  printf 'Status for the jail: nginx-botsearch\\n" + backtick + "- Actions\\n   |- Currently banned:\\t0\\n'\n" +
		"fi\n"
	writeScript(t, dir, "fail2ban-client", script)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	s := NewFail2banStatus()
	checks, err := s.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(checks) != 2 {
		t.Fatalf("got %d checks, want 2: %+v", len(checks), checks)
	}
	byName := map[string]Check{}
	for _, c := range checks {
		byName[c.Name] = c
	}
	sshd, ok := byName["fail2ban.jail.sshd"]
	if !ok || sshd.Status != "ok" || sshd.Meta["currently_banned"] != 2 {
		t.Fatalf("sshd check = %+v, ok=%v", sshd, ok)
	}
	nb, ok := byName["fail2ban.jail.nginx_botsearch"]
	if !ok || nb.Meta["currently_banned"] != 0 || nb.Meta["jail"] != "nginx-botsearch" {
		t.Fatalf("nginx-botsearch check = %+v, ok=%v", nb, ok)
	}
}

func TestCheckNameSafeSanitizesHyphenatedJailNames(t *testing.T) {
	cases := map[string]string{
		"sshd":                 "sshd",
		"nginx-limit-req":      "nginx_limit_req",
		"infraege-nginx-limit": "infraege_nginx_limit",
	}
	for jail, want := range cases {
		if got := checkNameSafe(jail); got != want {
			t.Errorf("checkNameSafe(%q) = %q, want %q", jail, got, want)
		}
	}
	if !checkNameRe.MatchString(checkNameSafe("infraege-nginx-limit")) {
		t.Fatal("sanitized name must satisfy the server's name regex")
	}
}

func TestFail2banStatusCollectFailsWhenClientIsMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	s := NewFail2banStatus()
	if _, err := s.Collect(); err == nil {
		t.Fatal("want an error when fail2ban-client is missing")
	}
}
