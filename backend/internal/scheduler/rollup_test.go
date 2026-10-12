package scheduler

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

type recordingRollups struct {
	mu     sync.Mutex
	ranges [][2]time.Time
	pruned []time.Time
}

func (r *recordingRollups) RollupCheckResults(_ context.Context, from, to time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ranges = append(r.ranges, [2]time.Time{from, to})
	return nil
}

func (r *recordingRollups) DeleteRollupsBefore(_ context.Context, cutoff time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pruned = append(r.pruned, cutoff)
	return 0, nil
}

func (r *recordingRollups) snapshot() ([][2]time.Time, []time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][2]time.Time(nil), r.ranges...), append([]time.Time(nil), r.pruned...)
}

func TestOldestCompleteHour(t *testing.T) {
	now := time.Date(2026, 10, 12, 14, 25, 0, 0, time.UTC)
	tests := []struct {
		retention time.Duration
		want      time.Time
	}{
		// Cutoff 11:25: the 11:00 hour is already being pruned, 12:00 is whole.
		{3 * time.Hour, time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)},
		// Cutoff exactly on a boundary keeps that hour.
		{2*time.Hour + 25*time.Minute, time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)},
		{7 * 24 * time.Hour, time.Date(2026, 10, 5, 15, 0, 0, 0, time.UTC)},
		// Shorter than the time into the hour: nothing is complete yet.
		{10 * time.Minute, time.Date(2026, 10, 12, 15, 0, 0, 0, time.UTC)},
	}
	for _, tt := range tests {
		if got := oldestCompleteHour(now, tt.retention); !got.Equal(tt.want) {
			t.Errorf("oldestCompleteHour(retention=%s) = %s, want %s", tt.retention, got, tt.want)
		}
	}
}

func TestRunRollupsBackfillsThenRefreshesRecentHours(t *testing.T) {
	st := &recordingRollups{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	started := time.Now().UTC()
	go func() {
		defer close(done)
		RunRollups(ctx, st, RollupOptions{
			RawRetention: 48 * time.Hour,
			Interval:     5 * time.Millisecond,
			Retention:    30 * 24 * time.Hour,
			Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		})
	}()
	waitFor(t, "periodic rollups", func() bool {
		ranges, _ := st.snapshot()
		return len(ranges) >= 3
	})
	cancel()
	<-done

	ranges, pruned := st.snapshot()
	for i, r := range ranges {
		if !r[0].Equal(r[0].Truncate(store.RollupBucket)) {
			t.Errorf("range %d starts off the hour: %s", i, r[0])
		}
		if !r[0].Before(r[1]) {
			t.Errorf("range %d is empty: %v", i, r)
		}
	}
	// The first run reaches back over the whole raw retention.
	if age := started.Sub(ranges[0][0]); age < 46*time.Hour || age > 48*time.Hour {
		t.Errorf("backfill starts %s ago, want just under 48h", age)
	}
	// Later runs only redo the previous and the current hour.
	if age := started.Sub(ranges[1][0]); age < time.Hour-time.Second || age > 2*time.Hour {
		t.Errorf("periodic run starts %s ago, want between 1h and 2h", age)
	}
	if len(pruned) == 0 {
		t.Fatal("rollup retention never ran")
	}
	if age := started.Sub(pruned[0]); age < 30*24*time.Hour-time.Minute || age > 30*24*time.Hour+time.Minute {
		t.Errorf("rollup retention cutoff is %s ago, want 30 days", age)
	}
}

func TestRunRollupsNeverRecomputesPrunedHours(t *testing.T) {
	st := &recordingRollups{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		RunRollups(ctx, st, RollupOptions{
			RawRetention: time.Nanosecond, // everything but the future is pruned
			Interval:     5 * time.Millisecond,
			Retention:    time.Hour,
			Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		})
	}()
	waitFor(t, "a few ticks", func() bool {
		_, pruned := st.snapshot()
		return len(pruned) >= 3
	})
	cancel()
	<-done

	if ranges, _ := st.snapshot(); len(ranges) != 0 {
		t.Errorf("rolled up hours whose raw results are gone: %v", ranges)
	}
}
