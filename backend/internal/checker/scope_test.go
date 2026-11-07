package checker

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/yusufceng/Kubeat/backend/internal/explain"
	"github.com/yusufceng/Kubeat/backend/internal/netguard"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// The scope tests' world. One server at 127.0.0.1 answers for everything;
// a DNS server places names, as the pod's resolver with its search domains
// would: a two-label short name on a Service, a public name in the cluster.
// 127.0.0.2 stands for a cluster address. The company range holds
// 127.0.0.1, so for the scope it is an address outside the cluster, as a
// public one is (where a test leaves the range out, it is a cluster address
// too). A refused connection is never made, so 127.0.0.2 need not answer.
var scopeNames = map[string]string{
	"public.example.test.":     "127.0.0.1",
	"web.shop.svc.":            "127.0.0.1",
	"dns.shop.svc.":            "127.0.0.1",
	"route.apps.example.test.": "127.0.0.1",
	"other.apps.example.test.": "127.0.0.1",
	"api.payments.":            "127.0.0.2", // ndots turns it into another team's Service
	"kubernetes.default.":      "127.0.0.2", // and this into the API server
	"kube-dns.kube-system.":    "127.0.0.2",
	"10-0-0-1.sslip.example.":  "127.0.0.2", // a public name pointing into the cluster
	"rebind.example.test.":     "127.0.0.2", // an answer that changed after the target was set
	"dns.example.test.":        "127.0.0.2",
}

// scopeDNS serves scopeNames, and a CNAME of alias.example.test to another
// team's Service.
func scopeDNS(t *testing.T) string {
	return dnsServer(t, func(q dnsmessage.Question) []dnsmessage.ResourceBody {
		name := strings.ToLower(q.Name.String())
		if name == "alias.example.test." && q.Type == dnsmessage.TypeCNAME {
			return []dnsmessage.ResourceBody{&dnsmessage.CNAMEResource{CNAME: dnsmessage.MustNewName("api.payments.svc.cluster.local.")}}
		}
		ip, ok := scopeNames[name]
		if !ok {
			return nil
		}
		if q.Type != dnsmessage.TypeA {
			return []dnsmessage.ResourceBody{} // the name exists, without records of this type
		}
		return []dnsmessage.ResourceBody{&dnsmessage.AResource{A: netip.MustParseAddr(ip).As4()}}
	})
}

// scopeResolver is the test DNS server as a resolver: for the checks'
// connections (*net.Resolver) and for the scope's own lookups.
func scopeResolver(server string) *net.Resolver {
	return &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, server)
	}}
}

// refusals collects what the checker tells of the checks the scope refused.
type refusals struct {
	mu   sync.Mutex
	seen []refusalSeen
}

type refusalSeen struct {
	monitor int64
	host    string
	why     netguard.Refusal
	first   bool
}

func (r *refusals) tell(m store.Monitor, host string, why netguard.Refusal, first bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = append(r.seen, refusalSeen{m.ID, host, why, first})
}

func (r *refusals) take() []refusalSeen {
	r.mu.Lock()
	defer r.mu.Unlock()
	seen := r.seen
	r.seen = nil
	return seen
}

// scopeServer answers /ok, sends /to?host=h on to h's /ok on its own port,
// and knows nothing else.
func scopeServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	var port string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/to":
			http.Redirect(w, r, "http://"+net.JoinHostPort(r.URL.Query().Get("host"), port)+"/ok", http.StatusFound)
		case "/ok":
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	_, port, _ = net.SplitHostPort(srv.Listener.Addr().String())
	return srv, port
}

// scopedChecker is a checker in the world, with or without the company
// range, telling r of its refusals.
func scopedChecker(t *testing.T, dns string, company bool, r *refusals, opts Options) *Checker {
	t.Helper()
	var external netguard.ExternalRanges
	if company {
		var err error
		if external, err = netguard.ParseExternalRanges([]string{"127.0.0.1/32"}); err != nil {
			t.Fatal(err)
		}
	}
	opts.Resolver = scopeResolver(dns)
	opts.Scope = ScopeOptions{Rules: netguard.ScopeRules{External: external, Resolver: scopeResolver(dns)}, Refused: r.tell}
	return New(opts)
}

