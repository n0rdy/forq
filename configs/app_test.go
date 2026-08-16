package configs

import (
	"testing"
	"time"
)

// TestServerTimeouts pins the timeout values in real time.Duration terms. The
// original bug here was milliseconds multiplied by time.Second, which produced
// ~8.3-hour timeouts while the comments claimed 40-45 seconds.
func TestServerTimeouts(t *testing.T) {
	cfg := NewAppConfig(false, 24, 168)
	timeouts := cfg.ServerConfig.Timeouts

	if timeouts.Handle != 40*time.Second {
		t.Errorf("Handle timeout = %v, want 40s", timeouts.Handle)
	}
	if timeouts.Write != 45*time.Second {
		t.Errorf("Write timeout = %v, want 45s", timeouts.Write)
	}
	if timeouts.Read != 45*time.Second {
		t.Errorf("Read timeout = %v, want 45s", timeouts.Read)
	}
	if timeouts.ReadHeader != 10*time.Second {
		t.Errorf("ReadHeader timeout = %v, want 10s", timeouts.ReadHeader)
	}

	// all handler-facing timeouts must exceed the long-polling duration,
	// otherwise every empty long poll gets cut off by the server
	polling := time.Duration(cfg.PollingDurationMs) * time.Millisecond
	for name, d := range map[string]time.Duration{
		"Handle": timeouts.Handle,
		"Write":  timeouts.Write,
		"Read":   timeouts.Read,
	} {
		if d <= polling {
			t.Errorf("%s timeout (%v) must exceed the long-polling duration (%v)", name, d, polling)
		}
	}
}

func TestJobsIntervals(t *testing.T) {
	cfg := NewAppConfig(false, 24, 168)

	// PRAGMA optimize is recommended hourly; running it every minute was a bug
	if cfg.JobsIntervals.DbOptimizationMs != 60*60*1000 {
		t.Errorf("DbOptimizationMs = %d, want 1 hour (3600000)", cfg.JobsIntervals.DbOptimizationMs)
	}

	// every job's per-tick context timeout is derived as interval-1s, so
	// intervals must be comfortably above 1s
	for name, interval := range map[string]int64{
		"ExpiredMessagesCleanupMs":    cfg.JobsIntervals.ExpiredMessagesCleanupMs,
		"ExpiredDlqMessagesCleanupMs": cfg.JobsIntervals.ExpiredDlqMessagesCleanupMs,
		"FailedMessagesCleanupMs":     cfg.JobsIntervals.FailedMessagesCleanupMs,
		"FailedDqlMessagesCleanupMs":  cfg.JobsIntervals.FailedDqlMessagesCleanupMs,
		"StaleMessagesCleanupMs":      cfg.JobsIntervals.StaleMessagesCleanupMs,
		"QueuesDepthMetricsMs":        cfg.JobsIntervals.QueuesDepthMetricsMs,
	} {
		if interval < 10_000 {
			t.Errorf("%s = %dms, suspiciously low", name, interval)
		}
	}
}

func TestTtlConversion(t *testing.T) {
	cfg := NewAppConfig(false, 24, 168)
	if cfg.QueueTtlMs != 24*60*60*1000 {
		t.Errorf("QueueTtlMs = %d, want 24h in ms", cfg.QueueTtlMs)
	}
	if cfg.DlqTtlMs != 168*60*60*1000 {
		t.Errorf("DlqTtlMs = %d, want 168h in ms", cfg.DlqTtlMs)
	}
	if len(cfg.BackoffDelaysMs) != cfg.MaxDeliveryAttempts {
		t.Errorf("BackoffDelaysMs has %d entries, want MaxDeliveryAttempts (%d)", len(cfg.BackoffDelaysMs), cfg.MaxDeliveryAttempts)
	}
}
