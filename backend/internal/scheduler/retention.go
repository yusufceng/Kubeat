package scheduler

import (
	"context"
	"log/slog"
	"time"
)

// ResultPruner deletes check results older than a cutoff.
type ResultPruner interface {
	DeleteCheckResultsBefore(ctx context.Context, cutoff time.Time) (int64, error)
}

// RunRetention deletes check results older than retention, once at start and
// then every interval, until ctx is cancelled.
func RunRetention(ctx context.Context, pruner ResultPruner, retention, interval time.Duration, logger *slog.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		n, err := pruner.DeleteCheckResultsBefore(ctx, time.Now().Add(-retention))
		switch {
		case err != nil && ctx.Err() == nil:
			logger.Error("retention cleanup failed", "error", err)
		case n > 0:
			logger.Info("old check results deleted", "rows", n)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