// held is a monitor set up by a user of the namespaces shop and billing.
func held(m store.Monitor) store.Monitor {
	m.Namespace = "shop"
	m.Scope = &store.TargetScope{Namespaces: []string{"billing", "shop"}}
	return m
}

func check(c *Checker, m store.Monitor) store.CheckResult {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.Check(ctx, m)
}

// refusedByTheScope asserts that got is the scope's refusal: down, with the
// one generic error and nothing else of the connection refused, explained
// as the scope's, and told once with host, as one that leads outside the
// scope.
func refusedByTheScope(t *testing.T, what string, got store.CheckResult, r *refusals, host string) {
	t.Helper()
	refusedFor(t, what, got, r, host, netguard.RefusedOutside)
}

// unresolvedByTheScope asserts that got is the scope's refusal of a name
// that did not resolve: the same result as any refusal, told with host as
// unresolved, so that only the operator tells the two apart.
func unresolvedByTheScope(t *testing.T, what string, got store.CheckResult, r *refusals, host string) {
	t.Helper()
	refusedFor(t, what, got, r, host, netguard.RefusedUnresolved)
}

func refusedFor(t *testing.T, what string, got store.CheckResult, r *refusals, host string, why netguard.Refusal) {
	t.Helper()
	if got.Up || got.Error != scopeRefusalText {
		t.Errorf("%s: up = %v, error %q; want the scope's refusal", what, got.Up, got.Error)
	}
	want := &store.Diagnostics{Explanation: &store.Explanation{Code: explain.BlockedByScope}}
	if !reflect.DeepEqual(got.Diagnostics, want) {
		t.Errorf("%s: diagnostics = %+v, want only the scope's explanation", what, got.Diagnostics)
	}
	for _, leak := range []string{"127.0.0", "payments", "kube", "sslip", "10.0.0", "10.96"} {
		if strings.Contains(got.Error+got.Detail, leak) {
			t.Errorf("%s: the result names %q: %q", what, leak, got.Error)
		}
	}
	seen := r.take()
	if len(seen) != 1 || seen[0].host != host || seen[0].why != why {
		t.Errorf("%s: told %+v, want one refusal of %s, %v", what, seen, host, why)
	}
}

