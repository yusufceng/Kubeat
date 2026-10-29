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
//
// The schedule also keeps what a channel did not take (retry): an outage
// notification, or the end of one, that an attempt failed to deliver is
// attempted again while the outage lasts; the end of an outage for
// Options.RetryFor at most, and only while no newer outage of the monitor
// has opened: told the end after the start of the next, the channel would
// read the monitor as up while it is down. How long it waits depends on
// what the failure said (failure): after one that did not reach the
// channel, a minute and then twice the wait each time, up to
// Options.RetryMax; after a refusal, RetryMax; after a busy answer, its
// Retry-After, without doubling the wait. A grouped message is attempted
// again as one, less the monitors whose outages ended meanwhile. A channel
// software reads that an attempt did not reach, or that was busy, is
// paused until its next retry (Engine.paused): what comes up for it
// meanwhile is not attempted but held back for that retry's pass, where it
// goes with what else waits for the channel, once only if it is not
// attempted again otherwise (retry.once), and what the channel refused
// fewer times goes first (outbound.order). A refusal does not pause it:
// the receiver is there, and the rest goes. These too are in memory: at a
// start the delivery log says which open outages a channel was sent and
// did not take, and they are sent again, and which a channel without a
// delay was never sent, having been queued or waiting for its group at the
// restart, and they are told then (restore); an end of an outage still to
// be sent again is lost with the restart.

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

// retry is a notification a channel did not take, to be sent to it again:
// the message as it went, grouped or not, with when the next attempt is
// due and how long the one after it waits.
type retry struct {
	channel store.Channel
	note    Notification
	// since is when the first attempt failed, or, for a note held back and
	// not attempted yet, when that attempt is due: it moves with the note
	// until then (retryLater, deliverOwed). The end of an outage is
	// attempted again for Options.RetryFor from then.
	since time.Time
	due   time.Time
	// wait is how long the attempt after the next waits: twice the last
	// wait, Options.RetryMax at most.
	wait time.Duration
	// attempts are the attempts that failed so far, a busy answer aside:
	// for the log, and for the order a channel that pauses is sent its
	// notes in (outbound.order). tried says whether the note was attempted
	// at all, a busy answer included: until it is, its since moves with it.
	attempts int
	tried    bool
	// refused says the last attempt was refused (4xx): the note waits
	// RetryMax for the receiver to be fixed, and that wait is no pass of
	// the channel's, which a channel that pauses would wait for (paused).
	refused bool
	// once marks a notification held back for a paused channel, or one a
	// busy receiver asked to be sent later, that is not attempted again
	// otherwise (Engine.retries): a reminder, a certificate or slowness
	// notification, the end of an outage with RetryFor zero. It goes in the
	// pass it was held back for, or, held back again there behind a note
	// the channel did not take, in a later one; it is given up if the
	// channel refuses it or the attempt does not reach the channel, and
	// goes again later if the channel is busy. A reminder or slowness
	// notification gives way meanwhile to a newer one about the same
	// monitor (supersede).
	once bool
}

// retryBase is the wait before the first attempt again.
const retryBase = time.Minute

// members are the notifications a message stands for: the group's, or
// the one.
func members(n Notification) []Notification {
	if len(n.Group) > 1 {
		return n.Group
	}
	return []Notification{n}
}

// narrow keeps of the message the members that keep returns, as keep
// returns them, and reports whether any is left. A grouped message of one
// member left is that member's own.
func (r *retry) narrow(keep func(Notification) (Notification, bool)) bool {
	var kept []Notification
	for _, m := range members(r.note) {
		if m, ok := keep(m); ok {
			kept = append(kept, m)
		}
	}
	if len(kept) == 0 {
		return false
	}
	r.note, _ = grouped(r.note.Event, kept)
	return true
}

