package alert

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// log is what was sent so far, as "channel:event" per delivery.
func (r *recorder) log() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]string, len(r.sent))
	for i, n := range r.sent {
		items[i] = r.channels[i] + ":" + string(n.Event)
		if len(n.Group) > 1 {
			names := make([]string, len(n.Group))
			for j, g := range n.Group {
				names[j] = g.Monitor.Name
			}
			items[i] += "[" + strings.Join(names, " ") + "]"
		}
	}
	return strings.Join(items, ",")
}

// useClock makes the delivery loop read the fixture's clock, which the
// checks advance by 30 seconds each, and the engine start at it.
func (f *fixture) useClock() {
	f.engine.now = func() time.Time { return f.clock }
	f.engine.started = f.clock
}

func (f *fixture) addChannel(ch store.Channel) store.Channel {
	f.t.Helper()
	ch.Enabled = true
	created, err := f.db.CreateChannel(context.Background(), ch)
	if err != nil {
		f.t.Fatal(err)
	}
	return created
}

// A channel with a delay hears of an outage only once the delay has
// passed and the outage is still going on; of one over before then it
// hears nothing, neither the start nor the end.
func TestChannelDelayTellsOnlyOutagesThatLast(t *testing.T) {
	f := newFixture(t, 1, 1) // "ops" is a webhook that hears at once
	f.useClock()
	f.addChannel(store.Channel{Name: "pager", Type: store.ChannelPagerDuty, Secret: "key", Delay: 10 * time.Minute})

	// A short outage: over after five minutes.
	f.check(false)
	f.flush()
	if got := f.sender.log(); got != "ops:down" {
		t.Fatalf("at the start of the outage: %q, want ops:down", got)
	}
	for i := 0; i < 9; i++ {
		f.check(false)
	}
	f.check(true)
	f.flush()
	if got := f.sender.log(); got != "ops:down,ops:up" {
		t.Fatalf("after a 5-minute outage: %q, want ops:down,ops:up (pager hears nothing)", got)
	}
	// The pager is owed nothing any more.
	if len(f.engine.owed.entries) != 0 {
		t.Errorf("entries after the outage = %d, want none", len(f.engine.owed.entries))
	}

	// A long one: the pager is told after ten minutes, and of the end.
	f.sender.reset()
	opened := f.check(false)
	f.flush()
	for i := 0; i < 19; i++ { // 9.5 minutes: not yet
		f.check(false)
		f.flush()
	}
	if got := f.sender.log(); got != "ops:down" {
		t.Fatalf("9.5 minutes in: %q, want ops:down", got)
	}
	f.check(false) // 10 minutes
	f.flush()
	if got := f.sender.log(); got != "ops:down,pager:down" {
		t.Fatalf("10 minutes in: %q, want ops:down,pager:down", got)
	}
	// The message is about the outage as it opened; the delivery log says
	// so and when it was attempted.
	f.sender.mu.Lock()
	late := f.sender.sent[1]
	f.sender.mu.Unlock()
	incident := f.incidents()[0]
	if late.Incident.ID != incident.ID || !late.At.Equal(opened.CheckedAt) {
		t.Errorf("delayed notification = %+v, want incident %d opened at %v", late, incident.ID, opened.CheckedAt)
	}
	var logged []string
	for _, d := range f.deliveries(incident.ID) {
		logged = append(logged, d.ChannelName+":"+d.Event)
	}
	if got := strings.Join(logged, ","); got != "ops:down,pager:down" {
		t.Errorf("delivery log = %q, want ops:down,pager:down", got)
	}

	f.check(true)
	f.flush()
	if got := f.sender.log(); got != "ops:down,pager:down,ops:up,pager:up" {
		t.Errorf("after the recovery: %q", got)
	}
}

