package netguard

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"sync"
	"testing"
	"time"
)

// scopeTable resolves the names the scope tests use, as the pod's resolver
// would with its search domains: a two-label short name lands on a Service,
// a public name may point into the cluster, a company name at a company
// address; anything else does not exist (NXDOMAIN).
func scopeTable(t *testing.T) *countingResolver {
	return &countingResolver{table: map[string][]netip.Addr{
		"api.payments":        addrsOf(t, "172.30.44.9"),
		"kubernetes.default":  addrsOf(t, "172.30.0.1"),
		"172-30-0-1.sslip.io": addrsOf(t, "172.30.0.1"),
		"status.example.com":  addrsOf(t, "93.184.216.34"),
		"intranet.corp.test":  addrsOf(t, "10.50.0.10"),
		"mixed.example.com":   addrsOf(t, "93.184.216.34", "172.30.44.9"),
	}}
}

// shop is a scope of the namespaces shop and billing.
func shop(ns string) bool { return ns == "shop" || ns == "billing" }

// Every form a host can be written in is read alike by the API's check of a
// target and by a check's connections: refused by its spelling, in scope by
// its namespace, or external and then judged where it lands.
func TestScopeClassifiesEveryForm(t *testing.T) {
	company, err := ParseExternalRanges([]string{"10.50.0.0/16"})
	if err != nil {
		t.Fatal(err)
	}
	rules := ScopeRules{External: company, Resolver: scopeTable(t)}
	for host, want := range map[string]hostClass{
		// A bare ClusterIP or pod IP names no namespace.
		"172.30.0.1":      hostRefused,
		"10.128.2.7":      hostRefused,
		"::ffff:10.0.0.1": hostRefused,
		"fd00::1":         hostRefused,
		// A short name is a Service in Kubeat's own namespace.
		"api": hostRefused,
		// Another team's Service, its full name and a pod name.
		"api.payments.svc":                    hostRefused,
		"api.payments.svc.cluster.local":      hostRefused,
		"10-0-0-1.payments.pod.cluster.local": hostRefused,
		// The API server's names.
		"kubernetes.default.svc":               hostRefused,
		"kubernetes.default.svc.cluster.local": hostRefused,
		// Spellings a proxy or glibc reads otherwise.
		"api.payments.svc..":     hostRefused,
		"KUBERNETES.default.svc": hostRefused,
		"172.30.1":               hostRefused,
		"0xac.0x1e.0.1":          hostRefused,
		"2887647233":             hostRefused,
		"kubernetes.default­":    hostRefused,
		"...":                    hostRefused,
		// One's own namespaces.
		"api.shop.svc":                     hostInScope,
		"api.billing.svc.cluster.local":    hostInScope,
		"10-0-0-1.shop.pod.cluster.local.": hostInScope,
		// External by their spelling, judged where they land: a two-label
		// short name (ndots), a public name, a company address.
		"api.payments":        hostExternal,
		"kubernetes.default":  hostExternal,
		"172-30-0-1.sslip.io": hostExternal,
		"status.example.com":  hostExternal,
		"10.50.1.1":           hostExternal,
		"93.184.216.34":       hostExternal,
	} {
		if _, got := rules.classify(host, shop); got != want {
			t.Errorf("classify(%q) = %v, want %v", host, got, want)
		}
	}
}

