package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/n0rdy/forq/common"
	"github.com/n0rdy/forq/configs"

	"github.com/rs/zerolog/log"
	_ "modernc.org/sqlite"
)

type ForqRepo struct {
	dbRead     *sql.DB
	dbWrite    *sql.DB
	appConfigs *configs.AppConfigs
}

func NewSQLiteRepo(dbPath string, appConfigs *configs.AppConfigs) (*ForqRepo, error) {
	// Create read connection with multiple connections for concurrent reads
	dbRead, err := sql.Open("sqlite", applyConnectionSettings(dbPath, false))
	if err != nil {
		return nil, err
	}
	dbRead.SetMaxOpenConns(runtime.NumCPU()*2 + 1)
	dbRead.SetMaxIdleConns(runtime.NumCPU()*2 + 1)
	dbRead.SetConnMaxLifetime(0)

	if err := dbRead.Ping(); err != nil {
		dbRead.Close()
		return nil, fmt.Errorf("ping read database: %w", err)
	}

	// Create write connection with single connection to serialize writes
	// we are running optimizations pragma ONLY on the write connection, as it might lock the database for a while
	// and our read flow doesn't expect any locks.
	dbWrite, err := sql.Open("sqlite", applyConnectionSettings(dbPath, true))
	if err != nil {
		dbRead.Close()
		return nil, err
	}
	dbWrite.SetMaxOpenConns(1)
	dbWrite.SetMaxIdleConns(1)
	dbWrite.SetConnMaxLifetime(0)

	if err := dbWrite.Ping(); err != nil {
		dbRead.Close()
		dbWrite.Close()
		return nil, fmt.Errorf("ping write database: %w", err)
	}

	return &ForqRepo{
		dbRead:     dbRead,
		dbWrite:    dbWrite,
		appConfigs: appConfigs,
	}, nil
}

func applyConnectionSettings(dbPath string, withOptimization bool) string {
	urlWithSettings := dbPath + "?_pragma=synchronous(NORMAL)&_pragma=temp_store(MEMORY)"
	if withOptimization {
		// from SQLite docs:
		// Applications with long-lived database connections should run "PRAGMA optimize=0x10002" when the database connection first opens,
		// then run "PRAGMA optimize" again at periodic intervals - perhaps once per day.
		urlWithSettings += "&_pragma=optimize(0x10002)"
	}
	return urlWithSettings
}

func (fr *ForqRepo) InsertMessage(newMessage *NewMessage, ctx context.Context) error {
	query := `
		INSERT INTO messages (id, queue, content, process_after, received_at, updated_at, expires_after)
		VALUES (?, ?, ?, ?, ?, ?, ?);
	`

	_, err := fr.dbWrite.ExecContext(ctx, query,
		newMessage.Id,           // id
		newMessage.QueueName,    // queue
		newMessage.Content,      // content
		newMessage.ProcessAfter, // process_after
		newMessage.ReceivedAt,   // received_at
		newMessage.UpdatedAt,    // updated_at
		newMessage.ExpiresAfter, // expires_after
	)
	if err != nil {
		log.Error().Err(err).Str("queue", newMessage.QueueName).Msg("failed to insert new message")
		return common.ErrInternal
	}
	return nil
}

func (fr *ForqRepo) SelectMessageForConsuming(queueName string, ctx context.Context) (*MessageForConsuming, error) {
	nowMs := time.Now().UnixMilli()

	// we are ignoring expires_after here for performance boost reasons, as expired messaged are cleanup by the jobs.
	// This query uses COVERING INDEX via `idx_queue_ready_for_consuming`, so it is very fast.
	query := `
		UPDATE messages
        SET
            status = ?,
            attempts = attempts + 1,
            processing_started_at = ?,
            updated_at = ?
        WHERE id = (
            SELECT id
            FROM messages
            WHERE queue = ?
              AND status = ?
              AND process_after <= ?
            ORDER BY received_at ASC
            LIMIT 1
        )
        RETURNING id, content;`

	var msg MessageForConsuming
	err := fr.dbWrite.QueryRowContext(ctx, query,
		common.ProcessingStatus, // SET status = ?
		nowMs,                   // processing_started_at = ?
		nowMs,                   // updated_at = ?
		queueName,               // WHERE queue = ?
		common.ReadyStatus,      // AND status = ?
		nowMs,                   // AND process_after <= ?
	).Scan(&msg.Id, &msg.Content)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		log.Error().Err(err).Str("queue", queueName).Msg("failed to select message for consuming")
		return nil, common.ErrInternal
	}
	return &msg, nil
}