// Every connection of a held monitor's check is held to the scope, not only
// the target it was set up with, in every form a host can be written in,
// whether it is refused by its spelling or by the address it lands on.
func TestScopeEveryConnection_HTTP(t *testing.T) {
	dns := scopeDNS(t)
	_, port := scopeServer(t)
	r := &refusals{}
	c := scopedChecker(t, dns, true, r, Options{})
	at := func(host, path string) string { return "http://" + net.JoinHostPort(host, port) + path }

	// In scope: a Service of one's own namespace, an address outside the
	// cluster, a name that lands outside it.
	for _, host := range []string{"web.shop.svc", "127.0.0.1", "public.example.test"} {
		if got := check(c, held(httpMonitor(at("public.example.test", "/to?host="+host)))); !got.Up {
			t.Errorf("on to %s: %+v", host, got)
		}
	}
	if seen := r.take(); len(seen) != 0 {
		t.Errorf("checks in scope told refusals: %+v", seen)
	}

	for _, host := range []string{
		"10.0.0.1",                       // a bare ClusterIP
		"fd00::1",                        // a bare cluster address over IPv6
		"api",                            // a short name, in Kubeat's own namespace
		"api.payments",                   // two labels: ndots lands it on the Service
		"api.payments.svc",               // another team's Service
		"api.payments.svc.cluster.local", // its full name
		"kubernetes.default.svc",         // the API server
		"kubernetes.default",             // its two-label name
		"10-0-0-1.sslip.example",         // a public name pointing into the cluster
		"api.payments.svc..",             // the trailing dots a proxy drops
		"172.30.1",                       // a number read as 172.30.0.1
	} {
		m := held(httpMonitor(at("public.example.test", "/to?host="+url.QueryEscape(host))))
		refusedByTheScope(t, "on to "+host, check(c, m), r, netguard.ScopeHost(host))
	}

	// A name the check is led to that does not resolve is refused as one
	// that lands in the cluster is, with the same result: a result of its
	// own would tell which names exist there.
	landing := check(c, held(httpMonitor(at("public.example.test", "/to?host=api.payments"))))
	missing := check(c, held(httpMonitor(at("public.example.test", "/to?host=nosuch.payments"))))
	if landing.Error != missing.Error || !reflect.DeepEqual(landing.Diagnostics, missing.Diagnostics) || landing.Up || missing.Up {
		t.Errorf("a name in the cluster and one that does not exist differ: %+v and %+v", landing, missing)
	}
	r.take()
	for _, host := range []string{"nosuch.payments", "gone.example.test"} {
		m := held(httpMonitor(at("public.example.test", "/to?host="+host)))
		unresolvedByTheScope(t, "on to "+host+", which does not resolve", check(c, m), r, host)
	}
	// The monitor's own target that does not resolve fails as it would
	// without a scope: it was held to the scope when it was set.
	if got := check(c, held(httpMonitor(at("gone.example.test", "/ok")))); got.Up || got.Error == scopeRefusalText {
		t.Errorf("a target that does not resolve: %+v, want its own failure", got)
	}
	if seen := r.take(); len(seen) != 0 {
		t.Errorf("a target that does not resolve told refusals: %+v", seen)
	}

	// The target itself is held to the scope where the check connects: an
	// answer that changed since it was set, and a monitor set up before
	// its scope was recorded, which is held to its namespace.
	refusedByTheScope(t, "a target whose answer changed", check(c, held(httpMonitor(at("rebind.example.test", "/ok")))), r, "rebind.example.test")
	unrecorded := httpMonitor(at("api.payments.svc", "/ok"))
	unrecorded.Namespace = "shop"
	refusedByTheScope(t, "a target outside the namespace of a monitor without a record", check(c, unrecorded), r, "api.payments.svc")
}

// A check through a proxy is held to the scope at every host the proxy is
// asked for, with the target's own rules: resolution, and the exception for
// a proxied NXDOMAIN where it is on.
func TestScopeEveryConnection_HTTPBehindAProxy(t *testing.T) {
	dns := scopeDNS(t)
	var asked []string
	var mu sync.Mutex
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.Host)
		mu.Unlock()
		if r.URL.Path == "/to" {
			http.Redirect(w, r, "http://"+r.URL.Query().Get("host")+"/ok", http.StatusFound)
		}
	}))
	defer proxy.Close()
	proxyURL, _ := url.Parse(proxy.URL)
	toProxy := func(*http.Request) (*url.URL, error) { return proxyURL, nil }
	// The proxy listens at 127.0.0.1, which the company range holds; the
	// target policy, which trusts the proxy at its address in a real
	// install, is left out (TestScopedDialerTrustsTheProxy).
	r := &refusals{}
	c := scopedChecker(t, dns, true, r, Options{Proxy: toProxy})

	withHost := func(target, host string) store.Monitor {
		m := held(httpMonitor(target))
		if host != "" {
			m.HTTP.Headers = map[string]string{"Host": host}
		}
		return m
	}
	for _, host := range []string{"", "web.shop.svc", "public.example.test:8080"} {
		if got := check(c, withHost("http://public.example.test/ok", host)); !got.Up {
			t.Errorf("through the proxy with Host %q: %+v", host, got)
		}
	}
	if got := check(c, withHost("http://public.example.test/to?host=web.shop.svc", "")); !got.Up {
		t.Errorf("through the proxy on to one's own Service: %+v", got)
	}
	if seen := r.take(); len(seen) != 0 {
		t.Errorf("checks in scope told refusals: %+v", seen)
	}

	for _, host := range []string{
		"10.0.0.1", "[fd00::1]:80", "api", "api.payments", "api.payments.svc", "api.payments.svc.cluster.local:8080",
		"kubernetes.default.svc", "kubernetes.default", "10-0-0-1.sslip.example", "gone.example.test",
	} {
		name := host
		if h, _, err := net.SplitHostPort(host); err == nil {
			name = strings.Trim(h, "[]")
		}
		why := netguard.RefusedOutside
		if host == "gone.example.test" {
			why = netguard.RefusedUnresolved
		}
		refusedFor(t, "Host "+host, check(c, withHost("http://public.example.test/ok", host)), r, name, why)
		refusedFor(t, "on to "+host, check(c, withHost("http://public.example.test/to?host="+url.QueryEscape(host), "")), r, name, why)
	}
	// Over https alike.
	if got := check(c, withHost("https://api.payments/", "")); got.Up || got.Error != scopeRefusalText {
		t.Errorf("an https request to a name in the cluster: %+v", got)
	}
	r.take()
	mu.Lock()
	for _, host := range asked {
		if !strings.Contains(host, "public.example.test") && !strings.Contains(host, "web.shop.svc") {
			t.Errorf("the proxy was asked for %s", host)
		}
	}
	mu.Unlock()

	// Behind the exception, a proxied name the backend cannot resolve goes
	// to the proxy; one landing in the cluster still does not.
	trusting := scopedChecker(t, dns, true, r, Options{Proxy: toProxy})
	trusting.opts.Scope.Rules.TrustProxy, trusting.opts.Scope.Rules.Proxy = true, toProxy
	if got := check(trusting, withHost("http://gone.example.test/ok", "")); !got.Up {
		t.Errorf("a proxied NXDOMAIN behind the exception: %+v", got)
	}
	refusedByTheScope(t, "a proxied name in the cluster behind the exception", check(trusting, withHost("http://api.payments/ok", "")), r, "api.payments")
}

