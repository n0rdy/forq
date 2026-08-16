package maintenance

import (
	"context"

	"github.com/n0rdy/forq/db"
	"github.com/n0rdy/forq/jobs"
)

func NewDbOptimizationJob(repo *db.ForqRepo, intervalMs int64, maxDurationMs int64) *jobs.Runner {
	return jobs.NewRunner("db-optimization", intervalMs, maxDurationMs, func(ctx context.Context) {
		// errors are already logged inside Optimize
		_ = repo.Optimize(ctx)
	})
}
