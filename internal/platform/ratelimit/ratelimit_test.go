package ratelimit

import (
	"fmt"
	"testing"
	"time"
)

func TestLimiterBurstThenRefillPerKey(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	l := New(time.Second, 2)
	l.now = func() time.Time { return now }
	if !l.Allow("a") || !l.Allow("a") || l.Allow("a") {
		t.Fatal("want burst of exactly 2 for key a")
	}
	if !l.Allow("b") {
		t.Fatal("keys must be independent")
	}
	now = now.Add(1500 * time.Millisecond)
	if !l.Allow("a") || l.Allow("a") {
		t.Fatal("want one token refilled after 1.5s")
	}
}

func TestLimiterPrunesIdleKeysAtCapacity(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	l := New(time.Second, 1)
	l.now = func() time.Time { return now }
	for i := 0; i < maxKeys; i++ {
		l.Allow(string(rune('a')) + time.Duration(i).String())
	}
	now = now.Add(idleAfter + time.Minute)
	l.Allow("fresh")
	if len(l.entries) >= maxKeys {
		t.Fatalf("idle entries were not pruned: %d", len(l.entries))
	}
}

// A flood from many sources that are all active must not grow the map past its bound, and must
// evict the least recently seen keys, not the ones still in use (docs/SPEC.md §4i).
func TestLimiterStaysBoundedWhenEveryKeyIsActive(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	l := New(time.Second, 1)
	l.now = func() time.Time { return now }
	for i := range 3 * maxKeys {
		now = now.Add(time.Millisecond) // all well inside idleAfter
		l.Allow(fmt.Sprintf("k%d", i))
		if len(l.entries) > maxKeys {
			t.Fatalf("after %d keys the map holds %d, want <= %d", i+1, len(l.entries), maxKeys)
		}
	}
	if _, ok := l.entries[fmt.Sprintf("k%d", 3*maxKeys-1)]; !ok {
		t.Fatal("the most recent key was evicted")
	}
	if _, ok := l.entries["k0"]; ok {
		t.Fatal("the oldest key survived eviction")
	}
}

func TestClientKeyGroupsIPv6ByPrefix64(t *testing.T) {
	for addr, want := range map[string]string{
		"203.0.113.7":          "203.0.113.7",
		"::ffff:203.0.113.7":   "203.0.113.7",
		"2001:db8:1:2::1":      "2001:db8:1:2::/64",
		"2001:db8:1:2:ffff::9": "2001:db8:1:2::/64",
		"2001:db8:1:3::1":      "2001:db8:1:3::/64",
		"not-an-ip":            "not-an-ip",
	} {
		if got := ClientKey(addr); got != want {
			t.Errorf("ClientKey(%q) = %q, want %q", addr, got, want)
		}
	}
	l := New(time.Hour, 1)
	if !l.Allow(ClientKey("2001:db8:1:2::1")) || l.Allow(ClientKey("2001:db8:1:2::2")) {
		t.Fatal("two addresses in one /64 must share a bucket")
	}
}
