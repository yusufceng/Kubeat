package scheduler

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/metrics"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// A bucket holds a minute's worth of checks and fills at the ceiling's
// rate; debt from the requests a check made beyond its token is waited
// out; a nil limiter allows everything.
func TestHostLimiterBucket(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0)
	l := newHostLimiter(6) // one token every 10s
	for i := 0; i < 6; i++ {
		if !l.take("a.example", t0) {
			t.Fatalf("take %d refused within the minute's worth", i+1)
		}
	}
	if l.take("a.example", t0) {
		t.Error("a seventh check in the same instant was allowed")
	}
	if !l.take("b.example", t0) {
		t.Error("another host was held to the first's bucket")
	}
	if l.take("a.example", t0.Add(9*time.Second)) {
		t.Error("a token came before 10s")
	}
	if !l.take("a.example", t0.Add(10*time.Second)) {
		t.Error("no token after 10s")
	}
	l.refund("a.example", t0.Add(10*time.Second))
	if !l.take("a.example", t0.Add(10*time.Second)) {
		t.Error("the refunded token was not there")
	}
	// Two requests owed on top of the one taken: the next token comes 20s
	// later than it would have.
	l.debit("a.example", 2, t0.Add(10*time.Second))
	if l.take("a.example", t0.Add(39*time.Second)) {
		t.Error("the debt was not waited out")
	}
	if !l.take("a.example", t0.Add(40*time.Second)) {
		t.Error("no token once the debt was paid")
	}
	// A bucket never fills past the ceiling, however long it rests.
	for i := 0; i < 6; i++ {
		if !l.take("b.example", t0.Add(time.Hour)) {
			t.Fatalf("after an hour: take %d refused", i+1)
		}
	}
	if l.take("b.example", t0.Add(time.Hour)) {
		t.Error("the bucket filled past the ceiling")
	}
	var none *hostLimiter
	if !none.take("x", t0) {
		t.Error("a nil limiter refused a check")
	}
	none.refund("x", t0)
	none.debit("x", 3, t0)
	none.keep(nil, t0)
}

// A bucket outlives the last monitor of its host by a minute, so that a
// host does not get a full bucket back by having its monitor deleted and
// made anew, or paused and resumed; a bucket in use is kept however long
// its host is without a monitor.
func TestBucketOutlivesItsHostForAMinute(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0)
	l := newHostLimiter(1)
	if !l.take("a.example", t0) || !l.take("b.example", t0) || !l.take("c.example", t0) {
		t.Fatal("the first checks were refused")
	}
	l.keep(map[string]bool{"b.example": true}, t0) // a and c have no monitor now
	if len(l.buckets) != 3 {
		t.Fatalf("buckets forgotten on the tick their hosts vanished: %v", l.buckets)
	}
	if l.take("a.example", t0.Add(30*time.Second)) {
		t.Error("a.example got a full bucket back within the minute")
	}
	// Used at +30s, a's bucket lives on; c's, unused, is forgotten at +60s.
	l.keep(map[string]bool{"b.example": true}, t0.Add(time.Minute))
	if _, ok := l.buckets["c.example"]; ok {
		t.Error("c.example's bucket was kept a minute after its host vanished")
	}
	if _, ok := l.buckets["a.example"]; !ok {
		t.Error("a.example's bucket was forgotten while in use")
	}
	if _, ok := l.buckets["b.example"]; !ok {
		t.Error("b.example's bucket was forgotten while it has a monitor")
	}
	l.keep(map[string]bool{"b.example": true}, t0.Add(90*time.Second))
	if _, ok := l.buckets["a.example"]; ok || len(l.buckets) != 1 {
		t.Errorf("after a minute unused: %v, want only b.example", l.buckets)
	}
}

