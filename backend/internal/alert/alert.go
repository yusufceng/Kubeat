// Package alert turns check results into incidents and notifies the
// configured channels when an incident opens or resolves.
package alert

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/metrics"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// Event is what a notification reports.
type Event string

const (
	EventDown Event = "down" // an incident opened
	EventUp   Event = "up"   // an incident resolved
	EventTest Event = "test" // a channel test requested by a user

	EventReminder Event = "reminder" // an outage is still going on

	EventCertExpiring Event = "cert_expiring" // a certificate is about to expire
	EventCertRenewed  Event = "cert_renewed"  // it was replaced in time
)

// Notification is one message to deliver to every enabled channel.
type Notification struct {
	Event    Event
	Monitor  store.Monitor
	Incident store.Incident
	// CertExpiresAt is set for the certificate events.
	CertExpiresAt time.Time
	// At is when what the notification reports happened, as Kubeat saw
	// it: the check that confirmed the outage or the recovery or that saw
	// the certificate, and for a reminder the moment it fell due. A
	// reminder's text counts the outage up to it; the delivery log keeps
	// it, so that the incident's page can tell the event from its delivery.
	At time.Time
	// Language is what the texts are written in (LanguageEnglish or
	// LanguageTurkish); the engine sets it from its options. PublicURL is
	// where Kubeat is reached from outside, so a message can link to what
	// it is about; empty leaves the links out.
	Language  string
	PublicURL string
	// Group holds the notifications a grouped one stands for: several
	// monitors of one namespace with the same event within GroupWait.
	// Monitor and Incident are then those of the first. Empty for a
	// notification about one monitor.
	Group []Notification
}

// Kind is the channel-facing class of an event (store.ChannelEvents): the
// two certificate events are one kind, a test is none.
func (e Event) Kind() string {
	switch e {
	case EventDown, EventUp, EventReminder:
		return string(e)
	case EventCertExpiring, EventCertRenewed:
		return "cert"
	}
	return ""
}

// machine says whether a channel type is read by software rather than by
// people: such channels get every notification on its own, never grouped,
// so that their consumers keep one record per incident.
func machine(t store.ChannelType) bool {
	return t == store.ChannelWebhook || t == store.ChannelPagerDuty
}

// Store is the part of the persistence layer the engine needs.
type Store interface {
	GetMonitor(ctx context.Context, id int64) (store.Monitor, error)
	ListCheckResults(ctx context.Context, monitorID int64, limit int) ([]store.CheckResult, error)
	GetOpenIncident(ctx context.Context, monitorID int64, kind store.IncidentKind) (store.Incident, error)
	OpenIncident(ctx context.Context, i store.Incident) (store.Incident, error)
	ResolveIncident(ctx context.Context, id int64, resolvedAt time.Time) error
	MarkIncidentNotified(ctx context.Context, id int64, at time.Time) error
	ListChannels(ctx context.Context) ([]store.Channel, error)
	RecordChannelDelivery(ctx context.Context, id int64, at time.Time, deliveryError string) error
	RecordDeliveries(ctx context.Context, deliveries []store.Delivery) error
	InMaintenance(ctx context.Context, m store.Monitor, at time.Time) (bool, error)
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
	// ReminderInterval is how often an outage that is still going on is
	// announced again; zero sends no reminders.
	ReminderInterval time.Duration
	// CertWarning is how long before a certificate's expiry a warning
	// incident opens.
	CertWarning time.Duration
	// QueueSize bounds notifications waiting for delivery; when it is full
	// new ones are dropped and logged rather than stalling result writes.
	QueueSize int
	// Attempts is the total number of tries per channel, RetryDelay the
	// pause between them and SendTimeout the limit of a single try.
	Attempts    int
	RetryDelay  time.Duration
	SendTimeout time.Duration
	// Language the notifications are written in; English when empty.
	Language string
	// PublicURL is Kubeat's address as its users reach it, for the links
	// in notifications; empty leaves them out.
	PublicURL string
	// GroupWait is how long a notification waits for others of the same
	// event in the same namespace, so that an outage of many monitors is
	// one message to people rather than one per monitor; zero sends each
	// at once. Channels read by software (webhook, PagerDuty) are never
	// grouped and never wait: they get every notification at once.
	GroupWait time.Duration
	// Metrics may be nil.
	Metrics *metrics.Metrics
	Logger  *slog.Logger
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
	// One evaluation per monitor, as of its newest result in the batch.
	var order []int64
	newest := make(map[int64]time.Time, len(results))
	for _, r := range results {
		at, seen := newest[r.MonitorID]
		if !seen {
			order = append(order, r.MonitorID)
		}
		if !seen || r.CheckedAt.After(at) {
			newest[r.MonitorID] = r.CheckedAt
		}
	}
	for _, id := range order {
		if err := e.evaluate(ctx, id, newest[id]); err != nil {
			e.opts.Logger.Error("alert evaluation failed", "monitor_id", id, "error", err)
		}
	}
}

