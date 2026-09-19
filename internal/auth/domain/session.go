// Package domain holds the Session entity and the admin-password hashing logic, per
// docs/SPEC.md §6: single admin password, bcrypt hash in SQLite, HttpOnly session cookie.
package domain

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/avatarsik6699/smotryashchiy/internal/platform/apierror"
)

// Session is one issued login session.
type Session struct {
	Token     string
	ExpiresAt time.Time
}

// Expired reports whether the session's TTL has elapsed as of now.
func (s Session) Expired(now time.Time) bool {
	return now.After(s.ExpiresAt)
}

// ErrInvalidCredentials is returned when a login attempt's password doesn't match the stored hash.
var ErrInvalidCredentials = apierror.Unauthorized("invalid password")

// ErrSessionInvalid is returned when a session token is missing, unknown, or expired.
var ErrSessionInvalid = apierror.Unauthorized("session invalid or expired")

// HashPassword bcrypt-hashes a plaintext password for storage.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// VerifyPassword reports whether password matches hash (as produced by HashPassword).
func VerifyPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// NewToken generates a random, URL-safe session token.
func NewToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// GeneratePassword generates a random admin password for first-run bootstrap (docs/SPEC.md §6).
func GeneratePassword() (string, error) {
	buf := make([]byte, 18)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
