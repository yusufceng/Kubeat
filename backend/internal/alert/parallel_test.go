package alert

import (
	"context"
	"fmt"
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

// dead is a Sender to which one channel does not answer: a send to it
// lasts until its timeout. The other channels' sends go to the recorder.
type dead struct {
	*recorder
	channel  string
	attempts atomic.Int32
}

func (d *dead) Send(ctx context.Context, ch store.Channel, n Notification) error {
	if ch.Name != d.channel {
		return d.recorder.Send(ctx, ch, n)
	}
	d.attempts.Add(1)
	<-ctx.Done()
	return ctx.Err()
}

// A hundred monitors of a namespace go down at once while the webhook
// does not answer: it is attempted once and then paused, so that the loop
// keeps up with the queue instead of waiting out a send timeout for every
// notification (ten seconds here; with the defaults, 25 seconds each).
// Nothing is dropped, the channels people read get the hundred in one
// message within GroupWait, and the hundred ends that follow find room in
// the queue too.
func TestABurstWithADeadWebhookKeepsTheQueueMoving(t *testing.T) {
	f := newFixture(t, 1, 1) // "ops", a webhook
	f.engine.opts.Metrics = metrics.New("test")
	f.engine.opts.QueueSize = 100 // the default
	f.engine.queue = make(chan Notification, f.engine.opts.QueueSize)
	f.engine.opts.GroupWait = 2 * time.Second
	f.engine.opts.RetryMax, f.engine.opts.RetryFor = 15*time.Minute, time.Hour
	f.engine.opts.SendTimeout, f.engine.opts.Attempts = 100*time.Millisecond, 1
	f.addChannel(store.Channel{Name: "chat", Type: store.ChannelSlack, Target: "http://unused"})
	f.addChannel(store.Channel{Name: "mail", Type: store.ChannelEmail, Target: "ops@example.com"})
	webhook := &dead{recorder: f.sender, channel: "ops"}
	f.engine.sender = webhook
	gathered := func(name string) float64 {
		t.Helper()
		values, err := f.engine.opts.Metrics.Gather()
		if err != nil {
			t.Fatal(err)
		}
		return values[name]
	}

	ctx := context.Background()
	const burst = 100
	opened := time.Now().UTC()
	var outages []Notification
	for i := 1; i <= burst; i++ {
		m := f.monitorIn("api-"+strconv.Itoa(i), "shop")
		incident, err := f.db.OpenIncident(ctx, store.Incident{MonitorID: m.ID, StartedAt: opened, OpenedAt: opened, Cause: "refused"})
		if err != nil {
			t.Fatal(err)
		}
		outages = append(outages, Notification{Event: EventDown, Monitor: m, Incident: incident, At: opened})
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.engine.Run(runCtx)
	}()
	start := time.Now()
	for _, n := range outages {
		f.engine.enqueue(n)
	}
	if !await(2*time.Second, func() bool { return len(f.engine.queue) == 0 }) {
		t.Fatalf("the queue still holds %d notifications 2 seconds after the burst", len(f.engine.queue))
	}
	told := func(event Event) bool {
		got := f.sender.log()
		return strings.Contains(got, "chat:"+string(event)+"[") && strings.Contains(got, "mail:"+string(event)+"[")
	}
	if !await(time.Until(start.Add(f.engine.opts.GroupWait+time.Second)), func() bool { return told(EventDown) }) {
		t.Fatalf("%s after the burst: %q, want chat and mail told within GroupWait", time.Since(start).Round(time.Millisecond), f.sender.log())
	}

	// The hundred outages end at once.
	resolved := time.Now().UTC()
	for _, n := range outages {
		if err := f.db.ResolveIncident(ctx, n.Incident.ID, resolved); err != nil {
			t.Fatal(err)
		}
		n.Event, n.Incident.ResolvedAt, n.At = EventUp, resolved, resolved
		f.engine.enqueue(n)
	}
	if !await(2*time.Second, func() bool { return len(f.engine.queue) == 0 }) {
		t.Fatalf("the queue still holds %d ends 2 seconds after they came", len(f.engine.queue))
	}
	cancel()
	<-done
	f.sender.settle()

	got := map[string][]string{}
	f.sender.mu.Lock()
	for i, n := range f.sender.sent {
		got[f.sender.channels[i]] = append(got[f.sender.channels[i]], fmt.Sprintf("%s:%d", n.Event, max(len(n.Group), 1)))
	}
	f.sender.mu.Unlock()
	for _, ch := range []string{"chat", "mail"} {
		if g := strings.Join(got[ch], ","); g != "down:100,up:100" {
			t.Errorf("%s got %q, want down:100,up:100: the outages in one message, their ends in another", ch, g)
		}
	}
	if n := webhook.attempts.Load(); n != 1 {
		t.Errorf("the webhook was attempted %d times, want once", n)
	}
	// The outages ended before the webhook took them; their ends wait for it.
	var waiting int
	for _, r := range f.engine.owed.retries {
		if r.channel.Name == "ops" && r.note.Event == EventUp && !r.once {
			waiting++
		}
	}
	if waiting != burst || len(f.engine.owed.retries) != burst {
		t.Errorf("%d retries, %d of them ends for the webhook; want the %d ends alone", len(f.engine.owed.retries), waiting, burst)
	}
	for key, want := range map[string]float64{
		"kubeat_notifications_dropped_total":                                          0,
		"kubeat_notification_attempts_total{channel_type=webhook}{result=failed}":     1,
		"kubeat_notifications_total{channel_type=webhook}{event=down}{result=failed}": burst,
		"kubeat_notifications_total{channel_type=slack}{event=down}{result=sent}":     1,
		"kubeat_notifications_total{channel_type=email}{event=up}{result=sent}":       1,
	} {
		if got := gathered(key); got != want {
			t.Errorf("%s = %v, want %v", key, got, want)
		}
	}
}

// gated holds the sends to a channel about a monitor, keyed
// "channel/monitor", until that gate is released; the other sends go to
// the recorder at once.
type gated struct {
	*recorder
	gates   map[string]chan struct{}
	once    map[string]*sync.Once
	holding atomic.Int32 // sends waiting at a gate right now
}

func (f *fixture) gate(keys ...string) *gated {
	g := &gated{recorder: f.sender, gates: make(map[string]chan struct{}), once: make(map[string]*sync.Once)}
	for _, key := range keys {
		g.gates[key], g.once[key] = make(chan struct{}), new(sync.Once)
	}
	f.engine.sender = g
	f.t.Cleanup(func() {
		for _, key := range keys {
			g.release(key)
		}
	})
	return g
}

func (g *gated) release(key string) { g.once[key].Do(func() { close(g.gates[key]) }) }

func (g *gated) Send(ctx context.Context, ch store.Channel, n Notification) error {
	if gate, ok := g.gates[ch.Name+"/"+n.Monitor.Name]; ok {
		g.holding.Add(1)
		defer g.holding.Add(-1)
		select {
		case <-gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return g.recorder.Send(ctx, ch, n)
}

// Shutting down, the groups people wait for go before the queue is
// drained: a webhook that does not answer about what is still queued does
// not keep chat from the outage it is waiting to hear of.
func TestShutdownTellsPeopleBeforeTheQueue(t *testing.T) {
	f := newFixture(t, 1, 1) // "ops", a webhook
	// Chat's groups are not due while the test runs.
	f.engine.opts.GroupWait = time.Minute
	f.engine.opts.SendTimeout, f.engine.opts.Attempts = 25*time.Second, 1
	f.addChannel(store.Channel{Name: "chat", Type: store.ChannelSlack, Target: "http://unused"})
	sender := f.gate("ops/a", "ops/b")
	outage := func(name string) Notification {
		return Notification{Event: EventDown, Monitor: store.Monitor{Name: name, Namespace: "shop"}, Incident: store.Incident{StartedAt: f.clock, Cause: "refused"}}
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.engine.Run(ctx)
	}()
	// The webhook is slow to take a's outage and chat's group waits; b's
	// outage comes meanwhile, and so does the shutdown.
	f.engine.enqueue(outage("a"))
	if !await(time.Second, func() bool { return sender.holding.Load() == 1 }) {
		t.Fatal("the send of a's outage to the webhook is not being held")
	}
	f.engine.enqueue(outage("b"))
	cancel()
	sender.release("ops/a")
	if !await(2*time.Second, func() bool { return strings.Contains(f.sender.log(), "chat:down") }) {
		t.Fatalf("while the webhook is slow to take b's outage: %q, want chat told of a's", f.sender.log())
	}
	if !await(time.Second, func() bool { return sender.holding.Load() == 1 }) {
		t.Fatal("the send of b's outage to the webhook is not being held")
	}
	sender.release("ops/b")
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the loop did not return once the webhook took b's outage")
	}
	f.sender.settle()
	got := map[string][]string{}
	f.sender.mu.Lock()
	for i, n := range f.sender.sent {
		got[f.sender.channels[i]] = append(got[f.sender.channels[i]], n.Monitor.Name)
	}
	f.sender.mu.Unlock()
	for ch, want := range map[string]string{"ops": "a b", "chat": "a b"} {
		if g := strings.Join(got[ch], " "); g != want {
			t.Errorf("%s got %q, want %q", ch, g, want)
		}
	}
}

// Shutting down takes DrainTimeout at most: a send still under way then
// is cut short, and what is left is not sent. The outages among it are
// sent at the next start: again to the webhook, which did not take a's,
// and to chat, which heard of neither.
func TestShutdownStopsAtTheDrainTimeout(t *testing.T) {
	f := newFixture(t, 1, 1) // "ops", a webhook
	f.engine.opts.GroupWait = time.Minute
	f.engine.opts.DrainTimeout = 200 * time.Millisecond
	f.engine.opts.RetryMax, f.engine.opts.RetryFor = 15*time.Minute, time.Hour
	f.engine.opts.SendTimeout, f.engine.opts.Attempts = 25*time.Second, 1
	f.addChannel(store.Channel{Name: "chat", Type: store.ChannelSlack, Target: "http://unused"})
	sender := f.gate("ops/a")

	time.Sleep(2 * time.Millisecond) // the outages open after the channels were made
	ctx := context.Background()
	var outages []Notification
	for _, name := range []string{"a", "b"} {
		m := f.monitorIn(name, "shop")
		opened := time.Now().UTC()
		incident, err := f.db.OpenIncident(ctx, store.Incident{MonitorID: m.ID, StartedAt: opened, OpenedAt: opened, Cause: "refused"})
		if err != nil {
			t.Fatal(err)
		}
		outages = append(outages, Notification{Event: EventDown, Monitor: m, Incident: incident, At: opened})
	}

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.engine.Run(runCtx)
	}()
	f.engine.enqueue(outages[0])
	if !await(time.Second, func() bool { return sender.holding.Load() == 1 }) {
		t.Fatal("the send of a's outage to the webhook is not being held")
	}
	f.engine.enqueue(outages[1])
	cancel()
	stopped := time.Now()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the loop did not stop at its drain timeout")
	}
	if took := time.Since(stopped); took > time.Second {
		t.Errorf("shutting down took %s, want about the drain timeout, %s", took.Round(time.Millisecond), f.engine.opts.DrainTimeout)
	}
	f.sender.settle()
	if got := f.sender.log(); got != "" {
		t.Errorf("while shutting down: %q, want nothing taken", got)
	}
	if d := f.deliveries(outages[0].Incident.ID); len(d) != 1 || d[0].ChannelName != "ops" || d[0].Delivered {
		t.Errorf("a's delivery log = %+v, want the webhook's attempt, cut short", d)
	}
	if n := len(f.deliveries(outages[1].Incident.ID)); n != 0 {
		t.Errorf("b's delivery log has %d entries, want none", n)
	}

	// The next start.
	sender.release("ops/a")
	f.engine = New(f.db, sender, f.engine.opts)
	f.flush()
	if got := f.sender.log(); got != "ops:down,ops:down,chat:down[a b]" {
		t.Errorf("at the next start: %q, want both outages to the webhook and to chat", got)
	}
}
