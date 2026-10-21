package alert

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// A channel's delay and repeat interval (store.Channel.Delay and Repeat)
// are kept by the delivery loop, as what the channel is owed about each
// open outage. An outage is told to a channel with a delay only once the
// delay has passed and the outage is still going on; one over before then
// is not told at all, nor is its end. A channel with a repeat interval is
// reminded of an outage it was told of at that interval, instead of at the
// engine's. A channel told of an outage keeps its entry until the end of
// the outage is delivered (route, forgetOutage), whatever happens to the
// channel's settings meanwhile: the end goes to whoever heard of the start
// and still listens, and nothing about the outage outlives it. The
// entries are in memory: at a start they are worked out again from the
// outages open then and the delivery log (restore), so a restart during
// an outage loses nothing but a reminder that fell due during it.

// owedKind is what a channel is still owed about an outage.
type owedKind int

const (
	owedNothing  owedKind = iota // the channel was told; nothing is pending
	owedOutage                   // the outage, once the channel's delay has passed
	owedSilence                  // the delay passed in maintenance: the outage is not told
	owedReminder                 // a reminder, at the channel's own interval
)

// owed is what one channel is owed about one outage.
type owed struct {
	kind owedKind
	// told says the channel heard of the outage; its end is told only then.
	told bool
	// channel as it was when the outage came up, read again when due; note
	// is the outage: EventDown with its monitor and incident.
	channel store.Channel
	note    Notification
	// due is when kind falls due; zero for nothing and silence.
	due time.Time
	seq int // the order the entries came up in
}

type owedKey struct{ channel, monitor int64 }

// schedule is what the channels with a delay or a repeat interval are
// owed about the open outages, one entry per channel and monitor. Only
// the delivery loop's goroutine touches it.
type schedule struct {
	entries map[owedKey]*owed
	seq     int
}

func newSchedule() *schedule {
	return &schedule{entries: make(map[owedKey]*owed)}
}

func (s *schedule) get(key owedKey) *owed { return s.entries[key] }

// put records what a channel is owed about a monitor's outage, in place
// of what it was owed before.
func (s *schedule) put(key owedKey, o *owed) {
	s.seq++
	o.seq = s.seq
	s.entries[key] = o
}

func (s *schedule) forget(key owedKey) { delete(s.entries, key) }

// forgetOutage forgets what every channel was owed about a monitor's
// outage once its end has been delivered, whatever the channel's state:
// route forgets the entries of the channels it reaches, but a channel
// disabled, narrowed or deleted meanwhile is not reached, and its entry
// must not outlive the outage, or the end of a later one it was never
// told of would find it told (route). An entry about a newer outage of
// the monitor, noted while the end waited to go out, is kept.
func (s *schedule) forgetOutage(monitorID, incidentID int64) {
	for key, o := range s.entries {
		if key.monitor == monitorID && o.note.Incident.ID <= incidentID {
			delete(s.entries, key)
		}
	}
}

// settle is what becomes of an entry that is not going to be delivered: a
// channel told of the outage is still owed its end, so its entry stays,
// owed nothing, until route forgets it with the up; one never told is
// forgotten, since it hears of neither.
func (s *schedule) settle(key owedKey, o *owed) {
	if !o.told {
		s.forget(key)
		return
	}
	o.kind, o.due = owedNothing, time.Time{}
}

// pending reports whether the entry is waiting for a time to come.
func (o *owed) pending() bool { return o.kind == owedOutage || o.kind == owedReminder }

// due returns what falls due by now, in the order it came up.
func (s *schedule) due(now time.Time) []*owed {
	var out []*owed
	for _, o := range s.entries {
		if o.pending() && !o.due.After(now) {
			out = append(out, o)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].seq < out[j].seq })
	return out
}

// next is when the earliest entry falls due; zero when none is pending.
func (s *schedule) next() time.Time {
	var next time.Time
	for _, o := range s.entries {
		if o.pending() && (next.IsZero() || o.due.Before(next)) {
			next = o.due
		}
	}
	return next
}

// owedRetry is how long a due entry waits after a store error, so that one
// does not turn the loop into a busy one.
const owedRetry = time.Minute

