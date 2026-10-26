// Package db owns the SQLite connection and the forward-only migration runner. Migration files are
// embedded so the binary stays self-contained.
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// MigrationCount is the number of migrations embedded in this binary.
func MigrationCount() int {
	entries, _ := fs.Glob(migrationsFS, "migrations/*.sql")
	return len(entries)
}

// AppliedMigrations is the number of migrations recorded in an open database (0 for a fresh one).
func AppliedMigrations(sqlDB *sql.DB) (int, error) {
	var exists int
	if err := sqlDB.QueryRow(`SELECT COUNT(1) FROM sqlite_master WHERE type = 'table' AND name = 'schema_migrations'`).Scan(&exists); err != nil {
		return 0, fmt.Errorf("db: inspect schema: %w", err)
	}
	if exists == 0 {
		return 0, nil
	}
	var n int
	if err := sqlDB.QueryRow(`SELECT COUNT(1) FROM schema_migrations`).Scan(&n); err != nil {
		return 0, fmt.Errorf("db: count migrations: %w", err)
	}
	return n, nil
}

// MaxConns bounds the connection pool: one writer at a time plus readers (Change 38).
const MaxConns = 4

// Open opens (creating if needed) the SQLite database at path with pragmas suited to an embedded
// app with one writer at a time. Callers should call Migrate next.
func Open(path string) (*sql.DB, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("db: create data dir: %w", err)
		}
	}
	// temp_store(MEMORY): the container rootfs is read-only, so SQLite has no temp dir to spill
	// sorts or statement journals into (docs/SPEC.md §4f).
	//
	// Change 38: a small pool instead of one connection, so screens read while the ingest, purge or
	// rollup writes (WAL readers never wait for the writer). Writers still take turns: every
	// transaction begins IMMEDIATE (_txlock), so a second writer waits on busy_timeout instead of
	// failing with SQLITE_BUSY when its read lock cannot be upgraded. synchronous(NORMAL) is durable
	// across application crashes in WAL mode; only a power loss can drop the last commits. The page
	// cache (32 MB) and the memory map (128 MB) are per connection limits, not allocations.
	dsn := path + "?_txlock=immediate&_pragma=busy_timeout(10000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)" +
		"&_pragma=temp_store(MEMORY)&_pragma=synchronous(NORMAL)&_pragma=cache_size(-32000)&_pragma=mmap_size(134217728)"
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("db: open %s: %w", path, err)
	}
	sqlDB.SetMaxOpenConns(MaxConns)
	sqlDB.SetMaxIdleConns(MaxConns)
	if err := sqlDB.Ping(); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("db: ping %s: %w", path, err)
	}
	return sqlDB, nil
}

// Migrate applies every embedded migration, in filename order, that is not yet recorded in
// schema_migrations. It is idempotent and safe to call on every startup.
func Migrate(sqlDB *sql.DB) error {
	if _, err := sqlDB.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return fmt.Errorf("db: create schema_migrations: %w", err)
	}
	entries, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		return fmt.Errorf("db: glob migrations: %w", err)
	}
	sort.Strings(entries)
	for _, entry := range entries {
		version := filepath.Base(entry)
		var applied int
		if err := sqlDB.QueryRow(`SELECT COUNT(1) FROM schema_migrations WHERE version = ?`, version).Scan(&applied); err != nil {
			return fmt.Errorf("db: check migration %s: %w", version, err)
		}
		if applied > 0 {
			continue
		}
		contents, err := migrationsFS.ReadFile(entry)
		if err != nil {
			return fmt.Errorf("db: read migration %s: %w", version, err)
		}
		tx, err := sqlDB.Begin()
		if err != nil {
			return fmt.Errorf("db: begin migration %s: %w", version, err)
		}
		if _, err := tx.Exec(string(contents)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("db: apply migration %s: %w", version, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, version); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("db: record migration %s: %w", version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("db: commit migration %s: %w", version, err)
		}
	}
	return nil
}

// optimizeInterval: SQLite suggests running PRAGMA optimize periodically on long-lived
// connections; daily is enough for this write pattern.
const optimizeInterval = 24 * time.Hour

// Optimize refreshes the planner statistics the tables need (Change 38). analysis_limit keeps a
// run to a bounded sample per index, so it is cheap even on a large database.
func Optimize(ctx context.Context, sqlDB *sql.DB) error {
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `PRAGMA analysis_limit=400`); err != nil {
		return fmt.Errorf("db: analysis limit: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `PRAGMA optimize=0x10002`); err != nil {
		return fmt.Errorf("db: optimize: %w", err)
	}
	return nil
}

// RunOptimize optimizes now and then daily until ctx is done; a failure is logged and retried at
// the next tick.
func RunOptimize(ctx context.Context, sqlDB *sql.DB) {
	tick := time.NewTicker(optimizeInterval)
	defer tick.Stop()
	for {
		if err := Optimize(ctx, sqlDB); err != nil && ctx.Err() == nil {
			slog.Error("db optimize failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
