-- drops indexes
DROP INDEX IF EXISTS idx_queue_ready_for_consuming;
DROP INDEX IF EXISTS idx_for_queue_depth;
DROP INDEX IF EXISTS idx_expired;
DROP INDEX IF EXISTS idx_for_requeueuing;

-- drops tables
DROP TABLE IF EXISTS messages;

-- resets PRAGMA settings
PRAGMA journal_mode = DELETE;