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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// recorder is a Sender that remembers what it was asked to send.
type recorder struct {
	mu       sync.Mutex
	sent     []Notification
	channels []string
	failures int // number of upcoming sends that fail
}

func (r *recorder) Send(_ context.Context, ch store.Channel, n Notification) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failures > 0 {
		r.failures--
		return errors.New("boom")
	}
	r.sent = append(r.sent, n)
	r.channels = append(r.channels, ch.Name)
	return nil
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
	if _, err := db.CreateChannel(ctx, store.Channel{Name: "ops", Type: store.ChannelWebhook, URL: "http://unused", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	sender := &recorder{}
	engine := New(db, sender, Options{
		RecoveryThreshold: recoveryThreshold,
		QueueSize:         10,
		Attempts:          3,
		RetryDelay:        time.Millisecond,
		SendTimeout:       time.Second,
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

// flush runs the delivery loop until the queue is empty.
func (f *fixture) flush() {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f.engine.Run(ctx)
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
	if _, err := f.db.CreateChannel(ctx, store.Channel{Name: "off", Type: store.ChannelWebhook, URL: "http://unused", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.CreateChannel(ctx, store.Channel{Name: "second", Type: store.ChannelWebhook, URL: "http://unused", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	f.sender.failures = 2 // first two tries to "ops" fail, the third succeeds
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
	ch := store.Channel{Name: "ops", Type: store.ChannelWebhook, URL: srv.URL}
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
	if b["event"] != "up" || !strings.Contains(text, "1m30s") || incident["resolved_at"] != "2026-10-12T12:01:30Z" {
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
	if err := sender.Send(ctx, store.Channel{URL: "http://127.0.0.1:1"}, down); err == nil {
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
