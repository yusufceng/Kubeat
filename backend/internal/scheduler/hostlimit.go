package scheduler

import (
	"sync"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/netguard"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// hostLimiter is a ceiling on the checks a minute every key, a host name
// or an address, gets from all monitors together: a token bucket per key,
// holding a minute's worth of tokens, filled at the ceiling's rate. A check takes a token as it is
// dispatched; a check that made more requests than one (redirects, every
// pod of a service) owes the rest afterwards, and the bucket goes into
// debt, which the next checks of that host wait out.
type hostLimiter struct {
	perMinute int

	mu      sync.Mutex
	buckets map[string]*bucket
}

// bucket counts in credit rather than tokens, so that the arithmetic is
// exact: one token is tokenCredit, and every nanosecond that passes earns
// perMinute credit, a full token every minute/perMinute.
type bucket struct {
	credit int64
	at     time.Time // when credit was last brought up to date
	seen   time.Time // when the bucket was last used, or its host last had a monitor
}

const tokenCredit = int64(time.Minute)

// bucketLife is how long a bucket outlives the last enabled monitor of its
// host and the last check that charged it. Forgotten on the tick its host
// vanished, a bucket would be full again for a monitor deleted and made
// anew, or paused and resumed; after a minute unused it is full anyway, so
// nothing is lost by forgetting it then.
const bucketLife = time.Minute

// newHostLimiter returns a limiter of perMinute checks a minute per host,
// or nil for perMinute of zero or less, which every method takes for "no
// limit".
func newHostLimiter(perMinute int) *hostLimiter {
	if perMinute <= 0 {
		return nil
	}
	return &hostLimiter{perMinute: perMinute, buckets: make(map[string]*bucket)}
}

// bucketFor returns the host's bucket, filled up to now and marked used;
// call with mu held.
func (l *hostLimiter) bucketFor(host string, now time.Time) *bucket {
	full := int64(l.perMinute) * tokenCredit
	b, ok := l.buckets[host]
	if !ok {
		b = &bucket{credit: full, at: now, seen: now}
		l.buckets[host] = b
		return b
	}
	switch elapsed := now.Sub(b.at); {
	case elapsed >= time.Minute:
		// A minute fills any bucket, whatever its debt; bucketLife and
		// keep rely on that.
		b.credit, b.at = full, now
	case elapsed > 0:
		b.credit, b.at = min(full, b.credit+int64(elapsed)*int64(l.perMinute)), now
	}
	if now.After(b.seen) {
		b.seen = now
	}
	return b
}

// take takes a token for a check of host at now, and reports whether there
// was one: false means the check must wait.
func (l *hostLimiter) take(host string, now time.Time) bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.bucketFor(host, now)
	if b.credit < tokenCredit {
		return false
	}
	b.credit -= tokenCredit
	return true
}

// refund gives back the token of a check that was not run after all.
func (l *hostLimiter) refund(host string, now time.Time) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.bucketFor(host, now)
	b.credit = min(int64(l.perMinute)*tokenCredit, b.credit+tokenCredit)
}

// debit charges n more requests a finished check made to host beyond the
// one it took a token for. The bucket may go into debt.
func (l *hostLimiter) debit(host string, n int, now time.Time) {
	if l == nil || n <= 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.bucketFor(host, now).credit -= int64(n) * tokenCredit
}

// keep marks the buckets of hosts as in use at now, and forgets every
// bucket unused for bucketLife whose host is not among them: those
// monitors are gone, and a bucket left for good would be a bucket per
// host ever seen.
func (l *hostLimiter) keep(hosts map[string]bool, now time.Time) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for host, b := range l.buckets {
		switch {
		case hosts[host]:
			if now.After(b.seen) {
				b.seen = now
			}
		case now.Sub(b.seen) >= bucketLife:
			delete(l.buckets, host)
		}
	}
}

// classLimiters are the ceilings on host names, for names outside and
// inside the cluster (netguard.InCluster of the name itself). An empty
// key is nobody's and is never held.
type classLimiters struct {
	external, cluster *hostLimiter
}

func newClassLimiters(external, cluster int) classLimiters {
	return classLimiters{external: newHostLimiter(external), cluster: newHostLimiter(cluster)}
}

// of returns the limiter the key is held to.
func (c classLimiters) of(key string) *hostLimiter {
	if netguard.InCluster(key) {
		return c.cluster
	}
	return c.external
}

func (c classLimiters) take(key string, now time.Time) bool {
	return key == "" || c.of(key).take(key, now)
}

func (c classLimiters) refund(key string, now time.Time) {
	if key != "" {
		c.of(key).refund(key, now)
	}
}

func (c classLimiters) debit(key string, n int, now time.Time) {
	if key != "" {
		c.of(key).debit(key, n, now)
	}
}

func (c classLimiters) keep(keys map[string]bool, now time.Time) {
	c.external.keep(keys, now)
	c.cluster.keep(keys, now)
}

// charge is the keys a check is held to: the host names its requests go
// to and the addresses that answer them, each once, canonical, the
// monitor's own host first. What admit takes a token from, and what is
// remembered of a monitor's last check for its next.
type charge struct {
	names, addrs []string
}

