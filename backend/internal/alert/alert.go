// Package alert turns check results into incidents and notifies the
// configured channels when an incident opens or resolves.
package alert

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// Event is what a notification reports.
type Event string

const (
	EventDown Event = "down" // an incident opened
	EventUp   Event = "up"   // an incident resolved
	EventTest Event = "test" // a channel test requested by a user
)

// Notification is one message to deliver to every enabled channel.
type Notification struct {
	Event    Event
	Monitor  store.Monitor
	Incident store.Incident
}

// Store is the part of the persistence layer the engine needs.
type Store interface {
	GetMonitor(ctx context.Context, id int64) (store.Monitor, error)
	ListCheckResults(ctx context.Context, monitorID int64, limit int) ([]store.CheckResult, error)
	GetOpenIncident(ctx context.Context, monitorID int64) (store.Incident, error)
	OpenIncident(ctx context.Context, i store.Incident) (store.Incident, error)
	ResolveIncident(ctx context.Context, id int64, resolvedAt time.Time) error
	ListChannels(ctx context.Context) ([]store.Channel, error)
}

// Sender delivers one notification to one channel.
type Sender interface {
	Send(ctx context.Context, channel store.Channel, n Notification) error
}

// Options configures an Engine.
type Options struct {
	// RecoveryThreshold is how many consecutive successful checks resolve an
	// open incident. Values above 1 keep a flapping target from producing a
	// stream of down/up notifications.
	RecoveryThreshold int
	// QueueSize bounds notifications waiting for delivery; when it is full
	// new ones are dropped and logged rather than stalling result writes.
	QueueSize int
	// Attempts is the total number of tries per channel, RetryDelay the
	// pause between them and SendTimeout the limit of a single try.
	Attempts    int
	RetryDelay  time.Duration
	SendTimeout time.Duration
	Logger      *slog.Logger
}

// Engine evaluates results and delivers notifications.
type Engine struct {
	store  Store
	sender Sender
	opts   Options
	queue  chan Notification
}

// New returns an Engine. Run must be started for notifications to be sent.
func New(st Store, sender Sender, opts Options) *Engine {
	return &Engine{store: st, sender: sender, opts: opts, queue: make(chan Notification, opts.QueueSize)}
}

// Evaluate updates incidents for the monitors in results, which must already
// be stored. It must not be called concurrently with itself.
func (e *Engine) Evaluate(ctx context.Context, results []store.CheckResult) {
	seen := make(map[int64]bool, len(results))
	for _, r := range results {
		if seen[r.MonitorID] {
			continue
		}
		seen[r.MonitorID] = true
		if err := e.evaluate(ctx, r.MonitorID); err != nil {
			e.opts.Logger.Error("alert evaluation failed", "monitor_id", r.MonitorID, "error", err)
		}
	}
}

func (e *Engine) evaluate(ctx context.Context, monitorID int64) error {
	m, err := e.store.GetMonitor(ctx, monitorID)
	if errors.Is(err, store.ErrNotFound) {
		return nil // deleted while its results were in flight
	}
	if err != nil {
		return err
	}
	if !m.Enabled {
		// A check that was already running when the monitor was paused must
		// not open an incident nobody is watching for.
		return nil
	}

	incident, err := e.store.GetOpenIncident(ctx, monitorID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return e.maybeOpen(ctx, m)
	case err != nil:
		return err
	default:
		return e.maybeResolve(ctx, m, incident)
	}
}

// maybeOpen opens an incident when the newest FailureThreshold results all failed.
func (e *Engine) maybeOpen(ctx context.Context, m store.Monitor) error {
	streak, err := e.streak(ctx, m.ID, m.FailureThreshold, false)
	if err != nil || streak == nil {
		return err
	}
	incident, err := e.store.OpenIncident(ctx, store.Incident{
		MonitorID: m.ID,
		StartedAt: streak[len(streak)-1].CheckedAt, // the first failure of the streak
		Cause:     streak[0].Error,                 // the most recent error
	})
	if err != nil {
		return err
	}
	incident.MonitorName = m.Name
	e.opts.Logger.Warn("incident opened", "monitor", m.Name, "incident_id", incident.ID, "cause", incident.Cause)
	e.enqueue(Notification{Event: EventDown, Monitor: m, Incident: incident})
	return nil
}