func (fr *ForqRepo) SelectMessageMetadata(messageId string, queueName string, ctx context.Context) (*MessageMetadata, error) {
	query := `
		SELECT id, status, attempts, received_at, process_after
		FROM messages
		WHERE id = ? AND queue = ?;`

	var msgMeta MessageMetadata
	err := fr.dbRead.QueryRowContext(ctx, query,
		messageId, // WHERE id = ?
		queueName, // AND queue = ?
	).Scan(&msgMeta.Id, &msgMeta.Status, &msgMeta.Attempts,
		&msgMeta.ReceivedAt, &msgMeta.ProcessAfter)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		log.Error().Err(err).Str("queue", queueName).Str("message_id", messageId).Msg("failed to select message metadata")
		return nil, common.ErrInternal
	}
	return &msgMeta, nil
}

func (fr *ForqRepo) SelectMessageDetails(messageId string, queueName string, ctx context.Context) (*MessageDetails, error) {
	query := `
		SELECT id, content, status, attempts, process_after, processing_started_at, failure_reason,
		       received_at, updated_at, expires_after
		FROM messages
		WHERE id = ? AND queue = ?;`

	var msgDetails MessageDetails
	err := fr.dbRead.QueryRowContext(ctx, query,
		messageId, // WHERE id = ?
		queueName, // AND queue = ?
	).Scan(&msgDetails.Id, &msgDetails.Content, &msgDetails.Status, &msgDetails.Attempts, &msgDetails.ProcessAfter,
		&msgDetails.ProcessingStartedAt, &msgDetails.FailureReason, &msgDetails.ReceivedAt, &msgDetails.UpdatedAt,
		&msgDetails.ExpiresAfter)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		log.Error().Err(err).Str("queue", queueName).Str("message_id", messageId).Msg("failed to select message details")
		return nil, common.ErrInternal
	}
	return &msgDetails, nil
}

func (fr *ForqRepo) SelectAllQueuesWithStats(ctx context.Context) ([]QueueMetadata, error) {
	query := `
		SELECT queue, COUNT(*) as messages_count, is_dlq
		FROM messages
		GROUP BY queue, is_dlq
		ORDER BY queue ASC;`

	rows, err := fr.dbRead.QueryContext(ctx, query)
	if err != nil {
		log.Error().Err(err).Msg("failed to select all queues with stats")
		return nil, common.ErrInternal
	}
	defer rows.Close()

	var queues []QueueMetadata
	for rows.Next() {
		var q QueueMetadata
		if err := rows.Scan(&q.Name, &q.MessagesCount, &q.IsDLQ); err != nil {
			log.Error().Err(err).Msg("failed to scan queue metadata")
			return nil, common.ErrInternal
		}
		queues = append(queues, q)
	}

	if err := rows.Err(); err != nil {
		log.Error().Err(err).Msg("error iterating over queue metadata rows")
		return nil, common.ErrInternal
	}
	return queues, nil
}

func (fr *ForqRepo) SelectQueueStats(queueName string, ctx context.Context) (*QueueMetadata, error) {
	query := `
		SELECT queue, COUNT(*) as messages_count, is_dlq
		FROM messages
		WHERE queue = ?
		GROUP BY queue, is_dlq;`

	var queueStats QueueMetadata
	err := fr.dbRead.QueryRowContext(ctx, query, queueName).Scan(
		&queueStats.Name,
		&queueStats.MessagesCount,
		&queueStats.IsDLQ,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		log.Error().Err(err).Str("queue", queueName).Msg("failed to select queue stats")
		return nil, common.ErrInternal
	}
	return &queueStats, nil
}

