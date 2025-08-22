package cleanup

import (
	"context"
	"forq/db"
	"time"

	"github.com/rs/zerolog/log"
)

type StaleMessagesCleanupJob struct {
	repo       *db.ForqRepo
	intervalMs int64
	ticker     *time.Ticker
	done       chan struct{}
}

func NewStaleMessagesCleanupJob(repo *db.ForqRepo, intervalMs int64) *StaleMessagesCleanupJob {
	ticker := time.NewTicker(time.Duration(intervalMs) * time.Millisecond)
	done := make(chan struct{})

	go func() {
		for {
			select {
			case <-ticker.C:
				ctx, cancelFunc := context.WithTimeout(context.Background(), time.Duration(intervalMs-1000)*time.Millisecond)
				if err := repo.UpdateStaleMessages(ctx); err != nil {
					log.Error().Err(err).Msg("failed to update stale messages")
				}
				cancelFunc()
			case <-done:
				return
			}
		}
	}()

	return &StaleMessagesCleanupJob{
		repo:       repo,
		intervalMs: intervalMs,
		ticker:     ticker,
	}
}

func (j *StaleMessagesCleanupJob) Close() error {
	j.ticker.Stop()
	close(j.done)
	return nil
}
