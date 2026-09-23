// Package ratelimit provides a small keyed token-bucket limiter for HTTP endpoints.
package ratelimit

import (
	"net/netip"
	"slices"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// maxKeys bounds memory. At capacity, entries idle for over idleAfter are dropped first; if that
// frees nothing (every key is active, e.g. a flood from many sources) the least recently seen
// evictFraction of the entries go, so the O(n) sweep runs once per maxKeys/evictFraction new keys,
// not on every one (docs/SPEC.md §4i).
const (
	maxKeys       = 10000
	idleAfter     = 10 * time.Minute
	evictFraction = 10
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

// ClientKey is the limiter key for a client address: the address itself for IPv4, its /64 prefix
// for IPv6 (one client usually holds a whole /64, so a per-address key is trivially bypassed). A
// string that is not an address is returned unchanged.
func ClientKey(addr string) string {
	ip, err := netip.ParseAddr(addr)
	if err != nil {
		return addr
	}
	ip = ip.Unmap()
	if ip.Is4() {
		return ip.String()
	}
	prefix, _ := ip.Prefix(64)
	return prefix.String()
}

// Allow consumes one token for key and reports whether the event may proceed.
func (l *Limiter) Allow(key string) bool {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[key]
	if !ok {
		if len(l.entries) >= maxKeys {
			l.makeRoom(now)
		}
		e = &entry{limiter: rate.NewLimiter(rate.Every(l.every), l.burst)}
		l.entries[key] = e
	}
	e.seen = now
	return e.limiter.AllowN(now, 1)
}

func (l *Limiter) makeRoom(now time.Time) {
	for k, e := range l.entries {
		if now.Sub(e.seen) > idleAfter {
			delete(l.entries, k)
		}
	}
	if len(l.entries) < maxKeys {
		return
	}
	type aged struct {
		key  string
		seen time.Time
	}
	all := make([]aged, 0, len(l.entries))
	for k, e := range l.entries {
		all = append(all, aged{k, e.seen})
	}
	slices.SortFunc(all, func(a, b aged) int { return a.seen.Compare(b.seen) })
	for _, a := range all[:len(all)/evictFraction] {
		delete(l.entries, a.key)
	}
}
