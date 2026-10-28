package alert

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// recorder is a Sender that remembers what it was asked to send. The
// channels are sent to in parallel (Engine.deliver), so the order in which
// their messages arrive within one pass is nothing to count on: settle puts
// what arrived in channel order, and flush does so after every run of the
// loop. A channel's own messages keep their order, as the engine keeps it.
type recorder struct {
	mu       sync.Mutex
	sent     []Notification
	channels []string
	ids      []int64 // the channels' IDs, what settle orders by
	settled  int     // how much of sent is in channel order
	failures int     // number of upcoming sends that fail
	// failuresOf is the number of upcoming sends to a channel, by name,
	// that fail while the other channels take theirs (fail).
	failuresOf map[string]int
}

func (r *recorder) Send(_ context.Context, ch store.Channel, n Notification) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failuresOf[ch.Name] > 0 {
		r.failuresOf[ch.Name]--
		return errors.New("boom")
	}
	if r.failures > 0 {
		r.failures--
		return errors.New("boom")
	}
	r.sent = append(r.sent, n)
	r.channels = append(r.channels, ch.Name)
	r.ids = append(r.ids, ch.ID)
	return nil
}

// settle puts what arrived since the last settle in the order the channels
// are listed in (by ID), which is the order the loop sent to them in before
// it sent in parallel; a channel's own messages keep their order.
func (r *recorder) settle() {
	r.mu.Lock()
	defer r.mu.Unlock()
	order := make([]int, len(r.sent)-r.settled)
	for i := range order {
		order[i] = r.settled + i
	}
	sort.SliceStable(order, func(a, b int) bool { return r.ids[order[a]] < r.ids[order[b]] })
	sent, channels, ids := make([]Notification, len(order)), make([]string, len(order)), make([]int64, len(order))
	for i, j := range order {
		sent[i], channels[i], ids[i] = r.sent[j], r.channels[j], r.ids[j]
	}
	copy(r.sent[r.settled:], sent)
	copy(r.channels[r.settled:], channels)
	copy(r.ids[r.settled:], ids)
	r.settled = len(r.sent)
}

// fail makes the next n sends to the named channel fail, as a target that
// answers with an error does; the other channels take theirs.
func (r *recorder) fail(channel string, n int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failuresOf == nil {
		r.failuresOf = make(map[string]int)
	}
	r.failuresOf[channel] = n
}

// reset forgets what was sent so far.
func (r *recorder) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent, r.channels, r.ids, r.settled = nil, nil, nil, 0
}

func (r *recorder) events() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	events := make([]Event, len(r.sent))
	for i, n := range r.sent {
		events[i] = n.Event
	}
	return events
}

type fixture struct {
	t       *testing.T
	db      *store.SQLite
	engine  *Engine
	sender  *recorder
	monitor store.Monitor
	clock   time.Time
}

