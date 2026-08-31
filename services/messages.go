package services

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/n0rdy/forq/common"
	"github.com/n0rdy/forq/configs"
	"github.com/n0rdy/forq/db"
	"github.com/n0rdy/forq/metrics"

	"github.com/rs/zerolog/log"
)

const (
	processAfterBufferMs = 10 * 1000 // 10 seconds buffer for process_after in case of clock skew or network delays
)

type MessagesService struct {
	metricsService metrics.Service
	forqRepo       *db.ForqRepo
	appConfigs     *configs.AppConfigs
}

func NewMessagesService(metricsService metrics.Service, forqRepo *db.ForqRepo, appConfigs *configs.AppConfigs) *MessagesService {
	return &MessagesService{
		metricsService: metricsService,
		forqRepo:       forqRepo,
		appConfigs:     appConfigs,
	}
}

func (ms *MessagesService) ProcessNewMessage(newMessage common.NewMessageRequest, queueName string, ctx context.Context) error {
	// producing directly into a "-dlq" queue would create rows with the DLQ
	// suffix but is_dlq = FALSE, confusing the dashboard/queue-page/DLQ-move
	// logic (and a 5x failure would mint "foo-dlq-dlq"). DLQ messages are
	// consumable, but they only enter a DLQ via the failure/expiry paths.
	if strings.HasSuffix(queueName, common.DlqSuffix) {
		log.Error().Str("queue", queueName).Msg("attempt to produce directly into a DLQ")
		return common.ErrBadRequestProduceToDlq
	}

	if len(newMessage.Content) > ms.appConfigs.MessageContentMaxSizeBytes {
		log.Error().Int("size", len(newMessage.Content)).Msg("message content exceeds limit")
		return common.ErrBadRequestContentExceedsLimit
	}

	nowMs := time.Now().UnixMilli()

	var processAfter int64
	if newMessage.ProcessAfter == 0 {
		processAfter = nowMs
	} else {
		if newMessage.ProcessAfter+processAfterBufferMs < nowMs {
			log.Error().Int64("process_after", newMessage.ProcessAfter).Msg("process_after is in the past")
			return common.ErrBadRequestProcessAfterInPast
		}
		if newMessage.ProcessAfter > nowMs+ms.appConfigs.MaxProcessAfterDelayMs {
			log.Error().Int64("process_after", newMessage.ProcessAfter).Msg("process_after is too far in the future")
			return common.ErrBadRequestProcessAfterTooFar
		}
		processAfter = newMessage.ProcessAfter
	}

	messageId := uuid.NewV7()
	messageToInsert := db.NewMessage{
		Id:           messageId.String(),
		QueueName:    queueName,
		Content:      newMessage.Content,
		ProcessAfter: processAfter,
		ReceivedAt:   nowMs,
		UpdatedAt:    nowMs,
		ExpiresAfter: processAfter + ms.appConfigs.QueueTtlMs,
	}

	err := ms.forqRepo.InsertMessage(&messageToInsert, ctx)
	if err != nil {
		return err
	}
	ms.metricsService.IncMessagesProducedTotalBy(1, queueName)
	return nil
}

func (ms *MessagesService) GetMessageForConsuming(queueName string, ctx context.Context) (*common.MessageResponse, error) {
	start := time.Now()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		message, err := ms.forqRepo.SelectMessageForConsuming(queueName, ctx)
		if err != nil {
			return nil, err
		}
		if message != nil {
			ms.metricsService.IncMessagesConsumedTotalBy(1, queueName)
			return &common.MessageResponse{
				Id:      message.Id,
				Content: message.Content,
				Receipt: strconv.FormatInt(message.ProcessingStartedAt, 10),
			}, nil
		}

		// no message found, check if we should keep polling. Return nil if polling duration exceeded
		if time.Since(start).Milliseconds() > ms.appConfigs.PollingDurationMs {
			return nil, nil
		}

		select {
		case <-ticker.C:
			// continue polling
		case <-ctx.Done():
			// client disconnected or request timed out - normal for long polling, not an error
			return nil, nil
		}
	}
}