// route says whether the channel hears the notification now. For the
// outage events it is where the channel's delay and repeat interval
// apply: an outage is noted for a channel with a delay, to be told once
// the delay has passed (deliverOwed); a channel told of an outage is
// reminded at its own interval, if it has one, instead of the engine's;
// and the end of an outage goes only to the channels that heard of it.
// Everything else, and every channel without a delay or interval, hears
// at once.
func (e *Engine) route(ch store.Channel, n Notification, now time.Time) bool {
	if !n.Event.Outage() || (ch.Delay <= 0 && ch.Repeat <= 0) {
		return true
	}
	key := owedKey{ch.ID, n.Monitor.ID}
	o := e.owed.get(key)
	// A channel without a delay heard of the outage when it opened, even
	// if nothing was noted for it then.
	told := o != nil && o.told || o == nil && ch.Delay <= 0
	switch n.Event {
	case EventDown:
		o = &owed{channel: ch, note: n}
		if ch.Delay > 0 {
			o.kind, o.due = owedOutage, now.Add(ch.Delay)
		} else {
			o.kind, o.told, o.due = owedReminder, true, now.Add(ch.Repeat)
		}
		e.owed.put(key, o)
		return o.told
	case EventReminder:
		return ch.Repeat <= 0 && told
	case EventUp:
		e.owed.forget(key)
		return told
	}
	return true
}

// deliverOwed sends what has fallen due to the channels owed it: outages
// whose delay has passed, as one message per channel for those due
// together, and the reminders of the channels with an interval of their
// own. An outage over by then is not told; one whose delay passes in
// maintenance is silenced, and a reminder due in maintenance is skipped.
// Whatever keeps a due entry from being delivered, a channel told of the
// outage stays told (settle): its reminder falling due after the recovery
// and before the end is delivered, or a channel that wants no reminders,
// must not cost it the end.
func (e *Engine) deliverOwed(ctx context.Context) {
	now := e.now()
	due := e.owed.due(now)
	if len(due) == 0 {
		return
	}
	// Reads and sends must still work while shutting down.
	ctx = context.WithoutCancel(ctx)
	channels, err := e.store.ListChannels(ctx)
	if err != nil {
		e.opts.Logger.Error("list channels failed, owed notifications wait", "count", len(due), "error", err)
		for _, o := range due {
			o.due = now.Add(owedRetry)
		}
		return
	}
	byID := make(map[int64]store.Channel, len(channels))
	for _, ch := range channels {
		byID[ch.ID] = ch
	}

	type batchKey struct {
		channel   int64
		event     Event
		namespace string
	}
	var order []batchKey
	batches := make(map[batchKey][]Notification)
	add := func(ch store.Channel, n Notification) {
		key := batchKey{ch.ID, n.Event, n.Monitor.Namespace}
		if _, ok := batches[key]; !ok {
			order = append(order, key)
		}
		batches[key] = append(batches[key], n)
	}

	for _, o := range due {
		key := owedKey{o.channel.ID, o.note.Monitor.ID}
		ch, exists := byID[o.channel.ID]
		m, err := e.store.GetMonitor(ctx, o.note.Monitor.ID)
		if !exists || errors.Is(err, store.ErrNotFound) {
			// The channel or the monitor is gone: there is nobody to tell,
			// or nothing more to tell about.
			e.owed.forget(key)
			continue
		}
		var incident store.Incident
		if err == nil {
			incident, err = e.store.GetOpenIncident(ctx, m.ID, store.IncidentDown)
		}
		switch {
		case err != nil && !errors.Is(err, store.ErrNotFound):
			// The store could not say; incident is empty, which must not
			// pass for the outage being over.
			e.opts.Logger.Error("owed notification waits", "channel", o.channel.Name, "monitor", o.note.Monitor.Name, "error", err)
			o.due = now.Add(owedRetry)
			continue
		case err != nil || incident.ID != o.note.Incident.ID:
			// The outage is over: nothing to tell now. Its end is still
			// on its way to a channel that was told of it.
			e.owed.settle(key, o)
			continue
		}
		if !ch.Enabled || !m.Enabled || !ch.Covers(m) {
			// The channel does not hear about the monitor any more.
			e.owed.settle(key, o)
			continue
		}
		quiet, err := e.store.InMaintenance(ctx, m, now)
		if err != nil {
			e.opts.Logger.Error("owed notification waits", "channel", ch.Name, "monitor", m.Name, "error", err)
			o.due = now.Add(owedRetry)
			continue
		}
		o.channel, o.note.Monitor, o.note.Incident = ch, m, incident
		switch o.kind {
		case owedOutage:
			if quiet {
				o.kind, o.due = owedSilence, time.Time{}
				continue
			}
			// Told, whether or not the channel listens for outages opening:
			// as a channel without a delay, it is then owed the end and
			// the reminders it listens for.
			o.told = true
			o.kind, o.due = owedNothing, time.Time{}
			if ch.Repeat > 0 && ch.Wants(EventReminder.Kind()) {
				o.kind, o.due = owedReminder, now.Add(ch.Repeat)
			}
			if ch.Wants(EventDown.Kind()) {
				add(ch, o.note)
			}
		case owedReminder:
			if ch.Repeat <= 0 || !ch.Wants(EventReminder.Kind()) {
				// The channel follows the engine's reminders again, or
				// wants none; it was told, and stays so.
				o.kind, o.due = owedNothing, time.Time{}
				continue
			}
			o.due = now.Add(ch.Repeat)
			if quiet {
				continue
			}
			n := o.note
			n.Event, n.At = EventReminder, now
			add(ch, n)
		}
	}

	for _, key := range order {
		ch, notes := byID[key.channel], batches[key]
		if machine(ch.Type) {
			for _, n := range notes {
				e.send(ctx, ch, n)
			}
			continue
		}
		e.sendGrouped(ctx, ch, key.event, notes)
	}
}

