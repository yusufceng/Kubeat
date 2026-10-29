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
// the notification of the outage counts as failed. The webhook, which did
// not take the outage, is sent nothing until its retry is due: the end
// goes then.
func TestSendingAgainStopsWhenTheOutageEnds(t *testing.T) {
	f := newFixture(t, 1, 1)
	f.retrying(15*time.Minute, time.Hour)
	f.sender.fail("ops", 1)
	f.check(false)
	f.flush()
	f.check(true) // 30 seconds later, before the attempt again
	f.flush()
	if got := f.sender.log(); got != "" {
		t.Fatalf("after the recovery: %q, want nothing before the webhook's retry", got)
	}
	if n := len(f.engine.owed.retries); n != 1 {
		t.Fatalf("%d retries after the recovery, want the end alone", n)
	}
	if r := f.engine.owed.retries[0]; r.note.Event != EventUp || !r.due.Equal(f.clock.Add(30*time.Second)) {
		t.Fatalf("the retry after the recovery = %s due in %s, want up due with the outage's retry", r.note.Event, r.due.Sub(f.clock))
	}
	f.check(true) // a minute after the failure
	f.flush()
	if got := f.sender.log(); got != "ops:up" {
		t.Fatalf("a minute after the failure: %q, want ops:up alone", got)
	}
	if n := len(f.engine.owed.retries); n != 0 {
		t.Errorf("%d retries after the end was taken, want none", n)
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
// of a later outage was told: the earlier end is moot by then. The
// webhook, which did not take the end, hears of the newer outage when its
// retry is due.
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
	f.check(false) // 30 seconds on: down again, held back for the webhook's retry
	f.flush()
	if got := f.sender.log(); got != "ops:down" {
		t.Fatalf("at the second outage: %q, want it held back", got)
	}
	// A minute after the failure the webhook's retry is due: the end of
	// the first outage is not sent, since the second is open, and the
	// second outage is, and taken.
	f.check(false)
	f.flush()
	if got := f.sender.log(); got != "ops:down,ops:down" {
		t.Fatalf("a minute after the failure: %q, want the second outage and not the end of the first", got)
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
	// before it is due again: the fourth outage, held back for the
	// webhook's retry, is over before it; once the end of the fourth is
	// told, the end of the third is not sent.
	f.check(false)
	f.flush()
	f.sender.fail("ops", 1)
	f.check(true)
	f.flush()
	f.check(false)
	f.flush()
	f.check(true) // a minute after the failure: the fourth's end goes as the third's falls due
	f.flush()
	const told = "ops:down,ops:down,ops:up,ops:down,ops:up"
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
		"kubeat_notifications_total{channel_type=webhook}{event=up}{result=failed}":   2,
		"kubeat_notifications_total{channel_type=webhook}{event=up}{result=sent}":     2,
		"kubeat_notifications_total{channel_type=webhook}{event=down}{result=failed}": 1,
	})
}

