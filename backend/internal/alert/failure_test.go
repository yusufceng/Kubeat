package alert

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/metrics"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// answering is a Sender whose receivers answer some notifications with a
// status, as a provider's sender reports it: by "channel/monitor", or by
// "channel" for all of a channel's. It counts the tries by
// "channel/monitor"; what is not answered goes to the recorder.
type answering struct {
	*recorder
	mu      sync.Mutex
	answers map[string]*statusError
	tries   map[string]int
}

func (a *answering) Send(ctx context.Context, ch store.Channel, n Notification) error {
	key := ch.Name + "/" + n.Monitor.Name
	a.mu.Lock()
	a.tries[key]++
	answer := a.answers[key]
	if answer == nil {
		answer = a.answers[ch.Name]
	}
	a.mu.Unlock()
	if answer != nil {
		return answer
	}
	return a.recorder.Send(ctx, ch, n)
}

// set makes the receiver answer status, with a Retry-After header unless
// it is empty, to what key names; status 0 takes it all again.
func (a *answering) set(key string, status int, retryAfter string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if status == 0 {
		delete(a.answers, key)
		return
	}
	a.answers[key] = &statusError{provider: "webhook", status: status, retryAfter: retryAfter}
}

func (a *answering) triesOf(key string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.tries[key]
}

// answer puts an answering sender in front of the fixture's recorder.
func (f *fixture) answer() *answering {
	a := &answering{recorder: f.sender, answers: make(map[string]*statusError), tries: make(map[string]int)}
	f.engine.sender = a
	return a
}

// retryOf returns what waits to be sent again to the channel about the
// monitor; nil when nothing does.
func (f *fixture) retryOf(channel, monitor string) *retry {
	for _, r := range f.engine.owed.retries {
		if r.channel.Name == channel && r.note.Monitor.Name == monitor {
			return r
		}
	}
	return nil
}

// channel reads one channel's row.
func (f *fixture) channel(name string) store.Channel {
	f.t.Helper()
	channels, err := f.db.ListChannels(context.Background())
	if err != nil {
		f.t.Fatal(err)
	}
	for _, ch := range channels {
		if ch.Name == name {
			return ch
		}
	}
	f.t.Fatalf("no channel %q", name)
	return store.Channel{}
}

// deliverLive delivers what is queued as the loop does with GroupWait off,
// with a context that is not done: an attempt makes all its tries. (flush
// runs the loop shutting down, where a new notification gets one try; what
// is sent again, deliverOwed, gets them all either way.)
func (f *fixture) deliverLive() {
	for len(f.engine.queue) > 0 {
		f.engine.deliverTo(context.Background(), <-f.engine.queue, nil)
	}
	f.sender.settle()
}

// disable turns a channel off.
func (f *fixture) disable(name string) {
	f.t.Helper()
	ch := f.channel(name)
	ch.Enabled = false
	if _, err := f.db.UpdateChannel(context.Background(), ch); err != nil {
		f.t.Fatal(err)
	}
}