// A channel with a repeat interval is reminded at its own cadence from
// when it was told, not at the engine's.
func TestChannelRepeatRemindsAtItsOwnInterval(t *testing.T) {
	f := newFixture(t, 1, 1)
	f.useClock()
	f.engine.opts.ReminderInterval = 2 * time.Minute
	f.addChannel(store.Channel{Name: "mail", Type: store.ChannelEmail, Target: "ops@example.com", Repeat: 5 * time.Minute})

	f.check(false)
	f.flush()
	for i := 0; i < 20; i++ { // ten minutes of outage, a check every 30 seconds
		f.check(false)
		f.flush()
	}
	want := "ops:down,mail:down,ops:reminder,ops:reminder,mail:reminder,ops:reminder,ops:reminder,ops:reminder,mail:reminder"
	if got := f.sender.log(); got != want {
		t.Errorf("ten minutes of reminders:\n got %q\nwant %q", got, want)
	}
	f.sender.mu.Lock()
	reminder := f.sender.sent[4]
	f.sender.mu.Unlock()
	if reminder.Event != EventReminder || !reminder.At.Equal(f.clock.Add(-5*time.Minute)) || !strings.Contains(Text(reminder), "5 dk") {
		t.Errorf("mail's first reminder = %+v, text %q", reminder, Text(reminder))
	}

	// The recovery ends them.
	f.check(true)
	for i := 0; i < 12; i++ {
		f.check(true)
		f.flush()
	}
	if got := f.sender.log(); !strings.HasSuffix(got, "ops:up,mail:up") || strings.Count(got, "mail:reminder") != 2 {
		t.Errorf("after the recovery: %q", got)
	}
}

// A delay and a repeat interval together: the first reminder comes one
// interval after the channel was told, which is after its delay.
func TestChannelDelayThenRepeat(t *testing.T) {
	f := newFixture(t, 1, 1)
	f.useClock()
	f.addChannel(store.Channel{Name: "pager", Type: store.ChannelPagerDuty, Secret: "key", Delay: 2 * time.Minute, Repeat: 3 * time.Minute})

	f.check(false)
	f.flush()
	for i := 0; i < 16; i++ { // eight minutes
		f.check(false)
		f.flush()
	}
	// Told at 2m, reminded at 5m and 8m.
	if got := f.sender.log(); got != "ops:down,pager:down,pager:reminder,pager:reminder" {
		t.Errorf("eight minutes in: %q", got)
	}
}

// What channels are owed is worked out again from the open outages at a
// start: a restart during a delay does not lose the notification, and does
// not repeat one whose delay had passed.
func TestOwedNotificationsSurviveARestart(t *testing.T) {
	f := newFixture(t, 1, 1)
	f.useClock()
	f.addChannel(store.Channel{Name: "pager", Type: store.ChannelPagerDuty, Secret: "key", Delay: 10 * time.Minute, Repeat: 10 * time.Minute})
	f.check(false)
	f.flush()

	restart := func() {
		f.engine = New(f.db, f.sender, f.engine.opts)
		f.useClock()
	}
	// Five minutes in, a restart; the new engine reads the open outage
	// and still tells the pager at ten minutes.
	for i := 0; i < 10; i++ {
		f.check(false)
	}
	restart()
	for i := 0; i < 10; i++ {
		f.check(false)
		f.flush()
	}
	if got := f.sender.log(); got != "ops:down,pager:down" {
		t.Fatalf("after a restart during the delay: %q, want ops:down,pager:down", got)
	}

	// Another restart after it was told: the pager is not told again,
	// and its next reminder is where its cadence falls (20 minutes in).
	for i := 0; i < 4; i++ {
		f.check(false)
	}
	restart()
	for i := 0; i < 16; i++ { // to 20 minutes
		f.check(false)
		f.flush()
	}
	if got := f.sender.log(); got != "ops:down,pager:down,pager:reminder" {
		t.Errorf("after a restart past the delay: %q, want one reminder at 20 minutes", got)
	}
	f.check(true)
	f.flush()
	if got := f.sender.log(); !strings.HasSuffix(got, "ops:up,pager:up") {
		t.Errorf("after the recovery: %q", got)
	}
}

