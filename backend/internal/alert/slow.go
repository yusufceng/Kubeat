package alert

import (
	"context"
	"errors"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// evaluateSlow keeps the monitor's slow-response state (store.Monitor
// .SlowSince): it turns slow when its newest FailureThreshold results all
// succeeded but each took longer than SlowAbove, and responds in time again
// when its newest RecoveryThreshold results all succeeded within it. Failed
// results change nothing: an outage is the incident's story, and the state
// is read again once the checks succeed. No incident is opened; each change
// is one notification, about no incident, so the delivery log keeps none.
// In maintenance a monitor does not turn slow, but one that was slow still
// recovers, so that its story gets an ending.
func (e *Engine) evaluateSlow(ctx context.Context, m store.Monitor, quiet bool) error {
	if m.SlowAbove <= 0 {
		return nil // the store clears SlowSince along with the threshold
	}
	if m.SlowSince.IsZero() {
		if quiet {
			return nil
		}
		streak, err := e.latencyStreak(ctx, m, max(m.FailureThreshold, 1), true)
		if err != nil || streak == nil {
			return err
		}
		since := streak[len(streak)-1].CheckedAt // the first slow check of the streak
		if err := e.store.SetMonitorSlowSince(ctx, m.ID, since); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil // deleted while its results were in flight
			}
			return err
		}
		e.opts.Logger.Warn("monitor slow", "monitor", m.Name, "threshold", m.SlowAbove, "latency", streak[0].Latency)
		e.enqueue(Notification{Event: EventSlow, Monitor: m, At: streak[0].CheckedAt, Since: since, Threshold: m.SlowAbove, Latency: streak[0].Latency})
		return nil
	}

	streak, err := e.latencyStreak(ctx, m, max(e.opts.RecoveryThreshold, 1), false)
	if err != nil || streak == nil {
		return err
	}
	if err := e.store.SetMonitorSlowSince(ctx, m.ID, time.Time{}); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		return err
	}
	e.opts.Logger.Info("monitor responds in time again", "monitor", m.Name, "slow_for", streak[0].CheckedAt.Sub(m.SlowSince).String())
	e.enqueue(Notification{Event: EventSlowResolved, Monitor: m, At: streak[0].CheckedAt, Since: m.SlowSince, Threshold: m.SlowAbove, Latency: streak[0].Latency})
	return nil
}

// latencyStreak returns the newest n results (newest first) if there are n
// of them, every one succeeded, and every one took longer than the
// monitor's threshold (slow) or none did (not slow); otherwise nil.
func (e *Engine) latencyStreak(ctx context.Context, m store.Monitor, n int, slow bool) ([]store.CheckResult, error) {
	results, err := e.store.ListCheckResults(ctx, m.ID, n)
	if err != nil {
		return nil, err
	}
	if len(results) < n {
		return nil, nil
	}
	for _, r := range results {
		if !r.Up || (r.Latency > m.SlowAbove) != slow {
			return nil, nil
		}
	}
	return results, nil
}