// At the scheduler: a host's monitor is deleted and another is made for
// the same host within the minute; the new one waits for the old one's
// tokens to come back.
func TestTickKeepsAVanishedHostsBucketForAMinute(t *testing.T) {
	st := &fakeStore{}
	st.setMonitors(targeting(1, store.MonitorHTTP, "https://example.com/a"))
	opts := testOptions()
	opts.TargetMaxRate = 1
	s := New(st, nil, opts)
	t0 := time.Unix(1_700_000_000, 0)
	schedules := map[int64]*schedule{1: {next: t0}}
	jobs := make(chan store.Monitor, 16)

	s.tick(context.Background(), t0.Add(time.Second), false, schedules, jobs)
	if got := drain(jobs); len(got) != 1 || got[0] != 1 {
		t.Fatalf("queued %v, want [1]", got)
	}
	st.setMonitors()
	s.tick(context.Background(), t0.Add(2*time.Second), false, schedules, jobs)
	st.setMonitors(targeting(2, store.MonitorHTTP, "https://example.com/b"))
	s.tick(context.Background(), t0.Add(3*time.Second), false, schedules, jobs)
	s.tick(context.Background(), t0.Add(4*time.Second), false, schedules, jobs)
	if got := drain(jobs); len(got) != 0 {
		t.Errorf("the new monitor on the same host ran at %v: the host got a full bucket back", got)
	}
	if s.ThrottledSince(2).IsZero() {
		t.Error("monitor 2 is not marked waiting")
	}
}

// Names are free to make up, addresses are not: once the checks of
// several names are known to reach one address, that address's bucket, at
// the address ceiling's rate, holds them all together; the note names the
// monitor whose check waited.
func TestTickHoldsNamesSharingAnAddressToOneCeiling(t *testing.T) {
	st := &fakeStore{}
	var ms []store.Monitor
	for i := int64(1); i <= 3; i++ {
		ms = append(ms, targeting(i, store.MonitorHTTP, fmt.Sprintf("https://a%d.attacker.example/", i)))
	}
	ms = append(ms, targeting(4, store.MonitorHTTP, "https://unrelated.example/"))
	st.setMonitors(ms...)
	opts := testOptions()
	opts.TargetMaxRate, opts.AddressMaxRate = 2, 2
	s := New(st, nil, opts)
	t0 := time.Unix(1_700_000_000, 0)
	// The last check of each attacker name reached the victim's address.
	for i := int64(1); i <= 3; i++ {
		s.settle(ms[i-1], []store.Hop{{Host: ms[i-1].Host(), Addr: "203.0.113.5"}}, t0.Add(-time.Hour))
	}
	s.settle(ms[3], []store.Hop{{Host: "unrelated.example", Addr: "198.51.100.9"}}, t0.Add(-time.Hour))
	schedules := map[int64]*schedule{1: {next: t0.Add(-3 * time.Second)}, 2: {next: t0.Add(-2 * time.Second)}, 3: {next: t0.Add(-time.Second)}}
	jobs := make(chan store.Monitor, 16)

	s.tick(context.Background(), t0, false, schedules, jobs)
	got := drain(jobs)
	if len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 4 {
		t.Errorf("queued %v, want [1 2 4]: two checks of the address, and the unrelated host", got)
	}
	if since := s.ThrottledSince(3); !since.Equal(t0) {
		t.Errorf("monitor 3: ThrottledSince = %v, want %v", since, t0)
	}
	// Each name's own bucket had tokens to spare: it was the address's that
	// was empty, and the name's token went back.
	if b := s.names.external.buckets["a3.attacker.example"]; b == nil || b.credit != 2*tokenCredit {
		t.Errorf("a3.attacker.example's bucket = %+v, want full", b)
	}
	if _, charged := s.charged[3]; charged {
		t.Error("a check that did not run is still charged")
	}
	// At +30s one token has come back to the address: the waiting check runs.
	s.tick(context.Background(), t0.Add(30*time.Second), false, schedules, jobs)
	if got := drain(jobs); len(got) != 1 || got[0] != 3 {
		t.Errorf("tick at +30s queued %v, want [3]", got)
	}
}

