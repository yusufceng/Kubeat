package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// Rollups is the part of the persistence layer the rollup job needs.
type Rollups interface {
	RollupCheckResults(ctx context.Context, from, to time.Time) error
	DeleteRollupsBefore(ctx context.Context, cutoff time.Time) (int64, error)
}

// RollupOptions configures RunRollups.
type RollupOptions struct {
	// RawRetention is how long raw results are kept. Hours older than that
	// are never recomputed, because their raw results are (partly) gone.
	RawRetention time.Duration
	// Interval is how often the current and previous hour are recomputed.
	Interval time.Duration
	// Retention is how long the summaries themselves are kept.
	Retention time.Duration
	Logger    *slog.Logger
}

// RunRollups keeps the hourly summaries up to date until ctx is cancelled.
// At start it summarizes every hour whose raw results are still complete,
// which also fills in whatever was missed while the process was down.
func RunRollups(ctx context.Context, st Rollups, opts RollupOptions) {
	// The backfill goes one hour at a time. Done in a single statement it
	// holds the database's only write connection for as long as it takes to
	// read every result there is, and check results cannot be stored
	// meanwhile; in steps, they get their turn between two hours.
	now := time.Now().UTC()
	for from := oldestCompleteHour(now, opts.RawRetention); from.Before(now) && ctx.Err() == nil; from = from.Add(store.RollupBucket) {
		rollup(ctx, st, from, minTime(from.Add(store.RollupBucket), now), opts.Logger)
	}

	ticker := time.NewTicker(opts.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		now = time.Now().UTC()
		// The previous hour too: results of its last seconds may have been
		// written after the hour ended.
		from := now.Truncate(store.RollupBucket).Add(-store.RollupBucket)
		if oldest := oldestCompleteHour(now, opts.RawRetention); from.Before(oldest) {
			from = oldest
		}
		rollup(ctx, st, from, now, opts.Logger)

		if n, err := st.DeleteRollupsBefore(ctx, now.Add(-opts.Retention)); err != nil && ctx.Err() == nil {
			opts.Logger.Error("rollup retention cleanup failed", "error", err)
		} else if n > 0 {
			opts.Logger.Info("old rollups deleted", "rows", n)
		}
	}
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// oldestCompleteHour is the first hour boundary at or after now-retention:
// the earliest hour whose raw results have not started to be pruned.
func oldestCompleteHour(now time.Time, retention time.Duration) time.Time {
	cutoff := now.Add(-retention)
	hour := cutoff.Truncate(store.RollupBucket)
	if hour.Before(cutoff) {
		hour = hour.Add(store.RollupBucket)
	}
	return hour
}

func rollup(ctx context.Context, st Rollups, from, to time.Time, logger *slog.Logger) {
	if !from.Before(to) {
		return
	}
	if err := st.RollupCheckResults(ctx, from, to); err != nil && ctx.Err() == nil {
		logger.Error("rollup failed", "from", from, "to", to, "error", err)
	}
}
