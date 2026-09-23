package http

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

// One IPv6 client usually holds a whole /64: rotating addresses inside it must not reset the
// failure count (docs/SPEC.md §3 Auth).
func TestLoginLimiterGroupsIPv6ByPrefix64(t *testing.T) {
	h, _ := newServer(t, Options{})
	for i := range loginFailureLimit {
		login(h, "bad", fmt.Sprintf("[2001:db8:1:2::%x]:1", i+1))
	}
	if rec := login(h, password, "[2001:db8:1:2::ff]:1"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("another address in the same /64 = %d, want 429", rec.Code)
	}
	if rec := login(h, password, "[2001:db8:1:3::1]:1"); rec.Code != http.StatusNoContent {
		t.Fatalf("a different /64 = %d, want 204", rec.Code)
	}
}

func TestLoginLimiterStaysBoundedUnderAFlood(t *testing.T) {
	l := newLoginLimiter(nil)
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return now }
	for i := range 3 * maxLoginClients {
		now = now.Add(time.Millisecond) // every entry stays inside its 15-minute window
		l.failure(fmt.Sprintf("c%d", i))
		if len(l.attempts) > maxLoginClients {
			t.Fatalf("after %d clients the map holds %d, want <= %d", i+1, len(l.attempts), maxLoginClients)
		}
	}
	if _, ok := l.attempts[fmt.Sprintf("c%d", 3*maxLoginClients-1)]; !ok {
		t.Fatal("the most recent client was evicted")
	}
}

func TestLoginLimiterDropsExpiredClientsFirst(t *testing.T) {
	l := newLoginLimiter(nil)
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return now }
	for i := range maxLoginClients {
		l.failure(fmt.Sprintf("c%d", i))
	}
	now = now.Add(loginFailureWindow + time.Minute) // every entry's window has ended
	l.failure("late")
	if len(l.attempts) != 1 {
		t.Fatalf("map holds %d entries, want only the new one (expired entries dropped first)", len(l.attempts))
	}
}
