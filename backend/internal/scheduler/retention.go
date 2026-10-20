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

// DeliveryPruner deletes the delivery log's entries older than a cutoff.
type DeliveryPruner interface {
	DeleteDeliveriesBefore(ctx context.Context, cutoff time.Time) (int64, error)
}

// RunRetention deletes check results older than retention, once at start and
// then every interval, until ctx is cancelled.
func RunRetention(ctx context.Context, pruner ResultPruner, retention, interval time.Duration, logger *slog.Logger) {
	prune(ctx, interval, logger, "check results", func(ctx context.Context) (int64, error) {
		return pruner.DeleteCheckResultsBefore(ctx, time.Now().Add(-retention))
	})
}

// RunDeliveryRetention deletes delivery log entries older than retention,
// once at start and then every interval, until ctx is cancelled. Every
// notification about every incident adds to the log; without a bound it
// would grow for as long as Kubeat runs. The incidents themselves stay,
// and an entry also goes with its incident.
func RunDeliveryRetention(ctx context.Context, pruner DeliveryPruner, retention, interval time.Duration, logger *slog.Logger) {
	prune(ctx, interval, logger, "delivery log entries", func(ctx context.Context) (int64, error) {
		return pruner.DeleteDeliveriesBefore(ctx, time.Now().Add(-retention))
	})
}

// prune runs one cleanup at start and then every interval, until ctx is
// cancelled, and logs what it did; what names the rows it deletes.
func prune(ctx context.Context, interval time.Duration, logger *slog.Logger, what string, cleanup func(context.Context) (int64, error)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		n, err := cleanup(ctx)
		switch {
		case err != nil && ctx.Err() == nil:
			logger.Error("retention cleanup failed", "of", what, "error", err)
		case n > 0:
			logger.Info("old "+what+" deleted", "rows", n)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