// The address ceiling is the address's own setting, whatever the class
// of the address or of the name that reaches it: a public name whose
// checks reach a private address, or a public one, is held to it; zero
// holds no address at all.
func TestAddressCeilingIsItsOwn(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0)
	for _, addr := range []string{"10.0.0.5", "203.0.113.5"} {
		m := targeting(1, store.MonitorHTTP, "https://intranet.corp/")
		st := &fakeStore{}
		st.setMonitors(m)
		opts := testOptions()
		opts.TargetMaxRate, opts.ClusterTargetMaxRate, opts.AddressMaxRate = 60, 600, 1
		s := New(st, nil, opts)
		s.settle(m, []store.Hop{{Host: "intranet.corp", Addr: addr}}, t0)
		if b := s.addrs.buckets[addr]; b == nil || b.credit != 0 {
			t.Fatalf("%s: the address was not charged at the address ceiling: %+v", addr, b)
		}
		schedules := map[int64]*schedule{1: {next: t0}}
		jobs := make(chan store.Monitor, 16)
		s.tick(context.Background(), t0.Add(time.Second), false, schedules, jobs)
		if got := drain(jobs); len(got) != 0 {
			t.Errorf("%s: queued %v, want nothing: the address's bucket is empty", addr, got)
		}
		if b := s.names.external.buckets["intranet.corp"]; b == nil || b.credit != 60*tokenCredit {
			t.Errorf("%s: the name's bucket = %+v, want full: its token went back", addr, b)
		}

		opts.AddressMaxRate = 0
		s = New(st, nil, opts)
		s.settle(m, []store.Hop{{Host: "intranet.corp", Addr: addr}}, t0)
		s.tick(context.Background(), t0.Add(time.Second), false, schedules, jobs)
		if got := drain(jobs); len(got) != 1 {
			t.Errorf("%s: with the address ceiling off, queued %v, want the check", addr, got)
		}
	}
}

// Many names behind one address, as the hosts of an ingress or a router
// are, fit under the address ceiling at the default rates: the names'
// own ceiling is the external one, the address's is its own and higher.
func TestNamesBehindOneAddressFitUnderTheAddressCeiling(t *testing.T) {
	st := &fakeStore{}
	const n = 60
	var ms []store.Monitor
	for i := 1; i <= n; i++ {
		m := targeting(int64(i), store.MonitorHTTP, fmt.Sprintf("https://app%d.apps.example.com/healthz", i))
		m.Interval = 30 * time.Second
		ms = append(ms, m)
	}
	st.setMonitors(ms...)
	opts := testOptions()
	opts.TargetMaxRate, opts.ClusterTargetMaxRate, opts.AddressMaxRate = 60, 600, 600
	s := New(st, nil, opts)
	t0 := time.Unix(1_700_000_000, 0)
	schedules := map[int64]*schedule{}
	for i := 1; i <= n; i++ {
		schedules[int64(i)] = &schedule{next: t0.Add(time.Duration(i) * 500 * time.Millisecond), interval: 30 * time.Second}
	}
	jobs := make(chan store.Monitor, 256)
	ran := 0
	for i := 0; i <= 300; i++ { // five minutes, a tick a second, the last one's checks included
		now := t0.Add(time.Duration(i) * time.Second)
		s.tick(context.Background(), now, false, schedules, jobs)
		for _, id := range drain(jobs) {
			ran++
			s.finish(ms[id-1], true, now)
			s.settle(ms[id-1], []store.Hop{{Host: ms[id-1].Host(), Addr: "203.0.113.10"}}, now) // one public load balancer
		}
		for id := 1; id <= n; id++ {
			if !s.ThrottledSince(int64(id)).IsZero() {
				t.Fatalf("at +%ds monitor %d waits: %d names at 30s behind one public address are held under the address ceiling of 600", i, id, n)
			}
		}
	}
	if ran != n*10 {
		t.Errorf("%d checks ran in five minutes, want every one of %d", ran, n*10)
	}
}

