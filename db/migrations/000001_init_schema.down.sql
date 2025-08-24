-- drops indexes
DROP INDEX IF EXISTS idx_queue;
DROP INDEX IF EXISTS idx_status;
DROP INDEX IF EXISTS idx_consuming;
DROP INDEX IF EXISTS idx_processing;
DROP INDEX IF EXISTS idx_failed_regular;
DROP INDEX IF EXISTS idx_dlq_operations;

-- drops tables
DROP TABLE IF EXISTS messages;

-- resets PRAGMA settings
PRAGMA journal_mode =
DELETE;