// schedule is what the channels with a delay or a repeat interval are
// owed about the open outages, one entry per channel and monitor, and
// what the channels did not take, to be sent again. Only the delivery
// loop's goroutine touches it.
type schedule struct {
	entries map[owedKey]*owed
	retries []*retry
	// failing are the channels software reads whose last attempt failed:
	// until they take one, they are paused (Engine.paused).
	failing map[int64]bool
	seq     int
}

func newSchedule() *schedule {
	return &schedule{entries: make(map[owedKey]*owed), failing: make(map[int64]bool)}
}

// addRetry notes a notification to send to the channel again at due, and
// after wait, doubling, should that fail too. The retries stay in the
// order they came up in.
func (s *schedule) addRetry(ch store.Channel, n Notification, since, due time.Time, wait time.Duration) *retry {
	r := &retry{channel: ch, note: n, since: since, due: due, wait: wait}
	s.retries = append(s.retries, r)
	return r
}

func (s *schedule) dropRetry(r *retry) {
	for i, x := range s.retries {
		if x == r {
			s.retries = append(s.retries[:i], s.retries[i+1:]...)
			return
		}
	}
}

// supersede drops the note about the notification's monitor still held
// back for the channel that n, newer, takes the place of (supersedes): a
// reminder, as a newer reminder does in a group (dropStaleReminders), or
// a slowness notification, the monitor's latest state being all there is
// to tell (PagerDuty keeps one alert of a monitor's slowness anyway).
// However long a channel is down, one of each about a monitor waits for
// it at most.
func (s *schedule) supersede(channelID int64, n Notification) {
	kept := s.retries[:0]
	for _, r := range s.retries {
		if !r.once || r.channel.ID != channelID || r.note.Monitor.ID != n.Monitor.ID || !supersedes(n.Event, r.note.Event) {
			kept = append(kept, r)
		}
	}
	s.retries = kept
}

// supersedes says whether a note of the event takes the place of one of
// old about the same monitor held back for a channel: a reminder of a
// reminder, a slowness notification of either kind of one.
func supersedes(event, old Event) bool {
	return event == EventReminder && old == EventReminder || slowness(event) && slowness(old)
}

// slowness says whether the event is a monitor turning slow or responding
// in time again.
func slowness(e Event) bool { return e == EventSlow || e == EventSlowResolved }

// hasRetry says whether the channel is to be sent the event about the
// incident again, on its own or in a group.
func (s *schedule) hasRetry(channelID int64, event Event, incidentID int64) bool {
	for _, r := range s.retries {
		if r.channel.ID != channelID || r.note.Event != event {
			continue
		}
		for _, m := range members(r.note) {
			if m.Incident.ID == incidentID {
				return true
			}
		}
	}
	return false
}

// undelivered counts, by channel, the outages still to be sent to it
// again: those it did not take, and those held back for it while it is
// paused. A grouped message counts each of its outages.
func (s *schedule) undelivered() map[int64]int {
	counts := make(map[int64]int)
	for _, r := range s.retries {
		if r.note.Event == EventDown && !r.once {
			counts[r.channel.ID] += len(members(r.note))
		}
	}
	return counts
}

