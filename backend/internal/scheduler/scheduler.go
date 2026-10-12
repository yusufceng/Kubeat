// Package scheduler decides when each monitor is checked, runs the checks on
// a bounded worker pool and writes the results in batches.
package scheduler

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/metrics"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// Store is the part of the persistence layer the scheduler needs.
type Store interface {
	ListMonitors(ctx context.Context) ([]store.Monitor, error)
	InsertCheckResults(ctx context.Context, results []store.CheckResult) error
}

// Checker runs a single check. The context carries the monitor's timeout.
type Checker interface {
	Check(ctx context.Context, m store.Monitor) store.CheckResult
}

// Options configures a Scheduler.
type Options struct {
	// Tick is how often the scheduler looks for due monitors and picks up
	// added, changed or deleted ones.
	Tick time.Duration
	// MaxJitter bounds the random delay before a monitor's first check, so
	// monitors loaded together do not all fire in the same instant.
	MaxJitter time.Duration
	// Workers is the maximum number of checks running at once.
	Workers int
	// BatchSize and FlushInterval control result writes: a batch is written
	// when it is full or when the interval elapses, whichever comes first.
	BatchSize     int
	FlushInterval time.Duration
	// OnResults, if set, is called with each group of results after they
	// have been stored. Calls are sequential.
	OnResults func(ctx context.Context, results []store.CheckResult)
	// Metrics may be nil.
	Metrics *metrics.Metrics
	Logger  *slog.Logger
}

// Scheduler runs checks for all enabled monitors.
type Scheduler struct {
	store   Store
	checker Checker
	opts    Options

	mu       sync.Mutex
	inFlight map[int64]bool

	lastTick atomic.Int64 // Unix nanoseconds of the last completed tick
}

// LastTick returns when the scheduler last completed a tick, that is, read
// the monitors from the store and queued the due ones. It is the zero time
// before the first tick.
func (s *Scheduler) LastTick() time.Time {
	ns := s.lastTick.Load()
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

// New returns a Scheduler; call Run to start it.
func New(st Store, checker Checker, opts Options) *Scheduler {
	return &Scheduler{store: st, checker: checker, opts: opts, inFlight: make(map[int64]bool)}
}

// Run blocks until ctx is cancelled, then waits for running checks to stop
// and writes the results collected so far.
func (s *Scheduler) Run(ctx context.Context) {
	jobs := make(chan store.Monitor, s.opts.Workers)
	results := make(chan store.CheckResult, s.opts.BatchSize)

	var workers sync.WaitGroup
	for i := 0; i < s.opts.Workers; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			s.work(ctx, jobs, results)
		}()
	}

	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		s.write(ctx, results)
	}()

	s.dispatch(ctx, jobs)

	close(jobs)
	workers.Wait()
	close(results)
	<-writerDone
}

type schedule struct {
	interval time.Duration
	next     time.Time
}

// dispatch queues due monitors on every tick until ctx is cancelled.
func (s *Scheduler) dispatch(ctx context.Context, jobs chan<- store.Monitor) {
	schedules := make(map[int64]*schedule)
	ticker := time.NewTicker(s.opts.Tick)
	defer ticker.Stop()

	for {
		s.tick(ctx, time.Now(), schedules, jobs)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Scheduler) tick(ctx context.Context, now time.Time, schedules map[int64]*schedule, jobs chan<- store.Monitor) {
	monitors, err := s.store.ListMonitors(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.opts.Logger.Error("list monitors failed", "error", err)
		}
		return
	}

	seen := make(map[int64]bool, len(monitors))
	var due []store.Monitor
	for _, m := range monitors {
		if !m.Enabled {
			continue
		}
		seen[m.ID] = true

		sc, ok := schedules[m.ID]
		if !ok {
			sc = &schedule{next: now.Add(jitter(min(s.opts.MaxJitter, m.Interval)))}
			schedules[m.ID] = sc
		}
		sc.interval = m.Interval

		if !now.Before(sc.next) && !s.isInFlight(m.ID) {
			due = append(due, m)
		}
	}

	// Most overdue first, so that when there are more due monitors than free
	// workers the ones skipped on this tick go first on the next.
	sort.Slice(due, func(i, j int) bool {
		return schedules[due[i].ID].next.Before(schedules[due[j].ID].next)
	})

dispatch:
	for i, m := range due {
		sc := schedules[m.ID]
		s.setInFlight(m.ID, true)
		select {
		case jobs <- m:
			sc.next = sc.next.Add(sc.interval)
			if sc.next.Before(now) {
				// Fell behind (slow check or full queue): do not try to catch up.
				sc.next = now.Add(sc.interval)
			}
		default:
			// All workers are busy; the rest stay due and are retried next tick.
			s.setInFlight(m.ID, false)
			s.opts.Metrics.DispatchSkipped(len(due) - i)
			break dispatch
		}
	}

	for id := range schedules {
		if !seen[id] {
			delete(schedules, id)
		}
	}

	s.lastTick.Store(now.UnixNano())
	s.opts.Metrics.SchedulerTick(now)
}

func jitter(max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	return rand.N(max)
}

func (s *Scheduler) isInFlight(id int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inFlight[id]
}

func (s *Scheduler) setInFlight(id int64, v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v {
		s.inFlight[id] = true
	} else {
		delete(s.inFlight, id)
	}
}

func (s *Scheduler) work(ctx context.Context, jobs <-chan store.Monitor, results chan<- store.CheckResult) {
	for m := range jobs {
		checkCtx, cancel := context.WithTimeout(ctx, m.Timeout)
		s.opts.Metrics.CheckStarted()
		result := s.checker.Check(checkCtx, m)
		s.opts.Metrics.CheckDone(string(m.Type), result.Up, result.Latency)
		cancel()
		s.setInFlight(m.ID, false)

		// A check interrupted by shutdown says nothing about the target.
		if ctx.Err() != nil {
			continue
		}
		results <- result
	}
}

// write batches results until the channel is closed.
func (s *Scheduler) write(ctx context.Context, results <-chan store.CheckResult) {
	// Writes must still succeed while shutting down.
	ctx = context.WithoutCancel(ctx)
	batch := make([]store.CheckResult, 0, s.opts.BatchSize)
	ticker := time.NewTicker(s.opts.FlushInterval)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		s.flush(ctx, batch)
		batch = batch[:0]
	}

	for {
		select {
		case r, ok := <-results:
			if !ok {
				flush()
				return
			}
			batch = append(batch, r)
			if len(batch) >= s.opts.BatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func (s *Scheduler) flush(ctx context.Context, batch []store.CheckResult) {
	stored := batch
	if err := s.store.InsertCheckResults(ctx, batch); err != nil {
		// One bad row (for example a monitor deleted while its check was
		// running) fails the whole transaction; retry row by row so the rest
		// is kept.
		stored = make([]store.CheckResult, 0, len(batch))
		for _, r := range batch {
			if err := s.store.InsertCheckResults(ctx, []store.CheckResult{r}); err != nil {
				s.opts.Logger.Warn("check result dropped", "monitor_id", r.MonitorID, "error", err)
				continue
			}
			stored = append(stored, r)
		}
		s.opts.Logger.Warn("batch insert failed, retried row by row",
			"error", err, "rows", len(batch), "dropped", len(batch)-len(stored))
	}
	s.opts.Metrics.Results(len(stored), len(batch)-len(stored))
	if s.opts.OnResults != nil && len(stored) > 0 {
		s.opts.OnResults(ctx, stored)
	}
}
