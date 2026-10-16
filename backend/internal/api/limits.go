package api

import (
	"context"
	"log/slog"
	"runtime"
	"sync"
	"time"
)

// slots bounds the password checks that run at once. Each is a bcrypt
// comparison, which keeps a core busy for a quarter of a second, or a bind
// to the directory. Without a bound, a flood of sign-ins, made-up names
// included, would take the CPU the monitors' checks need.
type slots struct {
	taken chan struct{}
	// wait is how long a check waits for a free slot; zero waits as long
	// as its request lasts.
	wait time.Duration
}

// newSlots returns n slots; zero or less means half the CPUs, at least one.
func newSlots(n int, wait time.Duration) *slots {
	if n <= 0 {
		n = max(1, runtime.GOMAXPROCS(0)/2)
	}
	return &slots{taken: make(chan struct{}, n), wait: wait}
}

// acquire takes a slot and returns the function that gives it back, which
// may be called more than once. It returns false when no slot came free
// in time, or the request ended first.
func (s *slots) acquire(ctx context.Context) (release func(), ok bool) {
	release = sync.OnceFunc(func() { <-s.taken })
	select {
	case s.taken <- struct{}{}:
		return release, true
	default:
	}
	var timeout <-chan time.Time
	if s.wait > 0 {
		timer := time.NewTimer(s.wait)
		defer timer.Stop()
		timeout = timer.C
	}
	select {
	case s.taken <- struct{}{}:
		return release, true
	case <-timeout:
	case <-ctx.Done():
	}
	return nil, false
}

// auditCap caps the failed sign-ins written to the audit log at perMinute
// a minute. Each made-up name fails for the first time, so without a cap
// a flood of them would write a row each: enough to fill the volume, and
// to push every other entry out of the log's view. What the cap leaves out
// is counted, and the count logged as a warning once the minute is over.
type auditCap struct {
	perMinute int
	logger    *slog.Logger
	// later runs f after d: time.AfterFunc, but replaceable in tests.
	later func(d time.Duration, f func())

	mu      sync.Mutex
	minute  time.Time // the one being counted
	written int
	dropped int
}

// newAuditCap returns a cap of perMinute rows a minute; zero or less means
// no cap.
func newAuditCap(perMinute int, logger *slog.Logger) *auditCap {
	return &auditCap{perMinute: perMinute, logger: logger, later: func(d time.Duration, f func()) { time.AfterFunc(d, f) }}
}

// allow reports whether a failed sign-in at now may be written to the
// audit log.
func (c *auditCap) allow(now time.Time) bool {
	if c.perMinute <= 0 {
		return true
	}
	minute := now.Truncate(time.Minute)
	c.mu.Lock()
	defer c.mu.Unlock()
	if !minute.Equal(c.minute) {
		c.report()
		c.minute, c.written = minute, 0
	}
	if c.written < c.perMinute {
		c.written++
		return true
	}
	if c.dropped == 0 {
		// Reported when the minute is over, also if nothing follows it.
		c.later(minute.Add(time.Minute).Sub(now), func() {
			c.mu.Lock()
			defer c.mu.Unlock()
			if c.minute.Equal(minute) {
				c.report()
			}
		})
	}
	c.dropped++
	return false
}

// report logs how many failed sign-ins the current minute left out of the
// audit log, if any; call with mu held.
func (c *auditCap) report() {
	if c.dropped == 0 {
		return
	}
	c.logger.Warn("failed sign-ins left out of the audit log: more than UPTIME_LOGIN_AUDIT_PER_MINUTE in a minute",
		"dropped", c.dropped, "minute", c.minute, "limit", c.perMinute)
	c.dropped = 0
}
