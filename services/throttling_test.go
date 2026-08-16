package services

import (
	"fmt"
	"testing"
	"time"
)

func TestThrottling_LockoutAfterThreshold(t *testing.T) {
	ts := NewThrottlingService()
	defer ts.Close()

	ip := "203.0.113.7"

	for i := 0; i < throttlingMaxFails-1; i++ {
		ts.RecordFailure(ip)
		if ts.IsLocked(ip) {
			t.Fatalf("locked after %d failures, threshold is %d", i+1, throttlingMaxFails)
		}
	}

	ts.RecordFailure(ip)
	if !ts.IsLocked(ip) {
		t.Fatalf("not locked after %d failures", throttlingMaxFails)
	}

	// a different IP is unaffected
	if ts.IsLocked("203.0.113.8") {
		t.Fatal("unrelated IP is locked")
	}
}

func TestThrottling_FreshBudgetAfterLockout(t *testing.T) {
	ts := NewThrottlingService()
	defer ts.Close()

	ip := "203.0.113.7"
	for i := 0; i < throttlingMaxFails; i++ {
		ts.RecordFailure(ip)
	}
	if !ts.IsLocked(ip) {
		t.Fatal("expected lockout")
	}

	// simulate lockout expiry
	ts.mu.Lock()
	ts.entries[ip].lockedUntil = time.Now().UnixMilli() - 1
	ts.mu.Unlock()

	if ts.IsLocked(ip) {
		t.Fatal("still locked after lockedUntil passed")
	}

	// the failures slice was cleared on lockout, so a single new failure
	// must NOT re-lock (fresh budget semantics)
	ts.RecordFailure(ip)
	if ts.IsLocked(ip) {
		t.Fatal("re-locked after a single post-lockout failure; budget was not reset")
	}
}

func TestThrottling_EvictionPrefersUnlockedEntries(t *testing.T) {
	ts := NewThrottlingService()
	defer ts.Close()

	nowMs := time.Now().UnixMilli()

	ts.mu.Lock()
	// the OLDEST entry is locked; a naive oldest-first eviction would flush
	// the attacker's own lockout
	ts.entries["locked-old"] = &throttlingEntry{lastSeenMs: nowMs - 10_000, lockedUntil: nowMs + 60_000}
	ts.entries["unlocked-newer"] = &throttlingEntry{lastSeenMs: nowMs - 5_000}
	ts.entries["unlocked-newest"] = &throttlingEntry{lastSeenMs: nowMs}
	ts.evictOldestEntryLocked()
	_, lockedSurvived := ts.entries["locked-old"]
	_, oldestUnlockedSurvived := ts.entries["unlocked-newer"]
	_, newestSurvived := ts.entries["unlocked-newest"]
	ts.mu.Unlock()

	if !lockedSurvived {
		t.Fatal("eviction removed a locked entry while unlocked entries existed")
	}
	if oldestUnlockedSurvived {
		t.Fatal("eviction did not remove the oldest unlocked entry")
	}
	if !newestSurvived {
		t.Fatal("eviction removed the newest entry instead of the oldest unlocked one")
	}
}

func TestThrottling_EvictionFallsBackToLockedWhenAllLocked(t *testing.T) {
	ts := NewThrottlingService()
	defer ts.Close()

	nowMs := time.Now().UnixMilli()

	ts.mu.Lock()
	ts.entries["locked-a"] = &throttlingEntry{lastSeenMs: nowMs - 10_000, lockedUntil: nowMs + 60_000}
	ts.entries["locked-b"] = &throttlingEntry{lastSeenMs: nowMs, lockedUntil: nowMs + 60_000}
	ts.evictOldestEntryLocked()
	size := len(ts.entries)
	_, oldestGone := ts.entries["locked-a"]
	ts.mu.Unlock()

	if size != 1 {
		t.Fatalf("expected exactly one entry evicted, %d remain", size)
	}
	if oldestGone {
		t.Fatal("expected the oldest locked entry to be evicted")
	}
}

func TestThrottling_CapEnforced(t *testing.T) {
	ts := NewThrottlingService()
	defer ts.Close()

	for i := 0; i < throttlingMaxEntries+100; i++ {
		ts.RecordFailure(fmt.Sprintf("10.0.%d.%d", i/256, i%256))
	}

	ts.mu.Lock()
	size := len(ts.entries)
	ts.mu.Unlock()

	if size > throttlingMaxEntries {
		t.Fatalf("entries map grew to %d, cap is %d", size, throttlingMaxEntries)
	}
}
