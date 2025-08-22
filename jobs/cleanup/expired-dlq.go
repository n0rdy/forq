package cleanup

import (
	"context"
	"forq/db"
	"time"

	"github.com/rs/zerolog/log"
)

type ExpiredDlqMessagesCleanupJob struct {
	repo       *db.ForqRepo
	intervalMs int64
	ticker     *time.Ticker
	done       chan struct{}
}

func NewExpiredDlqMessagesCleanupJob(repo *db.ForqRepo, intervalMs int64) *ExpiredDlqMessagesCleanupJob {
	ticker := time.NewTicker(time.Duration(intervalMs) * time.Millisecond)
	done := make(chan struct{})

	go func() {
		for {
			select {
			case <-ticker.C:
				ctx, cancelFunc := context.WithTimeout(context.Background(), time.Duration(intervalMs-1000)*time.Millisecond)
				if err := repo.DeleteExpiredMessagesFromDlq(ctx); err != nil {
					log.Error().Err(err).Msg("failed to delete expired DLQ messages")
				}
				cancelFunc()
			case <-done:
				return
			}
		}
	}()

	return &ExpiredDlqMessagesCleanupJob{
		repo:       repo,
		intervalMs: intervalMs,
		ticker:     ticker,
		done:       done,
	}
}

func (j *ExpiredDlqMessagesCleanupJob) Close() error {
	j.ticker.Stop()
	close(j.done)
	return nil
}
