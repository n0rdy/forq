package metrics

import (
	"strings"

	"github.com/n0rdy/forq/common"

	"github.com/prometheus/client_golang/prometheus"
)

type PrometheusMetricsService struct {
	messagesProducedTotal       *prometheus.CounterVec
	messagesConsumedTotal       *prometheus.CounterVec
	messagesAckedTotal          *prometheus.CounterVec
	messagesNackedTotal         *prometheus.CounterVec
	messagesRequeuedTotal       *prometheus.CounterVec
	queueDepth                  *prometheus.GaugeVec
	messagesMovedToDlqTotal     *prometheus.CounterVec
	messagesStaleRecoveredTotal prometheus.Counter
	messagesCleanupTotal        *prometheus.CounterVec
}

func newPrometheusMetricsService() *PrometheusMetricsService {
	srv := &PrometheusMetricsService{
		messagesProducedTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "forq_messages_produced_total",
				Help: "Total number of messages submitted to Forq by producers",
			},
			[]string{"queue_name", "queue_type"},
		),

		messagesConsumedTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "forq_messages_consumed_total",
				Help: "Total number of messages consumed by consumers. Note, this doesn't mean ack-ed or nack-ed, just fetched for processing",
			},
			[]string{"queue_name", "queue_type"},
		),

		messagesAckedTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "forq_messages_acked_total",
				Help: "Total number of messages acknowledged by Forq",
			},
			[]string{"queue_name", "queue_type"},
		),

		messagesNackedTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "forq_messages_nacked_total",
				Help: "Total number of messages nacknowledged by Forq",
			},
			[]string{"queue_name", "queue_type"},
		),

		// no queue type label here, as requeuing is done only from DLQ to Regular queue.
		// queue_name specifies which queue the message is requeued to, so a Regular queue name.
		messagesRequeuedTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "forq_messages_requeued_total",
				Help: "Total number of messages moved from DLQ back to main queue manually by the admin",
			},
			[]string{"queue_name"},
		),

		queueDepth: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "forq_queue_depth",
				Help: "Current depth of the queue",
			},
			[]string{"queue_name", "queue_type"},
		),

		// no queue name label here, as the moving op is performed by the cronjob,
		// so it will have a performance impact on SQL query to have to group by queue name instead of doing fire-and-forget UPDATE.
		// queue type is not relevant here, as this metric shows when the message is moved from the Regular queue to DQL.
		messagesMovedToDlqTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "forq_messages_moved_to_dlq_total",
				Help: "Total number of messages moved to dead-letter queue",
			},
			[]string{"reason"},
		),

		// no queue name and queue type labels here, as the stale recovery is performed by the cronjob,
		// so it will have a performance impact on SQL query to have to group by queue name instead of doing fire-and-forget UPDATE.
		messagesStaleRecoveredTotal: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "forq_messages_stale_recovered_total",
				Help: "Total number of stale messages recovered",
			},
		),

		// no queue name label here, as the moving op is performed by the cronjob,
		// so it will have a performance impact on SQL query to have to group by queue name instead of doing fire-and-forget UPDATE.
		// queue type is not relevant here, as this concerns only DLQs.
		messagesCleanupTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "forq_messages_cleanup_total",
				Help: "Total number of messages cleaned up from DLQs",
			},
			[]string{"reason"},
		),
	}

	prometheus.MustRegister(srv.messagesProducedTotal)
	prometheus.MustRegister(srv.messagesConsumedTotal)
	prometheus.MustRegister(srv.messagesAckedTotal)
	prometheus.MustRegister(srv.messagesNackedTotal)
	prometheus.MustRegister(srv.messagesRequeuedTotal)
	prometheus.MustRegister(srv.queueDepth)
	prometheus.MustRegister(srv.messagesMovedToDlqTotal)
	prometheus.MustRegister(srv.messagesStaleRecoveredTotal)
	prometheus.MustRegister(srv.messagesCleanupTotal)

	return srv
}

func (pms *PrometheusMetricsService) IncMessagesProducedTotalBy(count int64, queueName string) {
	pms.messagesProducedTotal.WithLabelValues(queueName, pms.queueType(queueName)).Add(float64(count))
}

func (pms *PrometheusMetricsService) IncMessagesConsumedTotalBy(count int64, queueName string) {
	pms.messagesConsumedTotal.WithLabelValues(queueName, pms.queueType(queueName)).Add(float64(count))
}

func (pms *PrometheusMetricsService) IncMessagesAckedTotalBy(count int64, queueName string) {
	pms.messagesAckedTotal.WithLabelValues(queueName, pms.queueType(queueName)).Add(float64(count))
}

func (pms *PrometheusMetricsService) IncMessagesNackedTotalBy(count int64, queueName string) {
	pms.messagesNackedTotal.WithLabelValues(queueName, pms.queueType(queueName)).Add(float64(count))
}

func (pms *PrometheusMetricsService) IncMessagesRequeuedTotalBy(count int64, queueName string) {
	pms.messagesRequeuedTotal.WithLabelValues(queueName).Add(float64(count))
}

func (pms *PrometheusMetricsService) SetQueueDepth(queueName string, depth int64) {
	pms.queueDepth.WithLabelValues(queueName, pms.queueType(queueName)).Set(float64(depth))
}

func (pms *PrometheusMetricsService) IncMessagesMovedToDlqTotalBy(count int64, reason string) {
	pms.messagesMovedToDlqTotal.WithLabelValues(reason).Add(float64(count))
}

func (pms *PrometheusMetricsService) IncMessagesStaleRecoveredTotalBy(count int64) {
	pms.messagesStaleRecoveredTotal.Add(float64(count))
}

func (pms *PrometheusMetricsService) IncMessagesCleanupTotalBy(count int64, reason string) {
	pms.messagesCleanupTotal.WithLabelValues(reason).Add(float64(count))
}

func (pms *PrometheusMetricsService) queueType(queueName string) string {
	if strings.HasSuffix(queueName, common.DlqSuffix) {
		return "dlq"
	}
	return "regular"
}