func (fr *ForqRepo) SelectMessagesForUI(queueName string, cursor string, limit int, ctx context.Context) ([]MessageMetadata, error) {
	var query string
	var args []interface{}

	if cursor == "" {
		// First page - no cursor
		query = `
			SELECT id, status, attempts, received_at, process_after
			FROM messages
			WHERE queue = ?
			ORDER BY id DESC
			LIMIT ?;`
		args = []interface{}{queueName, limit}
	} else {
		// Subsequent pages - use cursor
		query = `
			SELECT id, status, attempts, received_at, process_after
			FROM messages
			WHERE queue = ? AND id < ?
			ORDER BY id DESC
			LIMIT ?;`
		args = []interface{}{queueName, cursor, limit}
	}

	rows, err := fr.dbRead.QueryContext(ctx, query, args...)
	if err != nil {
		log.Error().Err(err).Str("queue", queueName).Str("cursor", cursor).Msg("failed to select messages for UI")
		return nil, common.ErrInternal
	}
	defer rows.Close()

	var messages []MessageMetadata
	for rows.Next() {
		var msg MessageMetadata
		if err := rows.Scan(&msg.Id, &msg.Status, &msg.Attempts, &msg.ReceivedAt, &msg.ProcessAfter); err != nil {
			log.Error().Err(err).Msg("failed to scan message metadata for UI")
			return nil, common.ErrInternal
		}
		messages = append(messages, msg)
	}

	if err := rows.Err(); err != nil {
		log.Error().Err(err).Msg("error iterating over message metadata rows for UI")
		return nil, common.ErrInternal
	}
	return messages, nil
}

func (fr *ForqRepo) UpdateMessageOnConsumingFailure(messageId string, queueName string, ctx context.Context) error {
	nowMs := time.Now().UnixMilli()

	query := fmt.Sprintf(`
        UPDATE messages 
        SET 
            status = CASE 
            	WHEN attempts >= ? THEN ?	-- failed if no more attempts left
            	ELSE ?						-- ready if there are attempts left
			END,
            process_after = CASE 
                %s
            END,
            processing_started_at = NULL,
            updated_at = ?
        WHERE id = ? AND queue = ? AND status = ?;`, fr.processAfterCases(nowMs))

	result, err := fr.dbWrite.ExecContext(ctx, query,
		fr.appConfigs.MaxDeliveryAttempts, // WHEN attempts = ? (status check)
		common.FailedStatus,               // THEN ?  		-- failed if no more attempts left
		common.ReadyStatus,                // ELSE ?		-- ready if there are attempts left
		nowMs,                             // updated_at = ?
		messageId,                         // WHERE id = ?
		queueName,                         // AND queue = ?
		common.ProcessingStatus,           // AND status = ?
	)

	if err != nil {
		log.Error().Err(err).Str("queue", queueName).Str("message_id", messageId).Msg("failed to update message on failure")
		return common.ErrInternal
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return common.ErrInternal
	}

	if rowsAffected == 0 {
		return common.ErrNotFoundMessage
	}
	return nil
}

func (fr *ForqRepo) UpdateStaleMessages(ctx context.Context) (int64, error) {
	nowMs := time.Now().UnixMilli()

	query := `
        UPDATE messages 
        SET 
            status = CASE 
            	WHEN attempts  >= ? THEN ?	-- failed if no more attempts left
            	ELSE ?						-- ready if there are attempts left
			END,
            process_after = ?,				-- immediate retry for stale messages (consumer likely crashed)
            processing_started_at = NULL,
            updated_at = ?
        WHERE status = ? AND processing_started_at < ?;`

	res, err := fr.dbWrite.ExecContext(ctx, query,
		fr.appConfigs.MaxDeliveryAttempts, // WHEN attempts >= ? (status check)
		common.FailedStatus,               // THEN ?  			-- failed if no more attempts left
		common.ReadyStatus,                // ELSE ?			-- ready if there are attempts left
		nowMs,                             // process_after = ? -- immediate retry
		nowMs,                             // updated_at = ?
		common.ProcessingStatus,           // WHERE status = ?
		nowMs-fr.appConfigs.MaxProcessingTimeMs, // AND processing_started_at < ?;
	)
	if err != nil {
		log.Error().Err(err).Msg("failed to update stale messages")
		return 0, common.ErrInternal
	}
	if res == nil {
		return 0, nil
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		log.Error().Err(err).Msg("failed to get rows affected after updating stale messages")
		return 0, common.ErrInternal
	}
	return rowsAffected, nil
}

