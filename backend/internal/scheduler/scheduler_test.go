package scheduler

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/heartbeat"
	"github.com/yusufceng/Kubeat/backend/internal/metrics"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

type fakeStore struct {
	mu       sync.Mutex
	monitors []store.Monitor
	results  []store.CheckResult
	// rejectID makes any batch containing that monitor fail, like a foreign
	// key violation does.
	rejectID int64
}

func (f *fakeStore) ListMonitors(context.Context) ([]store.Monitor, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]store.Monitor(nil), f.monitors...), nil
}

func (f *fakeStore) InsertCheckResults(_ context.Context, batch []store.CheckResult) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range batch {
		if r.MonitorID == f.rejectID {
			return errors.New("constraint failed")
		}
	}
	f.results = append(f.results, batch...)
	return nil
}

func (f *fakeStore) setMonitors(ms ...store.Monitor) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.monitors = ms
}

func (f *fakeStore) countFor(id int64) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.results {
		if r.MonitorID == id {
			n++
		}
	}
	return n
}

type fakeChecker struct {
	delay time.Duration
	// hops is what every result reports as made; none reports one request
	// to the monitor's host, whose address is not known.
	hops []store.Hop

	mu         sync.Mutex
	running    int
	maxRunning int
}

func (f *fakeChecker) Check(ctx context.Context, m store.Monitor) store.CheckResult {
	f.mu.Lock()
	f.running++
	f.maxRunning = max(f.maxRunning, f.running)
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.running--
		f.mu.Unlock()
	}()

	select {
	case <-time.After(f.delay):
	case <-ctx.Done():
		return store.CheckResult{MonitorID: m.ID, Error: ctx.Err().Error()}
	}
	hops := f.hops
	if len(hops) == 0 {
		hops = []store.Hop{{Host: m.Host()}}
	}
	return store.CheckResult{MonitorID: m.ID, Up: true, Hops: hops}
}

func monitor(id int64, interval time.Duration) store.Monitor {
	return store.Monitor{ID: id, Type: store.MonitorHTTP, Interval: interval, Timeout: time.Second, Enabled: true}
}

