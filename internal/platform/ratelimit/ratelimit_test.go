package ratelimit

import (
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
