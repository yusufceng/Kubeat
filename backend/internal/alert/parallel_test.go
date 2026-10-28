package alert

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/metrics"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// stalled is a Sender that holds every send to one channel until it is
// released, as a target that does not answer holds a send to its timeout,
// and passes the sends to the other channels to the recorder at once.
type stalled struct {
	*recorder
	channel string
	gate    chan struct{}
	once    sync.Once
	holding atomic.Int32 // sends waiting at the gate right now
}

func (s *stalled) Send(ctx context.Context, ch store.Channel, n Notification) error {
	if ch.Name == s.channel {
		s.holding.Add(1)
		defer s.holding.Add(-1)
		select {
		case <-s.gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return s.recorder.Send(ctx, ch, n)
}

// release lets the held sends, and every later one, through.
func (s *stalled) release() { s.once.Do(func() { close(s.gate) }) }

// stall makes the sends to the named channel wait until the returned sender
// is released. A held send is bounded by one try of 25 seconds, what a
// channel at its timeout costs the loop today (three tries of 5 seconds
// and two pauses of 5); the tests release it long before.
func (f *fixture) stall(channel string) *stalled {
	s := &stalled{recorder: f.sender, channel: channel, gate: make(chan struct{})}
	f.engine.sender = s
	f.engine.opts.SendTimeout, f.engine.opts.Attempts = 25*time.Second, 1
	f.t.Cleanup(s.release)
	return s
}

// await polls until cond holds, and reports whether it did before timeout.
func await(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(5 * time.Millisecond)
	}
	return true
}

// A channel that does not answer holds up only itself: the others are told
// at once, and the loop waits for the slowest channel rather than for the
// sum of them. Shutting down does not cut the held send short.
func TestASlowChannelDoesNotHoldUpTheOthers(t *testing.T) {
	f := newFixture(t, 1, 1) // "ops", the webhook, is first in the channels' order
	f.addChannel(store.Channel{Name: "chat", Type: store.ChannelSlack, Target: "http://unused"})
	slow := f.stall("ops")
	f.check(false)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.engine.Run(ctx)
	}()
	// Chat has the outage within a second, while the send to the webhook,
	// which comes first, is still being held.
	if !await(time.Second, func() bool { return f.sender.log() == "chat:down" }) {
		t.Fatalf("a second in: %q, want chat:down while the webhook is held", f.sender.log())
	}
	if !await(time.Second, func() bool { return slow.holding.Load() == 1 }) {
		t.Fatal("the send to the webhook is not being held")
	}

	// Shutdown waits for the send under way instead of cutting it short:
	// the loop returns once it is through.
	cancel()
	select {
	case <-done:
		t.Fatal("the loop returned with a send under way")
	case <-time.After(50 * time.Millisecond):
	}
	slow.release()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the loop did not return once the held send was through")
	}
	f.sender.settle()
	if got := f.sender.log(); got != "ops:down,chat:down" {
		t.Errorf("after the release: %q, want ops:down,chat:down", got)
	}

	// Both deliveries are on record: on the incident and on the channels.
	var logged []string
	for _, d := range f.deliveries(f.incidents()[0].ID) {
		logged = append(logged, d.ChannelName+":"+d.Event+":"+strconv.FormatBool(d.Delivered))
	}
	sort.Strings(logged)
	if got := strings.Join(logged, ","); got != "chat:down:true,ops:down:true" {
		t.Errorf("delivery log = %q, want both delivered", got)
	}
	channels, err := f.db.ListChannels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, ch := range channels {
		if ch.LastAttemptAt.IsZero() || !ch.LastOK {
			t.Errorf("%s: last attempt at %v, ok %v; want the delivery recorded", ch.Name, ch.LastAttemptAt, ch.LastOK)
		}
	}
}

