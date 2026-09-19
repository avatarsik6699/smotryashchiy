// Package application holds the auth use-cases: admin-password bootstrap, Login/Logout and
// in-memory session validation, per docs/SPEC.md §3.
package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/auth/domain"
)

const (
	sessionTTL          = 24 * time.Hour
	minAdminPasswordLen = 24
	maxAdminPasswordLen = 1024
)

// ErrAdminPasswordNotInitialized makes production startup fail closed without logging a secret.
var ErrAdminPasswordNotInitialized = errors.New("auth: admin password is not initialized")

// PasswordStore persists the admin password hash. ok is false when none has been stored yet.
type PasswordStore interface {
	Hash(ctx context.Context) (hash string, ok bool, err error)
	SetHash(ctx context.Context, hash string) error
}

// Service implements admin-password bootstrap, Login and session validation. Sessions are
// ephemeral: a restart just forces a re-login.
type Service struct {
	store PasswordStore
	now   func() time.Time

	mu       sync.Mutex
	sessions map[string]domain.Session
}

// NewService wires a Service to its PasswordStore.
func NewService(store PasswordStore) *Service {
	return &Service{store: store, now: time.Now, sessions: map[string]domain.Session{}}
}

// EnsureAdminPassword is the development-only first-run bootstrap: when no hash is stored it
// generates a random password, stores its hash and returns the plaintext once. It returns ""
// when a password is already configured.
func (s *Service) EnsureAdminPassword(ctx context.Context) (string, error) {
	_, ok, err := s.store.Hash(ctx)
	if err != nil {
		return "", fmt.Errorf("auth: read password hash: %w", err)
	}
	if ok {
		return "", nil
	}
	password, err := domain.GeneratePassword()
	if err != nil {
		return "", fmt.Errorf("auth: generate admin password: %w", err)
	}
	hash, err := domain.HashPassword(password)
	if err != nil {
		return "", fmt.Errorf("auth: hash admin password: %w", err)
	}
	if err := s.store.SetHash(ctx, hash); err != nil {
		return "", fmt.Errorf("auth: store admin password hash: %w", err)
	}
	return password, nil
}

// RequireAdminPassword verifies that the owner initialized the password beforehand.
func (s *Service) RequireAdminPassword(ctx context.Context) error {
	_, ok, err := s.store.Hash(ctx)
	if err != nil {
		return fmt.Errorf("auth: read password hash: %w", err)
	}
	if !ok {
		return ErrAdminPasswordNotInitialized
	}
	return nil
}

// SetAdminPassword hashes an owner-supplied password and invalidates current sessions. Callers
// must obtain the plaintext from stdin or another non-argument channel and never log it.
func (s *Service) SetAdminPassword(ctx context.Context, password string) error {
	if len(password) < minAdminPasswordLen || len(password) > maxAdminPasswordLen {
		return fmt.Errorf("auth: admin password must contain %d..%d bytes", minAdminPasswordLen, maxAdminPasswordLen)
	}
	if strings.ContainsAny(password, "\r\n\x00") {
		return errors.New("auth: admin password must be one line without NUL bytes")
	}
	hash, err := domain.HashPassword(password)
	if err != nil {
		return fmt.Errorf("auth: hash password: %w", err)
	}
	if err := s.store.SetHash(ctx, hash); err != nil {
		return fmt.Errorf("auth: store password hash: %w", err)
	}
	s.mu.Lock()
	s.sessions = map[string]domain.Session{}
	s.mu.Unlock()
	return nil
}

// Login verifies password and, on success, issues a session valid for sessionTTL.
func (s *Service) Login(ctx context.Context, password string) (domain.Session, error) {
	hash, ok, err := s.store.Hash(ctx)
	if err != nil {
		return domain.Session{}, fmt.Errorf("auth: read password hash: %w", err)
	}
	if !ok || !domain.VerifyPassword(hash, password) {
		return domain.Session{}, domain.ErrInvalidCredentials
	}
	token, err := domain.NewToken()
	if err != nil {
		return domain.Session{}, fmt.Errorf("auth: generate session token: %w", err)
	}
	now := s.now()
	session := domain.Session{Token: token, ExpiresAt: now.Add(sessionTTL)}

	s.mu.Lock()
	for t, existing := range s.sessions { // prune so abandoned sessions cannot accumulate
		if existing.Expired(now) {
			delete(s.sessions, t)
		}
	}
	s.sessions[token] = session
	s.mu.Unlock()
	return session, nil
}

// Logout invalidates the session identified by token; unknown tokens are ignored.
func (s *Service) Logout(token string) {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
}

// ValidateSession reports whether token identifies a live, unexpired session.
func (s *Service) ValidateSession(token string) error {
	s.mu.Lock()
	session, ok := s.sessions[token]
	s.mu.Unlock()
	if !ok || session.Expired(s.now()) {
		return domain.ErrSessionInvalid
	}
	return nil
}
