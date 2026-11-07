package api

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeHostResolver answers for the names it was given: a name with
// addresses resolves to them and is its own canonical name unless a CNAME
// was set for it; any other name does not exist. It counts the lookups, so
// that a test can tell none was made.
type fakeHostResolver struct {
	mu      sync.Mutex
	addrs   map[string][]netip.Addr
	cnames  map[string]string
	fail    error
	lookups int
}

func newFakeHostResolver() *fakeHostResolver {
	return &fakeHostResolver{addrs: map[string][]netip.Addr{}, cnames: map[string]string{}}
}

// set gives host (absolute, with its trailing dot) the addresses addrs, and
// no CNAME.
func (f *fakeHostResolver) set(host string, addrs ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.addrs[host] = nil
	for _, a := range addrs {
		f.addrs[host] = append(f.addrs[host], netip.MustParseAddr(a))
	}
	delete(f.cnames, host)
}

func (f *fakeHostResolver) cname(host, to string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cnames[host] = to
}

func (f *fakeHostResolver) failWith(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail = err
}

func (f *fakeHostResolver) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lookups
}

func (f *fakeHostResolver) LookupCNAME(_ context.Context, host string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lookups++
	if f.fail != nil {
		return "", f.fail
	}
	if to, ok := f.cnames[host]; ok {
		return to, nil
	}
	if _, ok := f.addrs[host]; !ok {
		return "", &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	return host, nil
}

func (f *fakeHostResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lookups++
	if f.fail != nil {
		return nil, f.fail
	}
	addrs, ok := f.addrs[host]
	if !ok {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	return addrs, nil
}

// lbAddr and lbAddr6 are the addresses of the load balancer in front of the
// UI, where the public URL's host points: a GCE Ingress's forwarding rule,
// which appends them to X-Forwarded-For after the client.
const (
	lbAddr  = "34.120.0.10"
	lbAddr6 = "2600:1901:0:1::10"
)

// The public URL's host is resolved to the addresses a source is compared
// with. It is silent where it cannot be sure the addresses are the load
// balancer's: a host that does not resolve, one behind a CNAME (a CDN, a
// cloud load balancer's own name), and one of loopback addresses only, as
// localhost behind a port-forward. An address in the URL is taken as it is,
// with no lookup. The pod's search domains do not apply: the name is looked
// up absolute.
func TestPublicAddressesResolve(t *testing.T) {
	for _, tc := range []struct {
		name       string
		publicURL  string
		setup      func(*fakeHostResolver)
		has, lacks []string
		lookups    int
	}{
		{
			name: "the host's addresses", publicURL: "https://kubeat.example.com",
			setup: func(f *fakeHostResolver) { f.set("kubeat.example.com.", lbAddr, lbAddr6) },
			has:   []string{lbAddr, lbAddr6, "::ffff:" + lbAddr}, lacks: []string{"203.0.113.9", "10.128.2.15"}, lookups: 2,
		},
		{
			name: "a host in capitals, with a port, a path and a trailing dot", publicURL: "https://Kubeat.Example.COM.:8443/kubeat/",
			setup: func(f *fakeHostResolver) { f.set("Kubeat.Example.COM.", lbAddr) },
			has:   []string{lbAddr}, lookups: 2,
		},
		{
			name: "a CNAME to another name, as for a CDN", publicURL: "https://kubeat.example.com",
			setup: func(f *fakeHostResolver) {
				f.set("kubeat.example.com.", lbAddr)
				f.cname("kubeat.example.com.", "kubeat.example.com.cdn.example.net.")
			},
			lacks: []string{lbAddr}, lookups: 1,
		},
		{
			name: "a canonical name that differs only in case is no CNAME", publicURL: "https://kubeat.example.com",
			setup: func(f *fakeHostResolver) {
				f.set("kubeat.example.com.", lbAddr)
				f.cname("kubeat.example.com.", "KUBEAT.example.com.")
			},
			has: []string{lbAddr}, lookups: 2,
		},
		{
			name: "a host that does not resolve", publicURL: "https://kubeat.example.com",
			setup: func(*fakeHostResolver) {},
			lacks: []string{lbAddr}, lookups: 1,
		},
		{
			name: "a lookup that fails", publicURL: "https://kubeat.example.com",
			setup: func(f *fakeHostResolver) {
				f.set("kubeat.example.com.", lbAddr)
				f.failWith(errors.New("i/o timeout"))
			},
			lacks: []string{lbAddr}, lookups: 1,
		},
		{
			name: "localhost, as behind a port-forward", publicURL: "http://localhost:3000",
			setup: func(f *fakeHostResolver) { f.set("localhost.", "127.0.0.1", "::1") },
			lacks: []string{"127.0.0.1", "::1"}, lookups: 2,
		},
		{
			name: "loopback, link-local and unspecified addresses beside a real one", publicURL: "https://kubeat.example.com",
			setup: func(f *fakeHostResolver) {
				f.set("kubeat.example.com.", "127.0.0.2", "169.254.1.1", "fe80::1", "0.0.0.0", lbAddr)
			},
			has: []string{lbAddr}, lacks: []string{"127.0.0.2", "169.254.1.1", "fe80::1", "0.0.0.0"}, lookups: 2,
		},
		{
			name: "a short name is looked up absolute, not through the search domains", publicURL: "http://kubeat",
			setup: func(f *fakeHostResolver) { f.set("kubeat.kubeat.svc.cluster.local.", "10.96.0.20") },
			lacks: []string{"10.96.0.20"}, lookups: 1,
		},
		{
			name: "an address in the URL", publicURL: "https://" + lbAddr,
			setup: func(*fakeHostResolver) {},
			has:   []string{lbAddr}, lacks: []string{"34.120.0.11"}, lookups: 0,
		},
		{
			name: "an IPv6 address in the URL", publicURL: "https://[" + lbAddr6 + "]:8443",
			setup: func(*fakeHostResolver) {},
			has:   []string{lbAddr6}, lookups: 0,
		},
		{
			name: "a loopback address in the URL", publicURL: "http://127.0.0.1:3000",
			setup: func(*fakeHostResolver) {},
			lacks: []string{"127.0.0.1"}, lookups: 0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolver := newFakeHostResolver()
			tc.setup(resolver)
			p := NewPublicAddresses(tc.publicURL, resolver, nil)
			if p == nil {
				t.Fatalf("NewPublicAddresses(%q) = nil", tc.publicURL)
			}
			p.Refresh(context.Background())
			for _, addr := range tc.has {
				if !p.Contains(addr) {
					t.Errorf("Contains(%q) = false, want true", addr)
				}
			}
			for _, addr := range tc.lacks {
				if p.Contains(addr) {
					t.Errorf("Contains(%q) = true, want false", addr)
				}
			}
			if got := resolver.calls(); got != tc.lookups {
				t.Errorf("%d lookups, want %d", got, tc.lookups)
			}
			// What a source may look like when it is not an address.
			for _, source := range []string{"", "not-an-address", lbAddr + ":443", "[" + lbAddr6 + "]"} {
				if p.Contains(source) {
					t.Errorf("Contains(%q) = true for no address", source)
				}
			}
			if got := resolver.calls(); got != tc.lookups {
				t.Errorf("Contains looked up: %d lookups, want %d", got, tc.lookups)
			}
		})
	}

	// Without a public URL there is nothing to compare with, and a nil
	// *PublicAddresses holds nothing and runs no loop.
	for _, publicURL := range []string{"", "://", "https://"} {
		if p := NewPublicAddresses(publicURL, newFakeHostResolver(), nil); p != nil {
			t.Errorf("NewPublicAddresses(%q) = %+v, want nil", publicURL, p)
		}
	}
	var none *PublicAddresses
	if none.Contains(lbAddr) {
		t.Error("a nil *PublicAddresses contains an address")
	}
	none.Refresh(context.Background())
	none.Run(context.Background())
}

// The host is resolved when Run starts and then every five minutes, by the
// loop's clock, and what a lookup finds takes the place of what the one
// before found: a load balancer that moves is followed, and a host that
// stops resolving quiets the signal rather than keep an address that may be
// a client's by now. The requests' path does no lookup. The log says what
// was found once, and again only when it changes.
func TestPublicAddressesRefreshEveryFiveMinutes(t *testing.T) {
	resolver := newFakeHostResolver()
	resolver.set("kubeat.example.com.", lbAddr)
	var logs syncBuffer
	p := NewPublicAddresses("https://kubeat.example.com", resolver, slog.New(slog.NewTextHandler(&logs, nil)))
	// The loop's clock: each wait it begins is handed to the test, which
	// ends it by hand. A wait begins once the lookup before it is done.
	waits := make(chan time.Duration)
	tick := make(chan time.Time)
	p.after = func(d time.Duration) <-chan time.Time {
		waits <- d
		return tick
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		p.Run(ctx)
	}()
	waited := func() {
		t.Helper()
		if d := <-waits; d != 5*time.Minute {
			t.Fatalf("the loop waits %s, want 5m0s", d)
		}
	}

	waited() // resolved at start
	if !p.Contains(lbAddr) || resolver.calls() != 2 {
		t.Fatalf("at start: Contains(%s) = %v after %d lookups, want true after 2", lbAddr, p.Contains(lbAddr), resolver.calls())
	}
	for range 100 {
		p.Contains(lbAddr)
		p.Contains("203.0.113.9")
	}
	if got := resolver.calls(); got != 2 {
		t.Errorf("Contains looked up: %d lookups, want 2", got)
	}

	// The same answer five minutes on: nothing new is logged.
	tick <- time.Time{}
	waited()
	if resolver.calls() != 4 {
		t.Errorf("%d lookups after the first refresh, want 4", resolver.calls())
	}

	// The load balancer moves.
	resolver.set("kubeat.example.com.", "34.120.0.11")
	tick <- time.Time{}
	waited()
	if p.Contains(lbAddr) || !p.Contains("34.120.0.11") {
		t.Errorf("after the move: Contains(%s) = %v, Contains(34.120.0.11) = %v; want false, true",
			lbAddr, p.Contains(lbAddr), p.Contains("34.120.0.11"))
	}

	// The host stops resolving: nothing is kept.
	resolver.failWith(errors.New("i/o timeout"))
	tick <- time.Time{}
	waited()
	if p.Contains("34.120.0.11") {
		t.Error("an address kept after the host stopped resolving")
	}

	cancel()
	<-done

	got := logs.String()
	if n := strings.Count(got, "level=INFO"); n != 3 {
		t.Errorf("%d lines logged, want 3 (found, moved, lost): %q", n, got)
	}
	for _, want := range []string{
		"addresses=" + lbAddr, "addresses=34.120.0.11", "host=kubeat.example.com",
		"no check of the hops against the public URL's addresses", "i/o timeout",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the log lacks %q: %q", want, got)
		}
	}
}

