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
	"sync"
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

	EventSlow         Event = "slow"          // a monitor answers, but slower than its threshold
	EventSlowResolved Event = "slow_resolved" // it answers in time again
)

// Notification is one message to deliver to every enabled channel.
type Notification struct {
	Event    Event
	Monitor  store.Monitor
	Incident store.Incident
	// CertExpiresAt is set for the certificate events.
	CertExpiresAt time.Time
	// Explanation is what the failure that opened an outage most likely
	// means, for EventDown; nil when nothing recognized it.
	Explanation *store.Explanation
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
	// For the slow-response events, which are about no incident: Since is
	// when the monitor turned slow, Threshold its limit, and Latency the
	// response time of the check that turned it slow or fast again.
	Since              time.Time
	Threshold, Latency time.Duration
}

// Kind is the channel-facing class of an event (store.ChannelEvents): the
// two certificate events are one kind, a test is none.
func (e Event) Kind() string {
	switch e {
	case EventDown, EventUp, EventReminder, EventSlow, EventSlowResolved:
		return string(e)
	case EventCertExpiring, EventCertRenewed:
		return "cert"
	}
	return ""
}

// Outage reports whether the event is about an outage: the events a
// channel's delay and repeat interval apply to.
func (e Event) Outage() bool {
	return e == EventDown || e == EventUp || e == EventReminder
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
	// FilterIncidents lists the outages open now, for what channels with
	// a delay or a repeat interval are still owed about them after a
	// restart.
	FilterIncidents(ctx context.Context, f store.IncidentFilter) ([]store.Incident, error)
	// DeliveryAt says whether the channel was told the event about the
	// incident before a restart, and when: whether the log has an attempt,
	// delivered or not, and when the first ended.
	DeliveryAt(ctx context.Context, incidentID, channelID int64, event string) (at time.Time, found bool, err error)
	// Delivered says whether an attempt to tell the channel the event
	// about the incident succeeded: whether, after a restart, an outage
	// still has to be sent again to the channel.
	Delivered(ctx context.Context, incidentID, channelID int64, event string) (bool, error)
	SetMonitorSlowSince(ctx context.Context, id int64, since time.Time) error
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
	// pause between them and SendTimeout the limit of a single try. The
	// three make one attempt to deliver a notification.
	Attempts    int
	RetryDelay  time.Duration
	SendTimeout time.Duration
	// A notification of an outage or of its end that a channel did not
	// take is attempted again while the outage lasts: after a minute, then
	// after twice the wait each time, RetryMax at most; zero attempts
	// nothing again. The end of an outage is attempted for RetryFor at
	// most, from the attempt that failed; zero does not attempt it again.
	// Nothing else is attempted again: a test, a reminder, a certificate
	// or a slowness notification goes once.
	RetryMax time.Duration
	RetryFor time.Duration
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
	// owed is what the channels with a delay or a repeat interval are
	// still owed about the open outages; only Run's goroutine touches it.
	owed *schedule
	// now is the clock of the delivery loop; tests set their own.
	now func() time.Time
}