// Delayed outages that fall due together go to a channel people read as
// one message, as they would have when they arose.
func TestDelayedOutagesDueTogetherAreGrouped(t *testing.T) {
	f := newFixture(t, 1, 1)
	f.useClock()
	f.engine.opts.GroupWait = 50 * time.Millisecond
	f.addChannel(store.Channel{Name: "chat", Type: store.ChannelSlack, Target: "http://unused", Delay: 5 * time.Minute})
	ctx := context.Background()
	var monitors []store.Monitor
	for _, name := range []string{"cart", "orders"} {
		m, err := f.db.CreateMonitor(ctx, store.Monitor{Name: name, Type: store.MonitorTCP, Target: name + ":80", Namespace: "shop",
			Interval: 30 * time.Second, Timeout: 5 * time.Second, FailureThreshold: 1, Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		monitors = append(monitors, m)
	}
	fail := func() {
		f.clock = f.clock.Add(30 * time.Second)
		var batch []store.CheckResult
		for _, m := range monitors {
			batch = append(batch, store.CheckResult{MonitorID: m.ID, CheckedAt: f.clock, Error: "refused"})
		}
		if err := f.db.InsertCheckResults(ctx, batch); err != nil {
			t.Fatal(err)
		}
		f.engine.Evaluate(ctx, batch)
	}
	fail()
	f.flush() // the webhook gets each at once; the chat channel is owed both
	if got := f.sender.log(); got != "ops:down,ops:down" {
		t.Fatalf("at the start: %q", got)
	}
	for i := 0; i < 10; i++ {
		fail()
	}
	f.flush()
	if got := f.sender.log(); got != "ops:down,ops:down,chat:down[cart orders]" {
		t.Errorf("five minutes in: %q, want one grouped message to chat", got)
	}
}

// In maintenance an outage whose delay passes is not told, and a
// channel's own reminders are skipped; neither resumes afterwards for
// that outage, as with the engine's reminders.
func TestOwedNotificationsRespectMaintenance(t *testing.T) {
	f := newFixture(t, 1, 1)
	f.useClock()
	f.addChannel(store.Channel{Name: "pager", Type: store.ChannelPagerDuty, Secret: "key", Delay: 2 * time.Minute})
	f.addChannel(store.Channel{Name: "mail", Type: store.ChannelEmail, Target: "ops@example.com", Repeat: 3 * time.Minute})
	f.check(false)
	f.flush()
	f.maintain(10 * time.Minute) // silenced right after the outage opened
	for i := 0; i < 10; i++ {
		f.check(false)
		f.flush()
	}
	if got := f.sender.log(); got != "ops:down,mail:down" {
		t.Errorf("silenced: %q, want ops:down,mail:down", got)
	}
	f.check(true)
	f.flush()
	// The pager was never told of the outage: it does not hear of its end.
	if got := f.sender.log(); got != "ops:down,mail:down,ops:up,mail:up" {
		t.Errorf("after the recovery: %q", got)
	}
}

// A channel told of an outage hears of its end even when it listens for
// no reminders (PagerDuty with the events down and up, as the README has
// it, with a delay and a repeat interval), or stops listening for them
// during the outage: what the channel is owed about an outage it was told
// of is forgotten only with the end.
func TestChannelToldOfAnOutageHearsItsEndWithoutReminders(t *testing.T) {
	f := newFixture(t, 1, 1)
	f.useClock()
	f.addChannel(store.Channel{Name: "pager", Type: store.ChannelPagerDuty, Secret: "key",
		Delay: 2 * time.Minute, Repeat: 3 * time.Minute, Events: []string{"down", "up"}})
	mail := f.addChannel(store.Channel{Name: "mail", Type: store.ChannelEmail, Target: "ops@example.com", Delay: time.Minute, Repeat: 4 * time.Minute})

	f.check(false)
	f.flush()
	for i := 0; i < 8; i++ { // to 4.5 minutes: mail told at 1 minute, the pager at 2
		f.check(false)
		f.flush()
	}
	if got := f.sender.log(); got != "ops:down,mail:down,pager:down" {
		t.Fatalf("four minutes in: %q, want ops:down,mail:down,pager:down", got)
	}
	// Mail stops listening for reminders. Both channels' first reminders
	// fall due at 5.5 minutes; the recovery follows at once, so that
	// nothing else passes through the loop between the two.
	mail.Events = []string{"down", "up"}
	if _, err := f.db.UpdateChannel(context.Background(), mail); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		f.check(false)
		f.flush()
	}
	if got := f.sender.log(); strings.Contains(got, ":reminder") {
		t.Errorf("reminders to channels that want none: %q", got)
	}
	f.check(true)
	f.flush()
	if got := f.sender.log(); got != "ops:down,mail:down,pager:down,ops:up,pager:up,mail:up" {
		t.Errorf("after the recovery: %q, want the end told to both", got)
	}
}

// A channel told of an outage hears of its end when its own reminder falls
// due after the recovery and before the end is delivered: the whole
// GroupWait for channels people read, the queue's latency for the rest.
func TestReminderDueAfterTheRecoveryKeepsTheEnd(t *testing.T) {
	f := newFixture(t, 1, 1)
	f.useClock()
	f.addChannel(store.Channel{Name: "mail", Type: store.ChannelEmail, Target: "ops@example.com", Delay: time.Minute, Repeat: 2 * time.Minute})
	f.check(false)
	f.flush()
	for i := 0; i < 2; i++ { // one minute: mail is told
		f.check(false)
		f.flush()
	}
	if got := f.sender.log(); got != "ops:down,mail:down" {
		t.Fatalf("a minute in: %q, want ops:down,mail:down", got)
	}
	// The recovery's notification waits in the queue while mail's reminder
	// falls due: the timer's tick comes before the queue is read.
	f.check(true)
	f.clock = f.clock.Add(2 * time.Minute)
	f.engine.deliverOwed(context.Background())
	f.flush()
	if got := f.sender.log(); got != "ops:down,mail:down,ops:up,mail:up" {
		t.Errorf("after the recovery: %q, want ops:down,mail:down,ops:up,mail:up", got)
	}
}

// After a restart the loop's timer is set for what it restored: a delayed
// outage is told when its delay passes, not when something else happens
// to pass through the loop.
func TestRestoredOwedNotificationsAreDeliveredOnTime(t *testing.T) {
	f := newFixture(t, 1, 1)
	f.addChannel(store.Channel{Name: "pager", Type: store.ChannelPagerDuty, Secret: "key", Delay: time.Second})
	f.clock = time.Now().UTC().Add(-30 * time.Second) // the outage opens now, by the loop's real clock
	f.check(false)
	f.flush()
	if got := f.sender.log(); got != "ops:down" {
		t.Fatalf("at the start of the outage: %q, want ops:down", got)
	}

	// A restart: the new engine runs with a live context, nothing passing
	// through its queue.
	f.engine = New(f.db, f.sender, f.engine.opts)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.engine.Run(ctx)
	}()
	want := "ops:down,pager:down"
	deadline := time.Now().Add(5 * time.Second)
	for f.sender.log() != want && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	got := f.sender.log()
	cancel()
	<-done
	if got != want {
		t.Errorf("while the loop ran after the restart: %q, want %q", got, want)
	}
}