// A monitor whose checks land on another host, by a redirect, is held to
// that host's ceiling like the host's own monitors: once a check of it is
// known to have landed there, its next check needs a token of the host it
// lands on too, and waits when there is none, its own host's token given
// back.
func TestRedirectingMonitorWaitsForTheHostItLandsOn(t *testing.T) {
	r := targeting(1, store.MonitorHTTP, "https://attacker.example/")
	m := targeting(2, store.MonitorHTTP, "https://victim.example/")
	st := &fakeStore{}
	st.setMonitors(r, m)
	opts := testOptions()
	opts.TargetMaxRate, opts.AddressMaxRate = 2, 600
	s := New(st, nil, opts)
	t0 := time.Unix(1_700_000_000, 0)
	landing := []store.Hop{{Host: "attacker.example", Addr: "203.0.113.1"}, {Host: "victim.example", Addr: "198.51.100.7"}}
	schedules := map[int64]*schedule{1: {next: t0.Add(-time.Second)}, 2: {next: t0}}
	jobs := make(chan store.Monitor, 16)

	// The first check of the redirecting monitor goes on its own name; its
	// landing is charged to the victim afterwards, and remembered.
	s.tick(context.Background(), t0, false, schedules, jobs)
	if got := drain(jobs); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("first tick queued %v, want [1 2]", got)
	}
	s.finish(r, true, t0)
	s.settle(r, landing, t0)
	s.finish(m, true, t0)
	s.settle(m, landing[1:], t0)
	if b := s.names.external.buckets["victim.example"]; b == nil || b.credit != 0 {
		t.Fatalf("victim.example's bucket = %+v, want empty: its own check and the landing", b)
	}
	if got := s.peer(1); fmt.Sprint(got.names) != "[attacker.example victim.example]" || fmt.Sprint(got.addrs) != "[203.0.113.1 198.51.100.7]" {
		t.Errorf("remembered of the redirecting monitor: %+v", got)
	}

	// Due again with the victim's bucket empty: the redirecting monitor
	// waits although its own host has a token, which it does not keep.
	schedules[1].next, schedules[2].next = t0.Add(time.Second), t0.Add(time.Hour)
	s.tick(context.Background(), t0.Add(time.Second), false, schedules, jobs)
	if got := drain(jobs); len(got) != 0 {
		t.Errorf("tick at +1s queued %v, want nothing: the host it lands on has no token", got)
	}
	if s.ThrottledSince(1).IsZero() {
		t.Error("the redirecting monitor is not marked waiting")
	}
	if b := s.names.external.buckets["attacker.example"]; b == nil || b.credit < tokenCredit {
		t.Errorf("attacker.example's bucket = %+v, want its token still there: the one taken was given back", b)
	}
	if _, charged := s.charged[1]; charged {
		t.Error("a check that did not run is still charged")
	}
	// At +30s the victim has a token again: the check runs, and its
	// landing is already paid for.
	s.tick(context.Background(), t0.Add(30*time.Second), false, schedules, jobs)
	if got := drain(jobs); len(got) != 1 || got[0] != 1 {
		t.Fatalf("tick at +30s queued %v, want [1]", got)
	}
	s.finish(r, true, t0.Add(30*time.Second))
	s.settle(r, landing, t0.Add(30*time.Second))
	if b := s.names.external.buckets["victim.example"]; b == nil || b.credit != 0 {
		t.Errorf("victim.example's bucket = %+v after the paid-for landing, want empty, not in debt", b)
	}
	// A check that no longer lands there gives the victim's token back and
	// forgets the victim. (The monitor's interval is a minute: due at +61s.)
	s.tick(context.Background(), t0.Add(61*time.Second), false, schedules, jobs)
	if got := drain(jobs); len(got) != 1 || got[0] != 1 {
		t.Fatalf("tick at +61s queued %v, want [1]", got)
	}
	s.finish(r, true, t0.Add(61*time.Second))
	s.settle(r, landing[:1], t0.Add(61*time.Second))
	if !s.names.take("victim.example", t0.Add(61*time.Second)) {
		t.Error("victim.example has no token after a check that did not land there: the one taken in advance was not given back")
	}
	if got := s.peer(1); fmt.Sprint(got.names) != "[attacker.example]" {
		t.Errorf("remembered after a check that did not land: %+v", got)
	}
}