func (e *Engine) evaluate(ctx context.Context, monitorID int64, at time.Time) error {
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

	// During maintenance nothing new is raised. Incidents that were already
	// open (and announced) still resolve, so their story gets an ending.
	quiet, err := e.store.InMaintenance(ctx, m, at)
	if err != nil {
		return err
	}

	// The two kinds are independent: a failing evaluation of one must not
	// hide the other.
	return errors.Join(e.evaluateDown(ctx, m, at, quiet), e.evaluateCert(ctx, m, quiet))
}

func (e *Engine) evaluateDown(ctx context.Context, m store.Monitor, at time.Time, quiet bool) error {
	incident, err := e.store.GetOpenIncident(ctx, m.ID, store.IncidentDown)
	switch {
	case errors.Is(err, store.ErrNotFound):
		if quiet {
			return nil
		}
		return e.maybeOpen(ctx, m, at)
	case err != nil:
		return err
	}

	resolved, err := e.maybeResolve(ctx, m, incident)
	if err != nil || resolved || quiet {
		return err
	}
	return e.maybeRemind(ctx, m, incident, at)
}

// maybeRemind announces an outage again once ReminderInterval has passed
// since the last notification about it.
func (e *Engine) maybeRemind(ctx context.Context, m store.Monitor, incident store.Incident, at time.Time) error {
	if e.opts.ReminderInterval <= 0 {
		return nil
	}
	last := incident.LastNotifiedAt
	if last.IsZero() {
		last = incident.StartedAt
	}
	if at.Sub(last) < e.opts.ReminderInterval {
		return nil
	}
	// Recorded before sending: a failure here must not turn into a reminder
	// on every following check.
	if err := e.store.MarkIncidentNotified(ctx, incident.ID, at); err != nil {
		return err
	}
	e.enqueue(Notification{Event: EventReminder, Monitor: m, Incident: incident, At: at})
	return nil
}

// evaluateCert opens a certificate incident when the newest result shows a
// certificate within CertWarning of its expiry, and resolves it once a
// result shows one that is not. A result without certificate information
// (plain HTTP, or a connection that failed) changes nothing.
func (e *Engine) evaluateCert(ctx context.Context, m store.Monitor, quiet bool) error {
	latest, err := e.store.ListCheckResults(ctx, m.ID, 1)
	if err != nil || len(latest) == 0 || latest[0].CertExpiresAt.IsZero() {
		return err
	}
	result := latest[0]
	expiring := result.CertExpiresAt.Sub(result.CheckedAt) < e.opts.CertWarning

	incident, err := e.store.GetOpenIncident(ctx, m.ID, store.IncidentCert)
	switch {
	case errors.Is(err, store.ErrNotFound):
		if !expiring || quiet {
			return nil
		}
		incident, err = e.store.OpenIncident(ctx, store.Incident{
			MonitorID: m.ID,
			Kind:      store.IncidentCert,
			StartedAt: result.CheckedAt,
			OpenedAt:  result.CheckedAt,
			Cause:     "certificate expires on " + result.CertExpiresAt.Format(time.RFC3339),
		})
		if err != nil {
			return err
		}
		incident.MonitorName = m.Name
		e.opts.Logger.Warn("certificate expiring", "monitor", m.Name, "incident_id", incident.ID, "expires_at", result.CertExpiresAt)
		e.enqueue(Notification{Event: EventCertExpiring, Monitor: m, Incident: incident, CertExpiresAt: result.CertExpiresAt, At: result.CheckedAt})
		return nil
	case err != nil:
		return err
	}

	if expiring {
		return nil
	}
	incident.ResolvedAt = result.CheckedAt
	if err := e.store.ResolveIncident(ctx, incident.ID, incident.ResolvedAt); err != nil {
		return err
	}
	e.opts.Logger.Info("certificate renewed", "monitor", m.Name, "incident_id", incident.ID, "expires_at", result.CertExpiresAt)
	e.enqueue(Notification{Event: EventCertRenewed, Monitor: m, Incident: incident, CertExpiresAt: result.CertExpiresAt, At: result.CheckedAt})
	return nil
}

