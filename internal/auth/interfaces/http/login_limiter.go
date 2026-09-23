package http

import (
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/platform/ratelimit"
)

const (
	loginFailureLimit  = 5
	loginFailureWindow = 15 * time.Minute
	// maxLoginClients bounds memory like ratelimit.Limiter (docs/SPEC.md §3 Auth): at capacity,
	// expired entries go first, then the least recently seen 1/loginEvictFraction of them.
	maxLoginClients    = 10000
	loginEvictFraction = 10
)

type loginAttempt struct {
	failures int
	resetAt  time.Time
	seen     time.Time
}

type loginLimiter struct {
	mu             sync.Mutex
	attempts       map[string]loginAttempt
	trustedProxies []netip.Prefix
	now            func() time.Time
}

func newLoginLimiter(trusted []netip.Prefix) *loginLimiter {
	return &loginLimiter{attempts: map[string]loginAttempt{}, trustedProxies: trusted, now: time.Now}
}

// clientKey identifies a client for the limiter: its address, or its /64 for IPv6
// (ratelimit.ClientKey), so one IPv6 client cannot rotate addresses to reset its failures.
func (l *loginLimiter) clientKey(r *http.Request) string {
	return ratelimit.ClientKey(l.clientIP(r))
}

func (l *loginLimiter) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil || !l.isTrusted(peer) {
		return host
	}
	forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0])
	if address, err := netip.ParseAddr(forwarded); err == nil {
		return address.String()
	}
	return host
}

func (l *loginLimiter) isTrusted(address netip.Addr) bool {
	for _, prefix := range l.trustedProxies {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func (l *loginLimiter) retryAfter(client string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	attempt, ok := l.attempts[client]
	now := l.now()
	if !ok || !now.Before(attempt.resetAt) {
		delete(l.attempts, client)
		return 0, false
	}
	if attempt.failures < loginFailureLimit {
		return 0, false
	}
	return attempt.resetAt.Sub(now), true
}

func (l *loginLimiter) failure(client string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	attempt, ok := l.attempts[client]
	if !ok && len(l.attempts) >= maxLoginClients {
		l.makeRoom(now)
	}
	if !now.Before(attempt.resetAt) {
		attempt = loginAttempt{resetAt: now.Add(loginFailureWindow)}
	}
	attempt.failures++
	attempt.seen = now
	l.attempts[client] = attempt
}

func (l *loginLimiter) makeRoom(now time.Time) {
	for k, a := range l.attempts {
		if !now.Before(a.resetAt) {
			delete(l.attempts, k)
		}
	}
	if len(l.attempts) < maxLoginClients {
		return
	}
	keys := make([]string, 0, len(l.attempts))
	for k := range l.attempts {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b string) int { return l.attempts[a].seen.Compare(l.attempts[b].seen) })
	for _, k := range keys[:len(keys)/loginEvictFraction] {
		delete(l.attempts, k)
	}
}

func (l *loginLimiter) success(client string) {
	l.mu.Lock()
	delete(l.attempts, client)
	l.mu.Unlock()
}