// The API's check resolves an external-looking name and refuses one that
// lands in the cluster, or that it cannot prove lands outside.
func TestScopeRulesRefuses(t *testing.T) {
	company, err := ParseExternalRanges([]string{"10.50.0.0/16"})
	if err != nil {
		t.Fatal(err)
	}
	rules := ScopeRules{External: company, Resolver: scopeTable(t)}
	ctx := context.Background()
	for host, refused := range map[string]bool{
		"api.payments":        true, // ndots turns it into another team's Service
		"kubernetes.default":  true, // the API server
		"172-30-0-1.sslip.io": true, // a public name pointing into the cluster
		"mixed.example.com":   true, // one answer in the cluster
		"gone.example.com":    true, // NXDOMAIN: not provably outside
		"status.example.com":  false,
		"intranet.corp.test":  false, // a company address
		"api.shop.svc":        false,
		"api.payments.svc":    true,
	} {
		if got := rules.Refuses(ctx, shop, host, nil); got != refused {
			t.Errorf("Refuses(%q) = %v, want %v", host, got, refused)
		}
	}

	// Behind the proxy exception, only an NXDOMAIN of a proxied request
	// goes through; a name landing in the cluster still does not.
	proxied := rules
	proxied.TrustProxy = true
	proxied.Proxy = func(*http.Request) (*url.URL, error) { return url.Parse("http://proxy.corp.test:3128") }
	request, _ := url.Parse("https://gone.example.com/")
	if proxied.Refuses(ctx, shop, "gone.example.com", request) {
		t.Error("a proxied NXDOMAIN was refused behind the exception")
	}
	if !proxied.Refuses(ctx, shop, "gone.example.com", nil) {
		t.Error("a direct NXDOMAIN went through")
	}
	if !proxied.Refuses(ctx, shop, "api.payments", request) {
		t.Error("a proxied name landing in the cluster went through")
	}
}

// brokenResolver answers every lookup with err, as a resolver in an outage
// does: a SERVFAIL, a timeout of its own.
type brokenResolver struct{ err error }

func (r brokenResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return nil, r.err
}

// stalledResolver answers no lookup: it returns when the lookup's context
// ends, whatever ends it.
type stalledResolver struct{}

