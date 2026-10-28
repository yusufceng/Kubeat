package alert

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/metrics"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// retrying turns on sending again what a channel does not take, with the
// loop on the fixture's clock, one try per attempt (the short tries within
// an attempt are tested elsewhere) and metrics to read.
func (f *fixture) retrying(max, forUp time.Duration) {
	f.useClock()
	f.engine.opts.RetryMax, f.engine.opts.RetryFor = max, forUp
	f.engine.opts.Attempts = 1
	f.engine.opts.Metrics = metrics.New("test")
}

// metric reads one value of the engine's metrics; a series not there yet
// reads 0.
func (f *fixture) metric(name string) float64 {
	f.t.Helper()
	values, err := f.engine.opts.Metrics.Gather()
	if err != nil {
		f.t.Fatal(err)
	}
	return values[name]
}

// counts checks the attempts and the notifications counted so far.
func (f *fixture) counts(want map[string]float64) {
	f.t.Helper()
	for key, want := range want {
		if got := f.metric(key); got != want {
			f.t.Errorf("%s = %v, want %v", key, got, want)
		}
	}
}

// An outage notification a channel does not take is sent to it again a
// minute later; every attempt is on the record, the notification is
// counted once, by how it ended, and the channel's row keeps the error
// until it takes one.
func TestAnOutageNotificationNotTakenIsSentAgain(t *testing.T) {
	f := newFixture(t, 1, 1) // "ops", a webhook without a delay
	f.retrying(15*time.Minute, time.Hour)
	ctx := context.Background()
	f.sender.fail("ops", 1)
	f.check(false)
	f.flush()
	if got := f.sender.log(); got != "" {
		t.Fatalf("while the webhook does not answer: %q, want nothing taken", got)
	}
	if n := len(f.engine.owed.retries); n != 1 {
		t.Fatalf("%d retries, want the outage to be sent again", n)
	}
	channels, err := f.db.ListChannels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ch := channels[0]; ch.LastOK || ch.LastError != "boom" {
		t.Errorf("the channel's row after the failure: ok %v, error %q; want the failure shown", ch.LastOK, ch.LastError)
	}

	// Not at 30 seconds, at a minute.
	f.check(false)
	f.flush()
	if got := f.sender.log(); got != "" {
		t.Fatalf("30 seconds after the failure: %q, want nothing yet", got)
	}
	f.check(false)
	f.flush()
	if got := f.sender.log(); got != "ops:down" {
		t.Fatalf("a minute after the failure: %q, want ops:down", got)
	}
	if n := len(f.engine.owed.retries); n != 0 {
		t.Errorf("%d retries after the channel took it, want none", n)
	}

	// Both attempts are on the record: on the incident and on the channel.
	var logged []string
	for _, d := range f.deliveries(f.incidents()[0].ID) {
		logged = append(logged, d.Event+":"+strconv.FormatBool(d.Delivered))
	}
	if got := strings.Join(logged, ","); got != "down:false,down:true" {
		t.Errorf("delivery log = %q, want the failed attempt and the one taken", got)
	}
	if channels, err = f.db.ListChannels(ctx); err != nil {
		t.Fatal(err)
	}
	if ch := channels[0]; !ch.LastOK || ch.LastError != "" {
		t.Errorf("the channel's row after the attempt taken: ok %v, error %q", ch.LastOK, ch.LastError)
	}
	f.counts(map[string]float64{
		"kubeat_notification_attempts_total{channel_type=webhook}{result=failed}":     1,
		"kubeat_notification_attempts_total{channel_type=webhook}{result=sent}":       1,
		"kubeat_notifications_total{channel_type=webhook}{event=down}{result=sent}":   1,
		"kubeat_notifications_total{channel_type=webhook}{event=down}{result=failed}": 0,
	})

	f.check(true)
	f.flush()
	if got := f.sender.log(); got != "ops:down,ops:up" {
		t.Errorf("after the recovery: %q, want ops:down,ops:up", got)
	}
}

// The outage ending ends the attempts: the channel hears of the end, and
// the notification of the outage counts as failed.
func TestSendingAgainStopsWhenTheOutageEnds(t *testing.T) {
	f := newFixture(t, 1, 1)
	f.retrying(15*time.Minute, time.Hour)
	f.sender.fail("ops", 1)
	f.check(false)
	f.flush()
	f.check(true) // 30 seconds later, before the attempt again
	f.flush()
	if got := f.sender.log(); got != "ops:up" {
		t.Fatalf("after the recovery: %q, want ops:up alone", got)
	}
	if n := len(f.engine.owed.retries); n != 0 {
		t.Errorf("%d retries after the recovery, want none", n)
	}
	f.clock = f.clock.Add(5 * time.Minute)
	f.flush()
	if got := f.sender.log(); got != "ops:up" {
		t.Errorf("five minutes after the recovery: %q, want no outage notification", got)
	}
	f.counts(map[string]float64{
		"kubeat_notification_attempts_total{channel_type=webhook}{result=failed}":     1,
		"kubeat_notifications_total{channel_type=webhook}{event=down}{result=failed}": 1,
		"kubeat_notifications_total{channel_type=webhook}{event=up}{result=sent}":     1,
	})
}

