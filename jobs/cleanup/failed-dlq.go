package cleanup

import (
	"context"
	"forq/db"
	"time"

	"github.com/rs/zerolog/log"
)

type FailedDlqMessagesCleanupJob struct {
	repo       *db.ForqRepo
	intervalMs int64
	ticker     *time.Ticker
	done       chan struct{}
}

func NewFailedDlqMessagesCleanupJob(repo *db.ForqRepo, intervalMs int64) *FailedDlqMessagesCleanupJob {
	ticker := time.NewTicker(time.Duration(intervalMs) * time.Millisecond)
	done := make(chan struct{})

	go func() {
		for {
			select {
			case <-ticker.C:
				ctx, cancelFunc := context.WithTimeout(context.Background(), time.Duration(intervalMs-1000)*time.Millisecond)
				if err := repo.DeleteFailedMessagesFromDlq(ctx); err != nil {
					log.Error().Err(err).Msg("failed to delete failed DLQ messages")
				}
				cancelFunc()
			case <-done:
				return
			}
		}
	}()

	return &FailedDlqMessagesCleanupJob{
		repo:       repo,
		intervalMs: intervalMs,
		ticker:     ticker,
		done:       done,
	}
}

func (j *FailedDlqMessagesCleanupJob) Close() error {
	j.ticker.Stop()
	close(j.done)
	return nil
}