func testOptions() Options {
	return Options{
		Tick:          2 * time.Millisecond,
		Workers:       4,
		BatchSize:     10,
		FlushInterval: 5 * time.Millisecond,
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// start runs the scheduler and returns a function that stops it and waits.
func start(st Store, c Checker, opts Options) (stop func()) {
	_, stop = startScheduler(st, c, opts)
	return stop
}

func startScheduler(st Store, c Checker, opts Options) (*Scheduler, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s := New(st, c, opts)
	go func() {
		defer close(done)
		s.Run(ctx)
	}()
	return s, func() {
		cancel()
		<-done
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestChecksRepeatAtInterval(t *testing.T) {
	st := &fakeStore{}
	st.setMonitors(monitor(1, 10*time.Millisecond), monitor(2, 10*time.Millisecond))
	stop := start(st, &fakeChecker{}, testOptions())
	defer stop()

	waitFor(t, "repeated results for both monitors", func() bool {
		return st.countFor(1) >= 3 && st.countFor(2) >= 3
	})
}

func TestDisabledAndDeletedMonitorsAreNotChecked(t *testing.T) {
	disabled := monitor(2, 5*time.Millisecond)
	disabled.Enabled = false
	st := &fakeStore{}
	st.setMonitors(monitor(1, 5*time.Millisecond), disabled, monitor(3, 5*time.Millisecond))
	stop := start(st, &fakeChecker{}, testOptions())
	defer stop()

	waitFor(t, "results for monitors 1 and 3", func() bool { return st.countFor(1) >= 2 && st.countFor(3) >= 2 })

	st.setMonitors(monitor(1, 5*time.Millisecond), disabled)
	// Let checks that were already queued finish before sampling.
	time.Sleep(30 * time.Millisecond)
	before := st.countFor(3)
	waitFor(t, "more results for monitor 1", func() bool { return st.countFor(1) >= before+5 })

	if got := st.countFor(3); got != before {
		t.Errorf("deleted monitor kept being checked: %d -> %d results", before, got)
	}
	if got := st.countFor(2); got != 0 {
		t.Errorf("disabled monitor was checked %d times", got)
	}
}

func TestMonitorAddedWhileRunningIsPickedUp(t *testing.T) {
	st := &fakeStore{}
	stop := start(st, &fakeChecker{}, testOptions())
	defer stop()

	st.setMonitors(monitor(1, 5*time.Millisecond))
	waitFor(t, "result for the added monitor", func() bool { return st.countFor(1) >= 1 })
}

func TestWorkerPoolBoundsConcurrency(t *testing.T) {
	st := &fakeStore{}
	var ms []store.Monitor
	for id := int64(1); id <= 20; id++ {
		ms = append(ms, monitor(id, 5*time.Millisecond))
	}
	st.setMonitors(ms...)
	checker := &fakeChecker{delay: 10 * time.Millisecond}
	opts := testOptions()
	opts.Workers = 3
	stop := start(st, checker, opts)

	waitFor(t, "every monitor to be checked", func() bool {
		for _, m := range ms {
			if st.countFor(m.ID) == 0 {
				return false
			}
		}
		return true
	})
	stop()

	if checker.maxRunning > 3 {
		t.Errorf("max concurrent checks = %d, want <= 3", checker.maxRunning)
	}
}

func TestSlowCheckDoesNotOverlapItself(t *testing.T) {
	st := &fakeStore{}
	st.setMonitors(monitor(1, time.Millisecond))
	checker := &fakeChecker{delay: 20 * time.Millisecond}
	stop := start(st, checker, testOptions())

	waitFor(t, "several results", func() bool { return st.countFor(1) >= 3 })
	stop()

	if checker.maxRunning != 1 {
		t.Errorf("max concurrent checks of one monitor = %d, want 1", checker.maxRunning)
	}
}

func TestShutdownFlushesResultsAndDropsInterruptedChecks(t *testing.T) {
	st := &fakeStore{}
	st.setMonitors(monitor(1, 5*time.Millisecond), monitor(2, 5*time.Millisecond))
	opts := testOptions()
	opts.FlushInterval = time.Hour // only the shutdown flush can write
	opts.BatchSize = 1000
	checker := &slowFor{slowID: 2, fakeChecker: fakeChecker{}}
	stop := start(st, checker, opts)

	waitFor(t, "checks of monitor 1 to run", func() bool { return checker.completed() >= 3 })
	if got := st.countFor(1); got != 0 {
		t.Fatalf("results written before flush: %d", got)
	}
	stop()

	if got := st.countFor(1); got < 3 {
		t.Errorf("results after shutdown = %d, want >= 3", got)
	}
	if got := st.countFor(2); got != 0 {
		t.Errorf("check interrupted by shutdown was recorded %d times", got)
	}
}

// slowFor blocks until cancelled for one monitor and answers at once for the rest.
type slowFor struct {
	fakeChecker
	slowID int64

	mu   sync.Mutex
	done int
}

func (s *slowFor) Check(ctx context.Context, m store.Monitor) store.CheckResult {
	if m.ID == s.slowID {
		<-ctx.Done()
		return store.CheckResult{MonitorID: m.ID, Error: ctx.Err().Error()}
	}
	s.mu.Lock()
	s.done++
	s.mu.Unlock()
	return store.CheckResult{MonitorID: m.ID, Up: true}
}

func (s *slowFor) completed() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.done
}

func TestBadRowDoesNotLoseTheBatch(t *testing.T) {
	st := &fakeStore{rejectID: 2}
	st.setMonitors(monitor(1, 5*time.Millisecond), monitor(2, 5*time.Millisecond))
	stop := start(st, &fakeChecker{}, testOptions())
	defer stop()

	waitFor(t, "results for the good monitor", func() bool { return st.countFor(1) >= 5 })
	if got := st.countFor(2); got != 0 {
		t.Errorf("rejected monitor has %d results", got)
	}
}

func TestJitterSpreadsFirstChecks(t *testing.T) {
	if got := jitter(0); got != 0 {
		t.Errorf("jitter(0) = %v, want 0", got)
	}
	seen := make(map[time.Duration]bool)
	for i := 0; i < 50; i++ {
		d := jitter(time.Second)
		if d < 0 || d >= time.Second {
			t.Fatalf("jitter out of range: %v", d)
		}
		seen[d] = true
	}
	if len(seen) < 25 {
		t.Errorf("only %d distinct jitter values in 50 draws", len(seen))
	}
}

type recordingPruner struct {
	mu      sync.Mutex
	cutoffs []time.Time
}

func (r *recordingPruner) DeleteCheckResultsBefore(_ context.Context, cutoff time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cutoffs = append(r.cutoffs, cutoff)
	return 1, nil
}

func TestRetentionRunsImmediatelyAndRepeats(t *testing.T) {
	p := &recordingPruner{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	started := time.Now()
	go func() {
		defer close(done)
		RunRetention(ctx, p, time.Hour, 5*time.Millisecond, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()

	waitFor(t, "repeated cleanups", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return len(p.cutoffs) >= 3
	})
	cancel()
	<-done

	want := started.Add(-time.Hour)
	if d := p.cutoffs[0].Sub(want); d < 0 || d > 5*time.Second {
		t.Errorf("cutoff = %v, want about %v", p.cutoffs[0], want)
	}
}

func (r *recordingPruner) DeleteDeliveriesBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	return r.DeleteCheckResultsBefore(ctx, cutoff)
}

// The delivery log is pruned like the results: at start, then every
// interval, by its own retention.
func TestDeliveryRetentionRunsImmediatelyAndRepeats(t *testing.T) {
	p := &recordingPruner{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	started := time.Now()
	go func() {
		defer close(done)
		RunDeliveryRetention(ctx, p, 30*24*time.Hour, 5*time.Millisecond, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()

	waitFor(t, "repeated cleanups", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return len(p.cutoffs) >= 3
	})
	cancel()
	<-done

	want := started.Add(-30 * 24 * time.Hour)
	if d := p.cutoffs[0].Sub(want); d < 0 || d > 5*time.Second {
		t.Errorf("cutoff = %v, want about %v", p.cutoffs[0], want)
	}
}

func TestOnResultsSeesOnlyStoredResults(t *testing.T) {
	st := &fakeStore{rejectID: 2}
	st.setMonitors(monitor(1, 5*time.Millisecond), monitor(2, 5*time.Millisecond))

	var mu sync.Mutex
	seen := map[int64]int{}
	opts := testOptions()
	opts.OnResults = func(_ context.Context, results []store.CheckResult) {
		mu.Lock()
		defer mu.Unlock()
		for _, r := range results {
			seen[r.MonitorID]++
		}
	}
	stop := start(st, &fakeChecker{}, opts)

	waitFor(t, "callbacks for the stored monitor", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return seen[1] >= 5
	})
	stop()

	if seen[2] != 0 {
		t.Errorf("callback saw %d results that were never stored", seen[2])
	}
	if seen[1] != st.countFor(1) {
		t.Errorf("callback saw %d results, store has %d", seen[1], st.countFor(1))
	}
}

type failingLister struct{ fakeStore }

func (f *failingLister) ListMonitors(context.Context) ([]store.Monitor, error) {
	return nil, errors.New("database is locked")
}

func TestLastTickAdvancesOnlyWhileTheStoreAnswers(t *testing.T) {
	st := &fakeStore{}
	st.setMonitors(monitor(1, 5*time.Millisecond))
	s, stop := startScheduler(st, &fakeChecker{}, testOptions())
	if !s.LastTick().IsZero() && time.Since(s.LastTick()) > time.Minute {
		t.Errorf("LastTick before any tick = %s", s.LastTick())
	}
	waitFor(t, "ticks", func() bool { return !s.LastTick().IsZero() })
	first := s.LastTick()
	waitFor(t, "more ticks", func() bool { return s.LastTick().After(first) })
	stop()

	// A scheduler that cannot read its monitors is not doing its job, and
	// its tick time must say so.
	broken, stop := startScheduler(&failingLister{}, &fakeChecker{}, testOptions())
	time.Sleep(30 * time.Millisecond)
	stop()
	if !broken.LastTick().IsZero() {
		t.Errorf("LastTick advanced although the store never answered: %s", broken.LastTick())
	}
}

func TestSchedulerRecordsMetrics(t *testing.T) {
	st := &fakeStore{rejectID: 2}
	st.setMonitors(monitor(1, 5*time.Millisecond), monitor(2, 5*time.Millisecond))
	m := metrics.New("test")
	opts := testOptions()
	opts.Metrics = m
	stop := start(st, &fakeChecker{}, opts)
	// Both monitors run every 5ms, but not in step: when the first has
	// five results stored, the second may have run only four times.
	waitFor(t, "stored results and checks of both monitors", func() bool {
		got, err := m.Gather()
		return err == nil && st.countFor(1) >= 5 && got["kubeat_checks_total{result=up}{type=http}"] >= 10
	})
	stop()

	got, err := m.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if got["kubeat_checks_total{result=up}{type=http}"] < 10 {
		t.Errorf("checks_total = %v, want at least 10", got["kubeat_checks_total{result=up}{type=http}"])
	}
	if got["kubeat_checks_in_flight"] != 0 {
		t.Errorf("checks_in_flight after stop = %v, want 0", got["kubeat_checks_in_flight"])
	}
	if written := got["kubeat_results_written_total"]; written != float64(st.countFor(1)) {
		t.Errorf("results_written = %v, store has %d", written, st.countFor(1))
	}
	if got["kubeat_results_dropped_total"] < 1 {
		t.Error("results rejected by the store were not counted as dropped")
	}
	if got["kubeat_scheduler_last_tick_timestamp_seconds"] == 0 {
		t.Error("last tick timestamp was never set")
	}
}

// throughputChecker counts checks and takes a fixed time for each.
type throughputChecker struct {
	took  time.Duration
	count atomic.Int64
}

func (c *throughputChecker) Check(ctx context.Context, m store.Monitor) store.CheckResult {
	select {
	case <-time.After(c.took):
	case <-ctx.Done():
	}
	c.count.Add(1)
	return store.CheckResult{MonitorID: m.ID, Up: true}
}

// With fast checks, how many run per tick must not be limited to the size
// of the worker pool.
func TestThroughputIsNotCappedByQueueSizePerTick(t *testing.T) {
	st := &fakeStore{}
	var ms []store.Monitor
	for id := int64(1); id <= 200; id++ {
		ms = append(ms, monitor(id, 50*time.Millisecond))
	}
	st.setMonitors(ms...)
	checker := &throughputChecker{took: time.Millisecond}
	opts := testOptions()
	opts.Tick = 50 * time.Millisecond
	opts.Workers = 4
	opts.BatchSize = 1000
	stop := start(st, checker, opts)

	// 200 monitors due every 50 ms tick, 4 workers at 1 ms per check: one
	// tick has room for all 200. Handing out only a queue-full (4) per tick
	// would manage 80 checks per second.
	time.Sleep(time.Second)
	stop()

	if got := checker.count.Load(); got < 1500 {
		t.Errorf("%d checks in one second, want well over 1500 (a queue-full per tick would give about 80)", got)
	}
}

func TestStartupSpreadsMonitorsOverTheirInterval(t *testing.T) {
	st := &fakeStore{}
	var ms []store.Monitor
	for id := int64(1); id <= 100; id++ {
		ms = append(ms, monitor(id, time.Second))
	}
	st.setMonitors(ms...)
	checker := &throughputChecker{}
	opts := testOptions()
	opts.Tick = 10 * time.Millisecond
	opts.MaxJitter = time.Millisecond // would bunch everything at the start
	opts.Workers = 50
	stop := start(st, checker, opts)

	// A quarter of the interval in: about a quarter of the monitors have had
	// their first check, not all of them.
	time.Sleep(250 * time.Millisecond)
	early := checker.count.Load()
	time.Sleep(900 * time.Millisecond)
	stop()

	if early < 5 || early > 60 {
		t.Errorf("%d of 100 monitors checked in the first quarter of their interval, want roughly 25", early)
	}
	if total := checker.count.Load(); total < 100 {
		t.Errorf("only %d checks after more than a full interval, want every monitor checked", total)
	}
}

func TestMonitorAddedLaterIsCheckedWithinMaxJitter(t *testing.T) {
	st := &fakeStore{}
	checker := &throughputChecker{}
	opts := testOptions()
	opts.MaxJitter = 20 * time.Millisecond
	stop := start(st, checker, opts)
	defer stop()

	time.Sleep(20 * time.Millisecond) // past the startup tick
	st.setMonitors(monitor(1, time.Hour))
	// Not spread over its hour-long interval: checked almost at once.
	waitFor(t, "the first check of the added monitor", func() bool { return checker.count.Load() >= 1 })
}

func TestNextAfterKeepsThePhase(t *testing.T) {
	base := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	interval := 30 * time.Second
	tests := []struct {
		name     string
		due, now time.Time
		want     time.Time
	}{
		{"on time", base, base, base.Add(30 * time.Second)},
		{"dispatched a little late", base, base.Add(2 * time.Second), base.Add(30 * time.Second)},
		{"exactly one interval late", base, base.Add(30 * time.Second), base.Add(60 * time.Second)},
		{"far behind", base, base.Add(95 * time.Second), base.Add(120 * time.Second)},
	}
	for _, tt := range tests {
		got := nextAfter(tt.due, interval, tt.now)
		if !got.Equal(tt.want) {
			t.Errorf("%s: next = %s, want %s", tt.name, got.Sub(base), tt.want.Sub(base))
		}
		if !got.After(tt.now) {
			t.Errorf("%s: next %s is not after now", tt.name, got.Sub(base))
		}
	}

	// Two monitors due 10 s apart that both fall behind stay 10 s apart.
	now := base.Add(5 * time.Minute)
	a, b := nextAfter(base, interval, now), nextAfter(base.Add(10*time.Second), interval, now)
	if gap := b.Sub(a); gap != 10*time.Second && gap != -20*time.Second {
		t.Errorf("monitors that fell behind together are %s apart, want their original 10s offset", gap)
	}
}

// A failing monitor is checked again after its retry interval until it has
// failed often enough to be an outage; its regular cycle does not move.
func TestRetryInterval(t *testing.T) {
	m := monitor(1, time.Minute)
	m.RetryInterval, m.FailureThreshold = 10*time.Second, 3
	st := &fakeStore{}
	st.setMonitors(m)
	s := New(st, nil, testOptions())

	schedules := make(map[int64]*schedule)
	jobs := make(chan store.Monitor, 1)
	t0 := time.Unix(1_700_000_000, 0)
	// dispatched reports whether a tick at t0+after queued the monitor.
	dispatched := func(after time.Duration) bool {
		s.tick(context.Background(), t0.Add(after), false, schedules, jobs)
		select {
		case <-jobs:
			return true
		default:
			return false
		}
	}
	finish := func(up bool, after time.Duration) { s.finish(m, up, t0.Add(after)) }
	expect := func(after time.Duration, want bool) {
		t.Helper()
		if got := dispatched(after); got != want {
			t.Fatalf("at +%s: dispatched = %v, want %v", after, got, want)
		}
	}

	expect(0, true)
	finish(false, 0) // first failure: retry at +10s
	expect(5*time.Second, false)
	expect(10*time.Second, true)
	finish(false, 10*time.Second) // second failure: retry at +20s
	expect(20*time.Second, true)
	finish(false, 20*time.Second) // third failure: the outage is confirmed
	expect(30*time.Second, false)
	expect(59*time.Second, false)
	expect(time.Minute, true) // the regular check, on its original cycle
	finish(true, time.Minute)
	expect(70*time.Second, false) // a success needs no retry
	expect(2*time.Minute, true)
	finish(false, 2*time.Minute) // the count started over
	expect(2*time.Minute+10*time.Second, true)

	// A check that is still running is not retried on top of itself.
	finish(false, 2*time.Minute+10*time.Second)
	s.start(m.ID)
	expect(2*time.Minute+20*time.Second, false)
	s.abandon(m.ID, t0.Add(2*time.Minute+20*time.Second))
	expect(2*time.Minute+21*time.Second, true)

	// Without a retry interval, or with one no shorter than the interval,
	// failures wait for the regular check.
	for _, retry := range []time.Duration{0, time.Minute, time.Hour} {
		plain := monitor(2, time.Minute)
		plain.RetryInterval, plain.FailureThreshold = retry, 3
		st.setMonitors(plain)
		s := New(st, nil, testOptions())
		schedules := make(map[int64]*schedule)
		s.tick(context.Background(), t0, false, schedules, jobs)
		<-jobs
		s.finish(plain, false, t0)
		s.tick(context.Background(), t0.Add(30*time.Second), false, schedules, jobs)
		select {
		case <-jobs:
			t.Errorf("retry interval %s: checked again before the interval", retry)
		default:
		}
	}

	// What is remembered about a monitor goes when the monitor does.
	st.setMonitors()
	s.tick(context.Background(), t0.Add(time.Hour), false, schedules, jobs)
	if len(s.failures) != 0 || len(s.retryAt) != 0 {
		t.Errorf("state kept for a removed monitor: failures=%v retryAt=%v", s.failures, s.retryAt)
	}
}

// clock is a time a test sets, for the scheduler's result writes and
// Healthy.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t
}

// fullStore stores no result, as a database on a full volume does.
type fullStore struct{ fakeStore }

func (f *fullStore) InsertCheckResults(context.Context, []store.CheckResult) error {
	return errors.New("database or disk is full")
}

// stuckStore holds every write until released, as a hung volume does.
type stuckStore struct {
	fakeStore
	release chan struct{}
}

func (f *stuckStore) InsertCheckResults(ctx context.Context, batch []store.CheckResult) error {
	<-f.release
	return f.fakeStore.InsertCheckResults(ctx, batch)
}

// resultsOf returns a successful result for each monitor.
func resultsOf(ids ...int64) []store.CheckResult {
	var rs []store.CheckResult
	for _, id := range ids {
		rs = append(rs, store.CheckResult{MonitorID: id, Up: true})
	}
	return rs
}

// onClock returns a scheduler on st that completed a tick at t0, and the
// clock its writes and Healthy read, set to t0.
func onClock(st Store, t0 time.Time) (*Scheduler, *clock) {
	c := &clock{t: t0}
	s := New(st, nil, testOptions())
	s.now = c.now
	tickAt(s, t0)
	return s, c
}

// tickAt runs one tick of s at t, of no monitors unless its store has some.
func tickAt(s *Scheduler, t time.Time) {
	s.tick(context.Background(), t, false, make(map[int64]*schedule), make(chan store.Monitor, 10))
}

// Healthy, which the heartbeat asks, holds the scheduler to a tick in the
// window and, if results came in it, to storing some of them. A window in
// which no result came is quiet, not broken.
func TestHealthy(t *testing.T) {
	ctx := context.Background()
	const window = time.Minute
	t0 := time.Unix(1_700_000_000, 0)
	unhealthy := func(t *testing.T, s *Scheduler, why string) {
		t.Helper()
		if err := s.Healthy(window); err == nil || !strings.Contains(err.Error(), why) {
			t.Errorf("Healthy = %v, want an error saying %q", err, why)
		}
	}
	healthy := func(t *testing.T, s *Scheduler) {
		t.Helper()
		if err := s.Healthy(window); err != nil {
			t.Errorf("Healthy = %v, want nil", err)
		}
	}

	t.Run("no result came", func(t *testing.T) {
		s, c := onClock(&fakeStore{}, t0)
		healthy(t, s)
		c.set(t0.Add(10 * window))
		tickAt(s, c.now())
		healthy(t, s)
	})

	t.Run("no tick in the window", func(t *testing.T) {
		s, c := onClock(&fakeStore{}, t0)
		c.set(t0.Add(window + time.Second))
		unhealthy(t, s, "has not completed a tick for 1m1s")
	})

	t.Run("no result could be stored", func(t *testing.T) {
		s, c := onClock(&fullStore{}, t0)
		s.flush(ctx, resultsOf(1, 2, 3))
		unhealthy(t, s, "none of them could be stored")

		// The next window brings no result: quiet again.
		c.set(t0.Add(window + time.Second))
		tickAt(s, c.now())
		healthy(t, s)
	})

	t.Run("one row of a batch dropped", func(t *testing.T) {
		st := &fakeStore{rejectID: 2}
		s, _ := onClock(st, t0)
		s.flush(ctx, resultsOf(1, 2, 3))
		if st.countFor(1) != 1 || st.countFor(2) != 0 || st.countFor(3) != 1 {
			t.Fatalf("stored %d, %d, %d results of monitors 1, 2, 3, want 1, 0, 1", st.countFor(1), st.countFor(2), st.countFor(3))
		}
		healthy(t, s)
	})

	t.Run("stored before the window, lost in it", func(t *testing.T) {
		s, c := onClock(&fakeStore{rejectID: 2}, t0)
		s.flush(ctx, resultsOf(1))
		c.set(t0.Add(90 * time.Second))
		tickAt(s, c.now())
		s.flush(ctx, resultsOf(2))
		unhealthy(t, s, "none of them could be stored")

		// A batch stored later in the window makes up for it.
		c.set(t0.Add(100 * time.Second))
		s.flush(ctx, resultsOf(1))
		healthy(t, s)
	})

	t.Run("a write that does not end", func(t *testing.T) {
		st := &stuckStore{release: make(chan struct{})}
		s, c := onClock(st, t0)
		done := make(chan struct{})
		go func() {
			defer close(done)
			s.flush(ctx, resultsOf(1))
		}()
		waitFor(t, "the write to begin", func() bool { return s.writing.Load() != 0 })

		c.set(t0.Add(30 * time.Second))
		tickAt(s, c.now())
		healthy(t, s) // busy, but for less than the window

		c.set(t0.Add(window + time.Second))
		tickAt(s, c.now())
		unhealthy(t, s, "busy with one batch of check results for 1m1s")

		close(st.release)
		<-done
		healthy(t, s)
	})
}

// heartbeats runs the heartbeat against a dead man's switch, with Healthy
// asking s about the window, until five heartbeats were sent or withheld.
// It returns how many requests reached the switch, and how many heartbeats
// were sent and how many withheld.
func heartbeats(t *testing.T, s *Scheduler, window time.Duration) (hits int64, sent, withheld float64) {
	t.Helper()
	var requests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer srv.Close()

	m := metrics.New("test")
	count := func(result string) float64 {
		got, err := m.Gather()
		if err != nil {
			t.Fatal(err)
		}
		return got["kubeat_heartbeats_total{result="+result+"}"]
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		heartbeat.Run(ctx, heartbeat.Options{
			URL: srv.URL, Interval: 5 * time.Millisecond, Timeout: time.Second,
			Healthy: func() error { return s.Healthy(window) },
			Client:  srv.Client(), Metrics: m, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		})
	}()
	waitFor(t, "five heartbeats", func() bool {
		return count(metrics.HeartbeatSent)+count(metrics.HeartbeatWithheld) >= 5
	})
	cancel()
	<-done
	return requests.Load(), count(metrics.HeartbeatSent), count(metrics.HeartbeatWithheld)
}

// The dead man's switch hears nothing while results come and none of them
// can be stored, and keeps hearing from a scheduler that drops a row now
// and then or has no result to store.
func TestHeartbeatStopsWhileNoResultCanBeStored(t *testing.T) {
	for _, tt := range []struct {
		name  string
		st    Store
		batch []store.CheckResult // written in the window; none for a quiet one
		sent  bool
	}{
		{"no result can be stored", &fullStore{}, resultsOf(1, 2, 3), false},
		{"one row of the batch is dropped", &fakeStore{rejectID: 2}, resultsOf(1, 2, 3), true},
		{"no result came", &fakeStore{}, nil, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := onClock(tt.st, time.Unix(1_700_000_000, 0))
			if tt.batch != nil {
				s.flush(context.Background(), tt.batch)
			}
			hits, sent, withheld := heartbeats(t, s, time.Minute)
			switch {
			case tt.sent && (withheld != 0 || sent < 5 || hits < 5):
				t.Errorf("sent %v, withheld %v, %d requests reached the switch; want every heartbeat sent", sent, withheld, hits)
			case !tt.sent && (sent != 0 || withheld < 5 || hits != 0):
				t.Errorf("sent %v, withheld %v, %d requests reached the switch; want every heartbeat withheld", sent, withheld, hits)
			}
		})
	}
}

// What Healthy judges is what Run's writes leave: a store that cannot
// write makes the scheduler unhealthy, one that drops a row now and then
// does not.
func TestHealthyFollowsTheWritesOfRun(t *testing.T) {
	full := &fullStore{}
	full.setMonitors(monitor(1, 5*time.Millisecond), monitor(2, 5*time.Millisecond))
	s, stop := startScheduler(full, &fakeChecker{}, testOptions())
	waitFor(t, "Healthy to tell that no result is stored", func() bool {
		err := s.Healthy(time.Second)
		return err != nil && strings.Contains(err.Error(), "none of them could be stored")
	})
	stop()

	st := &fakeStore{rejectID: 2}
	st.setMonitors(monitor(1, 5*time.Millisecond), monitor(2, 5*time.Millisecond))
	s, stop = startScheduler(st, &fakeChecker{}, testOptions())
	defer stop()
	waitFor(t, "stored results", func() bool { return st.countFor(1) >= 5 })
	if err := s.Healthy(time.Second); err != nil {
		t.Errorf("Healthy = %v while most results are stored", err)
	}
}

// A batch that could not be written is one line in the log, with how many
// of its rows were lost, not a line for each row: a full volume would
// otherwise log every result.
func TestFailedBatchIsOneLogLine(t *testing.T) {
	for _, tt := range []struct {
		name    string
		st      Store
		dropped string
	}{
		{"no row can be stored", &fullStore{}, "dropped=5"},
		{"one bad row", &fakeStore{rejectID: 2}, "dropped=1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			opts := testOptions()
			opts.Logger = slog.New(slog.NewTextHandler(&logs, nil))
			New(tt.st, nil, opts).flush(context.Background(), resultsOf(1, 2, 3, 4, 5))

			lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
			if len(lines) != 1 || !strings.Contains(lines[0], "level=WARN") || !strings.Contains(lines[0], "batch insert failed") ||
				!strings.Contains(lines[0], "rows=5") || !strings.Contains(lines[0], tt.dropped) {
				t.Errorf("log of a batch of 5 that failed:\n%s\nwant one WARN line with rows=5 and %s", logs.String(), tt.dropped)
			}
		})
	}
}
