package cleanup

import (
	"context"
	"time"

	"github.com/n0rdy/forq/db"
	"github.com/n0rdy/forq/metrics"

	"github.com/rs/zerolog/log"
)

type FailedMessagesCleanupJob struct {
	repo       *db.ForqRepo
	intervalMs int64
	ticker     *time.Ticker
	done       chan struct{}
}

func NewFailedMessagesCleanupJob(metricsService metrics.Service, repo *db.ForqRepo, intervalMs int64) *FailedMessagesCleanupJob {
	ticker := time.NewTicker(time.Duration(intervalMs) * time.Millisecond)
	done := make(chan struct{})

	go func() {
		for {
			select {
			case <-ticker.C:
				ctx, cancelFunc := context.WithTimeout(context.Background(), time.Duration(intervalMs-1000)*time.Millisecond)
				rowsAffected, err := repo.UpdateFailedMessagesForRegularQueues(ctx)
				if err != nil {
					log.Error().Err(err).Msg("failed to update failed messages for regular queues by FailedMessagesCleanupJob")
				} else {
					metricsService.IncMessagesMovedToDlqTotalBy(rowsAffected, metrics.FailedMovedToDlqReason)
				}
				cancelFunc()
			case <-done:
				return
			}
		}
	}()

	return &FailedMessagesCleanupJob{
		ticker: ticker,
		done:   done,
	}
}

func (j *FailedMessagesCleanupJob) Close() error {
	j.ticker.Stop()
	close(j.done)
	return nil
}
