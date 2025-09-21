package cleanup

import (
	"context"
	"time"

	"github.com/n0rdy/forq/db"
	"github.com/n0rdy/forq/metrics"

	"github.com/rs/zerolog/log"
)

type StaleMessagesCleanupJob struct {
	repo       *db.ForqRepo
	intervalMs int64
	ticker     *time.Ticker
	done       chan struct{}
}

func NewStaleMessagesCleanupJob(metricsService metrics.Service, repo *db.ForqRepo, intervalMs int64) *StaleMessagesCleanupJob {
	ticker := time.NewTicker(time.Duration(intervalMs) * time.Millisecond)
	done := make(chan struct{})

	go func() {
		for {
			select {
			case <-ticker.C:
				ctx, cancelFunc := context.WithTimeout(context.Background(), time.Duration(intervalMs-1000)*time.Millisecond)
				rowsAffected, err := repo.UpdateStaleMessages(ctx)
				if err != nil {
					log.Error().Err(err).Msg("failed to update stale messages by StaleMessagesCleanupJob")
				} else {
					metricsService.IncMessagesStaleRecoveredTotalBy(rowsAffected)
				}
				cancelFunc()
			case <-done:
				return
			}
		}
	}()

	return &StaleMessagesCleanupJob{
		ticker: ticker,
		done:   done,
	}
}

func (j *StaleMessagesCleanupJob) Close() error {
	j.ticker.Stop()
	close(j.done)
	return nil
}