// Several names at their own addresses, each redirecting to one victim
// every second, share the victim's ceiling with the victim's own monitor:
// the landings are bounded by the victim's ceiling, not the attackers',
// and the victim's own monitor keeps its cadence.
func TestRedirectLandingsShareTheVictimsCeilingWithItsOwnMonitor(t *testing.T) {
	var rs []store.Monitor
	for i := 1; i <= 5; i++ {
		r := targeting(int64(i), store.MonitorHTTP, fmt.Sprintf("https://a%d.attacker.example/", i))
		r.Interval = time.Second
		rs = append(rs, r)
	}
	m := targeting(6, store.MonitorHTTP, "https://victim.example/")
	m.Interval = 30 * time.Second
	st := &fakeStore{}
	st.setMonitors(append(rs, m)...)
	opts := testOptions()
	opts.TargetMaxRate, opts.AddressMaxRate = 60, 600
	s := New(st, nil, opts)
	t0 := time.Unix(1_700_000_000, 0)
	schedules := map[int64]*schedule{}
	for i := int64(1); i <= 5; i++ {
		schedules[i] = &schedule{next: t0, interval: time.Second}
	}
	schedules[6] = &schedule{next: t0, interval: 30 * time.Second}
	jobs := make(chan store.Monitor, 64)
	landings, ranM := 0, 0
	var lateM time.Duration
	for i := 0; i < 600; i++ { // ten minutes, a tick a second
		now := t0.Add(time.Duration(i) * time.Second)
		s.tick(context.Background(), now, false, schedules, jobs)
		for _, id := range drain(jobs) {
			if id == 6 {
				ranM++
				lateM = max(lateM, now.Sub(t0)%(30*time.Second))
				s.finish(m, true, now)
				s.settle(m, []store.Hop{{Host: "victim.example", Addr: "198.51.100.7"}}, now)
				continue
			}
			landings++
			r := rs[id-1]
			s.finish(r, true, now)
			s.settle(r, []store.Hop{{Host: r.Host(), Addr: fmt.Sprintf("203.0.113.%d", id)}, {Host: "victim.example", Addr: "198.51.100.7"}}, now)
		}
	}
	// A minute's worth at the start, a token a second after, and the five
	// first checks, which went on their own names alone.
	if landings > 60+600+5 || landings < 500 {
		t.Errorf("the victim received %d landings in ten minutes, want about the ceiling's 660", landings)
	}
	if ranM < 18 {
		t.Errorf("the victim's own 30s monitor ran %d times in ten minutes, want its 20", ranM)
	}
	if lateM > 10*time.Second {
		t.Errorf("the victim's own monitor ran up to %s after its due time, want the token within a few seconds", lateM)
	}
}