// An address written in the URL is taken once: Run does not loop.
func TestPublicAddressesLiteralRunsOnce(t *testing.T) {
	p := NewPublicAddresses("https://"+lbAddr, newFakeHostResolver(), nil)
	p.after = func(time.Duration) <-chan time.Time {
		t.Error("the loop waits for an address written in the URL")
		return nil
	}
	p.Run(context.Background())
	if !p.Contains(lbAddr) {
		t.Errorf("Contains(%s) = false", lbAddr)
	}
}

// A CNAME is said in the log, so that an operator who waits for the signal
// knows why it stays quiet.
func TestPublicAddressesLogsWhyQuiet(t *testing.T) {
	resolver := newFakeHostResolver()
	resolver.set("kubeat.example.com.", lbAddr)
	resolver.cname("kubeat.example.com.", "edge.cdn.example.net.")
	var logs bytes.Buffer
	p := NewPublicAddresses("https://kubeat.example.com", resolver, slog.New(slog.NewTextHandler(&logs, nil)))
	p.Refresh(context.Background())
	p.Refresh(context.Background())
	if got := logs.String(); strings.Count(got, "level=INFO") != 1 || !strings.Contains(got, "CNAME") {
		t.Errorf("want one INFO naming the CNAME, got %q", got)
	}
}
