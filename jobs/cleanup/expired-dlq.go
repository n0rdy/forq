package cleanup

import (
	"context"
	"forq/db"
	"forq/metrics"
	"time"

	"github.com/rs/zerolog/log"
)

type ExpiredDlqMessagesCleanupJob struct {
	ticker *time.Ticker
	done   chan struct{}
}

func NewExpiredDlqMessagesCleanupJob(metricsService metrics.Service, repo *db.ForqRepo, intervalMs int64) *ExpiredDlqMessagesCleanupJob {
	ticker := time.NewTicker(time.Duration(intervalMs) * time.Millisecond)
	done := make(chan struct{})

	go func() {
		for {
			select {
			case <-ticker.C:
				ctx, cancelFunc := context.WithTimeout(context.Background(), time.Duration(intervalMs-1000)*time.Millisecond)
				rowsAffected, err := repo.DeleteExpiredMessagesFromDlq(ctx)
				if err != nil {
					log.Error().Err(err).Msg("failed to delete expired DLQ messages by ExpiredDlqMessagesCleanupJob")
				} else {
					metricsService.IncMessagesCleanupTotalBy(rowsAffected, metrics.ExpiredCleanupReason)
				}
				cancelFunc()
			case <-done:
				return
			}
		}
	}()

	return &ExpiredDlqMessagesCleanupJob{
		ticker: ticker,
		done:   done,
	}
}

func (j *ExpiredDlqMessagesCleanupJob) Close() error {
	j.ticker.Stop()
	close(j.done)
	return nil
}