// failingResolver answers as next does, but fails the lookup of name as a
// resolver whose upstream does not answer does (SERVFAIL).
type failingResolver struct {
	next netguard.Resolver
	name string
}

func (r failingResolver) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	if netguard.ScopeHost(host) == r.name {
		return nil, &net.DNSError{Err: "server misbehaving", Name: host, IsTemporary: true}
	}
	return r.next.LookupNetIP(ctx, network, host)
}

// An annotation monitor is held to its object's namespace in every install
// (store.ObjectScope). Behind a proxy, in an install whose resolver knows no
// public name, its own host is in scope by that namespace without being
// resolved, and another external host its check reaches is held to the full
// rule: one the backend cannot resolve is refused, unless the proxy
// exception is on, which lets an NXDOMAIN through and nothing else. This is
// what README "Hedef politikası" and ADR-0003 (c) tell such an install.
func TestScopeEveryConnection_AnnotationMonitorBehindAProxy(t *testing.T) {
	dns := scopeDNS(t)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/to" {
			http.Redirect(w, r, "http://"+r.URL.Query().Get("host")+"/ok", http.StatusFound)
		}
	}))
	defer proxy.Close()
	proxyURL, _ := url.Parse(proxy.URL)
	toProxy := func(*http.Request) (*url.URL, error) { return proxyURL, nil }
	// Neither app.example.test nor login.example.test resolves here.
	annotated := func(path string) store.Monitor {
		m := httpMonitor("http://app.example.test" + path)
		m.Namespace, m.Source, m.Managed, m.Scope = "shop", "ingress/web", true, store.ObjectScope("shop")
		return m
	}
	onTo := annotated("/to?host=login.example.test")
	r := &refusals{}
	behind := func(trust bool, resolver func(netguard.Resolver) netguard.Resolver) *Checker {
		c := scopedChecker(t, dns, true, r, Options{Proxy: toProxy})
		c.opts.Scope.Rules.TrustProxy, c.opts.Scope.Rules.Proxy = trust, toProxy
		if resolver != nil {
			c.opts.Scope.Rules.Resolver = resolver(c.opts.Scope.Rules.Resolver)
		}
		return c
	}

	strict := behind(false, nil)
	if got := check(strict, annotated("/ok")); !got.Up {
		t.Errorf("its own host: %+v", got)
	}
	if seen := r.take(); len(seen) != 0 {
		t.Errorf("its own host told refusals: %+v", seen)
	}
	unresolvedByTheScope(t, "another external host the backend cannot resolve", check(strict, onTo), r, "login.example.test")

	if got := check(behind(true, nil), onTo); !got.Up {
		t.Errorf("another external host behind the exception: %+v", got)
	}
	if seen := r.take(); len(seen) != 0 {
		t.Errorf("behind the exception told refusals: %+v", seen)
	}
	servfail := behind(true, func(next netguard.Resolver) netguard.Resolver {
		return failingResolver{next: next, name: "login.example.test"}
	})
	unresolvedByTheScope(t, "another external host whose lookup fails, behind the exception", check(servfail, onTo), r, "login.example.test")
}

