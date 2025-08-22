package services

import (
	"context"
	"forq/common"
	"forq/configs"
	"forq/db"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

const (
	processAfterBufferMs = 10 * 1000 // 10 seconds buffer for process_after in case of clock skew or network delays
)

type MessagesService struct {
	forqRepo   *db.ForqRepo
	appConfigs *configs.AppConfigs
}

func NewMessagesService(forqRepo *db.ForqRepo, appConfigs *configs.AppConfigs) *MessagesService {
	return &MessagesService{
		forqRepo:   forqRepo,
		appConfigs: appConfigs,
	}
}

func (ms *MessagesService) ProcessNewMessage(newMessage common.NewMessageRequest, queueName string, ctx context.Context) error {
	// TODO: think whether we should allow sending empty messages
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

	messageId, err := uuid.NewV7()
	if err != nil {
		log.Error().Err(err).Msg("failed to generate new message ID")
		return common.ErrInternal
	}

	messageToInsert := db.NewMessage{
		Id:           messageId.String(),
		QueueName:    queueName,
		Content:      newMessage.Content,
		ProcessAfter: processAfter,
		ReceivedAt:   nowMs,
		UpdatedAt:    nowMs,
		ExpiresAfter: nowMs + ms.appConfigs.QueueTtlMs,
	}

	err = ms.forqRepo.InsertMessage(&messageToInsert, ctx)
	if err != nil {
		return err
	}
	return nil
}

func (ms *MessagesService) FetchMessage(queueName string, ctx context.Context) (*common.MessageResponse, error) {
	start := time.Now()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		message, err := ms.forqRepo.SelectMessageForConsuming(queueName, ctx)
		if err != nil {
			return nil, err
		}
		if message != nil {
			return &common.MessageResponse{
				Id:      message.Id,
				Content: message.Content,
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
			// client disconnected, stop polling and return
			log.Error().Err(ctx.Err()).Msg("context cancelled while fetching message")
			return nil, common.ErrInternal
		}
	}
}

func (ms *MessagesService) AckMessage(messageId string, queueName string, ctx context.Context) error {
	return ms.forqRepo.DeleteMessage(messageId, queueName, ctx)
}

func (ms *MessagesService) NackMessage(messageId string, queueName string, ctx context.Context) error {
	return ms.forqRepo.UpdateMessageOnConsumingFailure(messageId, queueName, ctx)
}