// What an answer says: busy for 429, and for 503 with a Retry-After in the
// future, with how long to wait; unreachable for 407, 408, the other 5xx
// and what is no answer at all; refused for the other 4xx. A Retry-After
// is seconds or an HTTP date, and a value that is neither, or not in the
// future, is no header; the wait is RetryMax at most.
func TestClassify(t *testing.T) {
	e := New(nil, nil, Options{RetryMax: 15 * time.Minute})
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	date := func(d time.Duration) string { return now.Add(d).Format(http.TimeFormat) }
	for _, tc := range []struct {
		status int
		header string
		class  failure
		wait   time.Duration
	}{
		{429, "30", busy, 30 * time.Second},
		{429, " 120 ", busy, 2 * time.Minute},
		{429, "", busy, time.Minute},
		{429, date(2 * time.Minute), busy, 2 * time.Minute},
		{429, "Wednesday, 29-Oct-26 12:03:00 GMT", busy, 3 * time.Minute},
		{429, "Wed Oct 29 12:04:00 2026", busy, 4 * time.Minute},
		{429, "3600", busy, 15 * time.Minute},
		{429, "99999999999999999999", busy, time.Minute}, // not a number Go reads: no header
		{429, "9223372036", busy, 15 * time.Minute},
		{429, date(-time.Minute), busy, time.Minute},
		{429, date(0), busy, time.Minute},
		{429, "0", busy, time.Minute},
		{429, "-5", busy, time.Minute},
		{429, "soon", busy, time.Minute},
		{503, "45", busy, 45 * time.Second},
		{503, date(time.Minute), busy, time.Minute},
		{503, "", unreachable, 0},
		{503, "0", unreachable, 0},
		{503, date(-time.Minute), unreachable, 0},
		{500, "30", unreachable, 0},
		{502, "", unreachable, 0},
		{504, "", unreachable, 0},
		{407, "", unreachable, 0},
		{408, "30", unreachable, 0},
		{301, "", unreachable, 0},
		{400, "", refused, 0},
		{401, "", refused, 0},
		{403, "", refused, 0},
		{404, "30", refused, 0},
		{409, "", refused, 0},
		{410, "", refused, 0},
		{413, "", refused, 0},
		{422, "", refused, 0},
	} {
		err := fmt.Errorf("posting: %w", &statusError{provider: "webhook", status: tc.status, retryAfter: tc.header})
		if class, wait := e.classify(err, now); class != tc.class || wait != tc.wait {
			t.Errorf("%d with Retry-After %q: %s for %s, want %s for %s", tc.status, tc.header, class, wait, tc.class, tc.wait)
		}
	}
	for _, err := range []error{errors.New("request failed: connection refused"), context.DeadlineExceeded, ErrEmailNotConfigured} {
		if class, wait := e.classify(err, now); class != unreachable || wait != 0 {
			t.Errorf("%v: %s for %s, want unreachable", err, class, wait)
		}
	}
	// With the attempts again off, a busy receiver is not waited for.
	off := New(nil, nil, Options{})
	if class, wait := off.classify(&statusError{status: 429, retryAfter: "30"}, now); class != busy || wait != 0 {
		t.Errorf("429 with RetryMax 0: %s for %s, want busy for 0s", class, wait)
	}
}