// What a finished check owes: every request to the host it went to and to
// the address that answered it, less what admit took in advance, which
// comes back where the check did not reach it after all; the hosts and
// addresses reached are remembered for the next check, and the spelling
// of each is canonical.
func TestSettleChargesRedirectHostsAndAddresses(t *testing.T) {
	m := targeting(1, store.MonitorHTTP, "https://a.example/")
	st := &fakeStore{}
	st.setMonitors(m)
	opts := testOptions()
	opts.TargetMaxRate, opts.AddressMaxRate = 10, 10
	s := New(st, nil, opts)
	t0 := time.Unix(1_700_000_000, 0)
	full := 10 * tokenCredit

	// The first check: nothing known, the name alone is charged.
	taken, ok := s.admit(m, t0)
	if !ok || fmt.Sprint(taken) != "{[a.example] []}" {
		t.Fatalf("first admit = %+v %v", taken, ok)
	}
	s.settle(m, []store.Hop{
		{Host: "a.example", Addr: "203.0.113.5"},
		{Host: "victim.example", Addr: "198.51.100.7"},
		{Host: "victim.example.", Addr: "::ffff:198.51.100.7"}, // the same host and address, spelt otherwise
	}, t0)
	credits := func() map[string]int64 {
		got := make(map[string]int64)
		for k, b := range s.names.external.buckets {
			got["name:"+k] = (b.credit - full) / tokenCredit
		}
		for k, b := range s.addrs.buckets {
			got["addr:"+k] = (b.credit - full) / tokenCredit
		}
		return got
	}
	want := map[string]int64{"name:a.example": -1, "name:victim.example": -2, "addr:203.0.113.5": -1, "addr:198.51.100.7": -2}
	if got := credits(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("after the first check, tokens owed:\n got  %v\n want %v", got, want)
	}
	if got := s.peer(1); fmt.Sprint(got) != "{[a.example victim.example] [203.0.113.5 198.51.100.7]}" {
		t.Errorf("remembered = %+v, want every host and address reached, once each", got)
	}

	// The second check: every host and address reached is charged in
	// advance, and the advance is settled against what the check actually
	// reached.
	taken, ok = s.admit(m, t0)
	if !ok || fmt.Sprint(taken) != "{[a.example victim.example] [203.0.113.5 198.51.100.7]}" || fmt.Sprint(s.charged[1]) != fmt.Sprint(taken) {
		t.Fatalf("second admit = %+v %v, charged %v", taken, ok, s.charged)
	}
	s.settle(m, []store.Hop{{Host: "a.example", Addr: "203.0.113.6"}}, t0) // the name moved, and no longer redirects
	want = map[string]int64{"name:a.example": -2, "name:victim.example": -2, "addr:203.0.113.5": -1, "addr:198.51.100.7": -2, "addr:203.0.113.6": -1}
	if got := credits(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("after the second check, tokens owed:\n got  %v\n want %v", got, want)
	}
	if got := s.peer(1); fmt.Sprint(got) != "{[a.example] [203.0.113.6]}" || len(s.charged) != 0 {
		t.Errorf("remembered = %+v, charged = %v; want the new address alone and nothing charged", got, s.charged)
	}

	// A check that could not connect: the advance goes back, the address
	// stays remembered.
	s.admit(m, t0)
	s.settle(m, []store.Hop{{Host: "a.example"}}, t0)
	want["name:a.example"] = -3
	if got := credits(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("after a failed connection, tokens owed:\n got  %v\n want %v", got, want)
	}
	if got := s.peer(1); fmt.Sprint(got) != "{[a.example] [203.0.113.6]}" {
		t.Errorf("remembered = %+v after a failed connection, want the last address known", got)
	}

	// A check abandoned after admit gives everything back.
	taken, _ = s.admit(m, t0)
	s.release(1, taken, t0)
	if got := credits(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("after an abandoned check, tokens owed:\n got  %v\n want %v", got, want)
	}
}

// targeting returns a monitor of the given type and target.
func targeting(id int64, typ store.MonitorType, target string) store.Monitor {
	m := monitor(id, time.Minute)
	m.Type, m.Target = typ, target
	return m
}

// drain returns the monitors a tick queued.
func drain(jobs <-chan store.Monitor) []int64 {
	var ids []int64
	for {
		select {
		case m := <-jobs:
			ids = append(ids, m.ID)
		default:
			return ids
		}
	}
}

