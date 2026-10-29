// Package scheduler decides when each monitor is checked, runs the checks on
// a bounded worker pool and writes the results in batches.
package scheduler

import (
	"context"
	"fmt"
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
	// MaxJitter bounds the random delay before the first check of a monitor
	// added while the scheduler is running, so that it is checked soon.
	// Monitors already there at start are instead spread over their whole
	// interval, which keeps hundreds of them from firing together forever.
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
	// TargetMaxRate and ClusterTargetMaxRate are how many checks a minute
	// one host name gets from all monitors together, for names outside and
	// inside the cluster (netguard.InCluster); AddressMaxRate how many one
	// IP address gets, whatever its class, from the checks of every name
	// that reaches it. A due check past a ceiling waits for a later tick,
	// and is not a result. Zero means no ceiling. Every request a check
	// makes counts: a redirect followed, charged to the host it went to,
	// each pod of a check of every pod; and a check is held to every host
	// and address its last check reached, before it is dispatched.
	TargetMaxRate        int
	ClusterTargetMaxRate int
	AddressMaxRate       int
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
	// failures counts each monitor's failed checks in a row, and retryAt is
	// when a failing monitor is checked again ahead of its interval.
	failures map[int64]int
	retryAt  map[int64]time.Time
	// throttled is since when each monitor's due check has been waiting
	// for its host's ceiling (see hostLimiter); absent while it is not.
	throttled map[int64]time.Time

	// The ceilings on checks a minute per host name, by its class, and
	// per address a check reaches, whatever its class (see hostLimiter and
	// admit): a name is free to make up, an address is not, so both are
	// held.
	names classLimiters
	addrs *hostLimiter
	// peers is what each monitor's last check reached, charged before its
	// next check is dispatched; charged is what a check in flight took its
	// tokens from.
	peers, charged map[int64]charge

	lastTick atomic.Int64 // Unix nanoseconds of the last completed tick
	// What Healthy judges the result writes by, in Unix nanoseconds: when
	// a write last stored some of its results, when one last stored none of
	// them, and when the writer began the batch it is still busy with,
	// storing it and handing it on to OnResults (0 while it is not busy).
	lastStored, lastLost, writing atomic.Int64

	// now is the clock of the result writes and of Healthy; tests set their
	// own.
	now func() time.Time
}

// LastTick returns when the scheduler last completed a tick, that is, read
// the monitors from the store and queued the due ones. It is the zero time
// before the first tick.
func (s *Scheduler) LastTick() time.Time {
	return unixTime(s.lastTick.Load())
}

// Healthy reports why the scheduler has not been doing its job over the
// last window, or nil. It has not if it completed no tick in that time, or
// if checks ran but their results went nowhere: results came and none of
// them could be stored (a full or read-only volume), or the writer has been
// busy with one batch for longer than the window (a hung volume). A result
// that is not stored opens no incident and sends no notification. A window
// in which no result came, because no check was due, is healthy, and so is
// one in which some results were lost and others stored.
func (s *Scheduler) Healthy(window time.Duration) error {
	now := s.now()
	if age := now.Sub(s.LastTick()); age > window {
		return fmt.Errorf("the scheduler has not completed a tick for %s", age.Round(time.Second))
	}
	if began := s.writing.Load(); began != 0 {
		if age := now.Sub(unixTime(began)); age > window {
			return fmt.Errorf("the writer has been busy with one batch of check results for %s", age.Round(time.Second))
		}
	}
	since := now.Add(-window)
	if !unixTime(s.lastLost.Load()).Before(since) && unixTime(s.lastStored.Load()).Before(since) {
		return fmt.Errorf("check results came in the last %s and none of them could be stored", window)
	}
	return nil
}