// maybeOpen opens an incident when the newest FailureThreshold results all failed.
func (e *Engine) maybeOpen(ctx context.Context, m store.Monitor, at time.Time) error {
	streak, err := e.streak(ctx, m.ID, m.FailureThreshold, false)
	if err != nil || streak == nil {
		return err
	}
	incident, err := e.store.OpenIncident(ctx, store.Incident{
		MonitorID: m.ID,
		StartedAt: streak[len(streak)-1].CheckedAt, // the first failure of the streak
		OpenedAt:  streak[0].CheckedAt,             // the one that reached the threshold
		Cause:     streak[0].Error,                 // the most recent error

		LastNotifiedAt: at,
	})
	if err != nil {
		return err
	}
	incident.MonitorName = m.Name
	e.opts.Logger.Warn("incident opened", "monitor", m.Name, "incident_id", incident.ID, "cause", incident.Cause)
	e.enqueue(Notification{Event: EventDown, Monitor: m, Incident: incident, At: incident.OpenedAt})
	return nil
}

// maybeResolve resolves the incident when the newest RecoveryThreshold results all succeeded.
// It reports whether the incident was resolved.
func (e *Engine) maybeResolve(ctx context.Context, m store.Monitor, incident store.Incident) (bool, error) {
	streak, err := e.streak(ctx, m.ID, e.opts.RecoveryThreshold, true)
	if err != nil || streak == nil {
		return false, err
	}
	incident.ResolvedAt = streak[len(streak)-1].CheckedAt // the first success of the streak
	if err := e.store.ResolveIncident(ctx, incident.ID, incident.ResolvedAt); err != nil {
		return false, err
	}
	e.opts.Logger.Info("incident resolved", "monitor", m.Name, "incident_id", incident.ID,
		"duration", incident.ResolvedAt.Sub(incident.StartedAt).String())
	// At is the check that confirmed the recovery, which with a recovery
	// threshold above one comes after the first success, ResolvedAt.
	e.enqueue(Notification{Event: EventUp, Monitor: m, Incident: incident, At: streak[0].CheckedAt})
	return true, nil
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
	n.Language, n.PublicURL = e.opts.Language, e.opts.PublicURL
	select {
	case e.queue <- n:
	default:
		e.opts.Metrics.NotificationDropped()
		e.opts.Logger.Error("notification queue full, notification dropped",
			"event", n.Event, "monitor", n.Monitor.Name, "incident_id", n.Incident.ID)
	}
}

// groupKey is what notifications are grouped by: the same event in the
// same namespace.
type groupKey struct {
	event     Event
	namespace string
}

// pendingGroup is a group waiting for GroupWait to pass since its first
// notification.
type pendingGroup struct {
	event Event
	notes []Notification
	due   time.Time
	seq   int // the order the groups began in
}

// monitorKey identifies the monitor a notification is about.
func monitorKey(n Notification) string {
	if n.Monitor.ID != 0 {
		return strconv.FormatInt(n.Monitor.ID, 10)
	}
	return n.Monitor.Name
}