func (ms *MessagesService) AckMessage(messageId string, queueName string, receipt string, ctx context.Context) error {
	parsedReceipt, err := ms.parseReceipt(receipt)
	if err != nil {
		return err
	}

	err = ms.forqRepo.DeleteMessageOnAck(messageId, queueName, parsedReceipt, ctx)
	if err != nil {
		return err
	}
	ms.metricsService.IncMessagesAckedTotalBy(1, queueName)
	return nil
}

func (ms *MessagesService) NackMessage(messageId string, queueName string, receipt string, ctx context.Context) error {
	parsedReceipt, err := ms.parseReceipt(receipt)
	if err != nil {
		return err
	}

	err = ms.forqRepo.UpdateMessageOnConsumingFailure(messageId, queueName, parsedReceipt, ctx)
	if err != nil {
		return err
	}
	ms.metricsService.IncMessagesNackedTotalBy(1, queueName)
	return nil
}

// parseReceipt validates the delivery receipt echoed back by the consumer.
// A missing receipt gets a distinct error code, as it is the loud signal of an
// outdated SDK/client rather than a malformed value.
func (ms *MessagesService) parseReceipt(receipt string) (int64, error) {
	if receipt == "" {
		return 0, common.ErrBadRequestReceiptMissing
	}
	parsed, err := strconv.ParseInt(receipt, 10, 64)
	if err != nil {
		return 0, common.ErrBadRequestReceiptInvalid
	}
	return parsed, nil
}

func (ms *MessagesService) RequeueAllDlqMessages(queueName string, ctx context.Context) error {
	if !strings.HasSuffix(queueName, common.DlqSuffix) {
		log.Error().Str("queue", queueName).Msg("attempt to requeue non-DLQ queue: only DLQ queues are supported for requeueing")
		return common.ErrBadRequestDlqOnlyOp
	}

	rowsAffected, err := ms.forqRepo.RequeueDlqMessages(queueName, ctx)
	if err != nil {
		return err
	}
	ms.metricsService.IncMessagesRequeuedTotalBy(rowsAffected, queueName)
	return nil
}

func (ms *MessagesService) RequeueDlqMessage(messageId string, queueName string, ctx context.Context) error {
	if !strings.HasSuffix(queueName, common.DlqSuffix) {
		log.Error().Str("queue", queueName).Msg("attempt to requeue non-DLQ queue: only DLQ queues are supported for requeueing")
		return common.ErrBadRequestDlqOnlyOp
	}

	err := ms.forqRepo.RequeueDlqMessage(messageId, queueName, ctx)
	if err != nil {
		return err
	}
	ms.metricsService.IncMessagesRequeuedTotalBy(1, queueName)
	return nil
}

func (ms *MessagesService) DeleteAllDlqMessages(queueName string, ctx context.Context) error {
	if !strings.HasSuffix(queueName, common.DlqSuffix) {
		log.Error().Str("queue", queueName).Msg("attempt to delete non-DLQ queue: only DLQ queues are supported for deleting all messages")
		return common.ErrBadRequestDlqOnlyOp
	}

	rowsAffected, err := ms.forqRepo.DeleteAllMessagesFromQueue(queueName, ctx)
	if err != nil {
		return err
	}
	ms.metricsService.IncMessagesCleanupTotalBy(rowsAffected, metrics.DeletedByUserCleanupReason)
	return nil
}

func (ms *MessagesService) DeleteDlqMessage(messageId string, queueName string, ctx context.Context) error {
	if !strings.HasSuffix(queueName, common.DlqSuffix) {
		log.Error().Str("queue", queueName).Msg("attempt to delete non-DLQ queue: only DLQ queues are supported for deleting messages")
		return common.ErrBadRequestDlqOnlyOp
	}

	err := ms.forqRepo.DeleteMessageFromDlq(messageId, queueName, ctx)
	if err != nil {
		return err
	}
	ms.metricsService.IncMessagesCleanupTotalBy(1, metrics.DeletedByUserCleanupReason)
	return nil
}