// A DNS check is held to the scope at every connection it makes, and in
// what it reads.
func TestScopeEveryConnection_DNS(t *testing.T) {
	dns := scopeDNS(t)
	_, port, _ := net.SplitHostPort(dns)
	r := &refusals{}
	c := scopedChecker(t, dns, true, r, Options{})
	lookup := func(name, recordType, resolver string) store.Monitor {
		return held(store.Monitor{ID: 9, Type: store.MonitorDNS, Target: name, DNS: store.DNSOptions{RecordType: recordType, Resolver: resolver}})
	}

	for _, resolver := range []string{dns, "dns.shop.svc:" + port} {
		if got := check(c, lookup("public.example.test", "A", resolver)); !got.Up {
			t.Errorf("asking %s: %+v", resolver, got)
		}
	}
	if seen := r.take(); len(seen) != 0 {
		t.Errorf("lookups in scope told refusals: %+v", seen)
	}

	for _, resolver := range []string{
		"10.96.0.10",                   // a bare ClusterIP: the cluster's DNS Service
		"[fd00::10]:53",                // over IPv6
		"dns:53",                       // a short name
		"kube-dns.kube-system:" + port, // two labels: ndots lands it on the Service
		"kube-dns.kube-system.svc",     // another namespace's Service
		"kube-dns.kube-system.svc.cluster.local:53", // its full name
		"kubernetes.default.svc",                    // the API server
		"dns.example.test:" + port,                  // a public name pointing into the cluster
	} {
		host := store.DNSOptions{Resolver: resolver}.ResolverHost()
		refusedByTheScope(t, "asking "+resolver, check(c, lookup("public.example.test", "A", resolver)), r, host)
	}

	// The answers: addresses in the cluster for a name that is external by
	// its spelling, and names of another team's.
	for _, name := range []string{"api.payments", "kubernetes.default", "10-0-0-1.sslip.example", "rebind.example.test"} {
		refusedByTheScope(t, "the A records of "+name, check(c, lookup(name, "A", dns)), r, name)
	}
	refusedByTheScope(t, "a CNAME to another team's Service", check(c, lookup("alias.example.test", "CNAME", dns)), r, "api.payments.svc.cluster.local")
	refusedByTheScope(t, "a cluster name looked up", check(c, lookup("api.payments.svc.cluster.local", "A", dns)), r, "api.payments.svc.cluster.local")
}