// dropStaleReminders removes the reminders still waiting about the monitor
// that n is about: by the time the group goes out they would say an outage
// is still going on after the message that it ended, or repeat a reminder
// that a newer one supersedes. Only reminders are dropped — an outage and
// its recovery are both told, however briefly it lasted.
func dropStaleReminders(groups map[groupKey]*pendingGroup, n Notification) {
	if n.Event == EventTest {
		return
	}
	key := monitorKey(n)
	for gk, g := range groups {
		if gk.event != EventReminder {
			continue
		}
		kept := g.notes[:0]
		for _, pending := range g.notes {
			if monitorKey(pending) != key {
				kept = append(kept, pending)
			}
		}
		g.notes = kept
		if len(g.notes) == 0 {
			delete(groups, gk)
		}
	}
}

// takeDue removes the groups due by now (all of them with everything set)
// and returns them in the order they began, so that notifications leave in
// the order they arose rather than in the map's.
func takeDue(groups map[groupKey]*pendingGroup, now time.Time, everything bool) []*pendingGroup {
	var due []*pendingGroup
	for key, g := range groups {
		if everything || !g.due.After(now) {
			due = append(due, g)
			delete(groups, key)
		}
	}
	sort.Slice(due, func(i, j int) bool { return due[i].seq < due[j].seq })
	return due
}

// Run delivers queued notifications until ctx is cancelled, then delivers
// what is still queued (without retries) and returns. With GroupWait set,
// notifications are held for that long and delivered together with the
// others of their group.
func (e *Engine) Run(ctx context.Context) {
	groups := make(map[groupKey]*pendingGroup)
	started := 0
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	// rearm sets the timer for the group due first.
	rearm := func() {
		timer.Stop()
		var next time.Time
		for _, g := range groups {
			if next.IsZero() || g.due.Before(next) {
				next = g.due
			}
		}
		if !next.IsZero() {
			timer.Reset(max(time.Until(next), 0))
		}
	}
	add := func(n Notification) {
		if e.opts.GroupWait <= 0 || n.Event == EventTest {
			e.deliverTo(ctx, n, nil)
			return
		}
		// The channels software reads get it now: a page must not sit out
		// the wait that exists only to spare human readers.
		e.deliverTo(ctx, n, func(ch store.Channel) bool { return machine(ch.Type) })
		dropStaleReminders(groups, n)
		key := groupKey{n.Event, n.Monitor.Namespace}
		g, ok := groups[key]
		if !ok {
			started++
			g = &pendingGroup{event: n.Event, due: time.Now().Add(e.opts.GroupWait), seq: started}
			groups[key] = g
		}
		g.notes = append(g.notes, n)
	}
	flush := func(everything bool) {
		for _, g := range takeDue(groups, time.Now(), everything) {
			e.deliverGroup(ctx, g.event, g.notes)
		}
	}

	for {
		select {
		case n := <-e.queue:
			add(n)
			rearm()
		case <-timer.C:
			flush(false)
			rearm()
		case <-ctx.Done():
			timer.Stop()
			for {
				select {
				case n := <-e.queue:
					add(n)
				default:
					// Shutting down: what is waiting goes now.
					flush(true)
					return
				}
			}
		}
	}
}

// deliverGroup sends the notifications of one group to the channels
// people read, as one message where more than one is for that channel.
// The machine channels got each notification when it arose.
func (e *Engine) deliverGroup(ctx context.Context, event Event, notes []Notification) {
	if len(notes) == 1 {
		e.deliverTo(ctx, notes[0], func(ch store.Channel) bool { return !machine(ch.Type) })
		return
	}
	channels, err := e.store.ListChannels(context.WithoutCancel(ctx))
	if err != nil {
		e.opts.Logger.Error("list channels failed, notifications lost", "event", event, "count", len(notes), "error", err)
		return
	}
	for _, ch := range channels {
		if !ch.Enabled || machine(ch.Type) || !ch.Wants(event.Kind()) {
			continue
		}
		var mine []Notification
		for _, n := range notes {
			if ch.Covers(n.Monitor) {
				mine = append(mine, n)
			}
		}
		switch {
		case len(mine) == 0:
		case len(mine) == 1:
			e.send(ctx, ch, mine[0])
		default:
			first := mine[0]
			e.send(ctx, ch, Notification{Event: event, Monitor: first.Monitor, Incident: first.Incident, CertExpiresAt: first.CertExpiresAt,
				At: first.At, Language: first.Language, PublicURL: first.PublicURL, Group: mine})
		}
	}
}