func (stalledResolver) LookupNetIP(ctx context.Context, _, _ string) ([]netip.Addr, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// expiring is a context whose deadline passes when expire is called, on no
// clock, so that a test of a lookup cut short by its deadline waits for
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

// Judge tells why a host is refused, for the operator alone: one that leads
// outside the scope by its spelling or by where it resolves, apart from an
// external-looking one the resolver could not answer for. A form refused by
// its spelling is never resolved, so an outage of the resolver cannot pass
// an attempt at another team off as a fault of the resolver: a bare address,
// a short name, another namespace's .svc and full name, the API server's
// names. A two-label name the search domains (ndots) turn into a Service,
// and a public name pointing into the cluster, lead outside where the
// resolver answers and are unresolved where it does not. A lookup the
// context ended is unresolved too; telling it from a fault is the caller's.
func TestScopeRulesJudge(t *testing.T) {
	spelled := []string{
		"172.30.0.1", "10.128.2.7", "fd00::1", // bare cluster addresses
		"api", "kubernetes", // short names
		"api.payments.svc", "api.payments.svc.cluster.local", // another namespace's Service
		"kubernetes.default.svc", "kubernetes.default.svc.cluster.local", // the API server
		"api.payments.svc..", "172.30.1", // spellings a proxy reads otherwise
	}
	resolved := []string{
		"api.payments", "kubernetes.default", // two labels, ndots
		"172-30-0-1.sslip.io", "mixed.example.com", // public names into the cluster
	}
	ctx := context.Background()
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	for name, tc := range map[string]struct {
		ctx      context.Context
		resolver Resolver
		resolved Refusal // what the resolved names and the external one get
		missing  Refusal // what a name that does not exist gets
		external Refusal
	}{
		"resolver answers": {ctx, scopeTable(t), RefusedOutside, RefusedUnresolved, NotRefused},
		"servfail":         {ctx, brokenResolver{&net.DNSError{Err: "server misbehaving", Name: "x", IsTemporary: true}}, RefusedUnresolved, RefusedUnresolved, RefusedUnresolved},
		"resolver timeout": {ctx, brokenResolver{&net.DNSError{Err: "i/o timeout", Name: "x", IsTimeout: true}}, RefusedUnresolved, RefusedUnresolved, RefusedUnresolved},
		"cut short":        {cancelled, stalledResolver{}, RefusedUnresolved, RefusedUnresolved, RefusedUnresolved},
	} {
		t.Run(name, func(t *testing.T) {
			rules := ScopeRules{Resolver: tc.resolver}
			for _, host := range spelled {
				if got := rules.Judge(tc.ctx, shop, host, nil); got != RefusedOutside {
					t.Errorf("Judge(%q) = %v, want RefusedOutside", host, got)
				}
			}
			for _, host := range resolved {
				if got := rules.Judge(tc.ctx, shop, host, nil); got != tc.resolved {
					t.Errorf("Judge(%q) = %v, want %v", host, got, tc.resolved)
				}
			}
			if got := rules.Judge(tc.ctx, shop, "gone.example.com", nil); got != tc.missing {
				t.Errorf("Judge(gone.example.com) = %v, want %v", got, tc.missing)
			}
			if got := rules.Judge(tc.ctx, shop, "status.example.com", nil); got != tc.external {
				t.Errorf("Judge(status.example.com) = %v, want %v", got, tc.external)
			}
			if got := rules.Judge(tc.ctx, shop, "api.shop.svc", nil); got != NotRefused {
				t.Errorf("Judge(api.shop.svc) = %v, want NotRefused", got)
			}
		})
	}

	// The resolver is never asked about a form refused by its spelling.
	counting := scopeTable(t)
	for _, host := range spelled {
		ScopeRules{Resolver: counting}.Judge(ctx, shop, host, nil)
	}
	if counting.calls != 0 {
		t.Errorf("forms refused by their spelling made %d lookups, want 0", counting.calls)
	}

	// Behind the proxy exception a proxied NXDOMAIN goes through, and a
	// resolver that fails is still unresolved.
	request, _ := url.Parse("https://gone.example.com/")
	toProxy := func(*http.Request) (*url.URL, error) { return url.Parse("http://proxy.corp.test:3128") }
	trusting := ScopeRules{Resolver: scopeTable(t), TrustProxy: true, Proxy: toProxy}
	if got := trusting.Judge(ctx, shop, "gone.example.com", request); got != NotRefused {
		t.Errorf("a proxied NXDOMAIN behind the exception: %v", got)
	}
	trusting.Resolver = brokenResolver{&net.DNSError{Err: "server misbehaving", Name: "x", IsTemporary: true}}
	if got := trusting.Judge(ctx, shop, "gone.example.com", request); got != RefusedUnresolved {
		t.Errorf("a proxied SERVFAIL behind the exception: %v", got)
	}
}

// The dialer reads each connection's host as the API reads a target: one
// refused by its spelling is not dialed; an external-looking one may connect
// only to an address outside the cluster, which is checked on the address
// connected to, so an answer that changed since the target was set is
// judged as it is now. A host in one's namespaces reaches whatever address
// it is.
func TestScopedDialer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())

	// Loopback stands for an address outside the cluster where the
	// company range holds it (127.0.0.1), and for one inside where it does
	// not (127.0.0.2, or 127.0.0.1 without the range). A refused address
	// is never connected to, so it need not answer.
	company, err := ParseExternalRanges([]string{"127.0.0.1/32"})
	if err != nil {
		t.Fatal(err)
	}
	var none *Policy
	dial := func(rules ScopeRules, as, address string) error {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		s := rules.Connections(shop, "")
		conn, err := none.Dialer(&net.Dialer{}).In(s, as).DialContext(ctx, "tcp", address)
		if conn != nil {
			conn.Close()
		}
		if host, why := s.Refused(); (why == RefusedOutside) != errors.Is(err, ErrScope) {
			t.Errorf("%s as %q: refused = %v (host %q), but err = %v", address, as, why, host, err)
		}
		return err
	}
	withCompany := ScopeRules{External: company}

	// An address outside the cluster, and a name in one's namespace at a
	// cluster address, which an engine may have resolved itself, connect.
	if err := dial(withCompany, "", "127.0.0.1:"+port); err != nil {
		t.Errorf("an address outside the cluster: %v", err)
	}
	if err := dial(ScopeRules{}, "api.shop.svc", "127.0.0.1:"+port); err != nil {
		t.Errorf("one's own Service at a cluster address: %v", err)
	}
	if err := dial(withCompany, "status.example.com", "127.0.0.1:"+port); err != nil {
		t.Errorf("an external name at an address outside the cluster: %v", err)
	}
	// An external-looking name that lands in the cluster is refused at
	// the address it connects to; so are the forms refused by spelling.
	for _, c := range []struct{ as, address string }{
		{"api.payments", "127.0.0.2:" + port},        // a two-label short name (ndots)
		{"kubernetes.default", "127.0.0.2:" + port},  // the API server's two-label name
		{"172-30-0-1.sslip.io", "127.0.0.2:" + port}, // a public name pointing into the cluster
		{"", "127.0.0.2:" + port},                    // a bare cluster address
		{"", "[::ffff:127.0.0.2]:" + port},
		{"", "api:" + port},
		{"", "api.payments.svc:" + port},
		{"", "api.payments.svc.cluster.local:" + port},
		{"", "kubernetes.default.svc:" + port},
		{"api.payments.svc", "127.0.0.1:" + port}, // its name decides, not the address
	} {
		if err := dial(withCompany, c.as, c.address); !errors.Is(err, ErrScope) || errors.Is(err, ErrBlocked) {
			t.Errorf("%s as %q: err = %v, want the scope's refusal", c.address, c.as, err)
		}
	}

	// A nil scope dials as the dialer does.
	d := none.Dialer(&net.Dialer{})
	if d.In(nil, "x") != d {
		t.Error("In(nil) is not the dialer itself")
	}
}