// Checks of one external host beyond its ceiling wait for a later tick,
// most overdue first; the monitor says since when, and the metric counts
// the wait. In-cluster targets, and monitors that make no request, are
// not slowed by the external ceiling.
func TestTickHoldsExternalHostsToTheirCeiling(t *testing.T) {
	st := &fakeStore{}
	st.setMonitors(
		targeting(1, store.MonitorHTTP, "https://example.com/a"),
		targeting(2, store.MonitorHTTP, "https://EXAMPLE.com:443/b"), // the same host, however written
		targeting(3, store.MonitorTCP, "example.com:443"),
		targeting(4, store.MonitorHTTP, "https://other.example/"),
		targeting(5, store.MonitorHTTP, "http://api.shop.svc/healthz"),
		targeting(6, store.MonitorTCP, "10.0.0.1:5432"),
		targeting(7, store.MonitorPush, ""),
		targeting(8, store.MonitorWorkload, "shop/deployment/api"),
	)
	opts := testOptions()
	opts.TargetMaxRate = 2 // one token every 30s
	opts.Metrics = metrics.New("test")
	s := New(st, nil, opts)
	t0 := time.Unix(1_700_000_000, 0)
	// Monitors 1 and 2 are the more overdue of the three on example.com,
	// so they are the two that get the host's tokens.
	schedules := map[int64]*schedule{1: {next: t0.Add(-2 * time.Second)}, 2: {next: t0.Add(-time.Second)}}
	jobs := make(chan store.Monitor, 16)

	s.tick(context.Background(), t0, false, schedules, jobs)
	got := drain(jobs)
	want := map[int64]bool{1: true, 2: true, 4: true, 5: true, 6: true, 7: true, 8: true}
	if len(got) != len(want) {
		t.Fatalf("first tick queued %v, want %v", got, want)
	}
	for _, id := range got {
		if !want[id] {
			t.Errorf("first tick queued %d", id)
		}
	}
	if since := s.ThrottledSince(3); !since.Equal(t0) {
		t.Errorf("monitor 3: ThrottledSince = %v, want %v", since, t0)
	}
	if since := s.ThrottledSince(1); !since.IsZero() {
		t.Errorf("monitor 1 is marked waiting: %v", since)
	}
	gathered, _ := opts.Metrics.Gather()
	if gathered["kubeat_target_throttled_total{host=example.com}"] != 1 {
		t.Errorf("metric = %v, want 1 for example.com", gathered["kubeat_target_throttled_total{host=example.com}"])
	}

	// Still waiting at +29s: no job, no new count, the same "since".
	s.tick(context.Background(), t0.Add(29*time.Second), false, schedules, jobs)
	if got := drain(jobs); len(got) != 0 {
		t.Errorf("tick at +29s queued %v", got)
	}
	if since := s.ThrottledSince(3); !since.Equal(t0) {
		t.Errorf("monitor 3 at +29s: ThrottledSince = %v, want %v", since, t0)
	}
	gathered, _ = opts.Metrics.Gather()
	if gathered["kubeat_target_throttled_total{host=example.com}"] != 1 {
		t.Errorf("a wait that goes on was counted again: %v", gathered["kubeat_target_throttled_total{host=example.com}"])
	}

	// At +30s a token has come: the check runs, and the wait is over.
	s.tick(context.Background(), t0.Add(30*time.Second), false, schedules, jobs)
	if got := drain(jobs); len(got) != 1 || got[0] != 3 {
		t.Errorf("tick at +30s queued %v, want [3]", got)
	}
	if since := s.ThrottledSince(3); !since.IsZero() {
		t.Errorf("monitor 3 is still marked waiting: %v", since)
	}

	// What is remembered about a monitor goes when the monitor does.
	s.setThrottledForTest(3, t0)
	st.setMonitors()
	s.tick(context.Background(), t0.Add(time.Hour), false, schedules, jobs)
	if len(s.throttled) != 0 || len(s.names.external.buckets) != 0 {
		t.Errorf("state kept for removed monitors: throttled=%v buckets=%v", s.throttled, s.names.external.buckets)
	}
}

func (s *Scheduler) setThrottledForTest(id int64, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.throttled[id] = at
}

// The in-cluster ceiling is the cluster's own, and does not apply to
// external hosts.
func TestTickHoldsClusterHostsToTheirOwnCeiling(t *testing.T) {
	st := &fakeStore{}
	st.setMonitors(
		targeting(1, store.MonitorHTTP, "http://api.shop.svc/a"),
		targeting(2, store.MonitorHTTP, "http://api.shop.svc/b"),
		targeting(3, store.MonitorHTTP, "https://example.com/"),
		targeting(4, store.MonitorHTTP, "https://example.com/other"),
	)
	opts := testOptions()
	opts.TargetMaxRate, opts.ClusterTargetMaxRate = 0, 1
	s := New(st, nil, opts)
	t0 := time.Unix(1_700_000_000, 0)
	schedules := map[int64]*schedule{1: {next: t0.Add(-time.Second)}} // the more overdue of the two on api.shop.svc
	jobs := make(chan store.Monitor, 16)

	s.tick(context.Background(), t0, false, schedules, jobs)
	got := drain(jobs)
	if len(got) != 3 || got[0] != 1 {
		t.Errorf("queued %v, want monitor 1 and both external ones", got)
	}
	if s.ThrottledSince(2).IsZero() {
		t.Error("monitor 2 is not marked waiting")
	}
}