// A delayed outage the loop did not get to tell before a restart is told at
// the next start rather than counted as told: the delivery log says
// whether the channel heard of it.
func TestRestoreTellsWhatTheRestartMissed(t *testing.T) {
	f := newFixture(t, 1, 1)
	f.useClock()
	f.addChannel(store.Channel{Name: "pager", Type: store.ChannelPagerDuty, Secret: "key", Delay: time.Minute, Repeat: 10 * time.Minute})
	f.check(false)
	f.flush()

	// The process is gone for two minutes; the pager's delay passes
	// meanwhile, with nobody to tell it.
	f.clock = f.clock.Add(2 * time.Minute)
	f.engine = New(f.db, f.sender, f.engine.opts)
	f.useClock()
	f.flush()
	if got := f.sender.log(); got != "ops:down,pager:down" {
		t.Fatalf("after the restart: %q, want ops:down,pager:down", got)
	}
	var logged []string
	for _, d := range f.deliveries(f.incidents()[0].ID) {
		logged = append(logged, d.ChannelName+":"+d.Event)
	}
	if got := strings.Join(logged, ","); got != "ops:down,pager:down" {
		t.Errorf("delivery log = %q, want ops:down,pager:down", got)
	}
	// The pager's reminders run from when it was told: ten minutes later.
	for i := 0; i < 19; i++ {
		f.check(false)
		f.flush()
	}
	if got := f.sender.log(); got != "ops:down,pager:down" {
		t.Errorf("9.5 minutes after the restart: %q, want no reminder yet", got)
	}
	f.check(false)
	f.flush()
	if got := f.sender.log(); got != "ops:down,pager:down,pager:reminder" {
		t.Errorf("10 minutes after the restart: %q, want one reminder", got)
	}
	f.check(true)
	f.flush()
	if got := f.sender.log(); !strings.HasSuffix(got, "ops:up,pager:up") {
		t.Errorf("after the recovery: %q", got)
	}
}

// restart replaces the engine, as a restart of the backend does: what the
// channels are owed is worked out again at its start (restore).
func (f *fixture) restart() {
	f.engine = New(f.db, f.sender, f.engine.opts)
	f.useClock()
}

