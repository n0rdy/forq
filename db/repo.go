package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"forq/common"
	"forq/configs"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	_ "modernc.org/sqlite"
)

type ForqRepo struct {
	db         *sql.DB
	appConfigs *configs.AppConfigs
}

func NewSQLiteRepo(dbPath string, appConfigs *configs.AppConfigs) (*ForqRepo, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return &ForqRepo{
		db:         db,
		appConfigs: appConfigs,
	}, nil
}

func (fr *ForqRepo) InsertMessage(newMessage *NewMessage, ctx context.Context) error {
	query := `
		INSERT INTO messages (id, queue, content, process_after, received_at, updated_at, expires_after)
		VALUES (?, ?, ?, ?, ?, ?, ?);
	`

	_, err := fr.db.ExecContext(ctx, query,
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
              AND expires_after > ?
            ORDER BY received_at ASC
            LIMIT 1
        )
        RETURNING id, content;`

	var msg MessageForConsuming
	err := fr.db.QueryRowContext(ctx, query,
		common.ProcessingStatus, // SET status = ?
		nowMs,                   // processing_started_at = ?
		nowMs,                   // updated_at = ?
		queueName,               // WHERE queue = ?
		common.ReadyStatus,      // AND status = ?
		nowMs,                   // AND process_after <= ?
		nowMs,                   // AND expires_after > ?
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

func (fr *ForqRepo) UpdateMessageOnConsumingFailure(messageId string, queueName string, ctx context.Context) error {
	nowMs := time.Now().UnixMilli()

	query := fmt.Sprintf(`
        UPDATE messages 
        SET 
            attempts = attempts + 1,
            status = CASE 
            	WHEN attempts + 1 >= ? THEN ?	-- failed if no more attempts left
            	ELSE ?							-- ready if there are attempts left
			END,
            process_after = CASE 
                %s
            END,
            processing_started_at = NULL,
            updated_at = ?
        WHERE id = ? AND queue = ?;`, fr.processAfterCases(nowMs))

	result, err := fr.db.ExecContext(ctx, query,
		fr.appConfigs.MaxDeliveryAttempts, // WHEN attempts + 1 >= ? (status check)
		common.FailedStatus,               // THEN ?  		-- failed if no more attempts left
		common.ReadyStatus,                // ELSE ?			-- ready if there are attempts left
		nowMs,                             // updated_at = ?
		messageId,                         // WHERE id = ?
		queueName,                         // AND queue = ?
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

func (fr *ForqRepo) UpdateStaleMessages(ctx context.Context) error {
	nowMs := time.Now().UnixMilli()

	query := fmt.Sprintf(`
        UPDATE messages 
        SET 
            attempts = attempts + 1,
            status = CASE 
            	WHEN attempts + 1 >= ? THEN ?	-- failed if no more attempts left
            	ELSE ?							-- ready if there are attempts left
			END,
            process_after = CASE 
                %s
            END,
            processing_started_at = NULL,
            updated_at = ?
        WHERE status = ? AND processing_started_at < ?;`, fr.processAfterCases(nowMs))

	_, err := fr.db.ExecContext(ctx, query,
		fr.appConfigs.MaxDeliveryAttempts,       // WHEN attempts + 1 >= ? (status check)
		common.FailedStatus,                     // THEN ?  			-- failed if no more attempts left
		common.ReadyStatus,                      // ELSE ?			-- ready if there are attempts left
		nowMs,                                   // updated_at = ?
		common.ProcessingStatus,                 // WHERE status = ?
		nowMs-fr.appConfigs.MaxProcessingTimeMs, // AND processing_started_at < ?;
	)
	return err
}

func (fr *ForqRepo) UpdateFailedMessagesForRegularQueues(ctx context.Context) error {
	nowMs := time.Now().UnixMilli()

	query := `
        UPDATE messages
        SET
            attempts = 0,
            status = ?,
            queue = queue || '-dlq',
            is_dlq = TRUE,              -- Set DLQ flag
            process_after = ?,
            processing_started_at = NULL,
            failure_reason = ?,
            updated_at = ?,
            expires_after = ?
        WHERE status = ? AND is_dlq = FALSE;`

	_, err := fr.db.ExecContext(ctx, query,
		common.ReadyStatus,                     // status = ?
		nowMs,                                  // process_after = ?
		common.MaxAttemptsReachedFailureReason, // failure_reason = ?
		nowMs,                                  // updated_at = ?
		nowMs+fr.appConfigs.DlqTtlMs,           // expires_after = ?
		common.FailedStatus,                    // WHERE status = ?
	)
	return err
}

func (fr *ForqRepo) UpdateExpiredMessagesForRegularQueues(ctx context.Context) error {
	nowMs := time.Now().UnixMilli()

	query := `
        UPDATE messages
        SET
            attempts = 0,
            status = ?,
            queue = queue || '-dlq',
            is_dlq = TRUE,              -- Set DLQ flag
            process_after = ?,
            processing_started_at = NULL,
            failure_reason = ?,
            updated_at = ?,
            expires_after = ?
        WHERE status != ? AND expires_after < ? AND is_dlq = FALSE;`

	_, err := fr.db.ExecContext(ctx, query,
		common.ReadyStatus,                 // status = ?
		nowMs,                              // process_after = ?
		common.MessageExpiredFailureReason, // failure_reason = ?
		nowMs,                              // updated_at = ?
		nowMs+fr.appConfigs.DlqTtlMs,       // expires_after = ?
		common.ProcessingStatus,            // WHERE status != ?
		nowMs,                              // AND expires_after < ?
	)
	return err
}

func (fr *ForqRepo) DeleteMessage(messageId string, queueName string, ctx context.Context) error {
	query := `
		DELETE FROM messages
		WHERE id = ? AND queue = ?;`

	result, err := fr.db.ExecContext(ctx, query,
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

func (fr *ForqRepo) DeleteFailedMessagesFromDlq(ctx context.Context) error {
	query := `
        DELETE FROM messages
        WHERE status = ? AND is_dlq = TRUE;`

	_, err := fr.db.ExecContext(ctx, query,
		common.FailedStatus, // WHERE status = ?
	)
	return err
}

func (fr *ForqRepo) DeleteExpiredMessagesFromDlq(ctx context.Context) error {
	nowMs := time.Now().UnixMilli()

	query := `
        DELETE FROM messages
        WHERE status != ? AND expires_after < ? AND is_dlq = TRUE;`

	_, err := fr.db.ExecContext(ctx, query,
		common.ProcessingStatus, // WHERE status != ?
		nowMs,                   // expires_after < ?
	)
	return err
}

func (fr *ForqRepo) Close() error {
	return fr.db.Close()
}

func (fr *ForqRepo) processAfterCases(nowMs int64) string {
	var processAfterCases strings.Builder

	// Build WHEN clauses for each backoff delay
	for i, delay := range fr.appConfigs.BackoffDelaysMs {
		if i < len(fr.appConfigs.BackoffDelaysMs)-1 {
			processAfterCases.WriteString(fmt.Sprintf("WHEN attempts + 1 = %d THEN %d ", i+1, nowMs+delay))
		} else {
			processAfterCases.WriteString(fmt.Sprintf("ELSE %d ", nowMs+delay))
		}
	}
	return processAfterCases.String()
}
