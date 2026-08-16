package jobs

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
)

// Runner executes a tick function on a fixed interval with a per-tick context
// timeout and panic recovery. All background jobs share this lifecycle: the
// single goroutine per job means ticks never overlap themselves, and a panic
// in one tick is logged and swallowed so a background job can never take down
// the whole process - the next tick runs as usual.
type Runner struct {
	ticker *time.Ticker
	done   chan struct{}
}

func NewRunner(name string, intervalMs int64, tickTimeoutMs int64, tick func(ctx context.Context)) *Runner {
	ticker := time.NewTicker(time.Duration(intervalMs) * time.Millisecond)
	done := make(chan struct{})

	go func() {
		for {
			select {
			case <-ticker.C:
				runTick(name, tickTimeoutMs, tick)
			case <-done:
				return
			}
		}
	}()

	return &Runner{
		ticker: ticker,
		done:   done,
	}
}

func runTick(name string, tickTimeoutMs int64, tick func(ctx context.Context)) {
	defer func() {
		if r := recover(); r != nil {
			log.Error().Interface("panic", r).Str("job", name).Msg("panic in background job tick, will run again on the next tick")
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(tickTimeoutMs)*time.Millisecond)
	defer cancel()
	tick(ctx)
}

func (r *Runner) Close() error {
	r.ticker.Stop()
	close(r.done)
	return nil
}
