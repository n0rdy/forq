package metrics

import (
	"context"
	"time"

	"github.com/n0rdy/forq/db"
	"github.com/n0rdy/forq/metrics"

	"github.com/rs/zerolog/log"
)

type QueuesDepthMetricsJob struct {
	ticker *time.Ticker
	done   chan struct{}
}

func NewQueuesDepthMetricsJob(metricsService metrics.Service, repo *db.ForqRepo, intervalMs int64) *QueuesDepthMetricsJob {
	ticker := time.NewTicker(time.Duration(intervalMs) * time.Millisecond)
	done := make(chan struct{})

	go func() {
		for {
			select {
			case <-ticker.C:
				ctx, cancelFunc := context.WithTimeout(context.Background(), time.Duration(intervalMs-1000)*time.Millisecond)
				queuesStats, err := repo.SelectAllQueuesWithStats(ctx)
				if err != nil {
					log.Error().Err(err).Msg("failed to fetch queues stats by QueuesDepthMetricsJob")
				} else {
					for _, qs := range queuesStats {
						metricsService.SetQueueDepth(qs.Name, int64(qs.MessagesCount))
					}
				}
				cancelFunc()
			case <-done:
				return
			}
		}
	}()

	return &QueuesDepthMetricsJob{
		ticker: ticker,
		done:   done,
	}
}

func (j *QueuesDepthMetricsJob) Close() error {
	j.ticker.Stop()
	close(j.done)
	return nil
}