// The wait doubles with every attempt that fails, up to RetryMax, and the
// attempts go on until the channel takes the notification.
func TestTheWaitDoublesUpToTheCap(t *testing.T) {
	f := newFixture(t, 1, 1)
	f.retrying(4*time.Minute, time.Hour)
	f.sender.fail("ops", 6)
	f.check(false)
	f.flush()
	ctx := context.Background()
	for i, wait := range []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 4 * time.Minute, 4 * time.Minute, 4 * time.Minute} {
		if n := len(f.engine.owed.retries); n != 1 {
			t.Fatalf("after %d failed attempts: %d retries, want one", i+1, n)
		}
		r := f.engine.owed.retries[0]
		if !r.due.Equal(f.clock.Add(wait)) || r.attempts != i+1 {
			t.Fatalf("after %d failed attempts: due in %s with %d attempts counted, want due in %s", i+1, r.due.Sub(f.clock), r.attempts, wait)
		}
		f.clock = r.due
		f.engine.deliverOwed(ctx)
	}
	f.sender.settle()
	if got := f.sender.log(); got != "ops:down" {
		t.Errorf("after the seventh attempt: %q, want ops:down", got)
	}
	if n := len(f.engine.owed.retries); n != 0 {
		t.Errorf("%d retries after the channel took it, want none", n)
	}
	f.counts(map[string]float64{
		"kubeat_notification_attempts_total{channel_type=webhook}{result=failed}":   6,
		"kubeat_notification_attempts_total{channel_type=webhook}{result=sent}":     1,
		"kubeat_notifications_total{channel_type=webhook}{event=down}{result=sent}": 1,
	})
}

// The end of an outage is sent again for RetryFor at most: the attempts
// at 1, 3 and 7 minutes are made, the one that would fall at 15 is not.
func TestTheEndOfAnOutageIsSentAgainForAWhileOnly(t *testing.T) {
	f := newFixture(t, 1, 1)
	f.retrying(15*time.Minute, 10*time.Minute)
	f.check(false)
	f.flush()
	f.sender.fail("ops", 10)
	f.check(true)
	since := f.clock
	f.flush()
	if got := f.sender.log(); got != "ops:down" {
		t.Fatalf("while the webhook does not answer: %q, want ops:down alone", got)
	}
	ctx := context.Background()
	for i, at := range []time.Duration{time.Minute, 3 * time.Minute, 7 * time.Minute} {
		if n := len(f.engine.owed.retries); n != 1 {
			t.Fatalf("after %d failed attempts: %d retries, want the end to be sent again", i+1, n)
		}
		r := f.engine.owed.retries[0]
		if r.note.Event != EventUp || !r.due.Equal(since.Add(at)) {
			t.Fatalf("after %d failed attempts: %s due %s after the first failure, want up at %s", i+1, r.note.Event, r.due.Sub(since), at)
		}
		f.clock = r.due
		f.engine.deliverOwed(ctx)
	}
	if n := len(f.engine.owed.retries); n != 0 {
		t.Errorf("after 7 minutes: %d retries, want the end given up", n)
	}
	f.clock = f.clock.Add(time.Hour)
	f.flush()
	if got := f.sender.log(); got != "ops:down" {
		t.Errorf("an hour on: %q, want the end given up", got)
	}
	f.counts(map[string]float64{
		"kubeat_notification_attempts_total{channel_type=webhook}{result=failed}":   4,
		"kubeat_notifications_total{channel_type=webhook}{event=up}{result=failed}": 1,
		"kubeat_notifications_total{channel_type=webhook}{event=down}{result=sent}": 1,
	})
}