// Every engine is held to the scope where it connects, those that resolve a
// name themselves and the one that sends packets without a dialer too.
func TestScopeEveryConnection_Engines(t *testing.T) {
	dns := scopeDNS(t)
	_, port := scopeServer(t)
	r := &refusals{}
	c := scopedChecker(t, dns, true, r, Options{})

	for name, m := range map[string]store.Monitor{
		"tcp to two labels":       {Type: store.MonitorTCP, Target: "api.payments:" + port},
		"tcp to a ClusterIP":      {Type: store.MonitorTCP, Target: "10.0.0.1:" + port},
		"grpc":                    {Type: store.MonitorGRPC, Target: "kubernetes.default:" + port},
		"websocket":               {Type: store.MonitorWebSocket, Target: "ws://10-0-0-1.sslip.example:" + port + "/"},
		"postgres":                {Type: store.MonitorPostgres, Target: "api.payments:" + port, Database: store.DatabaseOptions{Username: "u", Query: "SELECT 1"}},
		"postgres to another one": {Type: store.MonitorPostgres, Target: "db.payments.svc:5432", Database: store.DatabaseOptions{Username: "u", Query: "SELECT 1"}},
		"mysql":                   {Type: store.MonitorMySQL, Target: "rebind.example.test:" + port, Database: store.DatabaseOptions{Username: "u", Query: "SELECT 1"}},
		"redis":                   {Type: store.MonitorRedis, Target: "api.payments.svc.cluster.local:6379"},
		"ping":                    {Type: store.MonitorPing, Target: "10.0.0.1"},
		"ping a short name":       {Type: store.MonitorPing, Target: "api"},
	} {
		m.ID = 11
		host, _, err := net.SplitHostPort(m.Target)
		if err != nil {
			host = m.Host()
		}
		if m.Type == store.MonitorWebSocket {
			host = "10-0-0-1.sslip.example"
		}
		refusedByTheScope(t, name, check(c, held(m)), r, host)
	}
	if got := check(c, held(store.Monitor{Type: store.MonitorTCP, Target: "web.shop.svc:" + port})); !got.Up {
		t.Errorf("tcp to one's own Service: %+v", got)
	}

	// A check of each pod reaches the pods behind the monitor's Service:
	// in scope while its namespace is.
	pods := fakeEndpoints{endpoints: []Endpoint{{Pod: "web-1", Address: "127.0.0.1:" + port}}}
	podChecker := scopedChecker(t, dns, false, r, Options{Endpoints: pods})
	each := held(httpMonitor("http://web.shop.svc:" + port + "/ok"))
	each.Source, each.EachPod = "service/web:"+port, true
	if got := check(podChecker, each); !got.Up {
		t.Errorf("each pod of one's own Service: %+v", got)
	}
	tcpEach := held(store.Monitor{Type: store.MonitorTCP, Target: "web.shop.svc:" + port, Source: "service/web:" + port, EachPod: true})
	if got := check(podChecker, tcpEach); !got.Up {
		t.Errorf("each pod of one's own Service over tcp: %+v", got)
	}
	// One moved to a namespace its scope does not hold, by someone who may
	// edit both, reaches the pods of neither.
	moved := each
	moved.Scope = &store.TargetScope{Namespaces: []string{"billing"}}
	refusedByTheScope(t, "the pods of a namespace outside the scope", check(podChecker, moved), r, "web.shop.svc")
}

// The monitors of whoever sees every namespace are held to nothing: an
// administrator's, and one without a namespace, which only such a user can
// have set up. A short name, which a held monitor may not reach, works.
func TestScopeEveryConnection_AdministratorUnchanged(t *testing.T) {
	dns := scopeDNS(t)
	_, port := scopeServer(t)
	r := &refusals{}
	c := scopedChecker(t, dns, true, r, Options{})
	onTo := httpMonitor("http://public.example.test:" + port + "/to?host=localhost")

	admins := onTo
	admins.Namespace, admins.Scope = "shop", &store.TargetScope{Namespaces: []string{store.AllNamespaces}}
	noNamespace := onTo
	for name, m := range map[string]store.Monitor{"administrator's": admins, "without a namespace": noNamespace} {
		if got := check(c, m); !got.Up {
			t.Errorf("%s monitor on to a short name: %+v", name, got)
		}
		direct := m
		direct.Target = "http://localhost:" + port + "/ok"
		if got := check(c, direct); !got.Up {
			t.Errorf("%s monitor at a short name: %+v", name, got)
		}
	}
	if seen := r.take(); len(seen) != 0 {
		t.Errorf("an administrator's checks told refusals: %+v", seen)
	}
	// A name it is led to that does not resolve fails on its own.
	missing := admins
	missing.Target = "http://public.example.test:" + port + "/to?host=nosuch.payments"
	if got := check(c, missing); got.Up || got.Error == scopeRefusalText {
		t.Errorf("an administrator's monitor on to a name that does not resolve: %+v", got)
	}
	if seen := r.take(); len(seen) != 0 {
		t.Errorf("an administrator's checks told refusals: %+v", seen)
	}
	// The same check of a held monitor is refused.
	refusedByTheScope(t, "a held monitor on to a short name", check(c, held(onTo)), r, "localhost")
}

