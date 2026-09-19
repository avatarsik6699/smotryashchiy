package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadPasswordStripsOneTrailingNewline(t *testing.T) {
	for in, want := range map[string]string{"secret\n": "secret", "secret\r\n": "secret", "secret": "secret", "a\n\n": "a\n"} {
		got, err := readPassword(strings.NewReader(in))
		if err != nil || got != want {
			t.Errorf("%q -> %q, %v (want %q)", in, got, err, want)
		}
	}
}

func TestReadPasswordRejectsOversizedInput(t *testing.T) {
	if _, err := readPassword(strings.NewReader(strings.Repeat("a", 5000))); err == nil {
		t.Fatal("expected error")
	}
}

func TestAdminRejectsPasswordFromArguments(t *testing.T) {
	err := runAdmin([]string{"set-password", "hunter2hunter2hunter2hunter2"}, strings.NewReader(""), &bytes.Buffer{})
	if err == nil {
		t.Fatal("password argument must be refused")
	}
}

func TestAdminSetPasswordStoresHashFromStdin(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "admin.db")
	t.Setenv("SMOTRYASHCHIY_DB_PATH", dbPath)
	t.Setenv("SMOTRYASHCHIY_PRODUCTION", "")
	password := "correct-horse-battery-staple-1"
	var out bytes.Buffer
	if err := runAdmin([]string{"set-password"}, strings.NewReader(password+"\n"), &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), password) {
		t.Fatal("password must never be printed")
	}
	raw, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(password)) {
		t.Fatal("plaintext password found in database file")
	}
	if err := runAdmin([]string{"set-password"}, strings.NewReader("short\n"), &out); err == nil {
		t.Fatal("short password must be refused")
	}
}

func TestRunDispatch(t *testing.T) {
	null, _ := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	defer null.Close()
	if code := run(nil, null, null, null); code != 2 {
		t.Errorf("no args = %d", code)
	}
	if code := run([]string{"bogus"}, null, null, null); code != 2 {
		t.Errorf("bogus = %d", code)
	}
	if code := run([]string{"agent"}, null, null, null); code != 2 {
		t.Errorf("agent stub = %d", code)
	}
}
