package metrics

import (
	"context"

	"github.com/n0rdy/forq/db"
	"github.com/n0rdy/forq/jobs"
	"github.com/n0rdy/forq/metrics"

	"github.com/rs/zerolog/log"
)

func NewQueuesDepthMetricsJob(metricsService metrics.Service, repo *db.ForqRepo, intervalMs int64) *jobs.Runner {
	return jobs.NewRunner("queues-depth-metrics", intervalMs, intervalMs-1000, func(ctx context.Context) {
		queuesStats, err := repo.SelectAllQueuesWithStats(ctx)
		if err != nil {
			log.Error().Err(err).Msg("failed to fetch queues stats by QueuesDepthMetricsJob")
			return
		}

		// reset so drained/purged queues drop to absent instead of
		// reporting their last non-zero depth forever
		metricsService.ResetQueueDepths()
		for _, qs := range queuesStats {
			metricsService.SetQueueDepth(qs.Name, int64(qs.MessagesCount))
		}
	})
}
