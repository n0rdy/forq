package metrics

type NoopMetricsService struct {
}

func newNoopMetricsService() *NoopMetricsService {
	return &NoopMetricsService{}
}

func (nms *NoopMetricsService) IncMessagesProducedTotalBy(count int64, queueName string) {
	// no-op
}

func (nms *NoopMetricsService) IncMessagesConsumedTotalBy(count int64, queueName string) {
	// no-op
}

func (nms *NoopMetricsService) IncMessagesAckedTotalBy(count int64, queueName string) {
	// no-op
}

func (nms *NoopMetricsService) IncMessagesNackedTotalBy(count int64, queueName string) {
	// no-op
}

func (nms *NoopMetricsService) IncMessagesRequeuedTotalBy(count int64, queueName string) {
	// no-op
}

func (nms *NoopMetricsService) SetQueueDepth(queueName string, depth int64) {
	// no-op
}

func (nms *NoopMetricsService) IncMessagesMovedToDlqTotalBy(count int64, reason string) {
	// no-op
}

func (nms *NoopMetricsService) IncMessagesStaleRecoveredTotalBy(count int64) {
	// no-op
}

func (nms *NoopMetricsService) IncMessagesCleanupTotalBy(count int64, reason string) {
	// no-op
}
