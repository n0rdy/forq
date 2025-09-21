package cleanup

import (
	"context"
	"time"

	"github.com/n0rdy/forq/db"
	"github.com/n0rdy/forq/metrics"

	"github.com/rs/zerolog/log"
)

type ExpiredMessagesCleanupJob struct {
	ticker *time.Ticker
	done   chan struct{}
}

func NewExpiredMessagesCleanupJob(metricsService metrics.Service, repo *db.ForqRepo, intervalMs int64) *ExpiredMessagesCleanupJob {
	ticker := time.NewTicker(time.Duration(intervalMs) * time.Millisecond)
	done := make(chan struct{})

	go func() {
		for {
			select {
			case <-ticker.C:
				ctx, cancelFunc := context.WithTimeout(context.Background(), time.Duration(intervalMs-1000)*time.Millisecond)
				rowsAffected, err := repo.UpdateExpiredMessagesForRegularQueues(ctx)
				if err != nil {
					log.Error().Err(err).Msg("failed to update expired messages by ExpiredMessagesCleanupJob")
				} else {
					metricsService.IncMessagesMovedToDlqTotalBy(rowsAffected, metrics.ExpiredMovedToDlqReason)
				}
				cancelFunc()
			case <-done:
				return
			}
		}
	}()

	return &ExpiredMessagesCleanupJob{
		ticker: ticker,
		done:   done,
	}
}

func (j *ExpiredMessagesCleanupJob) Close() error {
	j.ticker.Stop()
	close(j.done)
	return nil
}