// retriesDue returns the retries due by now, in the order they came up.
func (s *schedule) retriesDue(now time.Time) []*retry {
	var out []*retry
	for _, r := range s.retries {
		if !r.due.After(now) {
			out = append(out, r)
		}
	}
	return out
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
// the monitor, noted while the end waited to go out, is kept. The outage,
// and a reminder of it held back for a paused channel, also leave the
// messages still to be sent again: a grouped one goes on for its other
// monitors, and the retries left with nothing to tell are returned,
// dropped. The end of this outage still to be sent again stays; the end
// of an earlier outage of the monitor is moot now that a later end was
// told, and leaves too. A certificate or slowness notification held back
// is about no outage and stays.
func (s *schedule) forgetOutage(monitorID, incidentID int64) (dropped []*retry) {
	for key, o := range s.entries {
		if key.monitor == monitorID && o.note.Incident.ID <= incidentID {
			delete(s.entries, key)
		}
	}
	kept := s.retries[:0]
	for _, r := range s.retries {
		if !r.narrow(func(m Notification) (Notification, bool) {
			if m.Monitor.ID != monitorID {
				return m, true
			}
			switch r.note.Event {
			case EventDown, EventReminder:
				return m, m.Incident.ID > incidentID
			case EventUp:
				return m, m.Incident.ID >= incidentID
			}
			return m, true
		}) {
			dropped = append(dropped, r)
			continue
		}
		kept = append(kept, r)
	}
	s.retries = kept
	return dropped
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

// next is when the earliest entry or retry falls due; zero when none is
// pending.
func (s *schedule) next() time.Time {
	var next time.Time
	for _, o := range s.entries {
		if o.pending() && (next.IsZero() || o.due.Before(next)) {
			next = o.due
		}
	}
	for _, r := range s.retries {
		if next.IsZero() || r.due.Before(next) {
			next = r.due
		}
	}
	return next
}

// retries says whether a notification a channel does not take is sent
// again: one of an outage or of its end, with the attempts on.
func (e *Engine) retries(n Notification) bool {
	if e.opts.RetryMax <= 0 {
		return false
	}
	return n.Event == EventDown || n.Event == EventUp && e.opts.RetryFor > 0
}

// pauses says whether the channel is paused once an attempt does not
// reach it or it is busy: a channel software reads, with the attempts
// again on.
func (e *Engine) pauses(ch store.Channel) bool {
	return machine(ch.Type) && e.opts.RetryMax > 0
}

// paused returns the retry that a channel software reads waits for when
// its last pass stopped at a failure that did not reach it or a busy
// answer: the first of its retries still to come, those waiting out a
// refusal aside. Until it is due the channel is sent nothing, and what
// comes up for it is held back for that retry's pass (retryLater), where
// it goes with what else waits for the channel: a channel that is down
// costs the loop one attempt per wait rather than one per notification, a
// busy one is asked again when it said, and the queue keeps moving. Nil
// when the channel took or refused all it was sent last, or has nothing
// to be sent again but what it refused.
func (e *Engine) paused(ch store.Channel, now time.Time) *retry {
	if !e.pauses(ch) || !e.owed.failing[ch.ID] {
		return nil
	}
	var first *retry
	for _, r := range e.owed.retries {
		if r.channel.ID == ch.ID && !r.refused && r.due.After(now) && (first == nil || r.due.Before(first.due)) {
			first = r
		}
	}
	return first
}

// retryLater notes, after a pass through the channels, what to send
// again, by what each failure said (failure). A notification of an outage
// or of its end that did not reach a channel waits retryBase, then twice
// the last wait each time, RetryMax at most; one the receiver refused
// waits RetryMax; the end of an outage is not waited for past RetryFor
// from the first failure. One the receiver was busy for goes again when
// the receiver said, and so does any other but a test: not a refusal, it
// is not counted against the note, and the wait after it is not doubled.
// A note held back in a pass that stopped at a failure (deliver) waits the
// same way, behind a failure that did not reach the channel twice as long
// as before, behind a busy answer until the channel said, with no attempt
// counted against it. A first note held back for a channel software reads
// (deliver, paused) waits for the channel's next pass and goes in it, once
// only if it is not attempted again otherwise, and its RetryFor counts
// from that pass, its first attempt: not from when it was held back, which
// a wait longer than RetryFor would end before any attempt. Held back
// again before any attempt, a note's RetryFor moves with it to its next
// pass, and a note that goes once still goes then: only the channel
// refusing it or not taking it gives it up. Everything else, and
// everything with the attempts off, has failed for good.
func (e *Engine) retryLater(failures []failed) {
	now := e.now()
	held := make(map[string]int)
	for _, f := range failures {
		r := f.retry
		switch {
		case f.held && r != nil:
			if f.class == busy {
				r.due = now.Add(f.wait)
			} else {
				r.due, r.wait = now.Add(r.wait), min(r.wait*2, e.opts.RetryMax)
			}
			if !r.tried {
				r.since = r.due
			}
		case f.held:
			// Not attempted: it goes with what the channel waits for, or,
			// when nothing else waits, it starts the wait.
			wait := min(retryBase, e.opts.RetryMax)
			due, next := now.Add(wait), min(wait*2, e.opts.RetryMax)
			if p := e.paused(f.channel, now); p != nil {
				due, next = p.due, p.wait
			}
			if f.note.Event == EventReminder || slowness(f.note.Event) {
				e.owed.supersede(f.channel.ID, f.note)
			}
			r = e.owed.addRetry(f.channel, f.note, due, due, next)
			r.once = !e.retries(f.note)
		case f.class == busy && e.opts.RetryMax > 0 && (r != nil || f.note.Event != EventTest):
			if r == nil {
				if f.note.Event == EventReminder || slowness(f.note.Event) {
					e.owed.supersede(f.channel.ID, f.note)
				}
				r = e.owed.addRetry(f.channel, f.note, now, now, min(retryBase, e.opts.RetryMax))
				r.once = !e.retries(f.note)
			}
			r.due, r.tried, r.refused = now.Add(f.wait), true, false
		case r != nil && r.once:
			e.owed.dropRetry(r)
			e.gaveUp(f.channel, f.note, "not taken when attempted")
			continue
		case r != nil && f.class == refused:
			r.due, r.wait = now.Add(e.opts.RetryMax), e.opts.RetryMax
		case r != nil:
			r.due, r.wait = now.Add(r.wait), min(r.wait*2, e.opts.RetryMax)
		case e.retries(f.note) && f.class == refused:
			r = e.owed.addRetry(f.channel, f.note, now, now.Add(e.opts.RetryMax), e.opts.RetryMax)
		case e.retries(f.note):
			wait := min(retryBase, e.opts.RetryMax)
			r = e.owed.addRetry(f.channel, f.note, now, now.Add(wait), min(wait*2, e.opts.RetryMax))
		default:
			e.gaveUp(f.channel, f.note, "")
			continue
		}
		if !f.held && f.class != busy {
			r.attempts++
			r.tried, r.refused = true, f.class == refused
		}
		if f.note.Event == EventUp && !r.once && r.due.After(r.since.Add(e.opts.RetryFor)) {
			e.owed.dropRetry(r)
			e.gaveUp(f.channel, f.note, "the end of the outage was not taken within UPTIME_NOTIFY_RETRY_FOR")
			continue
		}
		if f.held {
			held[f.channel.Name]++
			continue
		}
		e.opts.Logger.Warn("notification to be sent again", "channel", f.channel.Name, "event", f.note.Event, "monitor", about(f.note),
			"in", r.due.Sub(now).String(), "failure", f.class, "failed_attempts", r.attempts)
	}
	for channel, count := range held {
		e.opts.Logger.Warn("notifications held back, the channel took none; to be sent later", "channel", channel, "count", count)
	}
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
// must not cost it the end. What the channels did not take goes again
// with it, when its wait is up (retryLater): less the monitors whose
// outage is over or that are gone, for the end of an outage less those
// with a newer outage open, and not at all to a channel gone or no
// longer listening, or past RetryFor for the end of an outage. A paused
// channel is sent none of it: what falls due for it waits for its next
// pass (paused).
func (e *Engine) deliverOwed(ctx context.Context) {
	now := e.now()
	due := e.owed.due(now)
	retries := e.owed.retriesDue(now)
	if len(due) == 0 && len(retries) == 0 {
		return
	}
	// Reads and sends must still work while shutting down.
	ctx = context.WithoutCancel(ctx)
	channels, err := e.store.ListChannels(ctx)
	if err != nil {
		e.opts.Logger.Error("list channels failed, owed notifications wait", "count", len(due)+len(retries), "error", err)
		for _, o := range due {
			o.due = now.Add(owedRetry)
		}
		for _, r := range retries {
			r.due = now.Add(owedRetry)
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

	// One share per channel, its batches in the order they fell due: the
	// channels software reads get each notification on its own, the rest
	// one message per batch. The channels are sent to in parallel; a
	// paused one is sent nothing, its notes are held back.
	var (
		shares []outbound
		held   []failed
	)
	at := make(map[int64]int)
	share := func(ch store.Channel) *outbound {
		i, ok := at[ch.ID]
		if !ok {
			i = len(shares)
			at[ch.ID] = i
			shares = append(shares, outbound{channel: ch})
		}
		return &shares[i]
	}
	for _, key := range order {
		ch, notes := byID[key.channel], batches[key]
		if !machine(ch.Type) {
			n, _ := grouped(key.event, notes) // a batch is never empty
			notes = []Notification{n}
		} else if e.paused(ch, now) != nil {
			for _, n := range notes {
				held = append(held, failed{channel: ch, note: n, held: true})
			}
			continue
		}
		s := share(ch)
		s.notes = append(s.notes, notes...)
	}

	// What the channels did not take, after the batches: the message as
	// it went, less what there is no longer anything to tell about. A
	// retry taken this time is done with; one not taken waits again
	// (retryLater).
	var sent []*retry
	for _, r := range retries {
		ch, exists := byID[r.channel.ID]
		switch {
		case !exists || !ch.Enabled || !ch.Wants(r.note.Event.Kind()):
			e.owed.dropRetry(r)
			e.gaveUp(r.channel, r.note, "the channel is gone, disabled or no longer listens for the event")
			continue
		case r.note.Event == EventUp && !r.once && now.After(r.since.Add(e.opts.RetryFor)):
			e.owed.dropRetry(r)
			e.gaveUp(r.channel, r.note, "the end of the outage was not taken within UPTIME_NOTIFY_RETRY_FOR")
			continue
		}
		var storeErr error
		left := r.narrow(func(n Notification) (Notification, bool) {
			if storeErr != nil {
				return n, true
			}
			m, err := e.store.GetMonitor(ctx, n.Monitor.ID)
			if errors.Is(err, store.ErrNotFound) {
				return n, false
			}
			if err != nil {
				storeErr = err
				return n, true
			}
			if !m.Enabled || !ch.Covers(m) {
				return n, false
			}
			n.Monitor = m
			incident, err := e.store.GetOpenIncident(ctx, m.ID, store.IncidentDown)
			switch {
			case err != nil && !errors.Is(err, store.ErrNotFound):
				storeErr = err
				return n, true
			case n.Event == EventDown || n.Event == EventReminder:
				// The outage, or a reminder of it, is told while it is
				// the one open.
				return n, err == nil && incident.ID == n.Incident.ID
			case n.Event == EventUp:
				// The end of an outage is told while no outage is open:
				// after the next one opened, the channel would read the
				// monitor as up again while it is down.
				return n, err != nil
			}
			// A certificate or slowness notification is about no outage.
			return n, true
		})
		if storeErr != nil {
			// The store could not say; nothing may pass for over.
			e.opts.Logger.Error("notification to be sent again waits", "channel", ch.Name, "monitor", about(r.note), "error", storeErr)
			r.due = now.Add(owedRetry)
			continue
		}
		if !left {
			e.owed.dropRetry(r)
			reason := "the outage ended, or its monitor is gone, before the channel took it"
			switch {
			case r.note.Event == EventUp:
				reason = "a newer outage opened, or the monitor is gone, before the channel took the end"
			case !r.note.Event.Outage():
				reason = "the monitor is gone, or the channel no longer hears of it"
			}
			e.gaveUp(r.channel, r.note, reason)
			continue
		}
		r.channel = ch
		if p := e.paused(ch, now); p != nil {
			// The channel took none of late: this goes in its next pass,
			// and a note not attempted yet counts its RetryFor from there.
			if !r.tried {
				r.since = p.due
			}
			r.due = p.due
			continue
		}
		s := share(ch)
		if s.retries == nil {
			s.retries = make(map[int]*retry)
		}
		s.retries[len(s.notes)] = r
		s.notes = append(s.notes, r.note)
		sent = append(sent, r)
	}
	failures := e.deliver(ctx, shares)
	notTaken := make(map[*retry]bool, len(failures))
	for _, f := range failures {
		notTaken[f.retry] = true
	}
	for _, r := range sent {
		if !notTaken[r] {
			e.owed.dropRetry(r)
		}
	}
	e.retryLater(append(failures, held...))
}

// restore works out, from the outages open now, what the channels with a
// delay or a repeat interval are owed about them: what the loop had in
// hand before a restart. An outage whose delay has not passed is told when
// it does. One whose delay has passed was told if the delivery log has
// the attempt: the channel's next reminder is then where its cadence from
// that attempt falls. Without the attempt, either the delay passed in
// maintenance and the loop silenced the outage, which stays silenced, or
// the restart came between the delay and the delivery, and the outage,
// still going on, is told now. A channel without a delay was sent the
// outage when it opened, unless the restart came while it waited in the
// queue or for its group: the log has no attempt then, and the outage is
// told now the same way, but not to a channel created after it opened,
// which was not there to hear of it, nor about an outage that opened after
// the engine started, which the engine sends itself, nor about one opened
// before the log was kept (no OpenedAt), of which the log says nothing.
// Whichever channel was sent the outage, at once or after its delay, and
// did not take it is sent it again now (retryUndelivered): the retry the
// loop had in hand before the restart.
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
			if !ch.Enabled || !ch.Covers(m) {
				continue
			}
			note := Notification{Event: EventDown, Monitor: m, Incident: incident, At: opened,
				Language: e.opts.Language, PublicURL: e.opts.PublicURL}
			key := owedKey{ch.ID, m.ID}
			if ch.Delay <= 0 && ch.Repeat <= 0 {
				// Sent the outage when it opened, or about to be; owed
				// nothing else.
				told, _ := e.wasTold(ctx, ch, incident)
				switch {
				case told:
					e.retryUndelivered(ctx, ch, note, now)
				case !incident.OpenedAt.IsZero() && ch.CreatedAt.Before(opened) && opened.Before(e.started) &&
					e.owed.get(key) == nil && !e.owed.hasRetry(ch.ID, EventDown, incident.ID):
					// Never attempted: it is told now (deliverOwed), and if
					// the channel does not take it, sent again. An outage
					// without OpenedAt is older than the log: no attempt
					// there says nothing.
					e.owed.put(key, &owed{channel: ch, note: note, kind: owedOutage, due: now})
				}
				continue
			}
			if e.owed.get(key) != nil {
				continue
			}
			o := &owed{channel: ch, note: note}
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
			if told {
				e.retryUndelivered(ctx, ch, note, now)
			}
		}
	}
}

// retryUndelivered sends the channel the outage again, now, when it was
// sent it and took none of the attempts, as the loop would have before
// the restart: the delivery log says. With the attempts off nothing is
// sent again; a channel that does not listen for outages opening was
// sent nothing; and when the log cannot be read, the channel counts as
// having taken it, as it would without the log: a page too few rather
// than one too many.
func (e *Engine) retryUndelivered(ctx context.Context, ch store.Channel, n Notification, now time.Time) {
	if e.opts.RetryMax <= 0 || !ch.Wants(EventDown.Kind()) || e.owed.hasRetry(ch.ID, EventDown, n.Incident.ID) {
		return
	}
	delivered, err := e.store.Delivered(ctx, n.Incident.ID, ch.ID, string(EventDown))
	if err != nil {
		e.opts.Logger.Error("delivery log not read; the channel counts as having taken the outage", "channel", ch.Name, "incident_id", n.Incident.ID, "error", err)
		return
	}
	if delivered {
		return
	}
	e.owed.addRetry(ch, n, now, now, min(retryBase, e.opts.RetryMax))
	e.opts.Logger.Warn("notification to be sent again after the restart", "channel", ch.Name, "event", n.Event, "monitor", n.Monitor.Name)
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
