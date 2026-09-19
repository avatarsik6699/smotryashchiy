// Package db owns the SQLite connection and the forward-only migration runner. Migration files are
// embedded so the binary stays self-contained.
package db

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Open opens (creating if needed) the SQLite database at path with pragmas suited to a
// single-writer embedded app. Callers should call Migrate next.
func Open(path string) (*sql.DB, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("db: create data dir: %w", err)
		}
	}
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)"
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("db: open %s: %w", path, err)
	}
	sqlDB.SetMaxOpenConns(1) // single writer; avoids SQLITE_BUSY under concurrency
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
