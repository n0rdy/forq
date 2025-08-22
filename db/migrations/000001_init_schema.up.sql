-- Enable WAL mode and other optimizations
PRAGMA journal_mode = WAL;   -- Multiple consumers can read messages while new messages are being inserted
PRAGMA synchronous = NORMAL; -- Safe in WAL mode, much faster than FULL
PRAGMA busy_timeout = 10000; -- Handle concurrent access gracefully

CREATE TABLE messages
(
    id                    TEXT PRIMARY KEY,               -- UUID v7 format
    queue                 TEXT    NOT NULL,               -- e.g., "emails" or "emails-dlq"
    is_dlq                BOOLEAN NOT NULL DEFAULT FALSE, -- Whether this is a DLQ message
    content               TEXT    NOT NULL,               -- 256KB max, TEXT only
    status                INTEGER NOT NULL DEFAULT 0,     -- 0=ready, 1=processing, 2=failed
    attempts              INTEGER NOT NULL DEFAULT 0,
    process_after         INTEGER NOT NULL,               -- Unix milliseconds - When the message should become visible for processing
    processing_started_at INTEGER,                        -- Unix milliseconds - When processing started (null if not processing)
    failure_reason        TEXT,                           -- Reason for ending up in DLQ (if applicable)
    received_at           INTEGER NOT NULL,               -- Unix milliseconds - When the message was received
    updated_at            INTEGER NOT NULL,               -- Unix milliseconds - Last update timestamp
    expires_after         INTEGER NOT NULL                -- Unix milliseconds - When the message expires and can be deleted
);

-- Optimized indexes for read/write heavy workload
CREATE INDEX idx_consuming ON messages (queue, status, process_after, expires_after, received_at);
CREATE INDEX idx_processing ON messages (status, processing_started_at) WHERE status = 1;
CREATE INDEX idx_failed_regular ON messages (status, updated_at) WHERE is_dlq = FALSE AND status = 2;
CREATE INDEX idx_dlq_operations ON messages (queue, status, expires_after) WHERE is_dlq = TRUE;