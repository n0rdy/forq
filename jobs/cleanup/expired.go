package cleanup

import (
	"context"
	"forq/db"
	"time"

	"github.com/rs/zerolog/log"
)

type ExpiredMessagesCleanupJob struct {
	repo       *db.ForqRepo
	intervalMs int64
	ticker     *time.Ticker
	done       chan struct{}
}

func NewExpiredMessagesCleanupJob(repo *db.ForqRepo, intervalMs int64) *ExpiredMessagesCleanupJob {
	ticker := time.NewTicker(time.Duration(intervalMs) * time.Millisecond)
	done := make(chan struct{})

	go func() {
		for {
			select {
			case <-ticker.C:
				ctx, cancelFunc := context.WithTimeout(context.Background(), time.Duration(intervalMs-1000)*time.Millisecond)
				if err := repo.UpdateExpiredMessagesForRegularQueues(ctx); err != nil {
					log.Error().Err(err).Msg("failed to update expired messages")
				}
				cancelFunc()
			case <-done:
				return
			}
		}
	}()

	return &ExpiredMessagesCleanupJob{
		repo:       repo,
		intervalMs: intervalMs,
		ticker:     ticker,
		done:       done,
	}
}

func (j *ExpiredMessagesCleanupJob) Close() error {
	j.ticker.Stop()
	close(j.done)
	return nil
}
