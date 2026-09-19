package http

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

const (
	loginFailureLimit  = 5
	loginFailureWindow = 15 * time.Minute
)

type loginAttempt struct {
	failures int
	resetAt  time.Time
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
	attempt := l.attempts[client]
	if !now.Before(attempt.resetAt) {
		attempt = loginAttempt{resetAt: now.Add(loginFailureWindow)}
	}
	attempt.failures++
	l.attempts[client] = attempt
}

func (l *loginLimiter) success(client string) {
	l.mu.Lock()
	delete(l.attempts, client)
	l.mu.Unlock()
}
