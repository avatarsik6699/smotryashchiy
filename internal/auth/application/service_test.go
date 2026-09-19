package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/auth/domain"
)

type fakeStore struct {
	hash string
	ok   bool
	err  error
}

func (f *fakeStore) Hash(context.Context) (string, bool, error) { return f.hash, f.ok, f.err }
func (f *fakeStore) SetHash(_ context.Context, h string) error  { f.hash, f.ok = h, true; return f.err }

const goodPassword = "correct-horse-battery-staple-1"

func newService(t *testing.T) (*Service, *fakeStore) {
	t.Helper()
	store := &fakeStore{}
	svc := NewService(store)
	if err := svc.SetAdminPassword(context.Background(), goodPassword); err != nil {
		t.Fatal(err)
	}
	return svc, store
}

func TestLoginSuccessAndFailure(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	if _, err := svc.Login(ctx, "wrong"); !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("err = %v", err)
	}
	session, err := svc.Login(ctx, goodPassword)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ValidateSession(session.Token); err != nil {
		t.Fatalf("valid session rejected: %v", err)
	}
}

func TestLoginWithoutPasswordIsInvalidCredentials(t *testing.T) {
	svc := NewService(&fakeStore{})
	if _, err := svc.Login(context.Background(), "anything"); !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("err = %v", err)
	}
}

func TestLoginSurfacesStoreFailureAsInternal(t *testing.T) {
	svc := NewService(&fakeStore{err: errors.New("db down")})
	_, err := svc.Login(context.Background(), "x")
	if err == nil || errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("err = %v", err)
	}
}

func TestSessionExpiresAfterTTL(t *testing.T) {
	svc, _ := newService(t)
	now := time.Now()
	svc.now = func() time.Time { return now }
	session, err := svc.Login(context.Background(), goodPassword)
	if err != nil {
		t.Fatal(err)
	}
	svc.now = func() time.Time { return now.Add(sessionTTL + time.Second) }
	if err := svc.ValidateSession(session.Token); !errors.Is(err, domain.ErrSessionInvalid) {
		t.Fatalf("err = %v", err)
	}
}

func TestLogoutAndPasswordChangeInvalidateSessions(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	a, _ := svc.Login(ctx, goodPassword)
	b, _ := svc.Login(ctx, goodPassword)
	svc.Logout(a.Token)
	if err := svc.ValidateSession(a.Token); err == nil {
		t.Fatal("logged-out session still valid")
	}
	if err := svc.SetAdminPassword(ctx, goodPassword+"-new"); err != nil {
		t.Fatal(err)
	}
	if err := svc.ValidateSession(b.Token); err == nil {
		t.Fatal("session survived password change")
	}
}

func TestExpiredSessionsArePrunedOnLogin(t *testing.T) {
	svc, _ := newService(t)
	now := time.Now()
	svc.now = func() time.Time { return now }
	_, _ = svc.Login(context.Background(), goodPassword)
	svc.now = func() time.Time { return now.Add(2 * sessionTTL) }
	_, _ = svc.Login(context.Background(), goodPassword)
	if len(svc.sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(svc.sessions))
	}
}

func TestSetAdminPasswordValidation(t *testing.T) {
	svc := NewService(&fakeStore{})
	for name, pw := range map[string]string{
		"short":   "too-short",
		"newline": strings.Repeat("a", 30) + "\nb",
		"nul":     strings.Repeat("a", 30) + "\x00",
		"long":    strings.Repeat("a", maxAdminPasswordLen+1),
	} {
		if err := svc.SetAdminPassword(context.Background(), pw); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestEnsureAndRequireAdminPassword(t *testing.T) {
	svc := NewService(&fakeStore{})
	ctx := context.Background()
	if err := svc.RequireAdminPassword(ctx); !errors.Is(err, ErrAdminPasswordNotInitialized) {
		t.Fatalf("err = %v", err)
	}
	password, err := svc.EnsureAdminPassword(ctx)
	if err != nil || password == "" {
		t.Fatalf("password = %q, err = %v", password, err)
	}
	if err := svc.RequireAdminPassword(ctx); err != nil {
		t.Fatal(err)
	}
	if again, _ := svc.EnsureAdminPassword(ctx); again != "" {
		t.Fatal("second bootstrap must not generate a new password")
	}
	if _, err := svc.Login(ctx, password); err != nil {
		t.Fatalf("generated password rejected: %v", err)
	}
}
