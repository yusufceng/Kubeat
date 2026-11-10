package checker

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/yusufceng/Kubeat/backend/internal/explain"
	"github.com/yusufceng/Kubeat/backend/internal/netguard"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// expiring is a context whose deadline passes when expire is called, on no
// clock, so that a test of a check cut short by its deadline waits for
// nothing.
type expiring struct {
	context.Context
	done chan struct{}
	once sync.Once
}

func newExpiring() *expiring {
	return &expiring{Context: context.Background(), done: make(chan struct{})}
}

func (e *expiring) Done() <-chan struct{} { return e.done }

func (e *expiring) Err() error {
	select {
	case <-e.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

func (e *expiring) expire() { e.once.Do(func() { close(e.done) }) }

// endingResolver ends the context of the check under way the moment the
// scope asks it about a name, by end, and answers once that context is
// done: a lookup cut short by the check's timeout or a shutdown.
type endingResolver struct{ end func() }

func (r endingResolver) LookupNetIP(ctx context.Context, _, _ string) ([]netip.Addr, error) {
	r.end()
	<-ctx.Done()
	return nil, ctx.Err()
}

// cutShort asserts that got is the failure of a check its context ended,
// with the context's error, and not the scope's refusal: nothing told, so
// nothing counted or written down, and nothing kept as reported.
func cutShort(t *testing.T, what string, c *Checker, got store.CheckResult, r *refusals, m store.Monitor, want error) {
	t.Helper()
	if got.Up || got.Error == scopeRefusalText || !strings.Contains(got.Error, want.Error()) {
		t.Errorf("%s: up = %v, error %q; want the context's error %q", what, got.Up, got.Error, want)
	}
	if d := got.Diagnostics; d != nil && (d.ScopeReason != "" ||
		d.Explanation != nil && (d.Explanation.Code == explain.BlockedByScope || d.Explanation.Code == explain.BlockedByScopeObject)) {
		t.Errorf("%s: explained as the scope's refusal: %+v", what, d)
	}
	if seen := r.take(); len(seen) != 0 {
		t.Errorf("%s: told %+v, want no refusal", what, seen)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if reported, ok := c.reported[m.ID]; ok {
		t.Errorf("%s: kept as reported: %v", what, reported)
	}
}

// A check whose context ends, by cancellation (a shutdown) or by its
// deadline (the monitor's timeout), while the scope still waits for the
// resolver about a connection the check opens, is no refusal of the scope:
// the counter, the log and the audit log are not told of it, and the check
// fails with the context's error. A host refused by its spelling is no
// lookup a context can end: its refusal stands whatever the context, and is
// told as the first; the next check's is not.
func TestScopeRefusal_CheckCutShort(t *testing.T) {
	dns := scopeDNS(t)
	proxyURL, _ := url.Parse("http://127.0.0.1:1") // never reached
	toProxy := func(*http.Request) (*url.URL, error) { return proxyURL, nil }
	r := &refusals{}
	m := held(httpMonitor("http://public.example.test/ok"))

	cancelled, cancel := context.WithCancel(context.Background())
	defer cancel()
	deadline := newExpiring()
	for name, tc := range map[string]struct {
		ctx  context.Context
		end  func()
		want error
	}{
		"cancelled":         {cancelled, cancel, context.Canceled},
		"past its deadline": {deadline, deadline.expire, context.DeadlineExceeded},
	} {
		c := scopedChecker(t, dns, true, r, Options{Proxy: toProxy})
		c.opts.Scope.Rules.Resolver = endingResolver{tc.end}
		cutShort(t, name, c, c.Check(tc.ctx, m), r, m, tc.want)
	}

	c := scopedChecker(t, dns, true, r, Options{})
	other := held(httpMonitor("http://api.payments.svc/"))
	ended, end := context.WithCancel(context.Background())
	end()
	told := refusedByTheScope(t, "another team's Service, the context ended", c.Check(ended, other), r, "api.payments.svc")
	if len(told) == 1 && !told[0].first {
		t.Error("another team's Service, the context ended: not told as the first refusal")
	}
	told = refusedByTheScope(t, "another team's Service, in time", check(c, other), r, "api.payments.svc")
	if len(told) == 1 && told[0].first {
		t.Error("another team's Service, in time: told as the first refusal again")
	}
}

// A connection the scope refused as leading outside it stands when the check
// then runs out of time: a name answering with a cluster address and an
// outside one is refused the first, the dial goes on to the second, and
// the check's deadline passes while that one hangs. The check is the
// scope's refusal, for a plain monitor and for an inverted one, which must
// not take it for the failure it expects; it is counted, logged and written
// down, as the first. Were it dropped with the deadline, whoever controls
// the name's answers could try another team's Service and leave no trace.
func TestScopeRefusal_OutsideStandsPastDeadline(t *testing.T) {
	dns := dnsServer(t, func(q dnsmessage.Question) []dnsmessage.ResourceBody {
		switch {
		case strings.ToLower(q.Name.String()) != "mixed.example.test.":
			return nil
		case q.Type != dnsmessage.TypeA:
			return []dnsmessage.ResourceBody{}
		}
		// In this order: the cluster address is dialed, and refused, first.
		return []dnsmessage.ResourceBody{
			&dnsmessage.AResource{A: netip.MustParseAddr("127.0.0.2").As4()},
			&dnsmessage.AResource{A: netip.MustParseAddr("127.0.0.1").As4()},
		}
	})
	for _, inverted := range []bool{false, true} {
		deadline := newExpiring()
		release := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
			deadline.expire() // the monitor's timeout passes while the target hangs
			select {
			case <-req.Context().Done():
			case <-release:
			}
		}))
		_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
		r := &refusals{}
		c := scopedChecker(t, dns, true, r, Options{})
		m := held(httpMonitor("http://mixed.example.test:" + port + "/hang"))
		m.Inverted = inverted
		what := fmt.Sprintf("inverted %v", inverted)
		got := c.Check(deadline, m)
		if deadline.Err() == nil {
			t.Errorf("%s: the check never reached the address outside the cluster", what)
		}
		told := refusedByTheScope(t, what, got, r, "mixed.example.test")
		if len(told) == 1 && !told[0].first {
			t.Errorf("%s: not told as the first refusal", what)
		}
		close(release)
		srv.Close()
	}
}