// The senders report an answer outside 2xx with its status and its
// Retry-After header, the text of the error as it was.
func TestSendersReportTheStatusAndRetryAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"status":"throttle event","message":"Requests for this service are arriving too quickly"}`)
	}))
	defer srv.Close()
	ctx := context.Background()
	for _, tc := range []struct {
		sender Sender
		ch     store.Channel
		text   string
	}{
		{&WebhookSender{Client: srv.Client()}, store.Channel{Target: srv.URL}, "webhook answered 429"},
		{&SlackSender{Client: srv.Client()}, store.Channel{Target: srv.URL}, "webhook answered 429"},
		{&PagerDutySender{Client: srv.Client(), URL: srv.URL}, store.Channel{Secret: "key"},
			"PagerDuty answered 429: Requests for this service are arriving too quickly"},
	} {
		err := tc.sender.Send(ctx, tc.ch, sampleDown())
		var answer *statusError
		if !errors.As(err, &answer) || answer.status != http.StatusTooManyRequests || answer.retryAfter != "30" || err.Error() != tc.text {
			t.Errorf("%T: err = %v (%+v), want %q with the status and the header", tc.sender, err, answer, tc.text)
		}
	}
}

// A receiver that refuses a notification (4xx) is tried once, whatever
// UPTIME_NOTIFY_ATTEMPTS says. The webhook is not paused: the next
// notification goes to it at once, and in a pass the notes behind the one
// it refused are sent. The refused note is sent again RetryMax later, and
// the channel's row and the log say the channel needs fixing.
func TestARefusalDoesNotPauseTheWebhook(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 409, 410, 413, 422} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			f := newFixture(t, 1, 1) // "ops", a webhook
			f.retrying(15*time.Minute, time.Hour)
			f.engine.opts.Attempts = 3
			receiver := f.answer()
			receiver.set("ops/bad", status, "")
			bad, good, x := f.monitorIn("bad", ""), f.monitorIn("good", ""), f.monitorIn("x", "")
			start := f.clock

			f.clock = start.Add(30 * time.Second)
			f.result(bad, false) // 0:30: refused, once
			f.deliverLive()
			if n := receiver.triesOf("ops/bad"); n != 1 {
				t.Errorf("%d tries of the refused outage, want 1", n)
			}
			r := f.retryOf("ops", "bad")
			if r == nil || !r.due.Equal(start.Add(15*time.Minute+30*time.Second)) || r.attempts != 1 || !r.refused {
				t.Fatalf("the refused outage waits as %+v, want to be sent again at 15:30", r)
			}
			if f.engine.owed.failing[f.channel("ops").ID] {
				t.Error("the webhook is paused after a refusal")
			}
			ops := f.channel("ops")
			if ops.LastOK || !strings.HasPrefix(ops.LastError, "webhook answered "+strconv.Itoa(status)) || !strings.Contains(ops.LastError, "fix the channel") {
				t.Errorf("the channel's row: ok %v, error %q; want the refusal and that the channel needs fixing", ops.LastOK, ops.LastError)
			}

			f.clock = start.Add(time.Minute)
			f.result(good, false) // 1:00: at once
			f.deliverLive()
			if got := f.sentTo("ops"); got != "down:good" {
				t.Fatalf("at 1:00: %q, want good's outage at once", got)
			}

			// 14:30: x's outage does not reach the webhook, in any of the
			// three tries; its retry falls due at 15:30 with bad's, which
			// is ahead of it and refused again, once: x's goes in the same
			// pass.
			f.clock = start.Add(14*time.Minute + 30*time.Second)
			f.sender.fail("ops", 3)
			f.result(x, false)
			f.deliverLive()
			f.clock = start.Add(15*time.Minute + 30*time.Second)
			f.flush()
			if got := f.sentTo("ops"); got != "down:good,down:x" {
				t.Fatalf("at 15:30: %q, want x's outage, behind bad's refused again", got)
			}
			if n := receiver.triesOf("ops/bad"); n != 2 {
				t.Errorf("%d tries of the refused outage, want 2", n)
			}
			if r := f.retryOf("ops", "bad"); r == nil || !r.due.Equal(start.Add(30*time.Minute+30*time.Second)) || r.attempts != 2 {
				t.Errorf("after the second refusal the outage waits as %+v, want to be sent again at 30:30", r)
			}
		})
	}
}

// A refused note waiting RetryMax is no pass of the webhook's: when an
// attempt then does not reach the webhook and pauses it, the webhook is
// asked again a minute later, not when the refused note is due.
func TestARefusedNoteDoesNotPutOffThePausedWebhook(t *testing.T) {
	f := newFixture(t, 1, 1) // "ops", a webhook
	f.retrying(15*time.Minute, time.Hour)
	receiver := f.answer()
	receiver.set("ops/bad", http.StatusNotFound, "")
	bad, x, y := f.monitorIn("bad", ""), f.monitorIn("x", ""), f.monitorIn("y", "")
	start := f.clock

	f.clock = start.Add(30 * time.Second)
	f.result(bad, false) // 0:30: refused; sent again at 15:30
	f.flush()
	receiver.set("ops/x", http.StatusBadGateway, "")
	f.clock = start.Add(time.Minute)
	f.result(x, false) // 1:00: does not reach the webhook, which is paused until 2:00
	f.flush()
	f.clock = start.Add(90 * time.Second)
	f.result(y, false) // 1:30: held back for 2:00
	f.flush()
	if got := f.sentTo("ops"); got != "" {
		t.Fatalf("at 1:30: %q, want nothing taken", got)
	}
	receiver.set("ops/x", 0, "")
	f.clock = start.Add(2 * time.Minute)
	f.flush()
	if got := f.sentTo("ops"); got != "down:y,down:x" {
		t.Errorf("at 2:00: %q, want y's and x's outages, not put off to bad's at 15:30", got)
	}
	if r := f.retryOf("ops", "bad"); r == nil || !r.due.Equal(start.Add(15*time.Minute+30*time.Second)) {
		t.Errorf("bad's outage waits as %+v, want 15:30", r)
	}
}

// A webhook that refuses every notification about one monitor (422, as
// a receiver with no mapping for it answers) takes the others as they come:
// their outages and their ends go at once, as to a channel that refuses
// nothing; 0.44 held them for the webhook's next pass. The refused outage
// is sent again every RetryMax while it lasts and its end for RetryFor,
// one request each time.
func TestAWebhookRefusingOneMonitorTakesTheOthersAtOnce(t *testing.T) {
	f := newFixture(t, 1, 1) // "ops", a webhook
	f.retrying(15*time.Minute, time.Hour)
	f.engine.opts.Attempts = 3
	receiver := f.answer()
	receiver.set("ops/bad", http.StatusUnprocessableEntity, "")
	bad, good, late := f.monitorIn("bad", ""), f.monitorIn("good", ""), f.monitorIn("late", "")
	start := f.clock

	f.clock = start.Add(30 * time.Second)
	f.result(bad, false) // refused at 0:30, 15:30 and 30:30
	f.flush()
	f.clock = start.Add(time.Minute)
	f.result(good, false)
	f.flush()
	if got := f.sentTo("ops"); got != "down:good" {
		t.Fatalf("at 1:00: %q, want good's outage at once", got)
	}
	f.passes(start, 20*time.Minute)
	f.result(late, false)
	f.flush()
	if got := f.sentTo("ops"); got != "down:good,down:late" {
		t.Fatalf("at 20:00: %q, want late's outage at once", got)
	}
	f.passes(start, 31*time.Minute)
	f.result(bad, true) // 31:00: its end is refused then, at 46:00, 1:01, 1:16 and 1:31, and given up
	f.flush()
	f.clock = start.Add(32 * time.Minute)
	f.result(good, true)
	f.flush()
	if got := f.sentTo("ops"); got != "down:good,down:late,up:good" {
		t.Fatalf("at 32:00: %q, want good's end at once", got)
	}
	f.passes(start, 2*time.Hour)
	f.result(late, true)
	f.flush()
	if got := f.sentTo("ops"); got != "down:good,down:late,up:good,up:late" {
		t.Errorf("at 2:00:00: %q, want late's end at once", got)
	}
	if n := receiver.triesOf("ops/bad"); n != 8 {
		t.Errorf("%d tries about bad, want 8: its outage three times, its end five", n)
	}
	if n := len(f.engine.owed.retries); n != 0 {
		t.Errorf("%d retries left, want none", n)
	}
	f.counts(map[string]float64{
		"kubeat_notification_attempts_total{channel_type=webhook}{result=failed}":     8,
		"kubeat_notifications_total{channel_type=webhook}{event=down}{result=sent}":   2,
		"kubeat_notifications_total{channel_type=webhook}{event=down}{result=failed}": 1,
		"kubeat_notifications_total{channel_type=webhook}{event=up}{result=sent}":     2,
		"kubeat_notifications_total{channel_type=webhook}{event=up}{result=failed}":   1,
	})
}

// A failure that did not reach a receiver able to take the notification
// (5xx, a 503 that says not when to come back, 407, 408) is tried
// UPTIME_NOTIFY_ATTEMPTS times, and the webhook is paused: the next
// notification waits for its retry, a minute on.
func TestAFailureThatDoesNotReachTheWebhookPausesIt(t *testing.T) {
	for _, tc := range []struct {
		status     int
		retryAfter string
	}{{500, ""}, {502, ""}, {503, ""}, {503, "Wed, 21 Oct 2015 07:28:00 GMT"}, {407, ""}, {408, ""}} {
		t.Run(fmt.Sprintf("%d %s", tc.status, tc.retryAfter), func(t *testing.T) {
			f := newFixture(t, 1, 1) // "ops", a webhook; "api"
			f.retrying(15*time.Minute, time.Hour)
			f.engine.opts.Attempts = 3
			receiver := f.answer()
			receiver.set("ops", tc.status, tc.retryAfter)
			cart := f.monitorIn("cart", "")
			f.check(false) // api: tried three times
			f.deliverLive()
			f.result(cart, false)
			f.deliverLive()
			if a, c := receiver.triesOf("ops/api"), receiver.triesOf("ops/cart"); a != 3 || c != 0 {
				t.Errorf("%d tries of api's outage and %d of cart's, want 3 and none: the webhook paused", a, c)
			}
			for _, name := range []string{"api", "cart"} {
				if r := f.retryOf("ops", name); r == nil || !r.due.Equal(f.clock.Add(time.Minute)) {
					t.Errorf("%s's outage waits as %+v, want to be sent again in a minute", name, r)
				}
			}
			if !f.engine.owed.failing[f.channel("ops").ID] {
				t.Error("the webhook is not paused")
			}
		})
	}
}

// A webhook that answers 429 with Retry-After: 30 is asked once, not
// UPTIME_NOTIFY_ATTEMPTS times, and left alone for 30 seconds: what comes
// up meanwhile waits for then, and if it is busy again the wait is again
// what it said, not doubled. Its answers are not refusals: nothing is
// counted against the notes, and nothing is given up.
func TestABusyWebhookIsAskedAgainWhenItSaid(t *testing.T) {
	f := newFixture(t, 1, 1) // "ops", a webhook
	f.retrying(15*time.Minute, time.Hour)
	f.engine.opts.Attempts = 3
	receiver := f.answer()
	receiver.set("ops", http.StatusTooManyRequests, "30")
	a, b := f.monitorIn("a", ""), f.monitorIn("b", "")
	start := f.clock

	f.clock = start.Add(10 * time.Second)
	f.result(a, false) // 0:10: asked once; asked again at 0:40
	f.deliverLive()
	f.clock = start.Add(20 * time.Second)
	f.result(b, false) // 0:20: held back for 0:40
	f.deliverLive()
	if na, nb := receiver.triesOf("ops/a"), receiver.triesOf("ops/b"); na != 1 || nb != 0 {
		t.Fatalf("%d tries of a's outage and %d of b's, want one and none", na, nb)
	}
	for _, name := range []string{"a", "b"} {
		if r := f.retryOf("ops", name); r == nil || !r.due.Equal(start.Add(40*time.Second)) || r.attempts != 0 {
			t.Fatalf("%s's outage waits as %+v, want to go at 0:40 with no refusal counted", name, r)
		}
	}
	for _, at := range []time.Duration{40 * time.Second, 70 * time.Second} { // busy at 0:40 and at 1:10
		f.clock = start.Add(at)
		f.flush()
		for _, name := range []string{"a", "b"} {
			if r := f.retryOf("ops", name); r == nil || !r.due.Equal(f.clock.Add(30*time.Second)) || r.attempts != 0 {
				t.Fatalf("after the answer at %s %s's outage waits as %+v, want 30 seconds more", at, name, r)
			}
		}
	}
	if na, nb := receiver.triesOf("ops/a"), receiver.triesOf("ops/b"); na != 3 || nb != 0 {
		t.Errorf("%d tries of a's outage and %d of b's, want three, one a pass, and none", na, nb)
	}
	receiver.set("ops", 0, "")
	f.clock = start.Add(100 * time.Second)
	f.flush()
	if got := f.sentTo("ops"); got != "down:a,down:b" {
		t.Errorf("at 1:40: %q, want both outages", got)
	}
	if n := len(f.engine.owed.retries); n != 0 {
		t.Errorf("%d retries left, want none", n)
	}
	f.counts(map[string]float64{
		"kubeat_notification_attempts_total{channel_type=webhook}{result=failed}":     3,
		"kubeat_notifications_total{channel_type=webhook}{event=down}{result=sent}":   2,
		"kubeat_notifications_total{channel_type=webhook}{event=down}{result=failed}": 0,
	})
}

// How long a busy webhook is left alone: its Retry-After, in seconds or as
// an HTTP date; a minute without a usable one; UPTIME_NOTIFY_RETRY_MAX at
// most. A 503 that says when to come back is busy too. Each time the
// webhook is asked once.
func TestHowLongABusyWebhookIsLeftAlone(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		header func(now time.Time) string
		want   time.Duration
	}{
		{"429 in seconds", 429, func(time.Time) string { return "30" }, 30 * time.Second},
		{"429 as an HTTP date", 429, func(now time.Time) string { return now.Add(2 * time.Minute).Format(http.TimeFormat) }, 2 * time.Minute},
		{"429 past RetryMax", 429, func(time.Time) string { return "3600" }, 15 * time.Minute},
		{"429 as an HTTP date past RetryMax", 429, func(now time.Time) string { return now.Add(2 * time.Hour).Format(http.TimeFormat) }, 15 * time.Minute},
		{"429 without Retry-After", 429, func(time.Time) string { return "" }, time.Minute},
		{"429 with a date gone by", 429, func(now time.Time) string { return now.Add(-time.Hour).Format(http.TimeFormat) }, time.Minute},
		{"503 with Retry-After", 503, func(time.Time) string { return "45" }, 45 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, 1, 1) // "ops", a webhook; "api"
			f.retrying(15*time.Minute, time.Hour)
			f.engine.opts.Attempts = 3
			receiver := f.answer()
			f.check(false)
			receiver.set("ops", tc.status, tc.header(f.clock))
			f.deliverLive()
			if n := receiver.triesOf("ops/api"); n != 1 {
				t.Errorf("%d tries, want one", n)
			}
			if r := f.retryOf("ops", "api"); r == nil || !r.due.Equal(f.clock.Add(tc.want)) {
				t.Errorf("the outage waits as %+v, want %s", r, tc.want)
			}
		})
	}
}

// A channel people read is not paused by a busy answer: only the message
// it was busy for waits, for as long as it said, and the next message is
// tried at once. A certificate warning, which is not sent again when it
// is not taken, goes once more after a busy answer: the receiver did not
// refuse it.
func TestABusyChatIsNotPaused(t *testing.T) {
	f := newFixture(t, 1, 1) // "ops", a webhook; "api" has a certificate
	f.retrying(15*time.Minute, time.Hour)
	f.engine.opts.Attempts = 3
	f.addChannel(store.Channel{Name: "chat", Type: store.ChannelSlack, Target: "http://unused"})
	receiver := f.answer()
	receiver.set("chat/a", http.StatusTooManyRequests, "20")
	receiver.set("chat/api", http.StatusTooManyRequests, "20")
	a, b := f.monitorIn("a", ""), f.monitorIn("b", "")
	start := f.clock

	f.clock = start.Add(10 * time.Second)
	f.result(a, false) // 0:10: chat is busy for it until 0:30
	f.deliverLive()
	f.clock = start.Add(15 * time.Second)
	f.result(b, false) // 0:15: tried at once
	f.deliverLive()
	if got := f.sentTo("chat"); got != "down:b" {
		t.Fatalf("at 0:15: %q, want b's outage at once", got)
	}
	if r := f.retryOf("chat", "a"); r == nil || !r.due.Equal(start.Add(30*time.Second)) || receiver.triesOf("chat/a") != 1 {
		t.Fatalf("a's outage waits as %+v after %d tries, want one try and 0:30", r, receiver.triesOf("chat/a"))
	}
	receiver.set("chat/a", 0, "")
	f.clock = start.Add(30 * time.Second)
	f.flush()
	if got := f.sentTo("chat"); got != "down:b,down:a" {
		t.Fatalf("at 0:30: %q, want a's outage", got)
	}
	f.checkCert(10 * 24 * time.Hour) // 1:00: chat is busy for the warning until 1:20
	f.deliverLive()
	if r := f.retryOf("chat", "api"); r == nil || !r.once || !r.due.Equal(start.Add(80*time.Second)) {
		t.Fatalf("the certificate warning waits as %+v, want to go once more at 1:20", r)
	}
	receiver.set("chat/api", 0, "")
	f.clock = start.Add(80 * time.Second)
	f.flush()
	if got := f.sentTo("chat"); got != "down:b,down:a,cert_expiring:api" {
		t.Errorf("at 1:20: %q, want the warning", got)
	}
	f.counts(map[string]float64{
		"kubeat_notifications_total{channel_type=slack}{event=cert_expiring}{result=sent}":   1,
		"kubeat_notifications_total{channel_type=slack}{event=cert_expiring}{result=failed}": 0,
	})
}

// A message a channel people read keeps refusing (4xx) does not keep its
// other messages to be sent again waiting: they go in the same pass,
// behind it (backlog, "S25'in incelemesinde kalan bildirim köşeleri", 1).
func TestARefusedChatMessageDoesNotHoldBackTheOthers(t *testing.T) {
	f := newFixture(t, 1, 1) // "ops", a webhook
	f.retrying(15*time.Minute, time.Hour)
	f.addChannel(store.Channel{Name: "chat", Type: store.ChannelSlack, Target: "http://unused"})
	receiver := f.answer()
	receiver.set("chat/bad", http.StatusBadRequest, "")
	bad, x := f.monitorIn("bad", ""), f.monitorIn("x", "")
	start := f.clock

	f.clock = start.Add(30 * time.Second)
	f.result(bad, false) // 0:30: refused; sent again at 15:30
	f.flush()
	f.clock = start.Add(14*time.Minute + 30*time.Second)
	f.sender.fail("chat", 1)
	f.result(x, false) // 14:30: not taken; sent again at 15:30, behind bad's
	f.flush()
	if got := f.sentTo("chat"); got != "" {
		t.Fatalf("at 14:30: %q, want nothing taken", got)
	}
	f.clock = start.Add(15*time.Minute + 30*time.Second)
	f.flush()
	if got := f.sentTo("chat"); got != "down:x" {
		t.Errorf("at 15:30: %q, want x's outage, behind bad's refused again", got)
	}
	if n := receiver.triesOf("chat/bad"); n != 2 {
		t.Errorf("%d tries of bad's outage, want 2", n)
	}
	if r := f.retryOf("chat", "bad"); r == nil || !r.due.Equal(start.Add(30*time.Minute+30*time.Second)) {
		t.Errorf("bad's outage waits as %+v, want 30:30", r)
	}
	if r := f.retryOf("chat", "x"); r != nil {
		t.Errorf("x's outage still waits: %+v", r)
	}
}

// rateLimited stands for PagerDuty's Events API under its rate limit: of
// the sends to one channel it takes limit a window and answers the rest
// 429, with a Retry-After that says when the next window opens. A window
// is a minute of clock's; with no clock, the whole test is one window and
// Retry-After says 60. The tries are counted by window.
type rateLimited struct {
	*recorder
	channel string
	limit   int
	clock   func() time.Time
	mu      sync.Mutex
	window  time.Time
	taken   int
	tries   map[time.Time]int
}

func (p *rateLimited) Send(ctx context.Context, ch store.Channel, n Notification) error {
	if ch.Name != p.channel {
		return p.recorder.Send(ctx, ch, n)
	}
	p.mu.Lock()
	retryAfter := "60"
	if p.clock != nil {
		now := p.clock()
		if window := now.Truncate(time.Minute); !window.Equal(p.window) {
			p.window, p.taken = window, 0
		}
		retryAfter = strconv.Itoa(int(p.window.Add(time.Minute).Sub(now) / time.Second))
	}
	if p.tries == nil {
		p.tries = make(map[time.Time]int)
	}
	p.tries[p.window]++
	if p.taken >= p.limit {
		p.mu.Unlock()
		return &statusError{provider: "PagerDuty", status: http.StatusTooManyRequests, reason: "Too many requests", retryAfter: retryAfter}
	}
	p.taken++
	p.mu.Unlock()
	return p.recorder.Send(ctx, ch, n)
}

// outages opens an outage for each of n new monitors of the namespace,
// opened at at, and returns their notifications.
func (f *fixture) outages(n int, namespace string, at time.Time) []Notification {
	f.t.Helper()
	ctx := context.Background()
	var out []Notification
	for i := range n {
		m := f.monitorIn(fmt.Sprintf("api-%03d", i), namespace)
		incident, err := f.db.OpenIncident(ctx, store.Incident{MonitorID: m.ID, StartedAt: at, OpenedAt: at, Cause: "refused"})
		if err != nil {
			f.t.Fatal(err)
		}
		out = append(out, Notification{Event: EventDown, Monitor: m, Incident: incident, At: at})
	}
	return out
}

// Three hundred outages at once, and a PagerDuty that takes a hundred a
// minute: it is sent a hundred a pass, one 429 ends the pass, and the rest
// go when the next minute opens, as Retry-After says; all of them within
// two minutes, none given up, and nothing dropped. No pass sends it more
// than one notification it does not take.
func TestABurstToARateLimitedPagerDutyGoesInMinutes(t *testing.T) {
	f := newFixture(t, 1, 1)
	f.retrying(15*time.Minute, time.Hour)
	f.engine.opts.Attempts = 3
	f.engine.opts.QueueSize = 1000
	f.engine.queue = make(chan Notification, f.engine.opts.QueueSize)
	f.disable("ops")
	f.addChannel(store.Channel{Name: "pager", Type: store.ChannelPagerDuty, Secret: "key"})
	const burst, limit = 300, 100
	pager := &rateLimited{recorder: f.sender, channel: "pager", limit: limit, clock: func() time.Time { return f.clock }}
	f.engine.sender = pager

	start := f.clock.Add(30 * time.Second) // 12:00:30, half a minute before PagerDuty's next window
	f.clock = start
	for _, n := range f.outages(burst, "shop", start) {
		f.engine.enqueue(n)
	}
	f.flush()
	for next := f.engine.owed.next(); !next.IsZero() && next.Before(start.Add(5*time.Minute)); next = f.engine.owed.next() {
		f.clock = next
		f.flush()
	}
	if got, want := strings.Count(f.sentTo("pager"), "down:"), burst; got != want {
		t.Fatalf("PagerDuty took %d outages, want all %d", got, want)
	}
	if done := f.clock.Sub(start); done > 2*time.Minute {
		t.Errorf("the last outage went %s after the burst, want within two minutes", done)
	}
	pager.mu.Lock()
	for window, tries := range pager.tries {
		if tries > limit+1 {
			t.Errorf("PagerDuty was sent %d notifications in the minute from %s, want %d at most: one 429 ends the pass", tries, window.Format("15:04"), limit+1)
		}
	}
	pager.mu.Unlock()
	f.counts(map[string]float64{
		"kubeat_notifications_dropped_total":                                            0,
		"kubeat_notifications_total{channel_type=pagerduty}{event=down}{result=sent}":   burst,
		"kubeat_notifications_total{channel_type=pagerduty}{event=down}{result=failed}": 0,
		"kubeat_notification_attempts_total{channel_type=pagerduty}{result=failed}":     2,
	})
}

// The same burst while the loop runs: PagerDuty takes the first fifty and
// answers 429 to the next, which is asked once rather than
// UPTIME_NOTIFY_ATTEMPTS times; it is then left alone, so the loop does not
// wait out UPTIME_NOTIFY_RETRY_DELAY a notification. The queue drains,
// nothing is dropped, and the channels people read get the burst in one
// message within UPTIME_NOTIFY_GROUP_WAIT.
func TestABurstWithARateLimitedPagerDutyKeepsTheQueueMoving(t *testing.T) {
	f := newFixture(t, 1, 1)
	f.engine.opts.Metrics = metrics.New("test")
	f.engine.opts.QueueSize = 1000
	f.engine.queue = make(chan Notification, f.engine.opts.QueueSize)
	f.engine.opts.GroupWait = 3 * time.Second
	f.engine.opts.RetryMax, f.engine.opts.RetryFor = 15*time.Minute, time.Hour
	// Two short tries a second apart for each 429 would hold the loop for
	// minutes.
	f.engine.opts.Attempts, f.engine.opts.RetryDelay = 3, time.Second
	f.disable("ops")
	f.addChannel(store.Channel{Name: "pager", Type: store.ChannelPagerDuty, Secret: "key"})
	f.addChannel(store.Channel{Name: "chat", Type: store.ChannelSlack, Target: "http://unused"})
	f.addChannel(store.Channel{Name: "mail", Type: store.ChannelEmail, Target: "ops@example.com"})
	const burst, limit = 300, 50
	pager := &rateLimited{recorder: f.sender, channel: "pager", limit: limit}
	f.engine.sender = pager
	outages := f.outages(burst, "shop", time.Now().UTC())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.engine.Run(ctx)
	}()
	start := time.Now()
	for _, n := range outages {
		f.engine.enqueue(n)
	}
	// Drained before the group is due, the burst is one group.
	if !await(f.engine.opts.GroupWait, func() bool { return len(f.engine.queue) == 0 }) {
		t.Fatalf("the queue still holds %d notifications %s after the burst", len(f.engine.queue), f.engine.opts.GroupWait)
	}
	told := func() bool {
		got := f.sender.log()
		return strings.Contains(got, "chat:down[") && strings.Contains(got, "mail:down[")
	}
	if !await(time.Until(start.Add(f.engine.opts.GroupWait+time.Second)), told) {
		t.Fatalf("%s after the burst: chat and mail not told within GroupWait", time.Since(start).Round(time.Millisecond))
	}
	cancel()
	<-done
	f.sender.settle()

	pager.mu.Lock()
	tries := pager.tries[time.Time{}]
	pager.mu.Unlock()
	if tries != limit+1 {
		t.Errorf("PagerDuty was tried %d times, want %d: the outages it took and one 429", tries, limit+1)
	}
	got := map[string][]string{}
	f.sender.mu.Lock()
	for i, n := range f.sender.sent {
		got[f.sender.channels[i]] = append(got[f.sender.channels[i]], fmt.Sprintf("%s:%d", n.Event, max(len(n.Group), 1)))
	}
	f.sender.mu.Unlock()
	for _, ch := range []string{"chat", "mail"} {
		if g := strings.Join(got[ch], ","); g != fmt.Sprintf("down:%d", burst) {
			t.Errorf("%s got %q, want the %d outages in one message", ch, g, burst)
		}
	}
	waiting := 0
	for _, r := range f.engine.owed.retries {
		if r.channel.Name == "pager" && r.note.Event == EventDown && !r.once {
			waiting++
		}
	}
	if waiting != burst-limit {
		t.Errorf("%d outages wait for PagerDuty, want %d", waiting, burst-limit)
	}
	values, err := f.engine.opts.Metrics.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]float64{
		"kubeat_notifications_dropped_total":                                            0,
		"kubeat_notifications_total{channel_type=pagerduty}{event=down}{result=sent}":   limit,
		"kubeat_notifications_total{channel_type=pagerduty}{event=down}{result=failed}": 0,
	} {
		if values[key] != want {
			t.Errorf("%s = %v, want %v", key, values[key], want)
		}
	}
}
