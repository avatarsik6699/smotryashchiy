// Package ratelimit provides a small keyed token-bucket limiter for HTTP endpoints.
package ratelimit

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// maxKeys bounds memory: when exceeded, entries idle for over idleAfter are dropped.
const (
	maxKeys   = 10000
	idleAfter = 10 * time.Minute
)

// Limiter allows `burst` events at once per key and refills at `every` per event.
type Limiter struct {
	every time.Duration
	burst int
	now   func() time.Time

	mu      sync.Mutex
	entries map[string]*entry
}

type entry struct {
	limiter *rate.Limiter
	seen    time.Time
}

// New returns a Limiter refilling one token per `every`, holding at most `burst` tokens.
func New(every time.Duration, burst int) *Limiter {
	return &Limiter{every: every, burst: burst, now: time.Now, entries: map[string]*entry{}}
}

// Allow consumes one token for key and reports whether the event may proceed.
func (l *Limiter) Allow(key string) bool {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[key]
	if !ok {
		if len(l.entries) >= maxKeys {
			l.prune(now)
		}
		e = &entry{limiter: rate.NewLimiter(rate.Every(l.every), l.burst)}
		l.entries[key] = e
	}
	e.seen = now
	return e.limiter.AllowN(now, 1)
}

func (l *Limiter) prune(now time.Time) {
	for k, e := range l.entries {
		if now.Sub(e.seen) > idleAfter {
			delete(l.entries, k)
		}
	}
}
