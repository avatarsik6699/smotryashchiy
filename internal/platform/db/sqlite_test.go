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

func TestMetricLatestBackfillAndTriggers(t *testing.T) {
	path := openTemp(t)
	sqlDB, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`CREATE TABLE schema_migrations (
		version TEXT PRIMARY KEY, applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		t.Fatal(err)
	}
	entries, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if filepath.Base(entry) >= "0008_" {
			continue
		}
		contents, err := migrationsFS.ReadFile(entry)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := sqlDB.Exec(string(contents)); err != nil {
			t.Fatalf("apply %s: %v", entry, err)
		}
		if _, err := sqlDB.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, filepath.Base(entry)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := sqlDB.Exec(`INSERT INTO hosts (id, name, created_at) VALUES ('h1', 'host', 0)`); err != nil {
		t.Fatal(err)
	}
	insert := func(labels string, ts int, value float64) {
		t.Helper()
		_, err := sqlDB.Exec(`INSERT INTO metrics
			(host_id, name, labels_json, ts, value, schema_version)
			VALUES ('h1', 'memory', ?, ?, ?, '1.0') ON CONFLICT DO NOTHING`, labels, ts, value)
		if err != nil {
			t.Fatal(err)
		}
	}
	api, web := `{"container":"api"}`, `{"container":"web"}`
	insert(api, 100, 0)
	insert(api, 200, 2)
	insert(web, 150, 3)
	if err := Migrate(sqlDB); err != nil {
		t.Fatalf("backfill migration: %v", err)
	}
	assertLatest := func(labels string, wantTS int, wantValue float64) {
		t.Helper()
		var ts int
		var value float64
		err := sqlDB.QueryRow(`SELECT ts, value FROM metric_latest
			WHERE host_id = 'h1' AND name = 'memory' AND labels_json = ?`, labels).Scan(&ts, &value)
		if err != nil || ts != wantTS || value != wantValue {
			t.Fatalf("latest %s = (%d,%g), err=%v; want (%d,%g)", labels, ts, value, err, wantTS, wantValue)
		}
	}
	assertLatest(api, 200, 2)
	assertLatest(web, 150, 3)
	insert(api, 200, 99) // replayed point cannot change the value
	insert(api, 120, 1)  // late older point cannot replace the latest
	assertLatest(api, 200, 2)
	insert(api, 250, 0) // measured zero must remain a real value
	assertLatest(api, 250, 0)
	tx, err := sqlDB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO metrics (host_id, name, labels_json, ts, value, schema_version)
		VALUES ('h1', 'memory', ?, 300, 10, '1.0')`, api); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	assertLatest(api, 250, 0)
	for _, ts := range []int{250, 200, 120, 100} {
		if _, err := sqlDB.Exec(`DELETE FROM metrics
			WHERE host_id = 'h1' AND name = 'memory' AND labels_json = ? AND ts = ?`, api, ts); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM metric_latest WHERE labels_json = ?`, api).Scan(&count); err != nil || count != 0 {
		t.Fatalf("deleted series cache rows = %d, err=%v", count, err)
	}
	if _, err := sqlDB.Exec(`DELETE FROM hosts WHERE id = 'h1'`); err != nil {
		t.Fatalf("host cascade: %v", err)
	}
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM metric_latest`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("host cache rows = %d, err=%v", count, err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := Migrate(reopened); err != nil {
		t.Fatalf("restart migration: %v", err)
	}
}