func (ms *MessagesService) GetMessagesForUI(queueName string, cursor string, limit int, ctx context.Context) (*common.MessagesComponentData, error) {
	// fetches limit+1 to check if there are more messages
	dbMessages, err := ms.forqRepo.SelectMessagesForUI(queueName, cursor, limit+1, ctx)
	if err != nil {
		return nil, err
	}

	// checks if there are more messages and determine pagination
	var hasMore bool
	var messages []common.MessageMetadata
	if len(dbMessages) > limit {
		hasMore = true
		messages = ms.convertToMessageMetadata(dbMessages[:limit])
	} else {
		hasMore = false
		messages = ms.convertToMessageMetadata(dbMessages)
	}

	// determines next cursor (last message ID)
	var nextCursor string
	if hasMore && len(messages) > 0 {
		nextCursor = messages[len(messages)-1].ID
	}

	// determines if this is a DLQ queue
	isDLQ := strings.HasSuffix(queueName, common.DlqSuffix)

	return &common.MessagesComponentData{
		Messages:   messages,
		NextCursor: nextCursor,
		HasMore:    hasMore,
		QueueName:  queueName,
		IsDLQ:      isDLQ,
	}, nil
}

func (ms *MessagesService) GetMessageDetails(messageId string, queueName string, ctx context.Context) (*common.MessageDetails, error) {
	dbMessage, err := ms.forqRepo.SelectMessageDetails(messageId, queueName, ctx)
	if err != nil {
		return nil, err
	}
	if dbMessage == nil {
		return nil, nil
	}

	processingStartedAt := ""
	if dbMessage.ProcessingStartedAt != nil {
		processingStartedAt = ms.formatTimestamp(*dbMessage.ProcessingStartedAt)
	}

	failureReason := ""
	if dbMessage.FailureReason != nil {
		failureReason = *dbMessage.FailureReason
	}

	return &common.MessageDetails{
		ID:                  dbMessage.Id,
		Content:             dbMessage.Content,
		Status:              ms.convertStatusToString(dbMessage.Status),
		Attempts:            dbMessage.Attempts,
		ReceivedAt:          ms.formatTimestamp(dbMessage.ReceivedAt),
		Age:                 ms.formatAge(dbMessage.ReceivedAt),
		ProcessAfter:        ms.formatTimestamp(dbMessage.ProcessAfter),
		ProcessingStartedAt: processingStartedAt,
		FailureReason:       failureReason,
		UpdatedAt:           ms.formatTimestamp(dbMessage.UpdatedAt),
	}, nil
}

func (ms *MessagesService) convertToMessageMetadata(dbMessages []db.MessageMetadata) []common.MessageMetadata {
	var messages []common.MessageMetadata
	for _, dbMsg := range dbMessages {
		messages = append(messages, common.MessageMetadata{
			ID:           dbMsg.Id,
			Status:       ms.convertStatusToString(dbMsg.Status),
			Attempts:     dbMsg.Attempts,
			Age:          ms.formatAge(dbMsg.ReceivedAt),
			ProcessAfter: ms.formatTimestamp(dbMsg.ProcessAfter),
		})
	}
	return messages
}

func (ms *MessagesService) convertStatusToString(status int) string {
	switch status {
	case common.ReadyStatus:
		return "ready"
	case common.ProcessingStatus:
		return "processing"
	case common.FailedStatus:
		return "failed"
	default:
		return "unknown"
	}
}

func (ms *MessagesService) formatTimestamp(timestampMs int64) string {
	if timestampMs == 0 {
		return ""
	}
	return time.UnixMilli(timestampMs).Format("2006-01-02 15:04:05")
}

func (ms *MessagesService) formatAge(timestampMs int64) string {
	if timestampMs == 0 {
		return ""
	}

	duration := time.Since(time.UnixMilli(timestampMs))

	// Handle negative durations (future timestamps) by taking absolute value
	if duration < 0 {
		duration = -duration
	}

	if duration < time.Minute {
		return fmt.Sprintf("%d seconds ago", int(duration.Seconds()))
	} else if duration < time.Hour {
		return fmt.Sprintf("%d minutes ago", int(duration.Minutes()))
	} else if duration < 24*time.Hour {
		return fmt.Sprintf("%d hours ago", int(duration.Hours()))
	} else {
		return fmt.Sprintf("%d days ago", int(duration.Hours()/24))
	}
}
