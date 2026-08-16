package cleanup

import (
	"context"

	"github.com/n0rdy/forq/db"
	"github.com/n0rdy/forq/jobs"
	"github.com/n0rdy/forq/metrics"

	"github.com/rs/zerolog/log"
)

func NewExpiredDlqMessagesCleanupJob(metricsService metrics.Service, repo *db.ForqRepo, intervalMs int64) *jobs.Runner {
	return jobs.NewRunner("expired-dlq-messages-cleanup", intervalMs, intervalMs-1000, func(ctx context.Context) {
		rowsAffected, err := repo.DeleteExpiredMessagesFromDlq(ctx)
		if err != nil {
			log.Error().Err(err).Msg("failed to delete expired DLQ messages by ExpiredDlqMessagesCleanupJob")
		} else {
			metricsService.IncMessagesCleanupTotalBy(rowsAffected, metrics.ExpiredCleanupReason)
		}
	})
}
