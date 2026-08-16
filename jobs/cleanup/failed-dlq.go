package cleanup

import (
	"context"

	"github.com/n0rdy/forq/db"
	"github.com/n0rdy/forq/jobs"
	"github.com/n0rdy/forq/metrics"

	"github.com/rs/zerolog/log"
)

func NewFailedDlqMessagesCleanupJob(metricsService metrics.Service, repo *db.ForqRepo, intervalMs int64) *jobs.Runner {
	return jobs.NewRunner("failed-dlq-messages-cleanup", intervalMs, intervalMs-1000, func(ctx context.Context) {
		rowsAffected, err := repo.DeleteFailedMessagesFromDlq(ctx)
		if err != nil {
			log.Error().Err(err).Msg("failed to delete failed DLQ messages by FailedDlqMessagesCleanupJob")
		} else {
			metricsService.IncMessagesCleanupTotalBy(rowsAffected, metrics.FailedCleanupReason)
		}
	})
}