// A channel people read that does not take a message is not sent, in the
// same pass, the other messages waiting to be sent to it again: a channel
// that is down costs a pass one attempt rather than one per message
// waiting on it. The messages held back wait again, their wait doubled,
// with no attempt on the record. (A channel software reads is sent
// nothing at all until its retry: TestAWebhookThatTookNoneIsSentNothingUntilItsRetry.)
func TestAChannelNotTakingOneIsSentNoOtherAgainInThatPass(t *testing.T) {
	f := newFixture(t, 1, 1) // "ops", a webhook, takes everything
	f.retrying(15*time.Minute, time.Hour)
	// With GroupWait off, chat gets every notification on its own.
	f.addChannel(store.Channel{Name: "chat", Type: store.ChannelSlack, Target: "http://unused"})
	ctx := context.Background()
	cart, err := f.db.CreateMonitor(ctx, store.Monitor{Name: "cart", Type: store.MonitorTCP, Target: "cart:80",
		Interval: 30 * time.Second, Timeout: 5 * time.Second, FailureThreshold: 1, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	// Both monitors go down at once and chat takes neither; a minute on it
	// does not take the first message sent again, and the second is not
	// sent.
	f.sender.fail("chat", 3)
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
	if got := f.sender.log(); got != "ops:down,ops:down" {
		t.Fatalf("a minute on: %q, want nothing taken by chat", got)
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
	var chatEntries int
	for _, d := range f.deliveries(cartOutage.ID) {
		if d.ChannelName == "chat" {
			chatEntries++
		}
	}
	if chatEntries != 1 {
		t.Errorf("cart's delivery log has %d entries for chat, want the first attempt alone", chatEntries)
	}
	f.counts(map[string]float64{
		"kubeat_notification_attempts_total{channel_type=slack}{result=failed}": 3,
		"kubeat_notification_attempts_total{channel_type=slack}{result=sent}":   0,
	})

	// Once chat answers, both go.
	f.clock = f.clock.Add(2 * time.Minute)
	f.flush()
	if got := f.sender.log(); got != "ops:down,ops:down,chat:down,chat:down" {
		t.Errorf("three minutes on: %q, want both taken", got)
	}
	if n := len(f.engine.owed.retries); n != 0 {
		t.Errorf("%d retries after chat took both, want none", n)
	}
	f.counts(map[string]float64{
		"kubeat_notification_attempts_total{channel_type=slack}{result=sent}":     2,
		"kubeat_notifications_total{channel_type=slack}{event=down}{result=sent}": 2,
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

// monitorIn creates a monitor of the namespace that one failed check
// takes down.
func (f *fixture) monitorIn(name, namespace string) store.Monitor {
	f.t.Helper()
	m, err := f.db.CreateMonitor(context.Background(), store.Monitor{Name: name, Type: store.MonitorTCP, Target: name + ":80", Namespace: namespace,
		Interval: 30 * time.Second, Timeout: 5 * time.Second, FailureThreshold: 1, Enabled: true})
	if err != nil {
		f.t.Fatal(err)
	}
	return m
}

// result stores a result of the monitor at the fixture's clock and
// evaluates it, as the scheduler does.
func (f *fixture) result(m store.Monitor, up bool) {
	f.t.Helper()
	r := store.CheckResult{MonitorID: m.ID, CheckedAt: f.clock, Up: up, Latency: time.Millisecond}
	if !up {
		r.Error = "refused"
	}
	if err := f.db.InsertCheckResults(context.Background(), []store.CheckResult{r}); err != nil {
		f.t.Fatal(err)
	}
	f.engine.Evaluate(context.Background(), []store.CheckResult{r})
}

// sentTo lists what the channel was sent so far, as "event:monitor".
func (f *fixture) sentTo(channel string) string {
	f.sender.mu.Lock()
	defer f.sender.mu.Unlock()
	var items []string
	for i, n := range f.sender.sent {
		if f.sender.channels[i] == channel {
			items = append(items, string(n.Event)+":"+n.Monitor.Name)
		}
	}
	return strings.Join(items, ",")
}

// A channel software reads that does not take a notification is sent
// nothing until its retry is due: what comes up meanwhile is held back,
// neither attempted nor on the record nor counted, and goes in that
// retry's pass with the rest, a certificate warning, which is not sent
// again otherwise, too; what the channel never refused goes first. Once
// the channel takes one, it is sent everything at once again.
func TestAWebhookThatTookNoneIsSentNothingUntilItsRetry(t *testing.T) {
	f := newFixture(t, 1, 1) // "ops", a webhook; the fixture's "api" has a certificate
	f.retrying(15*time.Minute, time.Hour)
	cart, orders := f.monitorIn("cart", ""), f.monitorIn("orders", "")
	f.sender.fail("ops", 1)
	f.clock = f.clock.Add(30 * time.Second)
	f.result(cart, false) // 0:30: not taken; the webhook's retry is at 1:30
	f.flush()

	f.checkCert(10 * 24 * time.Hour) // 1:00
	f.result(orders, false)
	f.flush()
	if got := f.sender.log(); got != "" {
		t.Fatalf("while the webhook waits for its retry: %q, want nothing sent", got)
	}
	var waiting []string
	for _, r := range f.engine.owed.retries {
		waiting = append(waiting, fmt.Sprintf("%s:%s:%d:%v", r.note.Monitor.Name, r.note.Event, r.attempts, r.once))
		if !r.due.Equal(f.clock.Add(30 * time.Second)) {
			t.Errorf("%s %s: due in %s, want with the webhook's retry", r.note.Monitor.Name, r.note.Event, r.due.Sub(f.clock))
		}
	}
	if got := strings.Join(waiting, ","); got != "cart:down:1:false,api:cert_expiring:0:true,orders:down:0:false" {
		t.Errorf("waiting for the webhook (monitor:event:failed attempts:once) = %s", got)
	}
	ordersOutage, err := f.db.GetOpenIncident(context.Background(), orders.ID, store.IncidentDown)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(f.deliveries(ordersOutage.ID)); n != 0 {
		t.Errorf("orders' delivery log has %d entries, want none: nothing was attempted", n)
	}
	f.counts(map[string]float64{
		"kubeat_notification_attempts_total{channel_type=webhook}{result=failed}": 1,
		"kubeat_notification_attempts_total{channel_type=webhook}{result=sent}":   0,
	})

	f.clock = f.clock.Add(30 * time.Second) // 1:30: the retry is due
	f.flush()
	if got := f.sentTo("ops"); got != "cert_expiring:api,down:orders,down:cart" {
		t.Fatalf("at the webhook's retry: %q, want all three, the two it never refused first, in the order they came up", got)
	}
	f.clock = f.clock.Add(30 * time.Second) // 2:00: it took them, and hears at once again
	f.result(cart, true)
	f.flush()
	if got := f.sentTo("ops"); got != "cert_expiring:api,down:orders,down:cart,up:cart" {
		t.Errorf("after the webhook took them: %q, want the end at once", got)
	}
	if n := len(f.engine.owed.retries); n != 0 {
		t.Errorf("%d retries left, want none", n)
	}
	f.counts(map[string]float64{
		"kubeat_notification_attempts_total{channel_type=webhook}{result=failed}":            1,
		"kubeat_notification_attempts_total{channel_type=webhook}{result=sent}":              4,
		"kubeat_notifications_total{channel_type=webhook}{event=down}{result=sent}":          2,
		"kubeat_notifications_total{channel_type=webhook}{event=cert_expiring}{result=sent}": 1,
	})
}

// A pass the webhook takes nothing in holds back the rest of it: the
// first it tries, the certificate warning it never refused, is not taken
// and, not sent again otherwise, given up; what waits for the webhook to
// be sent again waits for its next retry, the wait doubled. The two
// failures are the only attempts.
func TestWhatAWebhookTakesNoneOfInItsPassWaitsAgain(t *testing.T) {
	f := newFixture(t, 1, 1)
	f.retrying(15*time.Minute, time.Hour)
	cart, orders := f.monitorIn("cart", ""), f.monitorIn("orders", "")
	f.sender.fail("ops", 2)
	f.clock = f.clock.Add(30 * time.Second)
	f.result(cart, false) // 0:30: not taken
	f.flush()
	f.checkCert(10 * 24 * time.Hour) // 1:00: held back, with orders' outage
	f.result(orders, false)
	f.flush()
	f.clock = f.clock.Add(30 * time.Second) // 1:30: the warning is not taken
	f.flush()
	if got := f.sender.log(); got != "" {
		t.Fatalf("at the webhook's retry: %q, want nothing taken", got)
	}
	var waiting []string
	for _, r := range f.engine.owed.retries {
		waiting = append(waiting, fmt.Sprintf("%s:%d", r.note.Monitor.Name, r.attempts))
		if !r.due.Equal(f.clock.Add(2 * time.Minute)) {
			t.Errorf("%s: due in %s, want in 2 minutes", r.note.Monitor.Name, r.due.Sub(f.clock))
		}
	}
	if got := strings.Join(waiting, ","); got != "cart:1,orders:0" {
		t.Errorf("waiting for the webhook (monitor:failed attempts) = %s, want the two outages and the warning given up", got)
	}
	f.counts(map[string]float64{
		"kubeat_notification_attempts_total{channel_type=webhook}{result=failed}":              2,
		"kubeat_notifications_total{channel_type=webhook}{event=cert_expiring}{result=failed}": 1,
	})
	f.clock = f.clock.Add(2 * time.Minute) // 3:30
	f.flush()
	if got := f.sentTo("ops"); got != "down:orders,down:cart" {
		t.Errorf("at the next retry: %q, want both outages", got)
	}
}

// A reminder held back for a webhook that took none gives way to a newer
// one: the webhook's retry brings the outage and its latest reminder.
func TestAHeldBackReminderGivesWayToANewerOne(t *testing.T) {
	f := newFixture(t, 1, 1)
	f.retrying(15*time.Minute, time.Hour)
	f.engine.opts.ReminderInterval = 20 * time.Second
	f.sender.fail("ops", 1)
	opened := f.check(false).CheckedAt // not taken; the webhook's retry is a minute on
	f.flush()
	for _, after := range []time.Duration{20 * time.Second, 40 * time.Second} { // a reminder each
		f.clock = opened.Add(after)
		f.result(f.monitor, false)
		f.flush()
	}
	f.clock = opened.Add(time.Minute)
	f.flush()
	if got := f.sender.log(); got != "ops:down,ops:reminder" {
		t.Fatalf("at the webhook's retry: %q, want the outage and one reminder", got)
	}
	f.sender.mu.Lock()
	reminder := f.sender.sent[1]
	f.sender.mu.Unlock()
	if !reminder.At.Equal(opened.Add(40 * time.Second)) {
		t.Errorf("the reminder sent is of %s, want the newer one", reminder.At.Sub(opened))
	}
}

// failingOn is a Sender to whose channel every notification about one
// monitor fails as one that does not reach a receiver able to take it, as
// from a receiver answering 500 to whatever it cannot handle about that
// monitor; the channel takes the rest. (A refusal, 4xx, pauses nothing:
// TestAWebhookRefusingOneMonitorTakesTheOthersAtOnce.)
type failingOn struct {
	*recorder
	channel, monitor string
}

func (r *failingOn) Send(ctx context.Context, ch store.Channel, n Notification) error {
	if ch.Name == r.channel && n.Monitor.Name == r.monitor {
		return &statusError{provider: "webhook", status: 500}
	}
	return r.recorder.Send(ctx, ch, n)
}

// passes runs the loop at each time something falls due, up to the
// fixture's start plus until, and leaves the clock there. Each pass may
// see one failed attempt at most: a webhook that does not take what it
// is sent costs the loop one attempt a pass.
func (f *fixture) passes(start time.Time, until time.Duration) {
	f.t.Helper()
	failed := func() float64 {
		return f.metric("kubeat_notification_attempts_total{channel_type=webhook}{result=failed}")
	}
	for next := f.engine.owed.next(); !next.IsZero() && !next.After(start.Add(until)); next = f.engine.owed.next() {
		f.clock = next
		before := failed()
		f.flush()
		if n := failed() - before; n > 1 {
			f.t.Fatalf("at %s: %v failed attempts in one pass, want one at most", f.clock.Sub(start), n)
		}
	}
	f.clock = start.Add(until)
}

// A webhook that fails on every notification about one monitor (500), its
// outage and the end of it, still takes the others: in each of its passes
// what it refused fewer times goes first, and the failure ends the pass
// and pauses the webhook. The others wait for the webhook's next pass,
// RetryMax away at most; one behind a notification failed on for the
// first time waits a pass more.
func TestAWebhookFailingOnOneMonitorTakesTheOthers(t *testing.T) {
	f := newFixture(t, 1, 1) // "ops", a webhook
	f.retrying(15*time.Minute, time.Hour)
	f.engine.sender = &failingOn{recorder: f.sender, channel: "ops", monitor: "bad"}
	bad, good, late := f.monitorIn("bad", ""), f.monitorIn("good", ""), f.monitorIn("late", "")
	start := f.clock

	f.clock = start.Add(30 * time.Second)
	f.result(bad, false) // 0:30: failed on; the webhook waits for its retry at 1:30
	f.flush()
	f.clock = start.Add(time.Minute)
	f.result(good, false) // 1:00: held back for that retry
	f.flush()
	f.passes(start, 90*time.Second)
	if got := f.sentTo("ops"); got != "down:good" {
		t.Fatalf("at the webhook's retry: %q, want good's outage, sent ahead of bad's, failed on again", got)
	}

	// bad's outage goes on, failed on at 3:30, 7:30, 15:30 and 30:30; late's
	// goes in the webhook's pass at 30:30, 10.5 minutes after it opened.
	f.passes(start, 20*time.Minute)
	f.result(late, false)
	f.flush()
	f.passes(start, 30*time.Minute)
	if got := f.sentTo("ops"); got != "down:good" {
		t.Fatalf("at 30:00: %q, want late's outage held back for the webhook's pass", got)
	}
	f.passes(start, 30*time.Minute+30*time.Second)
	if got := f.sentTo("ops"); got != "down:good,down:late" {
		t.Fatalf("at 30:30: %q, want late's outage taken", got)
	}

	// The two outages end. The end of bad's, held back for the webhook's
	// pass at 45:30, comes ahead of good's there and is failed on for the
	// first time: good's waits for the next pass, at 1:00:30.
	f.clock = start.Add(31 * time.Minute)
	f.result(bad, true)
	f.flush()
	f.clock = start.Add(32 * time.Minute)
	f.result(good, true)
	f.flush()
	f.passes(start, time.Hour)
	if got := f.sentTo("ops"); got != "down:good,down:late" {
		t.Fatalf("at 1:00:00: %q, want good's end held back behind bad's", got)
	}
	f.passes(start, time.Hour+30*time.Second)
	if got := f.sentTo("ops"); got != "down:good,down:late,up:good" {
		t.Fatalf("at 1:00:30: %q, want good's end taken ahead of bad's", got)
	}

	// The end of bad's is given up UPTIME_NOTIFY_RETRY_FOR after its first
	// attempt, at 1:45:30; then the webhook hears at once again.
	f.passes(start, time.Hour+45*time.Minute)
	if n := len(f.engine.owed.retries); n != 1 {
		t.Errorf("%d retries at 1:45:00, want the end of bad's outage still sent again", n)
	}
	f.passes(start, 2*time.Hour)
	if n := len(f.engine.owed.retries); n != 0 {
		t.Errorf("%d retries at 2:00:00, want the end of bad's outage given up", n)
	}
	f.result(late, true)
	f.flush()
	if got := f.sentTo("ops"); got != "down:good,down:late,up:good,up:late" {
		t.Errorf("at 2:00:00: %q, want late's end at once", got)
	}
	f.counts(map[string]float64{
		"kubeat_notifications_total{channel_type=webhook}{event=down}{result=sent}":   2,
		"kubeat_notifications_total{channel_type=webhook}{event=down}{result=failed}": 1,
		"kubeat_notifications_total{channel_type=webhook}{event=up}{result=sent}":     2,
		"kubeat_notifications_total{channel_type=webhook}{event=up}{result=failed}":   1,
	})
}

// A webhook that takes nothing is tried once a pass, however many
// notifications wait for it, each time with one it refused least; back,
// it takes them all in its next pass.
func TestADeadWebhookIsTriedOnceAPass(t *testing.T) {
	f := newFixture(t, 1, 1) // "ops", a webhook
	f.retrying(15*time.Minute, time.Hour)
	f.sender.fail("ops", 1000)
	start := f.clock
	f.clock = start.Add(30 * time.Second)
	const outages = 20 // twice the fixture's queue: a pass each
	for i := range outages {
		f.result(f.monitorIn(fmt.Sprintf("m%02d", i), ""), false)
		f.flush()
	}
	f.passes(start, 6*time.Hour)
	tried := 0
	for _, r := range f.engine.owed.retries {
		if r.attempts > 0 {
			tried++
		}
	}
	if n := len(f.engine.owed.retries); n != outages || tried != outages {
		t.Errorf("%d retries, %d of them tried, want all %d tried in turn", n, tried, outages)
	}
	// 0:30, 1:30, 3:30, 7:30, then every 15 minutes from 15:30.
	if got, want := f.metric("kubeat_notification_attempts_total{channel_type=webhook}{result=failed}"), float64(4+23); got != want {
		t.Errorf("%v attempts in six hours, want one a pass, %v", got, want)
	}

	f.sender.fail("ops", 0)
	f.clock = f.engine.owed.next()
	f.flush()
	if got := strings.Count(f.sentTo("ops"), "down:"); got != outages {
		t.Errorf("the webhook back took %d outages in its next pass, want all %d", got, outages)
	}
	if n := len(f.engine.owed.retries); n != 0 {
		t.Errorf("%d retries left, want none", n)
	}
}

// The end of an outage held back for a webhook that waits longer than
// UPTIME_NOTIFY_RETRY_FOR goes in the webhook's next pass all the same:
// RetryFor counts from that pass, its first attempt, not from when it
// was held back.
func TestAHeldBackEndGoesInTheWebhooksNextPass(t *testing.T) {
	f := newFixture(t, 1, 1) // "ops", a webhook; the fixture's "api"
	f.retrying(15*time.Minute, 2*time.Minute)
	cart := f.monitorIn("cart", "")
	start := f.clock
	f.sender.fail("ops", 4)
	f.clock = start.Add(30 * time.Second)
	f.result(cart, false) // not taken at 0:30, 1:30, 3:30 and 7:30
	f.flush()
	f.passes(start, 7*time.Minute+30*time.Second)
	f.check(false) // 8:00: api's outage, held back for the webhook's pass at 15:30
	f.flush()
	f.check(true) // 8:30: its end, held back too, 7 minutes before that pass
	f.flush()
	var waiting []string
	for _, r := range f.engine.owed.retries {
		waiting = append(waiting, fmt.Sprintf("%s:%s:%s", r.note.Monitor.Name, r.note.Event, r.due.Sub(start)))
	}
	if got := strings.Join(waiting, ","); got != "cart:down:15m30s,api:up:15m30s" {
		t.Fatalf("waiting for the webhook (monitor:event:due) = %s, want cart's outage and api's end", got)
	}
	f.passes(start, 15*time.Minute+30*time.Second)
	if got := f.sentTo("ops"); got != "up:api,down:cart" {
		t.Errorf("at the webhook's pass: %q, want api's end and cart's outage", got)
	}
	f.counts(map[string]float64{
		"kubeat_notifications_total{channel_type=webhook}{event=up}{result=sent}":   1,
		"kubeat_notifications_total{channel_type=webhook}{event=up}{result=failed}": 0,
	})
}

// A reminder goes after its outage: a webhook with an interval of its own
// that did not take the outage hears of it first when its reminder falls
// due with the outage's retry.
func TestAReminderFollowsItsOutageToAWebhook(t *testing.T) {
	f := newFixture(t, 1, 1) // "ops", a webhook
	f.retrying(15*time.Minute, time.Hour)
	f.addChannel(store.Channel{Name: "pager", Type: store.ChannelWebhook, Target: "http://unused", Repeat: time.Minute})
	start := f.clock
	f.sender.fail("pager", 1)
	f.check(false) // 0:30: not taken by pager; its retry and its reminder fall due at 1:30
	f.flush()
	f.passes(start, 90*time.Second)
	if got := f.sentTo("pager"); got != "down:api,reminder:api" {
		t.Errorf("at 1:30: %q, want the outage, then its reminder", got)
	}
}

// What is held back for a webhook to go once, as it is not sent again
// otherwise (a certificate warning; the end of an outage with
// UPTIME_NOTIFY_RETRY_FOR=0), is not given up untried when a notification
// ahead of it is failed on for the first time and ends the webhook's
// pass: it goes in the next pass, ahead of what the webhook failed on.
func TestWhatGoesOnceIsNotGivenUpUntried(t *testing.T) {
	f := newFixture(t, 1, 1) // "ops", a webhook; the fixture's "api" has a certificate
	f.retrying(15*time.Minute, 0)
	f.engine.sender = &failingOn{recorder: f.sender, channel: "ops", monitor: "bad"}
	x, bad, good := f.monitorIn("x", ""), f.monitorIn("bad", ""), f.monitorIn("good", "")
	start := f.clock
	f.clock = start.Add(10 * time.Second)
	f.result(good, false) // 0:10: taken
	f.flush()
	f.sender.fail("ops", 1) // a failure the webhook gets over
	f.clock = start.Add(30 * time.Second)
	f.result(x, false) // 0:30: not taken; the webhook's retry is at 1:30
	f.flush()
	f.clock = start.Add(45 * time.Second)
	f.result(bad, false) // 0:45: held back for that retry
	f.flush()
	f.clock = start.Add(time.Minute)
	f.result(good, true) // 1:00: held back, to go once
	f.flush()
	f.clock = start.Add(45 * time.Second) // checkCert checks 30 seconds on
	f.checkCert(10 * 24 * time.Hour)      // 1:15: held back, to go once
	f.flush()

	// 1:30: bad's outage, which the webhook never failed on before, goes
	// first and is failed on; the rest of the pass is held back.
	f.passes(start, 90*time.Second)
	if got := f.sentTo("ops"); got != "down:good" {
		t.Fatalf("at 1:30: %q, want nothing more taken", got)
	}
	var waiting []string
	for _, r := range f.engine.owed.retries {
		waiting = append(waiting, fmt.Sprintf("%s:%s:%d:%v", r.note.Monitor.Name, r.note.Event, r.attempts, r.once))
		if !r.due.Equal(start.Add(3*time.Minute + 30*time.Second)) {
			t.Errorf("%s %s: due at %s, want with the webhook's next pass at 3m30s", r.note.Monitor.Name, r.note.Event, r.due.Sub(start))
		}
	}
	if got := strings.Join(waiting, ","); got != "x:down:1:false,bad:down:1:false,good:up:0:true,api:cert_expiring:0:true" {
		t.Errorf("waiting for the webhook (monitor:event:failed attempts:once) = %s, want the two that go once still there", got)
	}

	// 3:30: they go first, and are taken; bad's outage is failed on again.
	f.passes(start, 3*time.Minute+30*time.Second)
	if got := f.sentTo("ops"); got != "down:good,up:good,cert_expiring:api,down:x" {
		t.Errorf("at 3:30: %q, want good's end and the warning, then x's outage", got)
	}
	f.counts(map[string]float64{
		"kubeat_notifications_total{channel_type=webhook}{event=up}{result=sent}":              1,
		"kubeat_notifications_total{channel_type=webhook}{event=up}{result=failed}":            0,
		"kubeat_notifications_total{channel_type=webhook}{event=cert_expiring}{result=sent}":   1,
		"kubeat_notifications_total{channel_type=webhook}{event=cert_expiring}{result=failed}": 0,
	})
}

// UPTIME_NOTIFY_RETRY_FOR, shorter here than UPTIME_NOTIFY_RETRY_MAX, counts
// from the first attempt of the end of an outage held back for a webhook,
// however long the end waits for that attempt: when its pass is put off to
// a later retry of the webhook's, and when a failure ahead of it in its
// pass holds it back again.
func TestAHeldBackEndCountsRetryForFromItsFirstAttempt(t *testing.T) {
	t.Run("its pass put off", func(t *testing.T) {
		f := newFixture(t, 1, 1) // "ops", a webhook
		f.retrying(15*time.Minute, 5*time.Minute)
		a, b, d := f.monitorIn("a", ""), f.monitorIn("b", ""), f.monitorIn("d", "")
		start := f.clock
		f.clock = start.Add(10 * time.Second)
		f.result(d, false) // 0:10: taken
		f.flush()
		f.sender.fail("ops", 1000) // the webhook goes away
		f.clock = start.Add(30 * time.Second)
		f.result(a, false) // 0:30: not taken; due again at 1:30, 3:30, 7:30, 15:30, 30:30 and 45:30
		f.flush()
		f.passes(start, 45*time.Minute)
		f.clock = start.Add(45*time.Minute + 30*time.Second)
		f.result(b, false) // 45:30: tried as a's retry falls due, which then waits for b's
		f.flush()
		// 46:30: b's goes first and is not taken; the webhook's retries
		// drift apart, b's at 48:30, a's at 1:01:30.
		f.passes(start, 46*time.Minute+30*time.Second)
		f.clock = start.Add(47 * time.Minute)
		f.result(d, true) // 47:00: held back for 48:30, which is put off to 1:01:30
		f.flush()
		f.passes(start, time.Hour)
		for _, r := range f.engine.owed.retries {
			if r.note.Event == EventUp && (!r.due.Equal(start.Add(time.Hour+90*time.Second)) || r.attempts != 0) {
				t.Errorf("d's end due at %s with %d attempts, want it untried and due at 1h1m30s", r.due.Sub(start), r.attempts)
			}
		}
		f.sender.fail("ops", 0) // back
		f.passes(start, time.Hour+90*time.Second)
		if got := f.sentTo("ops"); got != "down:d,up:d,down:b,down:a" {
			t.Errorf("at 1:01:30: %q, want d's end, then the two outages", got)
		}
		f.counts(map[string]float64{
			"kubeat_notifications_total{channel_type=webhook}{event=up}{result=sent}":   1,
			"kubeat_notifications_total{channel_type=webhook}{event=up}{result=failed}": 0,
		})
	})
	t.Run("held back again in its pass", func(t *testing.T) {
		f := newFixture(t, 1, 1) // "ops", a webhook
		f.retrying(15*time.Minute, 5*time.Minute)
		f.engine.sender = &failingOn{recorder: f.sender, channel: "ops", monitor: "bad"}
		bad, d := f.monitorIn("bad", ""), f.monitorIn("d", "")
		start := f.clock
		f.clock = start.Add(10 * time.Second)
		f.result(d, false) // 0:10: taken
		f.flush()
		f.clock = start.Add(30 * time.Second)
		f.result(bad, false) // 0:30: failed on, and at 1:30, 3:30, 7:30, 15:30 and 30:30; the next pass is at 45:30
		f.flush()
		f.passes(start, 31*time.Minute)
		f.clock = start.Add(32 * time.Minute)
		f.result(bad, true) // 32:00: held back for 45:30
		f.flush()
		f.clock = start.Add(34 * time.Minute)
		f.result(d, true) // 34:00: held back for 45:30, behind bad's end
		f.flush()
		// 45:30: bad's end goes first and is failed on; d's is held back
		// again, to 1:00:30, and is not given up untried.
		f.passes(start, 45*time.Minute+30*time.Second)
		if n := len(f.engine.owed.retries); n != 1 {
			t.Fatalf("%d retries at 45:30, want d's end alone, bad's given up after its attempt", n)
		}
		if r := f.engine.owed.retries[0]; r.note.Monitor.Name != "d" || r.attempts != 0 || !r.due.Equal(start.Add(time.Hour+30*time.Second)) {
			t.Errorf("the retry at 45:30 = %s with %d attempts, due at %s; want d's end untried, due at 1h0m30s", r.note.Monitor.Name, r.attempts, r.due.Sub(start))
		}
		f.passes(start, time.Hour+30*time.Second)
		if got := f.sentTo("ops"); got != "down:d,up:d" {
			t.Errorf("at 1:00:30: %q, want d's end taken", got)
		}
		f.counts(map[string]float64{
			"kubeat_notifications_total{channel_type=webhook}{event=up}{result=sent}":   1,
			"kubeat_notifications_total{channel_type=webhook}{event=up}{result=failed}": 1,
		})
	})
}

// A slowness notification held back for a webhook that took none gives
// way to a newer one about the same monitor, as a reminder does: a monitor
// turning slow and back for hours while the webhook is down leaves one note
// about it waiting for the webhook, not one a turn, and the webhook back is
// sent the monitor's latest state with the outage that kept it waiting.
func TestAHeldBackSlownessNotificationGivesWayToANewerOne(t *testing.T) {
	f := newFixture(t, 1, 1) // "ops", a webhook
	f.retrying(15*time.Minute, time.Hour)
	x, s := f.monitorIn("x", ""), f.monitorIn("s", "")
	s.SlowAbove = 500 * time.Millisecond
	s, err := f.db.UpdateMonitor(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	start := f.clock
	// took evaluates a result of s that took d, at the fixture's clock,
	// and checks what waits for the webhook after it.
	took := func(d time.Duration) {
		t.Helper()
		r := store.CheckResult{MonitorID: s.ID, CheckedAt: f.clock, Up: true, Latency: d}
		if err := f.db.InsertCheckResults(context.Background(), []store.CheckResult{r}); err != nil {
			t.Fatal(err)
		}
		f.engine.Evaluate(context.Background(), []store.CheckResult{r})
		f.flush()
		if n := len(f.engine.owed.retries); n > 2 {
			t.Fatalf("at %s: %d notes wait for the webhook, want x's outage and s's latest state at most", f.clock.Sub(start), n)
		}
	}
	f.sender.fail("ops", 1_000_000) // the webhook goes away
	f.clock = start.Add(30 * time.Second)
	f.result(x, false) // 0:30: not taken; the webhook is tried once a pass from then on
	f.flush()
	// s turns slow a minute into every ten and back five minutes later,
	// for twelve hours; passes sees one failed attempt a pass at most.
	for at := time.Minute; at < 12*time.Hour; at += 10 * time.Minute {
		f.passes(start, at)
		took(900 * time.Millisecond)
		f.passes(start, at+5*time.Minute)
		took(100 * time.Millisecond)
	}

	// Back, the webhook is sent in its next pass, at 12:00:30, that s has
	// responded in time again since 11:56, then x's outage.
	f.sender.fail("ops", 0)
	f.passes(start, 12*time.Hour+30*time.Second)
	if got := f.sentTo("ops"); got != "slow_resolved:s,down:x" {
		t.Fatalf("the webhook back was sent %q, want s's latest state and x's outage", got)
	}
	f.sender.mu.Lock()
	latest := f.sender.sent[0]
	f.sender.mu.Unlock()
	if want := start.Add(11*time.Hour + 56*time.Minute); !latest.At.Equal(want) {
		t.Errorf("s's state sent is of %s, want the latest, of %s", latest.At.Sub(start), want.Sub(start))
	}
	if n := len(f.engine.owed.retries); n != 0 {
		t.Errorf("%d notes left waiting, want none", n)
	}
}