func (fr *ForqRepo) UpdateFailedMessagesForRegularQueues(ctx context.Context) (int64, error) {
	nowMs := time.Now().UnixMilli()

	query := `
        UPDATE messages
        SET
            attempts = 0,
            status = ?,
            queue = queue || ?,
            is_dlq = TRUE,              -- Set DLQ flag
            process_after = ?,
            processing_started_at = NULL,
            failure_reason = ?,
            updated_at = ?,
            expires_after = ?
        WHERE status = ? AND is_dlq = FALSE;`

	res, err := fr.dbWrite.ExecContext(ctx, query,
		common.ReadyStatus,                     // status = ?
		common.DlqSuffix,                       // queue = queue || ?
		nowMs,                                  // process_after = ?
		common.MaxAttemptsReachedFailureReason, // failure_reason = ?
		nowMs,                                  // updated_at = ?
		nowMs+fr.appConfigs.DlqTtlMs,           // expires_after = ?
		common.FailedStatus,                    // WHERE status = ?
	)
	if err != nil {
		log.Error().Err(err).Msg("failed to update failed messages for regular queues")
		return 0, common.ErrInternal
	}
	if res == nil {
		return 0, nil
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		log.Error().Err(err).Msg("failed to get rows affected after updating failed messages for regular queues")
		return 0, common.ErrInternal
	}
	return rowsAffected, nil
}

func (fr *ForqRepo) UpdateExpiredMessagesForRegularQueues(ctx context.Context) (int64, error) {
	nowMs := time.Now().UnixMilli()

	query := `
        UPDATE messages
        SET
            attempts = 0,
            status = ?,
            queue = queue || ?,
            is_dlq = TRUE,              -- Set DLQ flag
            process_after = ?,
            processing_started_at = NULL,
            failure_reason = ?,
            updated_at = ?,
            expires_after = ?
        WHERE status != ? AND is_dlq = FALSE AND expires_after < ?;`

	res, err := fr.dbWrite.ExecContext(ctx, query,
		common.ReadyStatus,                 // status = ?
		common.DlqSuffix,                   // queue = queue || ?
		nowMs,                              // process_after = ?
		common.MessageExpiredFailureReason, // failure_reason = ?
		nowMs,                              // updated_at = ?
		nowMs+fr.appConfigs.DlqTtlMs,       // expires_after = ?
		common.ProcessingStatus,            // WHERE status != ?
		nowMs,                              // AND expires_after < ?
	)
	if err != nil {
		log.Error().Err(err).Msg("failed to update expired messages for regular queues")
		return 0, common.ErrInternal
	}
	if res == nil {
		return 0, nil
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		log.Error().Err(err).Msg("failed to get rows affected after updating expired messages for regular queues")
		return 0, common.ErrInternal
	}
	return rowsAffected, nil
}