func newFixture(t *testing.T, failureThreshold, recoveryThreshold int) *fixture {
	t.Helper()
	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, filepath.Join(t.TempDir(), "test.db"), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	m, err := db.CreateMonitor(ctx, store.Monitor{
		Name: "api", Type: store.MonitorHTTP, Target: "https://example.com",
		Interval: 30 * time.Second, Timeout: 5 * time.Second, FailureThreshold: failureThreshold, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateChannel(ctx, store.Channel{Name: "ops", Type: store.ChannelWebhook, Target: "http://unused", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	sender := &recorder{}
	engine := New(db, sender, Options{
		RecoveryThreshold: recoveryThreshold,
		CertWarning:       14 * 24 * time.Hour,
		QueueSize:         10,
		Attempts:          3,
		RetryDelay:        time.Millisecond,
		SendTimeout:       time.Second,
		Language:          LanguageTurkish,
		Logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	return &fixture{t: t, db: db, engine: engine, sender: sender, monitor: m,
		clock: time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)}
}

// check stores one result and evaluates it, as the scheduler does.
func (f *fixture) check(up bool) store.CheckResult {
	f.t.Helper()
	f.clock = f.clock.Add(30 * time.Second)
	r := store.CheckResult{MonitorID: f.monitor.ID, CheckedAt: f.clock, Up: up, Latency: time.Millisecond}
	if !up {
		r.Error = "connection refused"
	}
	if err := f.db.InsertCheckResults(context.Background(), []store.CheckResult{r}); err != nil {
		f.t.Fatal(err)
	}
	f.engine.Evaluate(context.Background(), []store.CheckResult{r})
	return r
}

func (f *fixture) incidents() []store.Incident {
	f.t.Helper()
	list, err := f.db.ListIncidents(context.Background(), 0, 100)
	if err != nil {
		f.t.Fatal(err)
	}
	return list
}

// flush runs the delivery loop until the queue is empty, and settles what
// it sent into channel order.
func (f *fixture) flush() {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f.engine.Run(ctx)
	f.sender.settle()
}

func TestIncidentOpensOnlyAfterThresholdConsecutiveFailures(t *testing.T) {
	f := newFixture(t, 3, 1)

	f.check(false)
	f.check(false)
	f.check(true) // breaks the streak
	f.check(false)
	second := f.check(false)
	if got := f.incidents(); len(got) != 0 {
		t.Fatalf("incident opened before 3 consecutive failures: %+v", got)
	}
	_ = second

	f.check(false)
	got := f.incidents()
	if len(got) != 1 || !got[0].Open() || got[0].Cause != "connection refused" {
		t.Fatalf("incidents = %+v, want one open incident", got)
	}
	// The incident starts at the first failure of the streak, not at detection.
	if want := f.clock.Add(-60 * time.Second); !got[0].StartedAt.Equal(want) {
		t.Errorf("StartedAt = %v, want %v", got[0].StartedAt, want)
	}

	// Further failures neither open another incident nor notify again.
	f.check(false)
	f.check(false)
	if got := f.incidents(); len(got) != 1 {
		t.Errorf("incidents after more failures = %d, want 1", len(got))
	}
	f.flush()
	if events := f.sender.events(); len(events) != 1 || events[0] != EventDown {
		t.Errorf("notifications = %v, want [down]", events)
	}
}

func TestIncidentResolvesAndNotifies(t *testing.T) {
	f := newFixture(t, 2, 1)

	f.check(false)
	f.check(false)
	recovered := f.check(true)

	got := f.incidents()
	if len(got) != 1 || got[0].Open() || !got[0].ResolvedAt.Equal(recovered.CheckedAt) {
		t.Fatalf("incidents = %+v, want one resolved at %v", got, recovered.CheckedAt)
	}

	f.flush()
	if events := f.sender.events(); len(events) != 2 || events[0] != EventDown || events[1] != EventUp {
		t.Fatalf("notifications = %v, want [down up]", events)
	}
	up := f.sender.sent[1]
	if up.Incident.ID != got[0].ID || up.Incident.Open() || up.Monitor.Name != "api" {
		t.Errorf("up notification = %+v", up)
	}

	// A new outage opens a second incident.
	f.check(false)
	f.check(false)
	if got := f.incidents(); len(got) != 2 || !got[0].Open() {
		t.Errorf("incidents after second outage = %+v", got)
	}
}

func TestRecoveryThresholdSuppressesFlapping(t *testing.T) {
	f := newFixture(t, 2, 3)

	f.check(false)
	f.check(false) // opens
	f.check(true)
	f.check(true)
	f.check(false) // flap: recovery streak broken
	f.check(true)
	f.check(true)
	if got := f.incidents(); len(got) != 1 || !got[0].Open() {
		t.Fatalf("incident resolved before 3 consecutive successes: %+v", got)
	}

	f.check(true)
	got := f.incidents()
	if len(got) != 1 || got[0].Open() {
		t.Fatalf("incident not resolved after 3 consecutive successes: %+v", got)
	}
	if want := f.clock.Add(-60 * time.Second); !got[0].ResolvedAt.Equal(want) {
		t.Errorf("ResolvedAt = %v, want first success of the streak %v", got[0].ResolvedAt, want)
	}

	f.flush()
	if events := f.sender.events(); len(events) != 2 {
		t.Errorf("notifications = %v, want exactly [down up]", events)
	}
}

func TestStateSurvivesRestart(t *testing.T) {
	f := newFixture(t, 2, 1)
	f.check(false)
	f.check(false)

	// A new engine over the same database, as after a process restart.
	f.engine = New(f.db, f.sender, f.engine.opts)
	f.check(false)
	if got := f.incidents(); len(got) != 1 {
		t.Fatalf("restart duplicated the incident: %+v", got)
	}
	f.check(true)
	if got := f.incidents(); got[0].Open() {
		t.Error("incident opened before restart was not resolved after it")
	}
}

func TestDeliveryRetriesAndSkipsDisabledChannels(t *testing.T) {
	f := newFixture(t, 1, 1)
	ctx := context.Background()
	if _, err := f.db.CreateChannel(ctx, store.Channel{Name: "off", Type: store.ChannelWebhook, Target: "http://unused", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.CreateChannel(ctx, store.Channel{Name: "second", Type: store.ChannelWebhook, Target: "http://unused", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	f.sender.failures = 2 // the first two tries, to either channel, fail; the retries succeed
	f.check(false)

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.engine.Run(runCtx)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for len(f.sender.events()) < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	f.sender.settle()

	f.sender.mu.Lock()
	defer f.sender.mu.Unlock()
	if strings.Join(f.sender.channels, ",") != "ops,second" {
		t.Errorf("delivered to %v, want [ops second]", f.sender.channels)
	}
}

func TestDeliveryGivesUpAfterAllAttempts(t *testing.T) {
	f := newFixture(t, 1, 1)
	f.sender.failures = 3
	f.check(false)

	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.engine.Run(runCtx)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		f.sender.mu.Lock()
		left := f.sender.failures
		f.sender.mu.Unlock()
		if left == 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond) // a fourth try would land here
	cancel()
	<-done

	if events := f.sender.events(); len(events) != 0 {
		t.Errorf("sent after 3 failed attempts: %v", events)
	}
}

func TestQueueOverflowDropsInsteadOfBlocking(t *testing.T) {
	f := newFixture(t, 1, 1)
	f.engine.opts.QueueSize = 1
	f.engine.queue = make(chan Notification, 1)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 5; i++ { // nobody is running the delivery loop
			f.check(false)
			f.check(true)
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Evaluate blocked on a full notification queue")
	}
	if got := f.incidents(); len(got) != 5 {
		t.Errorf("incidents = %d, want 5 even though notifications were dropped", len(got))
	}
}

func TestWebhookSender(t *testing.T) {
	var (
		mu     sync.Mutex
		bodies []map[string]any
		status = http.StatusOK
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("User-Agent") != "kubeat/test" {
			t.Errorf("request: %s, Content-Type=%q, User-Agent=%q", r.Method, r.Header.Get("Content-Type"), r.Header.Get("User-Agent"))
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("body is not JSON: %v", err)
		}
		bodies = append(bodies, body)
		w.WriteHeader(status)
	}))
	defer srv.Close()

	sender := &WebhookSender{Client: srv.Client(), UserAgent: "kubeat/test"}
	ch := store.Channel{Name: "ops", Type: store.ChannelWebhook, Target: srv.URL}
	started := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	m := store.Monitor{ID: 7, Name: "api", Type: store.MonitorHTTP, Target: "https://example.com", FailureThreshold: 3}
	ctx := context.Background()

	down := Notification{Event: EventDown, Monitor: m, Incident: store.Incident{ID: 4, StartedAt: started, Cause: "timeout"}}
	if err := sender.Send(ctx, ch, down); err != nil {
		t.Fatalf("send down: %v", err)
	}
	up := down
	up.Event = EventUp
	up.Incident.ResolvedAt = started.Add(90 * time.Second)
	if err := sender.Send(ctx, ch, up); err != nil {
		t.Fatalf("send up: %v", err)
	}
	if err := sender.Send(ctx, ch, Notification{Event: EventTest}); err != nil {
		t.Fatalf("send test: %v", err)
	}

	if len(bodies) != 3 {
		t.Fatalf("received %d requests, want 3", len(bodies))
	}
	b := bodies[0]
	text, _ := b["text"].(string)
	if b["event"] != "down" || !strings.Contains(text, "api") || !strings.Contains(text, "timeout") || !strings.Contains(text, "3") {
		t.Errorf("down body = %v", b)
	}
	incident, _ := b["incident"].(map[string]any)
	monitor, _ := b["monitor"].(map[string]any)
	if incident["resolved_at"] != nil || incident["cause"] != "timeout" || monitor["target"] != "https://example.com" {
		t.Errorf("down body structure = %v", b)
	}

	b = bodies[1]
	text, _ = b["text"].(string)
	incident, _ = b["incident"].(map[string]any)
	if b["event"] != "up" || !strings.Contains(text, "1m 30s") || incident["resolved_at"] != "2026-10-12T12:01:30Z" {
		t.Errorf("up body = %v", b)
	}

	if b = bodies[2]; b["event"] != "test" || b["monitor"] != nil || b["incident"] != nil || b["text"] == "" {
		t.Errorf("test body = %v", b)
	}

	mu.Lock()
	status = http.StatusInternalServerError
	mu.Unlock()
	if err := sender.Send(ctx, ch, down); err == nil {
		t.Error("a 500 answer was treated as delivered")
	}
	if err := sender.Send(ctx, store.Channel{Target: "http://127.0.0.1:1"}, down); err == nil {
		t.Error("an unreachable webhook was treated as delivered")
	}
}

func TestPausedMonitorOpensNoIncident(t *testing.T) {
	f := newFixture(t, 2, 1)
	f.check(false)

	paused := f.monitor
	paused.Enabled = false
	if _, err := f.db.UpdateMonitor(context.Background(), paused); err != nil {
		t.Fatal(err)
	}
	f.check(false) // a result from a check that was in flight when paused
	f.check(false)

	if got := f.incidents(); len(got) != 0 {
		t.Errorf("paused monitor opened an incident: %+v", got)
	}
}

// checkCert stores a successful HTTPS result whose certificate has the given
// time left, and evaluates it.
func (f *fixture) checkCert(remaining time.Duration) store.CheckResult {
	f.t.Helper()
	f.clock = f.clock.Add(30 * time.Second)
	r := store.CheckResult{MonitorID: f.monitor.ID, CheckedAt: f.clock, Up: true, Latency: time.Millisecond, StatusCode: 200}
	if remaining != 0 {
		r.CertExpiresAt = f.clock.Add(remaining)
	}
	if err := f.db.InsertCheckResults(context.Background(), []store.CheckResult{r}); err != nil {
		f.t.Fatal(err)
	}
	f.engine.Evaluate(context.Background(), []store.CheckResult{r})
	return r
}

func TestCertificateWarning(t *testing.T) {
	f := newFixture(t, 3, 1)
	day := 24 * time.Hour

	f.checkCert(60 * day)
	if got := f.incidents(); len(got) != 0 {
		t.Fatalf("incident for a certificate with 60 days left: %+v", got)
	}

	first := f.checkCert(10 * day)
	f.checkCert(10 * day) // still expiring: no second incident, no second notification
	f.checkCert(0)        // a result without certificate info changes nothing
	got := f.incidents()
	if len(got) != 1 || got[0].Kind != store.IncidentCert || !got[0].Open() || !got[0].StartedAt.Equal(first.CheckedAt) {
		t.Fatalf("incidents = %+v, want one open cert incident", got)
	}
	if want := "certificate expires on " + first.CertExpiresAt.Format(time.RFC3339); got[0].Cause != want {
		t.Errorf("cause = %q, want %q", got[0].Cause, want)
	}

	// An outage is tracked separately and does not close the warning.
	f.check(false)
	f.check(false)
	f.check(false)
	if got := f.incidents(); len(got) != 2 {
		t.Fatalf("incidents during an outage = %+v, want cert + down", got)
	}
	f.check(true)

	renewed := f.checkCert(90 * day)
	for _, i := range f.incidents() {
		if i.Open() {
			t.Errorf("incident still open after renewal and recovery: %+v", i)
		}
		if i.Kind == store.IncidentCert && !i.ResolvedAt.Equal(renewed.CheckedAt) {
			t.Errorf("cert incident resolved at %s, want %s", i.ResolvedAt, renewed.CheckedAt)
		}
	}

	f.flush()
	events := f.sender.events()
	want := []Event{EventCertExpiring, EventDown, EventUp, EventCertRenewed}
	if len(events) != len(want) {
		t.Fatalf("notifications = %v, want %v", events, want)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Errorf("notification %d = %s, want %s", i, events[i], want[i])
		}
	}
	if n := f.sender.sent[0]; !n.CertExpiresAt.Equal(first.CertExpiresAt) || !strings.Contains(Text(n), "10 gün sonra") {
		t.Errorf("expiring notification = %+v, text %q", n, Text(n))
	}
	if text := Text(f.sender.sent[3]); !strings.Contains(text, "yenilendi") {
		t.Errorf("renewed text = %q", text)
	}
}

func TestCertificateTextForImminentAndPastExpiry(t *testing.T) {
	start := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	n := Notification{Event: EventCertExpiring, Language: LanguageTurkish, Monitor: store.Monitor{Name: "api"}, Incident: store.Incident{StartedAt: start}}

	n.CertExpiresAt = start.Add(5 * time.Hour)
	if text := Text(n); !strings.Contains(text, "24 saat içinde") {
		t.Errorf("5 hours left: %q", text)
	}
	n.CertExpiresAt = start.Add(-time.Hour)
	if text := Text(n); !strings.Contains(text, "süresi doldu") {
		t.Errorf("already expired: %q", text)
	}
}

// maintain puts the fixture's monitor in maintenance from the current clock
// for the given length.
func (f *fixture) maintain(length time.Duration) {
	f.t.Helper()
	_, err := f.db.CreateMaintenance(context.Background(), store.Maintenance{
		Name: "deploy", MonitorID: f.monitor.ID, StartsAt: f.clock, EndsAt: f.clock.Add(length),
	})
	if err != nil {
		f.t.Fatal(err)
	}
}

func TestMaintenanceSuppressesNewIncidents(t *testing.T) {
	f := newFixture(t, 2, 1)
	f.maintain(2 * time.Minute) // covers the next four 30-second checks

	f.check(false)
	f.check(false)
	f.check(false)
	if got := f.incidents(); len(got) != 0 {
		t.Fatalf("incident opened during maintenance: %+v", got)
	}

	// Still failing when the window is over: the incident opens then.
	f.check(false) // t+2m: the window has just ended
	got := f.incidents()
	if len(got) != 1 || !got[0].Open() {
		t.Fatalf("incidents after maintenance = %+v, want one open", got)
	}
	f.flush()
	if events := f.sender.events(); len(events) != 1 || events[0] != EventDown {
		t.Errorf("notifications = %v, want [down] sent after the window", events)
	}
}

func TestMaintenanceStillResolvesOpenIncidents(t *testing.T) {
	f := newFixture(t, 2, 1)
	f.check(false)
	f.check(false) // opens before the window
	f.maintain(time.Hour)

	f.check(false)
	f.check(true)
	if got := f.incidents(); len(got) != 1 || got[0].Open() {
		t.Fatalf("incident opened before maintenance did not resolve in it: %+v", got)
	}
	// A new failure inside the window stays silent.
	f.check(false)
	f.check(false)
	if got := f.incidents(); len(got) != 1 {
		t.Errorf("new incident during maintenance: %+v", got)
	}

	f.flush()
	if events := f.sender.events(); len(events) != 2 || events[1] != EventUp {
		t.Errorf("notifications = %v, want [down up]", events)
	}
}

func TestMaintenanceSuppressesCertificateWarnings(t *testing.T) {
	f := newFixture(t, 3, 1)
	f.maintain(time.Minute)
	f.checkCert(24 * time.Hour)
	if got := f.incidents(); len(got) != 0 {
		t.Fatalf("certificate incident during maintenance: %+v", got)
	}
	f.checkCert(24 * time.Hour) // t+1m: window over
	if got := f.incidents(); len(got) != 1 || got[0].Kind != store.IncidentCert {
		t.Errorf("after maintenance = %+v, want the certificate incident", got)
	}
}

func TestEvaluateUsesNewestResultOfABatch(t *testing.T) {
	f := newFixture(t, 1, 1)
	f.maintain(45 * time.Second)

	// Two results in one batch: the first inside the window, the second
	// after it. The monitor is evaluated once, as of the newer one.
	inside := store.CheckResult{MonitorID: f.monitor.ID, CheckedAt: f.clock.Add(30 * time.Second), Error: "x"}
	after := store.CheckResult{MonitorID: f.monitor.ID, CheckedAt: f.clock.Add(60 * time.Second), Error: "x"}
	batch := []store.CheckResult{inside, after}
	if err := f.db.InsertCheckResults(context.Background(), batch); err != nil {
		t.Fatal(err)
	}
	f.engine.Evaluate(context.Background(), batch)
	if got := f.incidents(); len(got) != 1 {
		t.Errorf("incidents = %+v, want one: the newest result is outside the window", got)
	}
}

func TestReminders(t *testing.T) {
	f := newFixture(t, 2, 1)
	f.engine.opts.ReminderInterval = 2 * time.Minute // four 30-second checks

	f.check(false)
	f.check(false) // opens; the "down" notification counts as the first
	for i := 0; i < 3; i++ {
		f.check(false)
	}
	f.flush()
	if events := f.sender.events(); len(events) != 1 {
		t.Fatalf("notifications 90s into the outage = %v, want only [down]", events)
	}

	f.check(false) // 2 minutes after the incident was announced
	f.flush()
	events := f.sender.events()
	if len(events) != 2 || events[1] != EventReminder {
		t.Fatalf("notifications = %v, want [down reminder]", events)
	}
	reminder := f.sender.sent[1]
	if text := Text(reminder); !strings.Contains(text, "hâlâ erişilemiyor") || !strings.Contains(text, "2 dk 30 sn") {
		t.Errorf("reminder text = %q (the outage is 2m30s old)", text)
	}

	// The next one comes a full interval later, not on every check.
	for i := 0; i < 3; i++ {
		f.check(false)
	}
	f.flush()
	if n := len(f.sender.events()); n != 2 {
		t.Fatalf("%d notifications before the second interval elapsed, want 2", n)
	}
	f.check(false)
	f.flush()
	if n := len(f.sender.events()); n != 3 {
		t.Fatalf("%d notifications after the second interval, want 3", n)
	}

	// Recovery ends the reminders.
	f.check(true)
	for i := 0; i < 6; i++ {
		f.check(true)
	}
	f.flush()
	events = f.sender.events()
	if len(events) != 4 || events[3] != EventUp {
		t.Errorf("notifications after recovery = %v, want [down reminder reminder up]", events)
	}
}

func TestNoRemindersByDefaultOrInMaintenance(t *testing.T) {
	f := newFixture(t, 1, 1) // ReminderInterval is zero
	f.check(false)
	for i := 0; i < 20; i++ {
		f.check(false)
	}
	f.flush()
	if events := f.sender.events(); len(events) != 1 {
		t.Fatalf("reminders sent with the interval at zero: %v", events)
	}

	f.engine.opts.ReminderInterval = time.Minute
	f.maintain(time.Hour)
	for i := 0; i < 10; i++ {
		f.check(false)
	}
	f.flush()
	if events := f.sender.events(); len(events) != 1 {
		t.Errorf("reminders sent during maintenance: %v", events)
	}
}

// A channel limited to some namespaces hears only about their monitors.
func TestDeliveryFollowsTheChannelScope(t *testing.T) {
	f := newFixture(t, 1, 1) // its monitor has no namespace; "ops" has no scope
	ctx := context.Background()
	for _, ch := range []store.Channel{
		{Name: "shop-team", Namespaces: []string{"shop"}},
		{Name: "billing-team", Namespaces: []string{"billing", "payments"}},
	} {
		ch.Type, ch.Target, ch.Enabled = store.ChannelWebhook, "http://unused", true
		if _, err := f.db.CreateChannel(ctx, ch); err != nil {
			t.Fatal(err)
		}
	}

	deliver := func(namespace string) string {
		f.sender.reset()
		f.engine.deliverTo(ctx, Notification{Event: EventDown, Monitor: store.Monitor{Name: "m", Namespace: namespace}}, nil)
		f.sender.settle()
		f.sender.mu.Lock()
		defer f.sender.mu.Unlock()
		return strings.Join(f.sender.channels, ",")
	}
	for namespace, want := range map[string]string{
		"":         "ops",
		"shop":     "ops,shop-team",
		"payments": "ops,billing-team",
		"other":    "ops",
	} {
		if got := deliver(namespace); got != want {
			t.Errorf("namespace %q: delivered to %q, want %q", namespace, got, want)
		}
	}
}
