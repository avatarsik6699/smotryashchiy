package db

import (
	"io/fs"
	"path/filepath"
	"testing"
)

func openTemp(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "nested", "test.db")
}

func TestOpenCreatesDirectoryAndEnablesWAL(t *testing.T) {
	sqlDB, err := Open(openTemp(t))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	var mode string
	if err := sqlDB.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal_mode = %q, err = %v", mode, err)
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	sqlDB, err := Open(openTemp(t))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	for i := 0; i < 3; i++ {
		if err := Migrate(sqlDB); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	embedded, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil || len(embedded) == 0 {
		t.Fatalf("embedded migrations = %v, err = %v", embedded, err)
	}
	var applied int
	if err := sqlDB.QueryRow(`SELECT COUNT(1) FROM schema_migrations`).Scan(&applied); err != nil || applied != len(embedded) {
		t.Fatalf("applied = %d, want %d, err = %v", applied, len(embedded), err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO settings (key, value) VALUES ('k', 'v')`); err != nil {
		t.Fatalf("settings table missing: %v", err)
	}
}

func TestMigrateFailsOnClosedDatabase(t *testing.T) {
	sqlDB, err := Open(openTemp(t))
	if err != nil {
		t.Fatal(err)
	}
	_ = sqlDB.Close()
	if err := Migrate(sqlDB); err == nil {
		t.Fatal("expected error")
	}
}