// Forty monitors of one namespace go down at once while a channel people
// read does not answer: the webhook gets every notification on its own and
// in order, the other channel people read gets the group within a second,
// the held channel gets it once it answers, and nothing is dropped.
func TestABurstReachesEveryChannelAndDropsNothing(t *testing.T) {
	f := newFixture(t, 1, 1) // "ops", a webhook
	f.engine.opts.GroupWait = 50 * time.Millisecond
	f.engine.opts.Metrics = metrics.New("test")
	f.engine.opts.QueueSize = 100 // the default
	f.engine.queue = make(chan Notification, f.engine.opts.QueueSize)
	f.addChannel(store.Channel{Name: "chat", Type: store.ChannelSlack, Target: "http://unused"})
	f.addChannel(store.Channel{Name: "mail", Type: store.ChannelEmail, Target: "ops@example.com"})
	slow := f.stall("chat")
	gathered := func(name string) float64 {
		t.Helper()
		values, err := f.engine.opts.Metrics.Gather()
		if err != nil {
			t.Fatal(err)
		}
		return values[name]
	}

	// One batch of results, as the scheduler stores them: forty outages
	// confirmed at once, forty notifications queued before the loop runs.
	ctx := context.Background()
	const burst = 40
	f.clock = f.clock.Add(30 * time.Second)
	var batch []store.CheckResult
	for i := 1; i <= burst; i++ {
		m, err := f.db.CreateMonitor(ctx, store.Monitor{Name: "api-" + strconv.Itoa(i), Type: store.MonitorTCP, Target: "api:80", Namespace: "shop",
			Interval: 30 * time.Second, Timeout: 5 * time.Second, FailureThreshold: 1, Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		batch = append(batch, store.CheckResult{MonitorID: m.ID, CheckedAt: f.clock, Error: "refused"})
	}
	if err := f.db.InsertCheckResults(ctx, batch); err != nil {
		t.Fatal(err)
	}
	f.engine.Evaluate(ctx, batch)
	if got := gathered("kubeat_notifications_dropped_total"); got != 0 {
		t.Fatalf("%v notifications dropped by a burst of %d", got, burst)
	}

	// The loop drains the queue (the webhook hears each at once) and,
	// shutting down, flushes the group to the channels people read: chat
	// holds it, mail must not wait for chat.
	runCtx, cancel := context.WithCancel(ctx)
	cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.engine.Run(runCtx)
	}()
	if !await(5*time.Second, func() bool { return slow.holding.Load() == 1 }) {
		t.Fatalf("the send to chat is not being held; sent %q", f.sender.log())
	}
	if !await(time.Second, func() bool { return strings.Contains(f.sender.log(), "mail:down[") }) {
		t.Fatalf("a second after chat was held: %q, want mail's grouped message", f.sender.log())
	}
	slow.release()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the loop did not return once the held send was through")
	}
	f.sender.settle()

	f.sender.mu.Lock()
	got := map[string][]string{}
	for i, n := range f.sender.sent {
		what := n.Monitor.Name
		if len(n.Group) > 1 {
			what = "group of " + strconv.Itoa(len(n.Group))
		}
		got[f.sender.channels[i]] = append(got[f.sender.channels[i]], what)
	}
	f.sender.mu.Unlock()
	var each []string
	for i := 1; i <= burst; i++ {
		each = append(each, "api-"+strconv.Itoa(i))
	}
	for ch, want := range map[string]string{
		"ops":  strings.Join(each, " "), // every one on its own, in the order they arose
		"chat": "group of 40",
		"mail": "group of 40",
	} {
		if g := strings.Join(got[ch], " "); g != want {
			t.Errorf("%s got %q, want %q", ch, g, want)
		}
	}
	if got := gathered("kubeat_notifications_dropped_total"); got != 0 {
		t.Errorf("%v notifications dropped, want none", got)
	}
	for key, want := range map[string]float64{
		"kubeat_notifications_total{channel_type=webhook}{event=down}{result=sent}": burst,
		"kubeat_notifications_total{channel_type=slack}{event=down}{result=sent}":   1,
		"kubeat_notifications_total{channel_type=email}{event=down}{result=sent}":   1,
	} {
		if got := gathered(key); got != want {
			t.Errorf("%s = %v, want %v", key, got, want)
		}
	}
}

// What the channels are owed goes the same way when it falls due: a held
// channel does not keep the others waiting for theirs.
func TestOwedDeliveriesDoNotWaitForASlowChannel(t *testing.T) {
	f := newFixture(t, 1, 1)
	f.useClock()
	f.addChannel(store.Channel{Name: "pager", Type: store.ChannelPagerDuty, Secret: "key", Delay: time.Minute})
	f.addChannel(store.Channel{Name: "mail", Type: store.ChannelEmail, Target: "ops@example.com", Delay: time.Minute})
	slow := f.stall("pager")
	f.check(false)
	f.flush()
	if got := f.sender.log(); got != "ops:down" {
		t.Fatalf("at the start: %q, want ops:down", got)
	}

	// Both delays pass together; the pager does not answer.
	f.clock = f.clock.Add(time.Minute)
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.engine.deliverOwed(context.Background())
	}()
	if !await(5*time.Second, func() bool { return slow.holding.Load() == 1 }) {
		t.Fatalf("the send to the pager is not being held; sent %q", f.sender.log())
	}
	if !await(time.Second, func() bool { return f.sender.log() == "ops:down,mail:down" }) {
		t.Fatalf("a second after the pager was held: %q, want ops:down,mail:down", f.sender.log())
	}
	slow.release()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the owed deliveries did not finish once the held send was through")
	}
	f.sender.settle()
	if got := f.sender.log(); got != "ops:down,pager:down,mail:down" {
		t.Errorf("after the release: %q, want ops:down,pager:down,mail:down", got)
	}
}