// deliverTo sends one notification to every enabled channel whose scope
// covers it and that include accepts; nil includes them all.
func (e *Engine) deliverTo(ctx context.Context, n Notification, include func(store.Channel) bool) {
	// Reads and sends must still work while shutting down.
	channels, err := e.store.ListChannels(context.WithoutCancel(ctx))
	if err != nil {
		e.opts.Logger.Error("list channels failed, notification lost", "event", n.Event, "monitor", n.Monitor.Name, "error", err)
		return
	}
	for _, ch := range channels {
		if !ch.Enabled || !ch.Covers(n.Monitor) || !ch.Wants(n.Event.Kind()) || (include != nil && !include(ch)) {
			continue
		}
		e.send(ctx, ch, n)
	}
}

// send delivers one notification (single or grouped) to one channel, with
// retries, and records the outcome: on the channel, and in the delivery
// log of every incident the message was about.
func (e *Engine) send(ctx context.Context, ch store.Channel, n Notification) {
	err := e.sendWithRetry(ctx, ch, n)
	e.opts.Metrics.Notification(string(ch.Type), string(n.Event), err == nil)
	deliveryError := ""
	if err != nil {
		deliveryError = err.Error()
	}
	now := time.Now().UTC()
	if recordErr := e.store.RecordChannelDelivery(context.WithoutCancel(ctx), ch.ID, now, deliveryError); recordErr != nil {
		e.opts.Logger.Error("channel delivery not recorded", "channel", ch.Name, "error", recordErr)
	}
	if logErr := e.store.RecordDeliveries(context.WithoutCancel(ctx), deliveryLog(ch, n, now, deliveryError)); logErr != nil {
		e.opts.Logger.Error("delivery log not written", "channel", ch.Name, "event", n.Event, "error", logErr)
	}
	what := n.Monitor.Name
	if len(n.Group) > 1 {
		what = fmt.Sprintf("%d monitors", len(n.Group))
	}
	if err != nil {
		e.opts.Logger.Error("notification failed", "channel", ch.Name, "event", n.Event, "monitor", what, "error", err)
		return
	}
	e.opts.Logger.Info("notification sent", "channel", ch.Name, "event", n.Event, "monitor", what)
}

// deliveryLog is the delivery log's record of one attempt: an entry for
// each incident the message was about, which for a grouped message is each
// of its monitors' and otherwise the one. A test is about no incident and
// leaves none. The error is redacted now, while the channel is at hand to
// redact it against: the entry outlives changes to the channel.
func deliveryLog(ch store.Channel, n Notification, at time.Time, deliveryError string) []store.Delivery {
	about := n.Group
	if len(about) <= 1 {
		about = []Notification{n}
	}
	var out []store.Delivery
	for _, a := range about {
		if a.Incident.ID == 0 {
			continue
		}
		eventAt := a.At
		if eventAt.IsZero() {
			eventAt = at
		}
		out = append(out, store.Delivery{
			IncidentID: a.Incident.ID, MonitorID: a.Monitor.ID,
			ChannelID: ch.ID, ChannelName: ch.Name, ChannelType: ch.Type,
			Event: string(a.Event), EventAt: eventAt, GroupSize: len(about),
			At: at, Delivered: deliveryError == "", Error: Redact(deliveryError, ch),
		})
	}
	return out
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
	if n.Language == "" {
		n.Language = e.opts.Language
	}
	if n.PublicURL == "" {
		n.PublicURL = e.opts.PublicURL
	}
	sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), e.opts.SendTimeout)
	defer cancel()
	return e.sender.Send(sendCtx, ch, n)
}