// The scope's refusal is not the failure an inverted monitor expects: the
// connection never left Kubeat, so the monitor is down, as with the target
// policy's.
func TestScopeEveryConnection_Inverted(t *testing.T) {
	dns := scopeDNS(t)
	_, port := scopeServer(t)
	r := &refusals{}
	c := scopedChecker(t, dns, true, r, Options{})
	m := held(httpMonitor("http://public.example.test:" + port + "/to?host=api.payments.svc"))
	m.Inverted = true
	refusedByTheScope(t, "an inverted monitor", check(c, m), r, "api.payments.svc")

	// A target that fails on its own still counts as expected.
	gone := held(httpMonitor("http://public.example.test:" + port + "/nothing"))
	gone.Inverted, gone.HTTP.ExpectedStatus = true, "200"
	if got := check(c, gone); !got.Up {
		t.Errorf("an inverted monitor whose target fails: %+v", got)
	}
}

// A target taken from a cluster object of the monitor's namespace is in
// scope by that namespace wherever it lands, as the import judged it; what
// else the check reaches is held to the scope.
func TestScopeEveryConnection_Discovered(t *testing.T) {
	dns := scopeDNS(t)
	_, port := scopeServer(t)
	r := &refusals{}
	// Without the company range, 127.0.0.1 is a cluster address: the
	// router's, say.
	c := scopedChecker(t, dns, false, r, Options{})
	route := held(httpMonitor("http://route.apps.example.test:" + port + "/ok"))
	route.Source, route.Scope.Discovered = "route/shop", true
	if got := check(c, route); !got.Up {
		t.Errorf("a Route's own host at the router: %+v", got)
	}
	// A monitor without a record from discovery is read alike.
	unrecorded := route
	unrecorded.Scope = nil
	if got := check(c, unrecorded); !got.Up {
		t.Errorf("a discovered monitor without a record: %+v", got)
	}
	if seen := r.take(); len(seen) != 0 {
		t.Errorf("told refusals: %+v", seen)
	}
	onTo := route
	onTo.Target = "http://route.apps.example.test:" + port + "/to?host=other.apps.example.test"
	refusedByTheScope(t, "on to another host at the router", check(c, onTo), r, "other.apps.example.test")
	typed := route
	typed.Scope = &store.TargetScope{Namespaces: []string{"shop"}}
	refusedByTheScope(t, "the same host entered by hand", check(c, typed), r, "route.apps.example.test")

	// The object's owner writes its host as they like: only its resolution
	// is spared, and a host refused by its spelling is refused, taken from
	// an object or not. An annotation monitor's record is its object's.
	for _, host := range []string{"api.payments.svc.cluster.local", "kubernetes.default.svc", "api"} {
		ingress := httpMonitor("http://" + net.JoinHostPort(host, port) + "/ok")
		ingress.Namespace, ingress.Source, ingress.Managed, ingress.Scope = "shop", "ingress/web", true, store.ObjectScope("shop")
		refusedByTheScope(t, "an object's host "+host, check(c, ingress), r, host)
	}
	annotated := route
	annotated.Managed, annotated.Scope = true, store.ObjectScope("shop")
	if got := check(c, annotated); !got.Up {
		t.Errorf("an annotated Route's own host at the router: %+v", got)
	}
}