// A check held back by the ceiling is not a result, let alone a failed
// one: the target was not asked. The monitor whose check ran first gets
// the host's only token of the minute, the other waits.
func TestDeferredCheckIsNotRecorded(t *testing.T) {
	st := &fakeStore{}
	st.setMonitors(
		targeting(1, store.MonitorHTTP, "https://example.com/a"),
		targeting(2, store.MonitorHTTP, "https://example.com/b"),
	)
	for i := range st.monitors {
		st.monitors[i].Interval = 5 * time.Millisecond
	}
	opts := testOptions()
	opts.TargetMaxRate = 1
	s, stop := startScheduler(st, &fakeChecker{}, opts)
	defer stop()

	waitFor(t, "the one check of the minute", func() bool { return st.countFor(1)+st.countFor(2) >= 1 })
	time.Sleep(50 * time.Millisecond) // many intervals, a few dozen ticks
	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.results) != 1 {
		t.Errorf("%d results, want 1: the ceiling is one check a minute", len(st.results))
	}
	for _, r := range st.results {
		if !r.Up {
			t.Errorf("a result was recorded as failed: %+v", r)
		}
	}
	ran := st.results[0].MonitorID
	waiting := int64(1)
	if ran == 1 {
		waiting = 2
	}
	if s.ThrottledSince(waiting).IsZero() {
		t.Errorf("monitor %d is not marked waiting", waiting)
	}
}

// Every request a check makes counts: a check of each pod that reaches
// three pods spends three of the host's tokens, so with a ceiling of three
// a minute the next check waits.
func TestEachRequestCountsAgainstTheCeiling(t *testing.T) {
	m := targeting(1, store.MonitorHTTP, "http://api.shop.svc:8080/healthz")
	m.Interval, m.EachPod, m.Source, m.Namespace = 5*time.Millisecond, true, "service/api:8080", "shop"
	st := &fakeStore{}
	st.setMonitors(m)
	opts := testOptions()
	opts.ClusterTargetMaxRate = 3
	_, stop := startScheduler(st, &fakeChecker{hops: []store.Hop{{Host: "api.shop.svc"}, {Host: "api.shop.svc"}, {Host: "api.shop.svc"}}}, opts)
	defer stop()

	waitFor(t, "the first check", func() bool { return st.countFor(1) >= 1 })
	time.Sleep(50 * time.Millisecond)
	if n := st.countFor(1); n != 1 {
		t.Errorf("%d checks ran, want 1: the first spent the minute's three tokens", n)
	}
}

// A check that connected nowhere says nothing about where the next one
// goes: the hosts and addresses of the last check that connected stay
// remembered, so the next one is still held to them in advance.
func TestFailedConnectionKeepsTheRememberedLandings(t *testing.T) {
	m := targeting(1, store.MonitorHTTP, "https://a.example/")
	st := &fakeStore{}
	st.setMonitors(m)
	opts := testOptions()
	opts.TargetMaxRate, opts.AddressMaxRate = 10, 10
	s := New(st, nil, opts)
	t0 := time.Unix(1_700_000_000, 0)

	s.admit(m, t0)
	s.settle(m, []store.Hop{{Host: "a.example", Addr: "203.0.113.5"}, {Host: "victim.example", Addr: "198.51.100.7"}}, t0)
	taken, _ := s.admit(m, t0)
	s.settle(m, []store.Hop{{Host: "a.example"}}, t0) // connected nowhere
	if got := s.peer(1); fmt.Sprint(got) != fmt.Sprint(taken) || fmt.Sprint(got) != "{[a.example victim.example] [203.0.113.5 198.51.100.7]}" {
		t.Errorf("remembered after a failed connection = %+v, want the last connected check's hosts and addresses", got)
	}
	taken, ok := s.admit(m, t0)
	if !ok || fmt.Sprint(taken) != "{[a.example victim.example] [203.0.113.5 198.51.100.7]}" {
		t.Errorf("admit after a failed connection = %+v %v, want the victim charged in advance", taken, ok)
	}
}