// maybeResolve resolves the incident when the newest RecoveryThreshold results all succeeded.
func (e *Engine) maybeResolve(ctx context.Context, m store.Monitor, incident store.Incident) error {
	streak, err := e.streak(ctx, m.ID, e.opts.RecoveryThreshold, true)
	if err != nil || streak == nil {
		return err
	}
	incident.ResolvedAt = streak[len(streak)-1].CheckedAt // the first success of the streak
	if err := e.store.ResolveIncident(ctx, incident.ID, incident.ResolvedAt); err != nil {
		return err
	}
	e.opts.Logger.Info("incident resolved", "monitor", m.Name, "incident_id", incident.ID,
		"duration", incident.ResolvedAt.Sub(incident.StartedAt).String())
	e.enqueue(Notification{Event: EventUp, Monitor: m, Incident: incident})
	return nil
}

// streak returns the newest n results (newest first) if there are n of them
// and every one has Up == up; otherwise nil.
func (e *Engine) streak(ctx context.Context, monitorID int64, n int, up bool) ([]store.CheckResult, error) {
	results, err := e.store.ListCheckResults(ctx, monitorID, n)
	if err != nil {
		return nil, err
	}
	if len(results) < n {
		return nil, nil
	}
	for _, r := range results {
		if r.Up != up {
			return nil, nil
		}
	}
	return results, nil
}

func (e *Engine) enqueue(n Notification) {
	select {
	case e.queue <- n:
	default:
		e.opts.Logger.Error("notification queue full, notification dropped",
			"event", n.Event, "monitor", n.Monitor.Name, "incident_id", n.Incident.ID)
	}
}

// Run delivers queued notifications until ctx is cancelled, then delivers
// what is still queued (without retries) and returns.
func (e *Engine) Run(ctx context.Context) {
	for {
		select {
		case n := <-e.queue:
			e.deliver(ctx, n)
		case <-ctx.Done():
			for {
				select {
				case n := <-e.queue:
					e.deliver(ctx, n)
				default:
					return
				}
			}
		}
	}
}

func (e *Engine) deliver(ctx context.Context, n Notification) {
	// Reads and sends must still work while shutting down.
	channels, err := e.store.ListChannels(context.WithoutCancel(ctx))
	if err != nil {
		e.opts.Logger.Error("list channels failed, notification lost", "event", n.Event, "monitor", n.Monitor.Name, "error", err)
		return
	}
	for _, ch := range channels {
		if !ch.Enabled {
			continue
		}
		if err := e.sendWithRetry(ctx, ch, n); err != nil {
			e.opts.Logger.Error("notification failed", "channel", ch.Name, "event", n.Event, "monitor", n.Monitor.Name, "error", err)
			continue
		}
		e.opts.Logger.Info("notification sent", "channel", ch.Name, "event", n.Event, "monitor", n.Monitor.Name)
	}
}

func (e *Engine) sendWithRetry(ctx context.Context, ch store.Channel, n Notification) error {
	var err error
	for attempt := 1; attempt <= e.opts.Attempts; attempt++ {
		if err = e.SendOnce(ctx, ch, n); err == nil {
			return nil
		}
		if attempt == e.opts.Attempts {
			break
		}
		select {
		case <-time.After(e.opts.RetryDelay):
		case <-ctx.Done():
			return err // shutting down: do not wait for another try
		}
	}
	return err
}

// SendOnce makes a single delivery attempt, bounded by SendTimeout. An
// attempt already under way is not cut short by shutdown.
func (e *Engine) SendOnce(ctx context.Context, ch store.Channel, n Notification) error {
	sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), e.opts.SendTimeout)
	defer cancel()
	return e.sender.Send(sendCtx, ch, n)
}
