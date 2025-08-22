package cleanup

import (
	"context"
	"forq/db"
	"time"

	"github.com/rs/zerolog/log"
)

type FailedMessagesCleanupJob struct {
	repo       *db.ForqRepo
	intervalMs int64
	ticker     *time.Ticker
	done       chan struct{}
}

func NewFailedMessagesCleanupJob(repo *db.ForqRepo, intervalMs int64) *FailedMessagesCleanupJob {
	ticker := time.NewTicker(time.Duration(intervalMs) * time.Millisecond)
	done := make(chan struct{})

	go func() {
		for {
			select {
			case <-ticker.C:
				ctx, cancelFunc := context.WithTimeout(context.Background(), time.Duration(intervalMs-1000)*time.Millisecond)
				if err := repo.UpdateFailedMessagesForRegularQueues(ctx); err != nil {
					log.Error().Err(err).Msg("failed to update failed messages for regular queues")
				}
				cancelFunc()
			case <-done:
				return
			}
		}
	}()

	return &FailedMessagesCleanupJob{
		repo:       repo,
		intervalMs: intervalMs,
		ticker:     ticker,
		done:       done,
	}
}

func (j *FailedMessagesCleanupJob) Close() error {
	j.ticker.Stop()
	close(j.done)
	return nil
}
