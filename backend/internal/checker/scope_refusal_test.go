package checker

import (
	"context"
	"net/http"
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
	if d := got.Diagnostics; d != nil && d.Explanation != nil && d.Explanation.Code == explain.BlockedByScope {
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
// fails with the context's error. Nor is one refused by the spelling of a
// host while its context had already ended; its next check, in time, is the
// first refusal.
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
	cutShort(t, "another team's Service, the context ended", c, c.Check(ended, other), r, other, context.Canceled)
	refusedByTheScope(t, "another team's Service, in time", check(c, other), r, "api.payments.svc")
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