// liveClock sets the fixture's clock to the store's, which stamps a
// channel's creation (to the millisecond), so that the channels made so
// far are older than an outage the next check opens. A channel made with
// addChannelLater after that check is newer than the outage.
func (f *fixture) liveClock() {
	time.Sleep(2 * time.Millisecond)
	f.clock = time.Now().UTC().Add(-30 * time.Second) // the next check is now
	f.useClock()
}

// addChannelLater adds a channel a moment after what happened so far, by
// the store's clock.
func (f *fixture) addChannelLater(ch store.Channel) store.Channel {
	time.Sleep(2 * time.Millisecond)
	return f.addChannel(ch)
}

// An outage still queued when the process went away was never attempted,
// so the delivery log has nothing of it: at the next start it is told to
// the channels without a delay that were there when it opened, and not
// again after that. A channel created after it opened does not hear of
// it.
func TestARestartSendsTheOutageThatWasStillQueued(t *testing.T) {
	f := newFixture(t, 1, 1) // "ops", a webhook
	f.addChannel(store.Channel{Name: "mail", Type: store.ChannelEmail, Target: "ops@example.com"})
	f.liveClock()
	f.check(false) // the outage opens, and its notification is queued
	f.addChannelLater(store.Channel{Name: "late", Type: store.ChannelWebhook, Target: "http://unused"})

	// The process goes away with the notification in its queue, and is
	// back a minute later.
	f.clock = f.clock.Add(time.Minute)
	f.restart()
	f.flush()
	if got := f.sender.log(); got != "ops:down,mail:down" {
		t.Fatalf("after the restart: %q, want ops:down,mail:down and nothing to the channel created since", got)
	}
	var logged []string
	for _, d := range f.deliveries(f.incidents()[0].ID) {
		logged = append(logged, d.ChannelName+":"+d.Event+":"+strconv.FormatBool(d.Delivered))
	}
	sort.Strings(logged)
	if got := strings.Join(logged, ","); got != "mail:down:true,ops:down:true" {
		t.Errorf("delivery log = %q, want both taken", got)
	}
	f.restart()
	f.flush()
	if got := f.sender.log(); got != "ops:down,mail:down" {
		t.Errorf("after a second restart: %q, want nothing told again", got)
	}
}

// An outage waiting for its group when the process went away reached the
// channels software reads, which do not wait, and none that people read:
// at the next start these are told it, the outages of a namespace in one
// message.
func TestARestartDuringGroupWaitTellsThePeopleChannels(t *testing.T) {
	f := newFixture(t, 1, 1) // "ops", a webhook
	f.engine.opts.GroupWait = time.Minute
	f.addChannel(store.Channel{Name: "chat", Type: store.ChannelSlack, Target: "http://unused"})
	cart, orders := f.monitorIn("cart", "shop"), f.monitorIn("orders", "shop")
	f.liveClock()
	f.clock = f.clock.Add(30 * time.Second)
	f.result(cart, false)
	f.result(orders, false)
	// What the loop does with them (Run's add): the webhook hears of each
	// at once, and chat's group waits; the process goes away before it is
	// due.
	for i := 0; i < 2; i++ {
		f.engine.deliverTo(context.Background(), <-f.engine.queue, func(ch store.Channel) bool { return machine(ch.Type) })
	}
	f.sender.settle()
	if got := f.sender.log(); got != "ops:down,ops:down" {
		t.Fatalf("before the restart: %q, want the webhook told of both", got)
	}

	f.clock = f.clock.Add(time.Minute)
	f.restart()
	f.flush()
	if got := f.sender.log(); got != "ops:down,ops:down,chat:down[cart orders]" {
		t.Errorf("after the restart: %q, want chat told of both in one message, the webhook not again", got)
	}
}

