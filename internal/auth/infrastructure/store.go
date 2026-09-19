// Package infrastructure persists auth state in the platform's SQLite settings table.
package infrastructure

import (
	"context"
	"database/sql"
	"errors"
)

const adminPasswordHashKey = "admin_password_hash"

// SettingsStore implements application.PasswordStore on the settings table.
type SettingsStore struct{ db *sql.DB }

// NewSettingsStore returns a store backed by db (migrations must have run).
func NewSettingsStore(db *sql.DB) *SettingsStore { return &SettingsStore{db: db} }

// Hash returns the stored admin password hash; ok is false when none exists.
func (s *SettingsStore) Hash(ctx context.Context) (string, bool, error) {
	var hash string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, adminPasswordHashKey).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return hash, true, nil
}

// SetHash stores or replaces the admin password hash.
func (s *SettingsStore) SetHash(ctx context.Context, hash string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, adminPasswordHashKey, hash)
	return err
}
