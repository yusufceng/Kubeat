package scheduler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

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
	return store.CheckResult{MonitorID: m.ID, Up: true}
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
	waitFor(t, "stored results", func() bool { return st.countFor(1) >= 5 })
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
