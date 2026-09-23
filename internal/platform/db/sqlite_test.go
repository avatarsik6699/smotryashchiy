package db

import (
	"context"
	"database/sql/driver"
	"errors"
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

// The production container has a read-only rootfs and no writable temp dir, so SQLite must never
// need a temp file (docs/SPEC.md §4f).
func TestOpenKeepsTempStorageInMemoryOnEveryConnection(t *testing.T) {
	sqlDB, err := Open(openTemp(t))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	ctx := context.Background()
	for i := range 2 {
		conn, err := sqlDB.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var store int
		if err := conn.QueryRowContext(ctx, `PRAGMA temp_store`).Scan(&store); err != nil {
			t.Fatal(err)
		}
		if store != 2 {
			t.Fatalf("connection %d: temp_store = %d, want 2 (MEMORY)", i, store)
		}
		// Discard the connection so the next iteration gets a freshly opened one.
		if err := conn.Raw(func(any) error { return driver.ErrBadConn }); !errors.Is(err, driver.ErrBadConn) {
			t.Fatalf("discard connection: %v", err)
		}
		_ = conn.Close()
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