func (fr *ForqRepo) RequeueDlqMessages(queueName string, ctx context.Context) (int64, error) {
	nowMs := time.Now().UnixMilli()
	destinationQueueName := strings.TrimSuffix(queueName, common.DlqSuffix)

	query := `
		UPDATE messages
		SET
			queue = ?, 			-- Move back to regular queue
			is_dlq = FALSE,     -- Unset DLQ flag
			status = ?,
			attempts = 0,
			process_after = ?,
			processing_started_at = NULL,
			failure_reason = NULL,
			updated_at = ?,
			expires_after = ?
		WHERE queue = ? AND status != ?;`

	res, err := fr.dbWrite.ExecContext(ctx, query,
		destinationQueueName,           // queue = ? -- Move back to regular queue
		common.ReadyStatus,             // status = ?
		nowMs,                          // process_after = ?
		nowMs,                          // updated_at = ?
		nowMs+fr.appConfigs.QueueTtlMs, // expires_after = ?
		queueName,                      // WHERE queue = ?
		common.ProcessingStatus,        // AND status != ?
	)
	if err != nil {
		log.Error().Err(err).Str("queue", queueName).Msg("failed to update messages by moving from DLQ to regular")
		return 0, common.ErrInternal
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		log.Error().Err(err).Str("queue", queueName).Msg("failed to get rows affected after requeueing DLQ messages")
		return 0, common.ErrInternal
	}
	return rowsAffected, nil
}

func (fr *ForqRepo) RequeueDlqMessage(messageId string, queueName string, ctx context.Context) error {
	nowMs := time.Now().UnixMilli()
	destinationQueueName := strings.TrimSuffix(queueName, common.DlqSuffix)

	query := `
		UPDATE messages
		SET
			queue = ?, 			-- Move back to regular queue
			is_dlq = FALSE,     -- Unset DLQ flag
			status = ?,
			attempts = 0,
			process_after = ?,
			processing_started_at = NULL,
			failure_reason = NULL,
			updated_at = ?,
			expires_after = ?
		WHERE id = ? AND queue = ? AND status != ?;`

	result, err := fr.dbWrite.ExecContext(ctx, query,
		destinationQueueName,           // queue = ? -- Move back to regular queue
		common.ReadyStatus,             // status = ?
		nowMs,                          // process_after = ?
		nowMs,                          // updated_at = ?
		nowMs+fr.appConfigs.QueueTtlMs, // expires_after = ?
		messageId,                      // WHERE id = ?
		queueName,                      // AND queue = ?
		common.ProcessingStatus,        // AND status != ?
	)
	if err != nil {
		log.Error().Err(err).Str("queue", queueName).Str("message_id", messageId).Msg("failed to update message by moving from DLQ to regular")
		return common.ErrInternal
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return common.ErrInternal
	}

	if rowsAffected == 0 {
		return common.ErrNotFoundMessage
	}
	return nil
}

func (fr *ForqRepo) DeleteMessageFromDlq(messageId string, queueName string, ctx context.Context) error {
	query := `
		DELETE FROM messages
		WHERE id = ? AND queue = ? AND is_dlq = TRUE;`

	result, err := fr.dbWrite.ExecContext(ctx, query,
		messageId, // WHERE id = ?
		queueName, // AND queue = ?
	)
	if err != nil {
		log.Error().Err(err).Str("queue", queueName).Msg("failed to delete message")
		return common.ErrInternal
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		log.Error().Err(err).Str("queue", queueName).Msg("failed to get rows affected after delete")
		return common.ErrInternal
	}

	if rowsAffected == 0 {
		log.Warn().Str("queue", queueName).Str("message_id", messageId).Msg("no rows deleted, message was either deleted already or does not exist")
	}
	return nil
}

func (fr *ForqRepo) DeleteMessageOnAck(messageId string, queueName string, ctx context.Context) error {
	query := `
		DELETE FROM messages
		WHERE id = ? AND queue = ? AND status = ?;`

	result, err := fr.dbWrite.ExecContext(ctx, query,
		messageId,               // WHERE id = ?
		queueName,               // AND queue = ?
		common.ProcessingStatus, // AND status = ?
	)
	if err != nil {
		log.Error().Err(err).Str("queue", queueName).Msg("failed to delete message on ack")
		return common.ErrInternal
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		log.Error().Err(err).Str("queue", queueName).Msg("failed to get rows affected after delete on ack")
		return common.ErrInternal
	}

	if rowsAffected == 0 {
		log.Warn().Str("queue", queueName).Str("message_id", messageId).Msg("no rows deleted on ack, message was either deleted already or does not exist")
	}
	return nil
}