// What the checker keeps of a monitor held to a scope — its transport, the
// settings it was last refused under — goes once the monitor is held to
// none, and when the scheduler says it is gone or paused.
func TestScopeEveryConnection_KeptOnlyWhileHeld(t *testing.T) {
	dns := scopeDNS(t)
	_, port := scopeServer(t)
	r := &refusals{}
	c := scopedChecker(t, dns, true, r, Options{})
	kept := func(id int64) (transport, refusal bool) {
		c.mu.Lock()
		defer c.mu.Unlock()
		_, transport = c.held[id]
		_, refusal = c.reported[id]
		return transport, refusal
	}
	up := held(httpMonitor("http://public.example.test:" + port + "/ok"))
	up.ID = 21
	refused := held(httpMonitor("http://public.example.test:" + port + "/to?host=api.payments.svc"))
	refused.ID = 22
	check(c, up)
	check(c, refused)
	if transport, _ := kept(21); !transport {
		t.Fatal("no transport kept for a held monitor")
	}
	if _, refusal := kept(22); !refusal {
		t.Fatal("no refusal kept for a refused monitor")
	}

	everywhere := up
	everywhere.Scope = &store.TargetScope{Namespaces: []string{store.AllNamespaces}}
	check(c, everywhere)
	if transport, refusal := kept(21); transport || refusal {
		t.Error("a monitor held to no scope any more kept its transport")
	}

	check(c, up)
	c.Prune(map[int64]bool{21: true})
	if transport, _ := kept(21); !transport {
		t.Error("an enabled monitor's transport was pruned")
	}
	if transport, refusal := kept(22); transport || refusal {
		t.Error("a monitor that is gone was not pruned")
	}
	c.Prune(map[int64]bool{})
	if len(c.held) != 0 || len(c.reported) != 0 {
		t.Errorf("after the last monitor went: held %d, reported %d", len(c.held), len(c.reported))
	}
}

// A monitor refused check after check is one finding: it is told as the
// first once, until where it connects changes.
func TestScopeEveryConnection_ToldOncePerSettings(t *testing.T) {
	dns := scopeDNS(t)
	_, port := scopeServer(t)
	r := &refusals{}
	c := scopedChecker(t, dns, true, r, Options{})
	m := held(httpMonitor("http://public.example.test:" + port + "/to?host=api.payments.svc"))
	for range 3 {
		check(c, m)
	}
	m.Target += "&again"
	check(c, m)
	seen := r.take()
	firsts := []bool{}
	for _, s := range seen {
		firsts = append(firsts, s.first)
	}
	if !reflect.DeepEqual(firsts, []bool{true, false, false, true}) {
		t.Errorf("first = %v, want true, false, false, then true after the change", firsts)
	}
}

// A check's connections are its own: every check opens new ones, a held
// monitor's as any other's, so that none is ever a connection made for
// another check, of another monitor, which may be held to another scope or
// to none. Within the check, a redirect to the same server goes on the
// connection the check came on.
func TestScopeEveryConnection_ConnectionsStayWithTheirCheck(t *testing.T) {
	dns := scopeDNS(t)
	var mu sync.Mutex
	var peers []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		peers = append(peers, r.RemoteAddr)
		mu.Unlock()
		if r.URL.Path == "/again" {
			http.Redirect(w, r, "/", http.StatusFound)
		}
	}))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	taken := func() []string {
		mu.Lock()
		defer mu.Unlock()
		seen := peers
		peers = nil
		return seen
	}
	r := &refusals{}
	c := scopedChecker(t, dns, true, r, Options{})
	a := held(httpMonitor("http://public.example.test:" + port + "/"))
	b := a
	b.ID = 8
	admins := a
	admins.ID, admins.Scope = 9, &store.TargetScope{Namespaces: []string{store.AllNamespaces}}
	for _, m := range []store.Monitor{a, a, b, admins, admins} {
		if got := check(c, m); !got.Up {
			t.Fatalf("monitor %d: %+v", m.ID, got)
		}
	}
	seen := taken()
	distinct := slices.Clone(seen)
	slices.Sort(distinct)
	if len(seen) != 5 || len(slices.Compact(distinct)) != 5 {
		t.Errorf("connections = %v: want each check its own", seen)
	}

	for _, m := range []store.Monitor{a, admins} {
		m.Target += "again"
		if got := check(c, m); !got.Up {
			t.Fatalf("monitor %d redirected: %+v", m.ID, got)
		}
		if seen := taken(); len(seen) != 2 || seen[0] != seen[1] {
			t.Errorf("monitor %d redirected to the same server: connections = %v, want the one", m.ID, seen)
		}
	}
}