// chargeOf is what a check that made hops owes, as a charge of the
// distinct keys and how many requests each got: every hop to the host it
// went to (host, the monitor's own, when the hop does not say), and to
// the address that answered it where the check knows it.
func chargeOf(host string, hops []store.Hop) (c charge, names, addrs map[string]int) {
	names, addrs = make(map[string]int), make(map[string]int)
	if len(hops) == 0 {
		hops = []store.Hop{{Host: host}}
	}
	for _, hop := range hops {
		name := store.CanonicalHost(hop.Host)
		if name == "" {
			name = host
		}
		if names[name] == 0 {
			c.names = append(c.names, name)
		}
		names[name]++
		if addr := store.CanonicalHost(hop.Addr); addr != "" {
			if addrs[addr] == 0 {
				c.addrs = append(c.addrs, addr)
			}
			addrs[addr]++
		}
	}
	return c, names, addrs
}

// admit takes the tokens a check of m needs at now: one from the bucket of
// its host, and one from the bucket of every other host and of every
// address its last check reached. A name is free to make up, an address
// is not, and a redirect is a request to the host it lands on: a monitor
// whose checks land on another host waits for that host's tokens like
// that host's own monitors do, and names pointing at one address share
// its bucket. The first check of a new name goes on its name alone. It
// reports what was taken and false when a bucket was empty, in which case
// nothing was. A monitor that makes no request is always admitted.
func (s *Scheduler) admit(m store.Monitor, now time.Time) (charge, bool) {
	host := m.Host()
	if host == "" {
		return charge{}, true
	}
	last := s.peer(m.ID)
	c := charge{names: []string{host}, addrs: last.addrs}
	for _, name := range last.names {
		if name != host {
			c.names = append(c.names, name)
		}
	}
	for i, name := range c.names {
		if !s.names.take(name, now) {
			s.release(m.ID, charge{names: c.names[:i]}, now)
			return charge{}, false
		}
	}
	for i, addr := range c.addrs {
		if !s.addrs.take(addr, now) {
			s.release(m.ID, charge{names: c.names, addrs: c.addrs[:i]}, now)
			return charge{}, false
		}
	}
	s.mu.Lock()
	s.charged[m.ID] = c
	s.mu.Unlock()
	return c, true
}

// release gives back what admit took for a check that was not run after
// all.
func (s *Scheduler) release(id int64, c charge, now time.Time) {
	for _, name := range c.names {
		s.names.refund(name, now)
	}
	for _, addr := range c.addrs {
		s.addrs.refund(addr, now)
	}
	s.mu.Lock()
	delete(s.charged, id)
	s.mu.Unlock()
}

// settle charges what a finished check of m made beyond what admit took
// in advance: every request to the host it went to and to the address
// that answered it, less the token each of those keys was charged before
// the check; a token taken for a key the check did not reach after all
// comes back. It remembers the hosts and addresses the check reached, for
// the next admit; the addresses of the last check that reached any, when
// this one connected nowhere.
func (s *Scheduler) settle(m store.Monitor, hops []store.Hop, now time.Time) {
	host := m.Host()
	if host == "" {
		return
	}
	s.mu.Lock()
	advance := s.charged[m.ID]
	delete(s.charged, m.ID)
	s.mu.Unlock()
	made, names, addrs := chargeOf(host, hops)
	for _, name := range advance.names {
		names[name]--
	}
	for _, addr := range advance.addrs {
		addrs[addr]--
	}
	for name, n := range names {
		switch {
		case n > 0:
			s.names.debit(name, n, now)
		case n < 0:
			s.names.refund(name, now)
		}
	}
	for addr, n := range addrs {
		switch {
		case n > 0:
			s.addrs.debit(addr, n, now)
		case n < 0:
			s.addrs.refund(addr, now)
		}
	}
	s.mu.Lock()
	// A check that connected nowhere says nothing about where the next one
	// goes: what the last connected check reached stays remembered, names
	// and addresses alike, so the next check is held to it in advance.
	if len(hops) == 1 && hops[0].Addr == "" {
		if last, ok := s.peers[m.ID]; ok {
			made = last
		}
	}
	s.peers[m.ID] = made
	s.mu.Unlock()
}

// peer is what the monitor's last check reached, empty when unknown.
func (s *Scheduler) peer(id int64) charge {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.peers[id]
}

// peerSet is the addresses the enabled monitors' last checks reached, for
// keeping their buckets.
func (s *Scheduler) peerSet() map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	set := make(map[string]bool, len(s.peers))
	for _, c := range s.peers {
		for _, addr := range c.addrs {
			set[addr] = true
		}
	}
	return set
}

// throttle notes that a due check of the monitor waited for a token at
// now: the first time, when the wait began, for the API to show, and for
// the metric.
func (s *Scheduler) throttle(id int64, host string, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, waiting := s.throttled[id]; waiting {
		return
	}
	s.throttled[id] = now
	s.opts.Metrics.TargetThrottled(host)
}

// unthrottle notes that the monitor's check got its tokens.
func (s *Scheduler) unthrottle(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.throttled, id)
}

// ThrottledSince returns since when the monitor's due check has been
// waiting for its host's ceiling on checks a minute, or the zero time when
// it is not waiting.
func (s *Scheduler) ThrottledSince(id int64) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.throttled[id]
}
