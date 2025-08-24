package services

import (
	"context"
	"forq/common"
	"forq/db"
)

type QueuesService struct {
	forqRepo *db.ForqRepo
}

func NewQueuesService(forqRepo *db.ForqRepo) *QueuesService {
	return &QueuesService{
		forqRepo: forqRepo,
	}
}

func (qs *QueuesService) GetQueuesStats(ctx context.Context) (*common.DashboardPageData, error) {
	queues, err := qs.forqRepo.SelectAllQueuesWithStats(ctx)
	if err != nil {
		return nil, err
	}

	totalMessages := 0
	dlqMessages := 0
	var queuesStats []common.QueueStats
	for _, q := range queues {
		queueType := "Regular"
		if q.IsDLQ {
			queueType = "DLQ"
			dlqMessages += q.MessagesCount
		}
		totalMessages += q.MessagesCount

		queuesStats = append(queuesStats, common.QueueStats{
			Name:          q.Name,
			TotalMessages: q.MessagesCount,
			Type:          queueType,
		})
	}

	return &common.DashboardPageData{
		Title:         "Dashboard",
		TotalQueues:   len(queues),
		TotalMessages: totalMessages,
		DLQMessages:   dlqMessages,
		Queues:        queuesStats,
	}, nil
}

func (qs *QueuesService) GetQueueStats(queueName string, ctx context.Context) (*common.QueueStats, error) {
	queueMeta, err := qs.forqRepo.SelectQueueStats(queueName, ctx)
	if err != nil {
		return nil, err
	}
	if queueMeta == nil {
		return nil, nil
	}

	queueType := "Regular"
	if queueMeta.IsDLQ {
		queueType = "DLQ"
	}

	return &common.QueueStats{
		Name:          queueMeta.Name,
		TotalMessages: queueMeta.MessagesCount,
		Type:          queueType,
	}, nil
}
