package configs

import "time"

type AppConfigs struct {
	MessageContentMaxSizeBytes int
	MaxProcessAfterDelayMs     int64 // Maximum delay after which a message can be processed, in milliseconds. Applies to delays provided by the users via API.
	MaxDeliveryAttempts        int
	BackoffDelaysMs            []int64
	QueueTtlMs                 int64
	DlqTtlMs                   int64
	PollingDurationMs          int64 // Duration for which the queue is polled for new messages via HTTP2 long-polling
	MaxProcessingTimeMs        int64 // Maximum time allowed for processing a message before it is considered stale
	JobsIntervals              JobsIntervals
	ServerConfig               ServerConfig // Configuration for the server, including timeouts
}

type JobsIntervals struct {
	ExpiredMessagesCleanupMs    int64 // Interval for cleaning up expired messages from the regular queue
	ExpiredDlqMessagesCleanupMs int64 // Interval for cleaning up expired messages from the DLQ
	FailedMessagesCleanupMs     int64 // Interval for cleaning up failed messages from the regular queue
	FailedDqlMessagesCleanupMs  int64 // Interval for cleaning up failed messages from the DLQ
	StaleMessagesCleanupMs      int64 // Interval for cleaning up stale messages from the regular queue and DLQ
}

type ServerConfig struct {
	Timeouts ServerTimeouts
}

type ServerTimeouts struct {
	Handle     time.Duration
	Write      time.Duration
	Read       time.Duration
	ReadHeader time.Duration
	Idle       time.Duration
}

func NewAppConfig() *AppConfigs {
	pollingDurationMs := 30 * 1000

	return &AppConfigs{
		MessageContentMaxSizeBytes: 256 * 1024,                // 256 KB
		MaxProcessAfterDelayMs:     366 * 24 * 60 * 60 * 1000, // 366 days
		MaxDeliveryAttempts:        5,
		BackoffDelaysMs:            []int64{1000, 5 * 1000, 15 * 1000, 30 * 1000, 60 * 1000}, // 1s, 5s, 15s, 30s, 60s
		QueueTtlMs:                 1 * 24 * 60 * 60 * 1000,                                  // 1 day
		DlqTtlMs:                   7 * 24 * 60 * 60 * 1000,                                  // 7 days
		PollingDurationMs:          int64(pollingDurationMs),                                 // 30 seconds
		MaxProcessingTimeMs:        30 * 1000,                                                // 30 seconds
		JobsIntervals: JobsIntervals{
			ExpiredMessagesCleanupMs:    5 * 60 * 1000, // 5 minutes
			ExpiredDlqMessagesCleanupMs: 5 * 60 * 1000, // 5 minutes
			FailedMessagesCleanupMs:     2 * 60 * 1000, // 2 minutes
			FailedDqlMessagesCleanupMs:  5 * 60 * 1000, // 5 minutes
			StaleMessagesCleanupMs:      1 * 60 * 1000, // 1 minute
		},
		ServerConfig: ServerConfig{
			Timeouts: ServerTimeouts{
				Handle:     time.Duration(pollingDurationMs+10) * time.Second, // 40s - polling + buffer
				Write:      time.Duration(pollingDurationMs+15) * time.Second, // 45s - handle + write buffer
				Read:       time.Duration(pollingDurationMs+15) * time.Second, // 45s - same as write
				ReadHeader: 10 * time.Second,                                  // 10s - headers shouldn't take long
				Idle:       5 * time.Minute,                                   // 5m - keep connections alive
			},
		},
	}
}