// With the attempts off, an outage still queued at a restart is told at
// the next start all the same: it was never attempted. Not taken then, it
// is not sent again, then or at a later start.
func TestARestartWithTheAttemptsOffTellsAQueuedOutageOnce(t *testing.T) {
	f := newFixture(t, 1, 1) // "ops", a webhook; RetryMax is zero
	f.liveClock()
	f.check(false)
	f.clock = f.clock.Add(time.Minute)
	f.restart()
	f.sender.fail("ops", 3) // every try of the attempt
	f.flush()
	if n := len(f.deliveries(f.incidents()[0].ID)); n != 1 {
		t.Fatalf("delivery log has %d entries after the restart, want the attempt", n)
	}
	if n := len(f.engine.owed.retries); n != 0 {
		t.Errorf("%d retries with the attempts off, want none", n)
	}
	f.restart()
	f.flush()
	if got, n := f.sender.log(), len(f.deliveries(f.incidents()[0].ID)); got != "" || n != 1 {
		t.Errorf("after a second restart: sent %q, %d entries; want nothing sent again", got, n)
	}
}

// outage keeps the monitor failing for the given number of minutes, a
// check and a pass through the loop every 30 seconds.
func (f *fixture) outage(minutes float64) {
	for i := 0; i < int(minutes*2); i++ {
		f.check(false)
		f.flush()
	}
}

// flakyStore fails the next GetMonitor or GetOpenIncident calls the way a
// busy database does: with an error that is not ErrNotFound.
type flakyStore struct {
	Store
	failMonitor, failIncident int
}

func (s *flakyStore) GetMonitor(ctx context.Context, id int64) (store.Monitor, error) {
	if s.failMonitor > 0 {
		s.failMonitor--
		return store.Monitor{}, errors.New("database is locked")
	}
	return s.Store.GetMonitor(ctx, id)
}

func (s *flakyStore) GetOpenIncident(ctx context.Context, monitorID int64, kind store.IncidentKind) (store.Incident, error) {
	if s.failIncident > 0 {
		s.failIncident--
		return store.Incident{}, errors.New("database is locked")
	}
	return s.Store.GetOpenIncident(ctx, monitorID, kind)
}

// A due outage that meets a store error waits owedRetry and is told then,
// rather than passing for over: the store not answering must not cost the
// channel the page.
func TestDueOutageWaitsOutAStoreError(t *testing.T) {
	for _, which := range []string{"GetMonitor", "GetOpenIncident"} {
		t.Run(which, func(t *testing.T) {
			f := newFixture(t, 1, 1)
			flaky := &flakyStore{Store: f.db}
			f.engine = New(flaky, f.sender, f.engine.opts)
			f.useClock()
			pager := f.addChannel(store.Channel{Name: "pager", Type: store.ChannelPagerDuty, Secret: "key", Delay: time.Minute})
			f.check(false)
			f.flush()
			key := owedKey{pager.ID, f.monitor.ID}
			if f.engine.owed.get(key) == nil {
				t.Fatal("nothing noted for the pager")
			}

			// The delay passes while the store is busy.
			f.clock = f.clock.Add(time.Minute)
			if which == "GetMonitor" {
				flaky.failMonitor = 1
			} else {
				flaky.failIncident = 1
			}
			f.engine.deliverOwed(context.Background())
			o := f.engine.owed.get(key)
			if o == nil {
				t.Fatalf("%s failed once: the pager's due outage was dropped; sent %q", which, f.sender.log())
			}
			if o.kind != owedOutage || o.told || !o.due.Equal(f.clock.Add(owedRetry)) {
				t.Errorf("entry after the error = kind %d told %v due %v, want the outage due again at %v", o.kind, o.told, o.due, f.clock.Add(owedRetry))
			}
			if got := f.sender.log(); got != "ops:down" {
				t.Errorf("while the store was busy: %q", got)
			}
			f.clock = f.clock.Add(owedRetry)
			f.engine.deliverOwed(context.Background())
			if got := f.sender.log(); got != "ops:down,pager:down" {
				t.Errorf("after the retry: %q, want ops:down,pager:down", got)
			}
		})
	}
}

