package netguard

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
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
		if host, refused := s.Refused(); refused != errors.Is(err, ErrScope) {
			t.Errorf("%s as %q: refused = %v (host %q), but err = %v", address, as, refused, host, err)
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
	if _, refused := s.Refused(); refused {
		t.Error("the scope refused the trusted proxy")
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
		refused      bool
	}{
		{"http://status.example.com/", "", false},
		{"http://api.shop.svc/", "", false},
		{"http://api.payments.svc/", "", true},
		{"http://172.30.0.1/", "", true},
		{"http://api/", "", true},
		{"http://api.payments/", "", true},        // ndots: lands on a Service
		{"http://kubernetes.default/", "", true},  // the API server
		{"http://172-30-0-1.sslip.io/", "", true}, // a public name pointing into the cluster
		{"http://gone.example.com/", "", true},    // NXDOMAIN, no exception
		{"http://status.example.com/", "api.payments.svc", true},
		{"http://status.example.com/", "api.payments:8080", true},
		{"http://status.example.com/", "kubernetes.default.svc.cluster.local", true},
		{"http://status.example.com/", "172.30.0.1", true},
		{"http://status.example.com/", "[fd00::1]:80", true},
		{"http://status.example.com/", "172-30-0-1.sslip.io", true},
		{"http://status.example.com/", "api.shop.svc", false},
		{"http://status.example.com/", "status.example.com:80", false},
		// Over https the header stays in the tunnel.
		{"https://status.example.com/", "api.payments.svc", false},
	} {
		s := rules.Connections(shop, "")
		req := httptest.NewRequest(http.MethodGet, c.target, nil)
		if c.host != "" {
			req.Host = c.host
		}
		got, err := s.Proxy(toProxy)(req)
		if _, refused := s.Refused(); refused != c.refused || errors.Is(err, ErrScope) != c.refused {
			t.Errorf("%s with Host %q: got %v, %v (refused %v), want refused %v", c.target, c.host, got, err, refused, c.refused)
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
	if host, refused := s.Refused(); !refused || host != "other.apps.example.test" {
		t.Errorf("Refused() = %q, %v", host, refused)
	}
	// The first refusal is the one remembered.
	_ = s.Host("api.payments.svc")
	if host, _ := s.Refused(); host != "other.apps.example.test" {
		t.Errorf("Refused() after a second refusal = %q", host)
	}
	var none *Scope
	if host, refused := none.Refused(); refused || host != "" || none.Host("api") != nil || none.Addr("api", netip.MustParseAddr("10.0.0.1")) != nil {
		t.Error("a nil scope refused something")
	}
}