// The end of an outage a channel did not take is not sent again once a
// newer outage of the monitor opened: told the end then, the channel
// would read the monitor as up again while it is down. Nor once the end
// of a later outage was told: the earlier end is moot by then.
func TestTheEndOfAnOutageIsNotSentAgainAfterANewerOutage(t *testing.T) {
	f := newFixture(t, 1, 1)
	f.retrying(15*time.Minute, time.Hour)
	f.check(false)
	f.flush()
	f.sender.fail("ops", 1)
	f.check(true) // the end is not taken
	f.flush()
	if got := f.sender.log(); got != "ops:down" {
		t.Fatalf("while the webhook does not answer: %q, want ops:down alone", got)
	}
	if n := len(f.engine.owed.retries); n != 1 {
		t.Fatalf("%d retries, want the end to be sent again", n)
	}
	f.check(false) // 30 seconds on: down again, and the channel takes it
	f.flush()
	if got := f.sender.log(); got != "ops:down,ops:down" {
		t.Fatalf("at the second outage: %q, want ops:down,ops:down", got)
	}
	// A minute after the failure the end of the first outage falls due,
	// and is not sent: the second outage is open.
	f.check(false)
	f.flush()
	if got := f.sender.log(); got != "ops:down,ops:down" {
		t.Fatalf("a minute after the failure: %q, want the end of the first outage not sent", got)
	}
	if n := len(f.engine.owed.retries); n != 0 {
		t.Errorf("%d retries, want the end of the first outage given up", n)
	}
	f.counts(map[string]float64{
		"kubeat_notification_attempts_total{channel_type=webhook}{result=failed}":   1,
		"kubeat_notifications_total{channel_type=webhook}{event=up}{result=failed}": 1,
		"kubeat_notifications_total{channel_type=webhook}{event=down}{result=sent}": 2,
	})
	f.check(true)
	f.flush()
	if got := f.sender.log(); got != "ops:down,ops:down,ops:up" {
		t.Fatalf("after the second outage: %q, want its end alone", got)
	}

	// The end of a third outage is not taken, and a fourth comes and goes
	// before it is due again: once the end of the fourth is told, the end
	// of the third is not sent.
	f.check(false)
	f.flush()
	f.sender.fail("ops", 1)
	f.check(true)
	f.flush()
	f.check(false)
	f.flush()
	f.check(true) // a minute after the failure: the fourth's end goes as the third's falls due
	f.flush()
	const told = "ops:down,ops:down,ops:up,ops:down,ops:down,ops:up"
	if got := f.sender.log(); got != told {
		t.Fatalf("after the fourth outage: %q, want the end of the third not sent", got)
	}
	if n := len(f.engine.owed.retries); n != 0 {
		t.Errorf("%d retries, want the end of the third outage given up", n)
	}
	f.clock = f.clock.Add(15 * time.Minute)
	f.flush()
	if got := f.sender.log(); got != told {
		t.Errorf("a quarter of an hour on: %q, want nothing more", got)
	}
	f.counts(map[string]float64{
		"kubeat_notifications_total{channel_type=webhook}{event=up}{result=failed}": 2,
		"kubeat_notifications_total{channel_type=webhook}{event=up}{result=sent}":   2,
	})
}