// unixTime returns the time kept as Unix nanoseconds, or the zero time for
// 0, which stands for never.
func unixTime(ns int64) time.Time {
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

// New returns a Scheduler; call Run to start it.
func New(st Store, checker Checker, opts Options) *Scheduler {
	return &Scheduler{
		store: st, checker: checker, opts: opts,
		inFlight: make(map[int64]bool), failures: make(map[int64]int), retryAt: make(map[int64]time.Time),
		throttled: make(map[int64]time.Time),
		names:     newClassLimiters(opts.TargetMaxRate, opts.ClusterTargetMaxRate),
		addrs:     newHostLimiter(opts.AddressMaxRate),
		peers:     make(map[int64]charge),
		charged:   make(map[int64]charge),
		now:       time.Now,
	}
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

	for first := true; ; first = false {
		s.tick(ctx, time.Now(), first, schedules, jobs)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// tick queues the monitors that are due. startup is true for the first tick,
// when every monitor is new to the scheduler.
func (s *Scheduler) tick(ctx context.Context, now time.Time, startup bool, schedules map[int64]*schedule, jobs chan<- store.Monitor) {
	monitors, err := s.store.ListMonitors(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.opts.Logger.Error("list monitors failed", "error", err)
		}
		return
	}

	seen := make(map[int64]bool, len(monitors))
	hosts := make(map[string]bool)
	var due []store.Monitor
	dueAt := make(map[int64]time.Time)
	for _, m := range monitors {
		if !m.Enabled {
			continue
		}
		seen[m.ID] = true
		if host := m.Host(); host != "" {
			hosts[host] = true
		}

		sc, ok := schedules[m.ID]
		if !ok {
			spread := m.Interval
			if !startup {
				spread = min(s.opts.MaxJitter, m.Interval)
			}
			sc = &schedule{next: now.Add(jitter(spread))}
			schedules[m.ID] = sc
		}
		sc.interval = m.Interval

		at, inFlight := s.dueTime(m.ID, sc.next)
		if !now.Before(at) && !inFlight {
			due = append(due, m)
			dueAt[m.ID] = at
		}
	}

	// Most overdue first, so that when there are more due monitors than free
	// workers the ones skipped on this tick go first on the next.
	sort.Slice(due, func(i, j int) bool {
		return dueAt[due[i].ID].Before(dueAt[due[j].ID])
	})

	// Hand the due monitors to the workers, waiting for a free one for as
	// long as this tick lasts. Giving up the moment the queue is full would
	// cap throughput at one queue-full per tick however fast the checks are.
	deadline := time.NewTimer(s.opts.Tick)
	defer deadline.Stop()

dispatch:
	for i, m := range due {
		sc := schedules[m.ID]
		// A host the check goes to, or an address it reaches, at its
		// ceiling of checks a minute makes the check wait for a later
		// tick, still due: no result says anything about a target that
		// was not asked. The most overdue checks come first, so the
		// longest wait ends first when a token comes.
		taken, ok := s.admit(m, now)
		if !ok {
			s.throttle(m.ID, m.Host(), now)
			continue
		}
		s.unthrottle(m.ID)
		retry := s.start(m.ID)
		select {
		case jobs <- m:
			// An early retry leaves the monitor's place in its cycle alone.
			if !now.Before(sc.next) {
				sc.next = nextAfter(sc.next, sc.interval, now)
			}
		case <-deadline.C:
			// The workers were busy for the whole tick; the rest stay due
			// and go first on the next one.
			s.abandon(m.ID, retry)
			s.release(m.ID, taken, now)
			s.opts.Metrics.DispatchSkipped(len(due) - i)
			break dispatch
		case <-ctx.Done():
			s.abandon(m.ID, retry)
			s.release(m.ID, taken, now)
			return
		}
	}

	for id := range schedules {
		if !seen[id] {
			delete(schedules, id)
			s.forget(id)
		}
	}
	s.prune(seen)
	s.names.keep(hosts, now)
	s.addrs.keep(s.peerSet(), now)

	s.lastTick.Store(now.UnixNano())
	s.opts.Metrics.SchedulerTick(now)
}

// nextAfter returns the first time after now that is a whole number of
// intervals past due. A monitor that fell behind (a slow check, a busy
// pool, a stalled database) skips the runs it missed instead of catching
// up, and keeps its place in the cycle: resetting to "now plus interval"
// would put every monitor that fell behind together on the same instant
// from then on.
func nextAfter(due time.Time, interval time.Duration, now time.Time) time.Time {
	next := due.Add(interval)
	if !next.After(now) {
		missed := now.Sub(next)/interval + 1
		next = next.Add(missed * interval)
	}
	return next
}

func jitter(max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	return rand.N(max)
}

// dueTime returns when the monitor is next due, which is its scheduled
// time or an earlier retry, and whether a check of it is still running.
func (s *Scheduler) dueTime(id int64, scheduled time.Time) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if retry, ok := s.retryAt[id]; ok && retry.Before(scheduled) {
		scheduled = retry
	}
	return scheduled, s.inFlight[id]
}

// start marks a check of the monitor as running and takes its pending
// retry, which the check about to run replaces. It returns that retry so
// that abandon can put it back.
func (s *Scheduler) start(id int64) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inFlight[id] = true
	retry := s.retryAt[id]
	delete(s.retryAt, id)
	return retry
}