// restore works out, from the outages open now, what the channels with a
// delay or a repeat interval are owed about them: what the loop had in
// hand before a restart. An outage whose delay has not passed is told when
// it does. One whose delay has passed was told if the delivery log has
// the attempt: the channel's next reminder is then where its cadence from
// that attempt falls. Without the attempt, either the delay passed in
// maintenance and the loop silenced the outage, which stays silenced, or
// the restart came between the delay and the delivery, and the outage,
// still going on, is told now.
func (e *Engine) restore(ctx context.Context) {
	ctx = context.WithoutCancel(ctx)
	open := true
	incidents, err := e.store.FilterIncidents(ctx, store.IncidentFilter{Kind: store.IncidentDown, Open: &open})
	if err != nil {
		e.opts.Logger.Error("open outages not read; delayed notifications and reminders of channels start afresh", "error", err)
		return
	}
	if len(incidents) == 0 {
		return
	}
	channels, err := e.store.ListChannels(ctx)
	if err != nil {
		e.opts.Logger.Error("channels not read; delayed notifications and reminders of channels start afresh", "error", err)
		return
	}
	now := e.now()
	for _, incident := range incidents {
		m, err := e.store.GetMonitor(ctx, incident.MonitorID)
		if err != nil || !m.Enabled {
			continue
		}
		opened := incident.OpenedAt
		if opened.IsZero() {
			opened = incident.StartedAt
		}
		for _, ch := range channels {
			if (ch.Delay <= 0 && ch.Repeat <= 0) || !ch.Enabled || !ch.Covers(m) {
				continue
			}
			key := owedKey{ch.ID, m.ID}
			if e.owed.get(key) != nil {
				continue
			}
			o := &owed{channel: ch, note: Notification{Event: EventDown, Monitor: m, Incident: incident, At: opened,
				Language: e.opts.Language, PublicURL: e.opts.PublicURL}}
			due := opened.Add(ch.Delay)
			if due.After(now) {
				o.kind, o.due = owedOutage, due
				e.owed.put(key, o)
				continue
			}
			told, at := e.wasTold(ctx, ch, incident)
			switch {
			case !told && e.silenced(ctx, ch, m, due):
				o.kind = owedSilence
			case !told:
				o.kind, o.due = owedOutage, now
			case ch.Repeat > 0 && ch.Wants(EventReminder.Kind()):
				// The first of the channel's reminders, at its interval
				// from when it was told, that is still to come. The log
				// says when that was if the loop was late with it; the
				// delay is where the loop would have been.
				if at.After(due) {
					due = at
				}
				passed := now.Sub(due) / ch.Repeat
				o.kind, o.told, o.due = owedReminder, true, due.Add((passed+1)*ch.Repeat)
			default:
				o.told = true
			}
			e.owed.put(key, o)
		}
	}
}

// wasTold says whether the channel heard of the outage before a restart,
// and when the attempt was. A channel that does not listen for outages
// opening is told by not hearing it, as the loop would have (deliverOwed);
// otherwise the delivery log says. When it cannot be read, the channel
// counts as told, as it would be without the log: a page too few rather
// than one too many.
func (e *Engine) wasTold(ctx context.Context, ch store.Channel, incident store.Incident) (told bool, at time.Time) {
	if !ch.Wants(EventDown.Kind()) {
		return true, time.Time{}
	}
	at, told, err := e.store.DeliveryAt(ctx, incident.ID, ch.ID, string(EventDown))
	if err != nil {
		e.opts.Logger.Error("delivery log not read; the channel counts as told of the outage", "channel", ch.Name, "incident_id", incident.ID, "error", err)
		return true, time.Time{}
	}
	return told, at
}

// silenced says whether the loop, had it been running, would have
// silenced the outage for the channel: whether its delay fell due in
// maintenance (deliverOwed). When the store cannot say, it did not: the
// outage is told, as it is without a window.
func (e *Engine) silenced(ctx context.Context, ch store.Channel, m store.Monitor, due time.Time) bool {
	quiet, err := e.store.InMaintenance(ctx, m, due)
	if err != nil {
		e.opts.Logger.Error("maintenance not read; the outage counts as told late", "channel", ch.Name, "monitor", m.Name, "error", err)
		return false
	}
	return quiet
}
