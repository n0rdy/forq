package maintenance

import (
	"context"
	"time"

	"github.com/n0rdy/forq/db"
)

type DbOptimizationJob struct {
	ticker *time.Ticker
	done   chan struct{}
}

func NewDbOptimizationJob(repo *db.ForqRepo, intervalMs int64, maxDurationMs int64) *DbOptimizationJob {
	ticker := time.NewTicker(time.Duration(intervalMs) * time.Millisecond)
	done := make(chan struct{})

	go func() {
		for {
			select {
			case <-ticker.C:
				ctx, cancelFunc := context.WithTimeout(context.Background(), time.Duration(maxDurationMs)*time.Millisecond)
				repo.Optimize(ctx)
				cancelFunc()
			case <-done:
				return
			}
		}
	}()

	return &DbOptimizationJob{
		ticker: ticker,
		done:   done,
	}
}

func (j *DbOptimizationJob) Close() error {
	j.ticker.Stop()
	close(j.done)
	return nil
}