// The proxy is the operator's endpoint: the policy trusts it at its exact
// address, and a request handed to it connects there whatever the scope,
// a proxy in the cluster too. The scope holds the hosts it is asked for
// (Scope.Proxy).
func TestScopedDialerTrustsTheProxy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	p := mustNew(t, nil, nil).Trust(srv.Listener.Addr().String())
	s := ScopeRules{}.Connections(shop, "")
	conn, err := p.Dialer(&net.Dialer{}).In(s, "").DialContext(ctx, "tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatalf("the trusted proxy at a cluster address: %v", err)
	}
	conn.Close()
	if _, why := s.Refused(); why != NotRefused {
		t.Error("the scope refused the trusted proxy")
	}
}

// netResolver is a resolver of the dialer's (*net.Resolver) whose every
// query goes to answer instead of a DNS server.
func netResolver(answer func(ctx context.Context) error) *net.Resolver {
	return &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return nil, answer(ctx)
	}}
}

// A connection the check opens to an external-looking name it was led to,
// that does not resolve, is refused as unresolved; one to a name the
// monitor's settings hold fails as it would. A lookup the dial's context
// ended, by cancellation or by its deadline, is no refusal: the dial fails
// with the context's error.
func TestScopedDialerUnresolved(t *testing.T) {
	var none *Policy
	dial := func(ctx context.Context, resolver *net.Resolver, address string) (string, Refusal, error) {
		t.Helper()
		s := ScopeRules{}.Connections(shop, "", "named.example.test")
		conn, err := none.Dialer(&net.Dialer{Resolver: resolver}).In(s, "").DialContext(ctx, "tcp", address)
		if conn != nil {
			conn.Close()
		}
		host, why := s.Refused()
		return host, why, err
	}
	unreachable := netResolver(func(context.Context) error { return errors.New("no DNS server answers") })
	if host, why, err := dial(context.Background(), unreachable, "elsewhere.example.test:80"); !errors.Is(err, ErrScope) || why != RefusedUnresolved || host != "elsewhere.example.test" {
		t.Errorf("a name the check was led to that does not resolve: err = %v, refused %q %v", err, host, why)
	}
	if _, why, err := dial(context.Background(), unreachable, "named.example.test:80"); errors.Is(err, ErrScope) || why != NotRefused {
		t.Errorf("a name of the monitor's that does not resolve: err = %v, refused %v", err, why)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	defer cancel()
	giveUp := netResolver(func(ctx context.Context) error {
		cancel()
		<-ctx.Done()
		return ctx.Err()
	})
	if _, why, err := dial(cancelled, giveUp, "elsewhere.example.test:80"); !errors.Is(err, context.Canceled) || why != NotRefused {
		t.Errorf("a lookup cancelled: err = %v, refused %v", err, why)
	}
	deadline := newExpiring()
	expire := netResolver(func(ctx context.Context) error {
		deadline.expire()
		<-ctx.Done()
		return ctx.Err()
	})
	if _, why, err := dial(deadline, expire, "elsewhere.example.test:80"); !errors.Is(err, context.DeadlineExceeded) || why != NotRefused {
		t.Errorf("a lookup past its deadline: err = %v, refused %v", err, why)
	}
}

// A request handed to the proxy never connects to its hosts here: every host
// the proxy is asked for is held to the full rule before it is handed over.
func TestScopedProxy(t *testing.T) {
	rules := ScopeRules{Resolver: scopeTable(t)}
	proxy, _ := url.Parse("http://proxy.corp.test:3128")
	toProxy := func(*http.Request) (*url.URL, error) { return proxy, nil }
	direct := func(*http.Request) (*url.URL, error) { return nil, nil }
	for _, c := range []struct {
		target, host string
		want         Refusal
	}{
		{"http://status.example.com/", "", NotRefused},
		{"http://api.shop.svc/", "", NotRefused},
		{"http://api.payments.svc/", "", RefusedOutside},
		{"http://172.30.0.1/", "", RefusedOutside},
		{"http://api/", "", RefusedOutside},
		{"http://api.payments/", "", RefusedOutside},        // ndots: lands on a Service
		{"http://kubernetes.default/", "", RefusedOutside},  // the API server
		{"http://172-30-0-1.sslip.io/", "", RefusedOutside}, // a public name pointing into the cluster
		{"http://gone.example.com/", "", RefusedUnresolved}, // NXDOMAIN, no exception
		{"http://status.example.com/", "api.payments.svc", RefusedOutside},
		{"http://status.example.com/", "api.payments:8080", RefusedOutside},
		{"http://status.example.com/", "kubernetes.default.svc.cluster.local", RefusedOutside},
		{"http://status.example.com/", "172.30.0.1", RefusedOutside},
		{"http://status.example.com/", "[fd00::1]:80", RefusedOutside},
		{"http://status.example.com/", "172-30-0-1.sslip.io", RefusedOutside},
		{"http://status.example.com/", "gone.example.com", RefusedUnresolved},
		{"http://status.example.com/", "api.shop.svc", NotRefused},
		{"http://status.example.com/", "status.example.com:80", NotRefused},
		// Over https the header stays in the tunnel.
		{"https://status.example.com/", "api.payments.svc", NotRefused},
	} {
		s := rules.Connections(shop, "")
		req := httptest.NewRequest(http.MethodGet, c.target, nil)
		if c.host != "" {
			req.Host = c.host
		}
		got, err := s.Proxy(toProxy)(req)
		if _, why := s.Refused(); why != c.want || errors.Is(err, ErrScope) != (c.want != NotRefused) {
			t.Errorf("%s with Host %q: got %v, %v (refused %v), want %v", c.target, c.host, got, err, why, c.want)
		}
		// Sent directly, the request is the dialer's to judge.
		if got, err := rules.Connections(shop, "").Proxy(direct)(req); got != nil || err != nil {
			t.Errorf("%s directly: %v, %v", c.target, got, err)
		}
	}

	// Behind the exception, the proxied NXDOMAIN goes through; a name in
	// the cluster still does not.
	trusting := rules
	trusting.TrustProxy, trusting.Proxy = true, toProxy
	req := httptest.NewRequest(http.MethodGet, "http://gone.example.com/", nil)
	if _, err := trusting.Connections(shop, "").Proxy(toProxy)(req); err != nil {
		t.Errorf("a proxied NXDOMAIN behind the exception: %v", err)
	}
	req.Host = "api.payments"
	if _, err := trusting.Connections(shop, "").Proxy(toProxy)(req); !errors.Is(err, ErrScope) {
		t.Errorf("a Host landing in the cluster behind the exception: %v", err)
	}

	// A lookup the request's context ended, by cancellation or by its
	// deadline, is no refusal: the request fails with the context's error.
	stalled := ScopeRules{Resolver: stalledResolver{}}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	deadline := newExpiring()
	deadline.expire()
	for name, ctx := range map[string]context.Context{"cancelled": cancelled, "past its deadline": deadline} {
		s := stalled.Connections(shop, "")
		req := httptest.NewRequest(http.MethodGet, "http://status.example.com/", nil).WithContext(ctx)
		_, err := s.Proxy(toProxy)(req)
		if _, why := s.Refused(); why != NotRefused || errors.Is(err, ErrScope) || !errors.Is(err, ctx.Err()) {
			t.Errorf("a lookup %s: err = %v, refused %v", name, err, why)
		}
	}
	// A host refused by its spelling is refused whatever the context.
	s := stalled.Connections(shop, "")
	req = httptest.NewRequest(http.MethodGet, "http://api.payments.svc/", nil).WithContext(cancelled)
	if _, err := s.Proxy(toProxy)(req); !errors.Is(err, ErrScope) {
		t.Errorf("another team's Service with the context ended: %v", err)
	}
}

// A target taken from a cluster object of the monitor's namespace is in
// scope by that namespace whatever it resolves to, as the import judged it;
// every other host is held to the rule.
func TestScopeOwnHost(t *testing.T) {
	s := ScopeRules{}.Connections(shop, "Shop.Apps.Example.Test.")
	if err := s.Addr("shop.apps.example.test", netip.MustParseAddr("10.0.0.100")); err != nil {
		t.Errorf("own host at the router's address: %v", err)
	}
	if err := s.Addr("other.apps.example.test", netip.MustParseAddr("10.0.0.100")); !errors.Is(err, ErrScope) {
		t.Errorf("another host at a cluster address: %v", err)
	}
	if host, why := s.Refused(); why != RefusedOutside || host != "other.apps.example.test" {
		t.Errorf("Refused() = %q, %v", host, why)
	}
	// The first refusal is the one remembered.
	_ = s.Host("api.payments.svc")
	if host, _ := s.Refused(); host != "other.apps.example.test" {
		t.Errorf("Refused() after a second refusal = %q", host)
	}
	var none *Scope
	if host, why := none.Refused(); why != NotRefused || host != "" || none.Host("api") != nil || none.Addr("api", netip.MustParseAddr("10.0.0.1")) != nil {
		t.Error("a nil scope refused something")
	}

	// Only the resolution is spared: an object's owner writes its host as
	// they like, and one refused by its spelling is refused, taken from an
	// object or not.
	for _, own := range []string{"api.payments.svc.cluster.local", "api.payments.svc", "kubernetes.default.svc", "api", "10.0.0.1", "fd00::1"} {
		s := ScopeRules{}.Connections(shop, own, own)
		if err := s.Host(own); !errors.Is(err, ErrScope) {
			t.Errorf("own host %s: Host() = %v, want the scope's refusal", own, err)
		}
		if err := s.Addr(own, netip.MustParseAddr("10.96.0.1")); !errors.Is(err, ErrScope) {
			t.Errorf("own host %s: Addr() = %v, want the scope's refusal", own, err)
		}
	}
	// A Service of one's namespace is in scope by its name, as any is.
	if err := (ScopeRules{}).Connections(shop, "api.shop.svc").Addr("api.shop.svc", netip.MustParseAddr("10.96.0.1")); err != nil {
		t.Errorf("own host of one's namespace: %v", err)
	}
}