// The end of an outage forgets what every channel was owed about it, a
// channel disabled or narrowed meanwhile included: enabled again during a
// later outage it was never told of, it must not hear that outage's end.
func TestEndOfAnOutageForgetsEveryChannel(t *testing.T) {
	for _, how := range []string{"disabled", "narrowed"} {
		t.Run(how, func(t *testing.T) {
			f := newFixture(t, 1, 1)
			f.useClock()
			pager := f.addChannel(store.Channel{Name: "pager", Type: store.ChannelPagerDuty, Secret: "key", Delay: time.Minute, Repeat: 5 * time.Minute})
			update := func(enabled bool, namespaces []string) {
				pager.Enabled, pager.Namespaces = enabled, namespaces
				if _, err := f.db.UpdateChannel(context.Background(), pager); err != nil {
					t.Fatal(err)
				}
			}
			f.outage(2) // told at 1:30
			if got := f.sender.log(); got != "ops:down,pager:down" {
				t.Fatalf("told: %q", got)
			}
			if how == "disabled" {
				update(false, nil)
			} else {
				update(true, []string{"elsewhere"})
			}
			f.outage(5) // to 7:00; the pager's reminder fell due at 6:30 while it did not listen
			f.check(true)
			f.flush() // 7:30: the end goes to whoever listens
			if got := f.sender.log(); got != "ops:down,pager:down,ops:up" {
				t.Fatalf("after the first outage: %q, want ops:down,pager:down,ops:up", got)
			}
			if n := len(f.engine.owed.entries); n != 0 {
				t.Errorf("%d entries left after the outage, want none", n)
			}

			f.check(false)
			f.flush() // 8:00: a second outage the pager does not hear of
			update(true, nil)
			f.check(false)
			f.flush()
			f.check(true)
			f.flush() // 9:00: its end
			if got := f.sender.log(); got != "ops:down,pager:down,ops:up,ops:down,ops:up" {
				t.Errorf("after the second outage: %q, want no pager:up for an outage it was not told of", got)
			}
		})
	}
}

// An outage whose delay passed in maintenance stays silenced for the
// channel across a restart: the delivery log has no attempt, but the loop
// did not miss it, it withheld it.
func TestRestartKeepsAnOutageSilencedInMaintenance(t *testing.T) {
	f := newFixture(t, 1, 1)
	f.useClock()
	pager := f.addChannel(store.Channel{Name: "pager", Type: store.ChannelPagerDuty, Secret: "key", Delay: time.Minute})
	f.check(false)
	f.flush()
	f.maintain(3 * time.Minute) // 0:30 to 3:30; the delay passes at 1:30
	f.outage(1.5)
	if got := f.sender.log(); got != "ops:down" {
		t.Fatalf("silenced: %q, want ops:down", got)
	}
	f.outage(3) // the window is over
	f.restart()
	f.flush()
	if got := f.sender.log(); got != "ops:down" {
		t.Fatalf("after a restart: %q, want the pager still not told", got)
	}
	if o := f.engine.owed.get(owedKey{pager.ID, f.monitor.ID}); o == nil || o.kind != owedSilence || o.told {
		t.Errorf("restored entry = %+v, want it silenced and not told", o)
	}
	f.check(true)
	f.flush()
	if got := f.sender.log(); got != "ops:down,ops:up" {
		t.Errorf("after the recovery: %q, want ops:down,ops:up (the pager hears neither)", got)
	}
}

// An outage told late, at a start after its delay had passed, runs the
// channel's reminders from that telling; a further restart reads it from
// the delivery log rather than counting from the delay.
func TestRestartRunsRemindersFromTheLateTelling(t *testing.T) {
	f := newFixture(t, 1, 1)
	f.useClock()
	f.addChannel(store.Channel{Name: "pager", Type: store.ChannelPagerDuty, Secret: "key", Delay: time.Minute, Repeat: 3 * time.Minute})
	f.check(false)
	f.flush()
	// The process is gone until 2:30; the pager is told at the start.
	f.clock = f.clock.Add(2 * time.Minute)
	f.restart()
	f.flush()
	if got := f.sender.log(); got != "ops:down,pager:down" {
		t.Fatalf("after the restart: %q, want ops:down,pager:down", got)
	}
	// Two more restarts: told once, and the reminder three minutes after
	// the telling (5:30), not after the delay (4:30).
	f.restart()
	f.flush()
	f.restart()
	f.outage(2.5)
	if got := f.sender.log(); got != "ops:down,pager:down" {
		t.Errorf("at 5:00 after three restarts: %q, want no reminder yet", got)
	}
	f.check(false)
	f.flush()
	if got := f.sender.log(); got != "ops:down,pager:down,pager:reminder" {
		t.Errorf("at 5:30: %q, want the reminder", got)
	}
	f.check(true)
	f.flush()
	if got := f.sender.log(); got != "ops:down,pager:down,pager:reminder,ops:up,pager:up" {
		t.Errorf("after the recovery: %q", got)
	}
}