// A channel that does not take a message is not sent, in the same pass,
// the other messages waiting to be sent to it again: a channel that is
// down costs a pass one attempt rather than one per message waiting on
// it. The messages held back wait again, their wait doubled, with no
// attempt on the record.
func TestAChannelNotTakingOneIsSentNoOtherAgainInThatPass(t *testing.T) {
	f := newFixture(t, 1, 1) // "ops", a webhook: every notification on its own
	f.retrying(15*time.Minute, time.Hour)
	ctx := context.Background()
	cart, err := f.db.CreateMonitor(ctx, store.Monitor{Name: "cart", Type: store.MonitorTCP, Target: "cart:80",
		Interval: 30 * time.Second, Timeout: 5 * time.Second, FailureThreshold: 1, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	// Both monitors go down at once and the webhook takes neither; a
	// minute on it does not take the first message sent again, and the
	// second is not sent.
	f.sender.fail("ops", 3)
	f.clock = f.clock.Add(30 * time.Second)
	batch := []store.CheckResult{
		{MonitorID: f.monitor.ID, CheckedAt: f.clock, Error: "refused"},
		{MonitorID: cart.ID, CheckedAt: f.clock, Error: "refused"},
	}
	if err := f.db.InsertCheckResults(ctx, batch); err != nil {
		t.Fatal(err)
	}
	f.engine.Evaluate(ctx, batch)
	f.flush()
	if n := len(f.engine.owed.retries); n != 2 {
		t.Fatalf("%d retries, want both outages to be sent again", n)
	}
	f.clock = f.clock.Add(time.Minute)
	f.flush()
	if got := f.sender.log(); got != "" {
		t.Fatalf("a minute on: %q, want nothing taken", got)
	}
	var attempts []int
	for _, r := range f.engine.owed.retries {
		if !r.due.Equal(f.clock.Add(2 * time.Minute)) {
			t.Errorf("%s: due in %s, want both to wait 2 minutes", r.note.Monitor.Name, r.due.Sub(f.clock))
		}
		attempts = append(attempts, r.attempts)
	}
	if got := fmt.Sprint(attempts); got != "[2 1]" {
		t.Errorf("failed attempts counted = %s, want [2 1]: one message sent again, the other held back", got)
	}
	// The record has the attempt that was made, not the message held back.
	cartOutage, err := f.db.GetOpenIncident(ctx, cart.ID, store.IncidentDown)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(f.deliveries(cartOutage.ID)); n != 1 {
		t.Errorf("cart's delivery log has %d entries, want the first attempt alone", n)
	}
	f.counts(map[string]float64{
		"kubeat_notification_attempts_total{channel_type=webhook}{result=failed}": 3,
		"kubeat_notification_attempts_total{channel_type=webhook}{result=sent}":   0,
	})

	// Once the webhook answers, both go.
	f.clock = f.clock.Add(2 * time.Minute)
	f.flush()
	if got := f.sender.log(); got != "ops:down,ops:down" {
		t.Errorf("three minutes on: %q, want both taken", got)
	}
	if n := len(f.engine.owed.retries); n != 0 {
		t.Errorf("%d retries after the webhook took both, want none", n)
	}
	f.counts(map[string]float64{
		"kubeat_notification_attempts_total{channel_type=webhook}{result=sent}":     2,
		"kubeat_notifications_total{channel_type=webhook}{event=down}{result=sent}": 2,
	})
}

// A grouped message a channel does not take is sent again as the one
// message it was; a monitor whose outage ends meanwhile leaves it.
func TestAGroupNotTakenIsSentAgainAsOneMessage(t *testing.T) {
	f := newFixture(t, 1, 1)
	f.retrying(15*time.Minute, time.Hour)
	f.engine.opts.GroupWait = 50 * time.Millisecond
	f.addChannel(store.Channel{Name: "chat", Type: store.ChannelSlack, Target: "http://unused"})
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
	results := func(up bool, monitors ...store.Monitor) {
		f.clock = f.clock.Add(30 * time.Second)
		var batch []store.CheckResult
		for _, m := range monitors {
			r := store.CheckResult{MonitorID: m.ID, CheckedAt: f.clock, Up: up}
			if !up {
				r.Error = "refused"
			}
			batch = append(batch, r)
		}
		if err := f.db.InsertCheckResults(ctx, batch); err != nil {
			t.Fatal(err)
		}
		f.engine.Evaluate(ctx, batch)
	}

	f.sender.fail("chat", 1)
	results(false, monitors...)
	f.flush()
	if got := f.sender.log(); got != "ops:down,ops:down" {
		t.Fatalf("while chat does not answer: %q, want the webhook told of each", got)
	}
	f.clock = f.clock.Add(time.Minute)
	f.flush()
	if got := f.sender.log(); got != "ops:down,ops:down,chat:down[cart orders]" {
		t.Fatalf("a minute on: %q, want chat sent the group again as one message", got)
	}
	for _, m := range monitors {
		incident, err := f.db.GetOpenIncident(ctx, m.ID, store.IncidentDown)
		if err != nil {
			t.Fatal(err)
		}
		var logged []string
		for _, d := range f.deliveries(incident.ID) {
			logged = append(logged, d.ChannelName+":"+strconv.FormatBool(d.Delivered)+":"+strconv.Itoa(d.GroupSize))
		}
		if got := strings.Join(logged, ","); got != "ops:true:1,chat:false:2,chat:true:2" {
			t.Errorf("%s: delivery log = %q, want both attempts of the group on it", m.Name, got)
		}
	}
	f.counts(map[string]float64{
		"kubeat_notification_attempts_total{channel_type=slack}{result=failed}":   1,
		"kubeat_notification_attempts_total{channel_type=slack}{result=sent}":     1,
		"kubeat_notifications_total{channel_type=slack}{event=down}{result=sent}": 1,
	})

	// Both recover, both fail again, and orders recovers before chat is
	// sent the group again: what chat gets is cart's outage alone.
	results(true, monitors...)
	f.flush()
	f.sender.reset()
	f.sender.fail("chat", 1)
	results(false, monitors...)
	f.flush()
	results(true, monitors[1])
	f.flush()
	if got := f.sender.log(); got != "ops:down,ops:down,ops:up,chat:up" {
		t.Fatalf("after orders recovered: %q", got)
	}
	f.clock = f.clock.Add(time.Minute)
	f.flush()
	if got := f.sender.log(); got != "ops:down,ops:down,ops:up,chat:up,chat:down" {
		t.Fatalf("a minute on: %q, want chat sent cart's outage alone", got)
	}
	f.sender.mu.Lock()
	last := f.sender.sent[len(f.sender.sent)-1]
	f.sender.mu.Unlock()
	if last.Monitor.Name != "cart" || len(last.Group) != 0 {
		t.Errorf("the message sent again = %s with a group of %d, want cart's own", last.Monitor.Name, len(last.Group))
	}
}

// After a restart the delivery log says which channels were sent an open
// outage and took none of the attempts: they are sent it again at the
// start, whether they have a delay or not; a channel that took it is not.
func TestARestartSendsAnOutageNotTakenAgain(t *testing.T) {
	f := newFixture(t, 1, 1) // "ops", a webhook without a delay
	f.retrying(15*time.Minute, time.Hour)
	f.addChannel(store.Channel{Name: "pager", Type: store.ChannelPagerDuty, Secret: "key", Delay: time.Minute})
	f.sender.fail("ops", 2)
	f.sender.fail("pager", 1)
	f.check(false) // 0:30: ops does not take it
	f.flush()
	f.check(false) // 1:00
	f.flush()
	f.check(false) // 1:30: the pager's delay is up and it does not take it; ops does not take it again
	f.flush()
	if got := f.sender.log(); got != "" {
		t.Fatalf("before the restart: %q, want nothing taken", got)
	}
	var logged []string
	for _, d := range f.deliveries(f.incidents()[0].ID) {
		logged = append(logged, d.ChannelName+":"+strconv.FormatBool(d.Delivered))
	}
	sort.Strings(logged) // the two attempts at 1:30 land in either order
	if got := strings.Join(logged, ","); got != "ops:false,ops:false,pager:false" {
		t.Fatalf("delivery log before the restart = %q", got)
	}

	// The process is gone for a minute; at the start both are sent the
	// outage again, and take it.
	f.clock = f.clock.Add(time.Minute)
	f.restart()
	f.flush()
	if got := f.sender.log(); got != "ops:down,pager:down" {
		t.Fatalf("after the restart: %q, want ops:down,pager:down", got)
	}
	// Another restart sends nothing again: the log has them taken.
	f.restart()
	f.flush()
	if got := f.sender.log(); got != "ops:down,pager:down" {
		t.Errorf("after a second restart: %q, want nothing sent again", got)
	}
	f.check(true)
	f.flush()
	if got := f.sender.log(); got != "ops:down,pager:down,ops:up,pager:up" {
		t.Errorf("after the recovery: %q", got)
	}
}

// Only the notifications of an outage and of its end are sent again: a
// test goes once, a reminder goes once and the next comes at the engine's
// interval, and with RetryMax zero nothing is sent again.
func TestOnlyOutageNotificationsAreSentAgain(t *testing.T) {
	f := newFixture(t, 1, 1)
	f.retrying(15*time.Minute, time.Hour)
	f.engine.opts.ReminderInterval = time.Minute

	f.sender.fail("ops", 1)
	f.engine.enqueue(Notification{Event: EventTest})
	f.flush()
	if n := len(f.engine.owed.retries); n != 0 {
		t.Errorf("%d retries after a failed test, want none", n)
	}

	f.check(false)
	f.flush()
	if got := f.sender.log(); got != "ops:down" {
		t.Fatalf("at the start of the outage: %q, want ops:down", got)
	}
	f.sender.fail("ops", 1)
	f.outage(1) // the reminder at a minute is not taken
	if n := len(f.engine.owed.retries); n != 0 {
		t.Errorf("%d retries after a failed reminder, want none", n)
	}
	f.outage(1) // the next, at the engine's interval, is
	if got := f.sender.log(); got != "ops:down,ops:reminder" {
		t.Errorf("two minutes of reminders: %q, want one taken, none sent again", got)
	}
	f.check(true)
	f.flush()

	f.engine.opts.RetryMax = 0
	f.sender.fail("ops", 1)
	f.check(false)
	f.flush()
	if n := len(f.engine.owed.retries); n != 0 {
		t.Errorf("%d retries with the attempts off, want none", n)
	}
	f.counts(map[string]float64{
		"kubeat_notifications_total{channel_type=webhook}{event=test}{result=failed}":     1,
		"kubeat_notifications_total{channel_type=webhook}{event=reminder}{result=failed}": 1,
		"kubeat_notifications_total{channel_type=webhook}{event=down}{result=failed}":     1,
	})
}