// New returns an Engine. Run must be started for notifications to be sent.
func New(st Store, sender Sender, opts Options) *Engine {
	return &Engine{store: st, sender: sender, opts: opts, queue: make(chan Notification, opts.QueueSize), owed: newSchedule(), now: time.Now}
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

	// The three are independent: a failing evaluation of one must not hide
	// the others.
	return errors.Join(e.evaluateDown(ctx, m, at, quiet), e.evaluateCert(ctx, m, quiet), e.evaluateSlow(ctx, m, quiet))
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
	down := Notification{Event: EventDown, Monitor: m, Incident: incident, At: incident.OpenedAt}
	if d := streak[0].Diagnostics; d != nil {
		down.Explanation = d.Explanation // of the error that became the cause
	}
	e.enqueue(down)
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
// others of their group. What channels with a delay or a repeat interval
// are owed (see schedule) is delivered when it falls due; at shutdown what
// is due by then goes, and the rest is worked out again from the open
// outages at the next start. Each pass through the channels sends to them
// in parallel and waits for the slowest (deliver): the loop is held up by
// a channel that does not answer for as long as one send to it takes, not
// once per channel, and the other channels are not held up at all. An
// outage notification a channel did not take is sent again when its wait
// is up (retryLater, deliverOwed), with what else is owed.
func (e *Engine) Run(ctx context.Context) {
	groups := make(map[groupKey]*pendingGroup)
	started := 0
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	// rearm sets the timer for whatever is due first: a group, or what a
	// channel is owed. Since Go 1.23 a stopped timer is reset without
	// draining its channel.
	rearm := func() {
		timer.Stop()
		next := e.owed.next()
		for _, g := range groups {
			if next.IsZero() || g.due.Before(next) {
				next = g.due
			}
		}
		if !next.IsZero() {
			timer.Reset(max(next.Sub(e.now()), 0))
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
			g = &pendingGroup{event: n.Event, due: e.now().Add(e.opts.GroupWait), seq: started}
			groups[key] = g
		}
		g.notes = append(g.notes, n)
	}
	flush := func(everything bool) {
		for _, g := range takeDue(groups, e.now(), everything) {
			e.deliverGroup(ctx, g.event, g.notes, func(ch store.Channel) bool { return !machine(ch.Type) })
		}
		e.deliverOwed(ctx)
	}

	// What the open outages owe the channels is due from the start: the
	// timer is set for it before anything passes through the queue.
	e.restore(ctx)
	rearm()
	// The loop ends as soon as ctx is done, before a timer that is due at
	// the same moment: the queue is then drained first, and what is owed
	// goes after it.
	for ctx.Err() == nil {
		select {
		case n := <-e.queue:
			add(n)
			rearm()
		case <-timer.C:
			flush(false)
			rearm()
		case <-ctx.Done():
		}
	}
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

// deliverGroup sends the notifications of one group to the channels that
// include accepts, as one message where more than one is for that channel.
// With GroupWait those are the channels people read; the machine channels
// got each notification when it arose.
func (e *Engine) deliverGroup(ctx context.Context, event Event, notes []Notification, include func(store.Channel) bool) {
	if len(notes) == 1 {
		e.deliverTo(ctx, notes[0], include)
		e.outagesOver(notes)
		return
	}
	channels, err := e.store.ListChannels(context.WithoutCancel(ctx))
	if err != nil {
		e.opts.Logger.Error("list channels failed, notifications lost", "event", event, "count", len(notes), "error", err)
		return
	}
	now := e.now()
	var shares []outbound
	for _, ch := range channels {
		if !ch.Enabled || (include != nil && !include(ch)) {
			continue
		}
		// route notes what the channel is owed even about an event it
		// does not want, so that its own reminders can follow.
		var mine []Notification
		for _, n := range notes {
			if ch.Covers(n.Monitor) && e.route(ch, n, now) && ch.Wants(event.Kind()) {
				mine = append(mine, n)
			}
		}
		if n, ok := grouped(event, mine); ok {
			shares = append(shares, outbound{channel: ch, notes: []Notification{n}})
		}
	}
	e.retryLater(e.deliver(ctx, shares))
	e.outagesOver(notes)
}

// outagesOver forgets what the channels were owed about the outages that
// the notifications end, once every channel has had its chance to hear
// of the end: those that route did not reach (disabled, narrowed or
// deleted meanwhile) must not stay owed anything about an outage that is
// over. An outage notification still to be sent again is not sent: the
// outage is over, and a channel that never took it has failed for good.
func (e *Engine) outagesOver(notes []Notification) {
	for _, n := range notes {
		if n.Event != EventUp {
			continue
		}
		for _, r := range e.owed.forgetOutage(n.Monitor.ID, n.Incident.ID) {
			e.gaveUp(r.channel, r.note, "the outage ended before the channel took it")
		}
	}
}

// grouped is what a channel gets of a group: nothing, the one
// notification, or the lot as one message.
func grouped(event Event, mine []Notification) (Notification, bool) {
	switch len(mine) {
	case 0:
		return Notification{}, false
	case 1:
		return mine[0], true
	}
	first := mine[0]
	return Notification{Event: event, Monitor: first.Monitor, Incident: first.Incident, CertExpiresAt: first.CertExpiresAt,
		At: first.At, Language: first.Language, PublicURL: first.PublicURL, Group: mine}, true
}

// deliverTo sends one notification to every enabled channel whose scope
// covers it and that include accepts; nil includes them all. A channel
// whose delay or repeat interval says otherwise hears of it later (route).
// With no filter it is the notification's only pass through the channels,
// and the end of an outage is forgotten after it (outagesOver); with one,
// the pass through the channels people read (deliverGroup) is the last.
func (e *Engine) deliverTo(ctx context.Context, n Notification, include func(store.Channel) bool) {
	// Reads and sends must still work while shutting down.
	channels, err := e.store.ListChannels(context.WithoutCancel(ctx))
	if err != nil {
		e.opts.Logger.Error("list channels failed, notification lost", "event", n.Event, "monitor", n.Monitor.Name, "error", err)
		return
	}
	now := e.now()
	var shares []outbound
	for _, ch := range channels {
		if !ch.Enabled || !ch.Covers(n.Monitor) || (include != nil && !include(ch)) {
			continue
		}
		// route notes what the channel is owed even about an event it
		// does not want, so that its own reminders can follow.
		if e.route(ch, n, now) && ch.Wants(n.Event.Kind()) {
			shares = append(shares, outbound{channel: ch, notes: []Notification{n}})
		}
	}
	e.retryLater(e.deliver(ctx, shares))
	if include == nil {
		e.outagesOver([]Notification{n})
	}
}

// outbound is a channel's share of one pass through the channels: what it
// is sent, in the order it is sent. A note sent again has its retry under
// its index in retries.
type outbound struct {
	channel store.Channel
	notes   []Notification
	retries map[int]*retry
}

// failed is a send that a channel did not take: what was sent, and the
// retry it was sent again for, if it was.
type failed struct {
	channel store.Channel
	note    Notification
	retry   *retry
}

// deliver sends each channel its share, and returns what failed once the
// last send is recorded. The channels are sent to in parallel, one
// goroutine each, and each channel serially, in order: a channel that is
// slow or down holds up only itself, and a pass costs the loop its slowest
// channel rather than the sum of them. Only the sends leave the loop's
// goroutine; what a channel is owed (route, retryLater, outagesOver) is
// settled in the loop, before and after, and the store and the metrics
// take calls from several goroutines.
func (e *Engine) deliver(ctx context.Context, shares []outbound) []failed {
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		failures []failed
	)
	for _, s := range shares {
		wg.Go(func() {
			for i, n := range s.notes {
				if err := e.send(ctx, s.channel, n); err != nil {
					mu.Lock()
					failures = append(failures, failed{channel: s.channel, note: n, retry: s.retries[i]})
					mu.Unlock()
				}
			}
		})
	}
	wg.Wait()
	return failures
}

// send delivers one notification (single or grouped) to one channel, with
// retries, and records the outcome: on the channel, and in the delivery
// log of every incident the message was about. It runs in a goroutine of
// deliver's, beside the sends to the other channels. A failure is the
// attempt's, not yet the notification's: whether it is attempted again is
// settled in the loop (retryLater), which counts the notification failed
// when it is not.
func (e *Engine) send(ctx context.Context, ch store.Channel, n Notification) error {
	err := e.sendWithRetry(ctx, ch, n)
	e.opts.Metrics.NotificationAttempt(string(ch.Type), err == nil)
	if err == nil {
		e.opts.Metrics.Notification(string(ch.Type), string(n.Event), true)
	}
	deliveryError := ""
	if err != nil {
		deliveryError = err.Error()
	}
	// The loop's clock, which restore reads the log against.
	now := e.now().UTC()
	if recordErr := e.store.RecordChannelDelivery(context.WithoutCancel(ctx), ch.ID, now, deliveryError); recordErr != nil {
		e.opts.Logger.Error("channel delivery not recorded", "channel", ch.Name, "error", recordErr)
	}
	if logErr := e.store.RecordDeliveries(context.WithoutCancel(ctx), deliveryLog(ch, n, now, deliveryError)); logErr != nil {
		e.opts.Logger.Error("delivery log not written", "channel", ch.Name, "event", n.Event, "error", logErr)
	}
	if err != nil {
		e.opts.Logger.Error("notification failed", "channel", ch.Name, "event", n.Event, "monitor", about(n), "error", err)
		return err
	}
	e.opts.Logger.Info("notification sent", "channel", ch.Name, "event", n.Event, "monitor", about(n))
	return nil
}

// about names what a notification is about, for the log lines.
func about(n Notification) string {
	if len(n.Group) > 1 {
		return fmt.Sprintf("%d monitors", len(n.Group))
	}
	return n.Monitor.Name
}

// gaveUp counts a notification a channel never took: the final outcome of
// its attempts. The reason is logged when the attempts spread over the
// outage (retryLater); a notification attempted once was logged by send.
func (e *Engine) gaveUp(ch store.Channel, n Notification, reason string) {
	e.opts.Metrics.Notification(string(ch.Type), string(n.Event), false)
	if reason != "" {
		e.opts.Logger.Error("notification given up", "channel", ch.Name, "event", n.Event, "monitor", about(n), "reason", reason)
	}
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
