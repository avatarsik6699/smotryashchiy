package backup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVacuumCompactsAStoppedDatabaseAndRefusesAnOpenOne(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "live.db")
	server := newDB(t, path)
	if _, err := server.Exec(`CREATE TABLE junk (v TEXT)`); err != nil {
		t.Fatal(err)
	}
	for range 2000 {
		if _, err := server.Exec(`INSERT INTO junk VALUES (?)`, strings.Repeat("x", 500)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := server.Exec(`DELETE FROM junk; INSERT INTO hosts (id, name, created_at) VALUES ('keep', 'keep', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := Vacuum(ctx, path, false, clock); err == nil || !strings.Contains(err.Error(), "in use") {
		t.Fatalf("vacuum of an open database = %v, want the in-use refusal", err)
	}
	server.Close()
	res, err := Vacuum(ctx, path, false, clock)
	if err != nil {
		t.Fatal(err)
	}
	if res.After >= res.Before || res.PreviousDB != "" {
		t.Fatalf("result = %+v, want a smaller file and the old one removed", res)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	for _, e := range entries {
		if strings.Contains(e.Name(), "vacuum") {
			t.Errorf("left behind %s", e.Name())
		}
	}
	reopened := newDB(t, path)
	var name string
	if err := reopened.QueryRow(`SELECT name FROM hosts WHERE id = 'keep'`).Scan(&name); err != nil || name != "keep" {
		t.Fatalf("data after vacuum: %q, %v", name, err)
	}
	reopened.Close()
	kept, err := Vacuum(ctx, path, true, clock)
	if err != nil || kept.PreviousDB == "" {
		t.Fatalf("vacuum --keep = %+v, %v", kept, err)
	}
	if _, err := os.Stat(kept.PreviousDB); err != nil {
		t.Fatalf("kept file: %v", err)
	}
}