func (fr *ForqRepo) DeleteFailedMessagesFromDlq(ctx context.Context) (int64, error) {
	query := `
        DELETE FROM messages
        WHERE status = ? AND is_dlq = TRUE;`

	res, err := fr.dbWrite.ExecContext(ctx, query,
		common.FailedStatus, // WHERE status = ?
	)
	if err != nil {
		log.Error().Err(err).Msg("failed to delete failed messages from DLQ")
		return 0, common.ErrInternal
	}
	if res == nil {
		return 0, nil
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		log.Error().Err(err).Msg("failed to get rows affected after deleting failed messages from DLQ")
		return 0, common.ErrInternal
	}
	return rowsAffected, nil
}

func (fr *ForqRepo) DeleteExpiredMessagesFromDlq(ctx context.Context) (int64, error) {
	nowMs := time.Now().UnixMilli()

	query := `
        DELETE FROM messages
        WHERE status != ? AND is_dlq = TRUE AND expires_after < ?;`

	res, err := fr.dbWrite.ExecContext(ctx, query,
		common.ProcessingStatus, // WHERE status != ?
		nowMs,                   // expires_after < ?
	)

	if err != nil {
		log.Error().Err(err).Msg("failed to delete expired messages from DLQ")
		return 0, common.ErrInternal
	}
	if res == nil {
		return 0, nil
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		log.Error().Err(err).Msg("failed to get rows affected after deleting expired messages from DLQ")
		return 0, common.ErrInternal
	}
	return rowsAffected, nil
}

func (fr *ForqRepo) DeleteAllMessagesFromQueue(queueName string, ctx context.Context) (int64, error) {
	query := `
		DELETE FROM messages
		WHERE queue = ?;`

	res, err := fr.dbWrite.ExecContext(ctx, query,
		queueName, // WHERE queue = ?
	)

	if err != nil {
		log.Error().Err(err).Str("queue", queueName).Msg("failed to delete all messages from queue")
		return 0, common.ErrInternal
	}
	if res == nil {
		return 0, nil
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		log.Error().Err(err).Str("queue", queueName).Msg("failed to get rows affected after deleting all messages from queue")
		return 0, common.ErrInternal
	}
	return rowsAffected, nil
}

func (fr *ForqRepo) Ping(ctx context.Context) error {
	err := fr.dbRead.PingContext(ctx)
	if err != nil {
		return err
	}
	return fr.dbWrite.Ping()
}

func (fr *ForqRepo) Optimize(ctx context.Context) error {
	// SQLite docs recommend running "PRAGMA optimize;" periodically to optimize the database
	// https://www.sqlite.org/pragma.html#pragma_optimize
	_, err := fr.dbWrite.ExecContext(ctx, "PRAGMA optimize;")
	if err != nil {
		log.Error().Err(err).Msg("failed to optimize database")
		return common.ErrInternal
	}
	return nil
}

func (fr *ForqRepo) Close() error {
	var err1, err2 error
	if fr.dbRead != nil {
		err1 = fr.dbRead.Close()
	}
	if fr.dbWrite != nil {
		err2 = fr.dbWrite.Close()
	}

	if err1 != nil {
		return err1
	}
	return err2
}

func (fr *ForqRepo) processAfterCases(nowMs int64) string {
	var processAfterCases strings.Builder

	// builds WHEN clauses for each backoff delay
	for i, delay := range fr.appConfigs.BackoffDelaysMs {
		if i < len(fr.appConfigs.BackoffDelaysMs)-1 {
			processAfterCases.WriteString(fmt.Sprintf("WHEN attempts + 1 = %d THEN %d ", i+1, nowMs+delay))
		} else {
			processAfterCases.WriteString(fmt.Sprintf("ELSE %d ", nowMs+delay))
		}
	}
	return processAfterCases.String()
}