// abandon undoes start for a check that was not handed to a worker.
func (s *Scheduler) abandon(id int64, retry time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.inFlight, id)
	if !retry.IsZero() {
		s.retryAt[id] = retry
	}
}

// finish records the outcome of a check. While a monitor is failing but has
// not yet failed often enough to open an incident, it is checked again
// after its retry interval, so that an outage is confirmed, or dismissed,
// sooner than a whole interval later.
func (s *Scheduler) finish(m store.Monitor, up bool, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.inFlight, m.ID)
	if up {
		delete(s.failures, m.ID)
		return
	}
	s.failures[m.ID]++
	if m.RetryInterval > 0 && m.RetryInterval < m.Interval && s.failures[m.ID] < m.FailureThreshold {
		s.retryAt[m.ID] = now.Add(m.RetryInterval)
	}
}

// forget drops what is remembered about a monitor that is gone or paused.
func (s *Scheduler) forget(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.failures, id)
	delete(s.retryAt, id)
	delete(s.throttled, id)
}

// prune forgets what the last checks of monitors not among the enabled
// ones reached: a check that finished after its monitor went would
// otherwise leave it.
func (s *Scheduler) prune(enabled map[int64]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range s.peers {
		if !enabled[id] {
			delete(s.peers, id)
		}
	}
}

func (s *Scheduler) work(ctx context.Context, jobs <-chan store.Monitor, results chan<- store.CheckResult) {
	for m := range jobs {
		checkCtx, cancel := context.WithTimeout(ctx, m.Timeout)
		s.opts.Metrics.CheckStarted()
		result := s.checker.Check(checkCtx, m)
		s.opts.Metrics.CheckDone(string(m.Type), result.Up, result.Latency)
		cancel()
		now := time.Now()
		s.finish(m, result.Up, now)
		// The check took its tokens when it was dispatched; the requests
		// it made beyond that (redirects, every pod) and the addresses it
		// reached are owed now.
		s.settle(m, result.Hops, now)

		// A check interrupted by shutdown says nothing about the target. One
		// that still succeeded was not interrupted: it finished just before
		// the cancel and its result is as good as any.
		if ctx.Err() != nil && !result.Up {
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
	s.writing.Store(s.now().UnixNano())
	defer s.writing.Store(0)

	stored := batch
	if err := s.store.InsertCheckResults(ctx, batch); err != nil {
		// One bad row (for example a monitor deleted while its check was
		// running) fails the whole transaction; retry row by row so the rest
		// is kept. A store that cannot write at all, such as one on a full
		// volume, fails every row the same way: one line tells of the
		// batch, not one for each of its rows.
		stored = make([]store.CheckResult, 0, len(batch))
		for _, r := range batch {
			if s.store.InsertCheckResults(ctx, []store.CheckResult{r}) == nil {
				stored = append(stored, r)
			}
		}
		s.opts.Logger.Warn("batch insert failed, retried row by row",
			"error", err, "rows", len(batch), "dropped", len(batch)-len(stored))
	}
	switch at := s.now().UnixNano(); {
	case len(stored) > 0:
		s.lastStored.Store(at)
	case len(batch) > 0:
		s.lastLost.Store(at)
	}
	s.opts.Metrics.Results(len(stored), len(batch)-len(stored))
	if s.opts.OnResults != nil && len(stored) > 0 {
		s.opts.OnResults(ctx, stored)
	}
}
