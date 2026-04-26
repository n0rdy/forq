package services

import (
	"sync"
	"time"
)

const (
	throttlingWindowMs  = 60 * 1000      // sliding window for counting failures
	throttlingMaxFails  = 5              // failures within window that trigger a lockout
	throttlingLockoutMs = 60 * 1000      // lockout duration after threshold reached
	throttlingSweepMs   = 5 * 60 * 1000  // background cleanup interval
	throttlingStaleMs   = 10 * 60 * 1000 // entries idle this long are removed
)

type throttlingEntry struct {
	failures    []int64
	lockedUntil int64
	lastSeenMs  int64
}

// ThrottlingService tracks failed-auth attempts per IP and locks out IPs
// that exceed the threshold within a sliding window. In-memory only:
// process restart wipes all state.
type ThrottlingService struct {
	entries map[string]*throttlingEntry
	mu      sync.Mutex
	ticker  *time.Ticker
}

func NewThrottlingService() *ThrottlingService {
	ticker := time.NewTicker(throttlingSweepMs * time.Millisecond)

	ts := &ThrottlingService{
		entries: make(map[string]*throttlingEntry),
		ticker:  ticker,
	}

	go func() {
		for now := range ticker.C {
			nowMs := now.UnixMilli()
			cutoff := nowMs - throttlingStaleMs
			ts.mu.Lock()
			for ip, e := range ts.entries {
				if e.lastSeenMs < cutoff && nowMs >= e.lockedUntil {
					delete(ts.entries, ip)
				}
			}
			ts.mu.Unlock()
		}
	}()

	return ts
}

func (ts *ThrottlingService) IsLocked(ip string) bool {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	e, ok := ts.entries[ip]
	if !ok {
		return false
	}
	return time.Now().UnixMilli() < e.lockedUntil
}

func (ts *ThrottlingService) RecordFailure(ip string) {
	now := time.Now().UnixMilli()
	cutoff := now - throttlingWindowMs

	ts.mu.Lock()
	defer ts.mu.Unlock()

	e, ok := ts.entries[ip]
	if !ok {
		e = &throttlingEntry{}
		ts.entries[ip] = e
	}

	pruned := e.failures[:0]
	for _, t := range e.failures {
		if t > cutoff {
			pruned = append(pruned, t)
		}
	}
	e.failures = append(pruned, now)
	e.lastSeenMs = now

	if len(e.failures) >= throttlingMaxFails {
		e.lockedUntil = now + throttlingLockoutMs
	}
}

func (ts *ThrottlingService) Close() error {
	ts.ticker.Stop()
	return nil
}
