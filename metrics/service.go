package metrics

const (
	FailedMovedToDlqReason  = "failed"
	ExpiredMovedToDlqReason = "expired"

	FailedCleanupReason        = "failed"
	ExpiredCleanupReason       = "expired"
	DeletedByUserCleanupReason = "deleted_by_user"
)

type Service interface {
	IncMessagesProducedTotalBy(count int64, queueName string)
	IncMessagesConsumedTotalBy(count int64, queueName string)
	IncMessagesAckedTotalBy(count int64, queueName string)
	IncMessagesNackedTotalBy(count int64, queueName string)
	IncMessagesRequeuedTotalBy(count int64, queueName string)
	SetQueueDepth(queueName string, depth int64)
	IncMessagesMovedToDlqTotalBy(count int64, reason string)
	IncMessagesStaleRecoveredTotalBy(count int64)
	IncMessagesCleanupTotalBy(count int64, reason string)
}

func NewMetricsService(metricsEnabled bool) Service {
	if metricsEnabled {
		return newPrometheusMetricsService()
	}
	return newNoopMetricsService()
}