// A check's refusal is told by why: a connection the check opens to a name
// that does not resolve is unresolved, one to a name that leads into the
// cluster is outside. A monitor refused check after check is one finding for
// each reason under its settings: each is told as the first once, so that a
// fault of the cluster's DNS before does not hide the refusal of an attempt
// after, and a name that stops resolving again is no new finding.
func TestScopeEveryConnection_ToldOncePerReason(t *testing.T) {
	var mu sync.Mutex
	answer := "" // where flip.example.test resolves; "" for nowhere
	dns := dnsServer(t, func(q dnsmessage.Question) []dnsmessage.ResourceBody {
		name := strings.ToLower(q.Name.String())
		ip := scopeNames[name]
		if name == "flip.example.test." {
			mu.Lock()
			ip = answer
			mu.Unlock()
		}
		switch {
		case ip == "":
			return nil
		case q.Type != dnsmessage.TypeA:
			return []dnsmessage.ResourceBody{}
		}
		return []dnsmessage.ResourceBody{&dnsmessage.AResource{A: netip.MustParseAddr(ip).As4()}}
	})
	_, port := scopeServer(t)
	r := &refusals{}
	c := scopedChecker(t, dns, true, r, Options{})
	m := held(httpMonitor("http://public.example.test:" + port + "/to?host=flip.example.test"))
	for _, ip := range []string{"", "", "127.0.0.2", "127.0.0.2", ""} {
		mu.Lock()
		answer = ip
		mu.Unlock()
		if got := check(c, m); got.Up || got.Error != scopeRefusalText {
			t.Errorf("with flip.example.test at %q: %+v, want the scope's refusal", ip, got)
		}
	}
	type told struct {
		why   netguard.Refusal
		first bool
	}
	var got []told
	for _, s := range r.take() {
		if s.host != "flip.example.test" {
			t.Errorf("told the refusal of %s", s.host)
		}
		got = append(got, told{s.why, s.first})
	}
	want := []told{
		{netguard.RefusedUnresolved, true}, {netguard.RefusedUnresolved, false},
		{netguard.RefusedOutside, true}, {netguard.RefusedOutside, false},
		{netguard.RefusedUnresolved, false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("told %+v, want %+v", got, want)
	}
}

// The error of a refused check is the same for every source and reason, so
// it offers no remedy of its own: a remedy fit for a user's monitor is wrong
// for an annotation monitor or a DNS outage, and the explanation a
// notification adds after this error gives the one that fits. It says both
// causes and names no address.
func TestScopeRefusalErrorOffersNoRemedy(t *testing.T) {
	for _, not := range []string{"administrator", "set up by", "service.namespace.svc"} {
		if strings.Contains(scopeRefusalText, not) {
			t.Errorf("the refusal error says %q: %s", not, scopeRefusalText)
		}
	}
	for _, says := range []string{"refused by the target scope", "outside the scope", "did not resolve"} {
		if !strings.Contains(scopeRefusalText, says) {
			t.Errorf("the refusal error does not say %q: %s", says, scopeRefusalText)
		}
	}
}
