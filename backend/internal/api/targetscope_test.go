package api

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/http/httpproxy"

	"github.com/yusufceng/Kubeat/backend/internal/metrics"
	"github.com/yusufceng/Kubeat/backend/internal/netguard"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// fakeResolver answers the dial-time scope check from a fixed table; an
// unknown name does not resolve (NXDOMAIN). Used by a test that needs some
// names to land in the cluster and others to fail, so a scope check never
// touches the real network.
type fakeResolver map[string][]netip.Addr

func (f fakeResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if addrs, ok := f[host]; ok {
		return addrs, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
}

// externalResolver is the test API's default resolver: every name resolves to
// one external address (203.0.113.10, TEST-NET-3), as most of a scoped user's
// targets do. It lets an ordinary external-target create work without each
// test declaring a resolver; a test that needs a name to land in-cluster, to
// fail, or to time out supplies its own (fakeResolver, failingResolver,
// blockingResolver).
type externalResolver struct{}

func (externalResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return []netip.Addr{netip.MustParseAddr("203.0.113.10")}, nil
}

// failingResolver answers every name with a resolver error, as a CoreDNS
// outage or a SERVFAIL would; it never returns an address.
type failingResolver struct{}

func (failingResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	return nil, &net.DNSError{Err: "server misbehaving", Name: host, IsTemporary: true}
}

// blockingResolver never answers until the context is cancelled, so the
// resolve deadline (Options.ResolveTimeout) is what ends the lookup.
type blockingResolver struct{}

func (blockingResolver) LookupNetIP(ctx context.Context, _, _ string) ([]netip.Addr, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func ipList(t *testing.T, ips ...string) []netip.Addr {
	t.Helper()
	out := make([]netip.Addr, len(ips))
	for i, ip := range ips {
		out[i] = netip.MustParseAddr(ip)
	}
	return out
}

// A user limited to some namespaces may aim a monitor or a channel webhook
// only at a cluster-internal address whose namespace is one of theirs: the
// API server and another team's Service, which share the ClusterIP range a
// deny-CIDR cannot separate, are refused (ADR-0003 c). Legitimate
// cross-namespace monitoring for a user whose scope includes the target's
// namespace, an external target, and an administrator are unaffected.
func TestTargetsStayInScope(t *testing.T) {
	srv, _ := newTestAPI(t)
	base := srv.URL
	addUser(t, base, "dev", store.RoleEditor, "shop", "billing")
	token, code := signIn(t, base, "dev", "parola-123")
	if code != http.StatusOK {
		t.Fatalf("sign in: %d", code)
	}
	monitor := func(name, target string) string {
		return monitorJSON(name, "http", target, `"namespace":"shop"`)
	}

	// Cluster-internal targets outside the user's namespaces are refused:
	// the API server by name and by ClusterIP, another team's Service, and a
	// short name in Kubeat's own namespace.
	for _, tc := range []struct{ name, target string }{
		{"apiserver-name", "https://kubernetes.default.svc/healthz"},
		{"apiserver-ip", "https://172.30.0.1/healthz"},
		{"other-team", "https://api.payments.svc.cluster.local/"},
		{"short-name", "https://api/"},
	} {
		var problem errorBody
		if code := doAsError(t, token, "POST", base+"/api/v1/monitors", monitor(tc.name, tc.target), &problem); code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403 (%q)", tc.name, code, problem.Error)
		}
	}

	// A Service in a namespace the user may see works, even when it is not
	// the monitor's group: legitimate cross-namespace monitoring.
	var own monitorResponse
	if code := doAs(t, token, "POST", base+"/api/v1/monitors", monitor("billing-db", "https://api.billing.svc.cluster.local/"), &own); code != http.StatusCreated {
		t.Fatalf("a Service in a readable namespace: status = %d", code)
	}
	// An external target is left to the target policy, as before.
	if code := doAs(t, token, "POST", base+"/api/v1/monitors", monitor("public", "https://status.example.com/"), nil); code != http.StatusCreated {
		t.Errorf("an external target: status = %d", code)
	}
	// Its target cannot then be moved to the API server.
	var refused errorBody
	if code := doAsError(t, token, "PATCH", base+"/api/v1/monitors/"+itoa(own.ID), `{"target":"https://kubernetes.default.svc/"}`, &refused); code != http.StatusForbidden {
		t.Errorf("retargeting at the API server: status = %d, %q", code, refused.Error)
	}

	// An administrator reaches any in-cluster address; a scoped user may not
	// clone the out-of-scope monitor the administrator made, even in a group
	// they can edit.
	var admins monitorResponse
	if code := do(t, "POST", base+"/api/v1/monitors", monitor("apiserver", "https://kubernetes.default.svc/healthz"), &admins); code != http.StatusCreated {
		t.Fatalf("administrator's monitor: status = %d", code)
	}
	if code := doAs(t, token, "POST", base+"/api/v1/monitors/"+itoa(admins.ID)+"/clone", `{"name":"apiserver-2"}`, nil); code != http.StatusForbidden {
		t.Errorf("cloning the out-of-scope monitor: status = %d, want 403", code)
	}

	// A channel webhook is held to the same rule.
	channel := func(name, target string) string {
		return `{"name":"` + name + `","type":"webhook","target":"` + target + `","namespaces":["shop"]}`
	}
	if code := doAs(t, token, "POST", base+"/api/v1/channels", channel("hook-out", "https://hooks.payments.svc/x"), nil); code != http.StatusForbidden {
		t.Errorf("webhook at another team's Service: status = %d, want 403", code)
	}
	if code := doAs(t, token, "POST", base+"/api/v1/channels", channel("hook-in", "https://hooks.shop.svc/x"), nil); code != http.StatusCreated {
		t.Errorf("webhook in a readable namespace: status = %d", code)
	}
}

// A scoped user cannot reach a cluster-internal target through a name that
// InCluster calls external by its spelling but that resolves into the
// cluster: a two-label short name the pod's search domains turn into a
// Service (api.payments, kubernetes.default), or a public name pointing at a
// cluster address (172-30-0-1.sslip.io). The scope check resolves the name
// (dial-time) and refuses it, with the machine-readable code target_scope.
// An external name that resolves externally, and a Service in a readable
// namespace, are unaffected (S67, ADR-0003 c "Kapsamın kalan boşluğu").
func TestDialTimeTargetScope(t *testing.T) {
	resolver := fakeResolver{
		"api.payments":        ipList(t, "172.30.44.9"),   // another team's Service, via ndots
		"kubernetes.default":  ipList(t, "172.30.0.1"),    // the API server, via ndots
		"172-30-0-1.sslip.io": ipList(t, "172.30.0.1"),    // a public name pointing at it
		"status.example.com":  ipList(t, "93.184.216.34"), // genuinely external
		"hooks.payments":      ipList(t, "172.30.44.9"),   // channel webhook, two-label
	}
	srv, _ := newTestAPIWith(t, func(o *Options) { o.Resolver = resolver })
	base := srv.URL
	addUser(t, base, "dev", store.RoleEditor, "shop", "billing")
	token, code := signIn(t, base, "dev", "parola-123")
	if code != http.StatusOK {
		t.Fatalf("sign in: %d", code)
	}
	monitor := func(name, target string) string {
		return monitorJSON(name, "http", target, `"namespace":"shop"`)
	}

	// Names external by spelling that resolve into the cluster are refused,
	// and the refusal carries code target_scope so the frontend need not read
	// the English wording.
	for _, tc := range []struct{ name, target string }{
		{"two-label-service", "https://api.payments/"},
		{"apiserver-two-label", "https://kubernetes.default/healthz"},
		{"public-name-internal-ip", "https://172-30-0-1.sslip.io/"},
	} {
		var problem errorBody
		if code := doAsError(t, token, "POST", base+"/api/v1/monitors", monitor(tc.name, tc.target), &problem); code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403", tc.name, code)
		}
		if problem.Code != codeTargetScope {
			t.Errorf("%s: code = %q, want %q (%q)", tc.name, problem.Code, codeTargetScope, problem.Error)
		}
	}

	// An external name that resolves externally is left to the target policy.
	if code := doAs(t, token, "POST", base+"/api/v1/monitors", monitor("public", "https://status.example.com/"), nil); code != http.StatusCreated {
		t.Errorf("an external target that resolves externally: status = %d", code)
	}
	// A name the resolver cannot answer for (here NXDOMAIN) can no longer be
	// proven external, so a scoped user's target is refused on the safe side
	// (resolver-error fail-safe, ADR-0003 c), with code target_scope.
	var unresolved errorBody
	if code := doAsError(t, token, "POST", base+"/api/v1/monitors", monitor("unresolved", "https://nowhere.example.net/"), &unresolved); code != http.StatusForbidden {
		t.Errorf("a name that does not resolve: status = %d, want 403", code)
	}
	if unresolved.Code != codeTargetScope {
		t.Errorf("unresolved: code = %q, want %q", unresolved.Code, codeTargetScope)
	}
	// A Service in a readable namespace, named in .svc form, still works: the
	// dial-time check does not narrow legitimate cross-namespace monitoring.
	if code := doAs(t, token, "POST", base+"/api/v1/monitors", monitor("billing-db", "https://api.billing.svc.cluster.local/"), nil); code != http.StatusCreated {
		t.Errorf("a Service in a readable namespace: status = %d", code)
	}

	// A channel webhook is held to the same dial-time rule.
	channel := func(name, target string) string {
		return `{"name":"` + name + `","type":"webhook","target":"` + target + `","namespaces":["shop"]}`
	}
	var hookProblem errorBody
	if code := doAsError(t, token, "POST", base+"/api/v1/channels", channel("hook-two-label", "https://hooks.payments/x"), &hookProblem); code != http.StatusForbidden {
		t.Errorf("webhook at a two-label cluster name: status = %d", code)
	}
	if hookProblem.Code != codeTargetScope {
		t.Errorf("webhook: code = %q, want %q", hookProblem.Code, codeTargetScope)
	}

	// An administrator (sees all) reaches a two-label cluster name, as before:
	// the dial-time check is only for scoped users.
	if code := do(t, "POST", base+"/api/v1/monitors", monitor("admin-reaches", "https://api.payments/"), nil); code != http.StatusCreated {
		t.Errorf("administrator at a two-label cluster name: status = %d", code)
	}
}

// A scoped user's external-looking target is refused when the resolver cannot
// answer (a CoreDNS outage, a SERVFAIL, an NXDOMAIN): a name that would
// resolve externally in normal times cannot be proven external during the
// outage, and accepting it would leave the fail-open this closes — a scoped
// user registers an external-looking name while the resolver is down and the
// checker dials it into the cluster once the resolver recovers (ADR-0003 c,
// resolver-error fail-safe). An administrator is unaffected: the scope check
// never resolves for a caller who sees every namespace. The refusal carries
// the single generic message, so its body is not an internal-address oracle.
func TestTargetScopeResolverFailSafe(t *testing.T) {
	srv, _ := newTestAPIWith(t, func(o *Options) { o.Resolver = failingResolver{} })
	base := srv.URL
	addUser(t, base, "dev", store.RoleEditor, "shop")
	token, code := signIn(t, base, "dev", "parola-123")
	if code != http.StatusOK {
		t.Fatalf("sign in: %d", code)
	}
	monitor := func(name, target string) string {
		return monitorJSON(name, "http", target, `"namespace":"shop"`)
	}

	var problem errorBody
	if code := doAsError(t, token, "POST", base+"/api/v1/monitors", monitor("outage", "https://status.example.com/"), &problem); code != http.StatusForbidden {
		t.Errorf("scoped create during a resolver outage: status = %d, want 403", code)
	}
	if problem.Code != codeTargetScope {
		t.Errorf("outage: code = %q, want %q", problem.Code, codeTargetScope)
	}
	// The body is the single generic message: it tells the caller nothing
	// about whether the name resolves into the cluster, or into which
	// namespace, so the refusal is not an internal-address oracle.
	if problem.Error != scopeRefused {
		t.Errorf("outage body = %q, want the generic scope message", problem.Error)
	}
	for _, leak := range []string{"resolve", "cluster-internal", "namespace \""} {
		if strings.Contains(problem.Error, leak) {
			t.Errorf("scope refusal body leaks %q: %q", leak, problem.Error)
		}
	}

	// A channel webhook is held to the same fail-safe.
	ch := `{"name":"hook","type":"webhook","target":"https://hook.example.com/x","namespaces":["shop"]}`
	if code := doAs(t, token, "POST", base+"/api/v1/channels", ch, nil); code != http.StatusForbidden {
		t.Errorf("scoped channel webhook during a resolver outage: status = %d, want 403", code)
	}

	// An administrator (sees all) never resolves, so the outage does not
	// touch them.
	if code := do(t, "POST", base+"/api/v1/monitors", monitor("admin", "https://status.example.com/"), nil); code != http.StatusCreated {
		t.Errorf("administrator during a resolver outage: status = %d, want 201", code)
	}
}

// A slow resolver does not hold a scoped user's create open to the server's
// write timeout: Options.ResolveTimeout bounds the lookup, the lookup ends in
// a deadline error, and the target is refused on the safe side.
func TestTargetScopeResolveTimeout(t *testing.T) {
	srv, _ := newTestAPIWith(t, func(o *Options) {
		o.Resolver = blockingResolver{}
		o.ResolveTimeout = 50 * time.Millisecond
	})
	base := srv.URL
	addUser(t, base, "dev", store.RoleEditor, "shop")
	token, code := signIn(t, base, "dev", "parola-123")
	if code != http.StatusOK {
		t.Fatalf("sign in: %d", code)
	}

	start := time.Now()
	var problem errorBody
	if code := doAsError(t, token, "POST", base+"/api/v1/monitors",
		monitorJSON("slow", "http", "https://status.example.com/", `"namespace":"shop"`), &problem); code != http.StatusForbidden {
		t.Errorf("scoped create with a slow resolver: status = %d, want 403", code)
	}
	if problem.Code != codeTargetScope {
		t.Errorf("slow resolver: code = %q, want %q", problem.Code, codeTargetScope)
	}
	// The resolve deadline (50ms), not the 2-minute write timeout, bounds the
	// request; a generous ceiling keeps this from flaking on a busy runner.
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("create took %s; the resolve deadline did not bound it", elapsed)
	}
}

// auditDenials returns the audit log's deny rows (S68), read from the store
// so the handler's page limit (MaxResults) does not hide one.
func auditDenials(t *testing.T, db *store.SQLite) []store.AuditEntry {
	t.Helper()
	entries, err := db.ListAudit(context.Background(), 100)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	var out []store.AuditEntry
	for _, e := range entries {
		if e.Action == "deny" {
			out = append(out, e)
		}
	}
	return out
}

// A scoped user's refused cross-tenant write leaves a trace an operator can
// read: a deny/target-scope audit row, beside login_failed, and a bump of
// kubeat_target_scope_denied_total by reason — target for a target host
// outside the scope, namespace for a namespace the caller cannot edit. The
// legitimate in-scope create is not counted, and the client response is the
// unchanged 403, so neither the log nor the metric is an enumeration oracle
// (S68, #109).
func TestScopeDenialAuditedAndCounted(t *testing.T) {
	meter := metrics.New("test")
	srv, db := newTestAPIWith(t, func(o *Options) { o.Meter = meter })
	base := srv.URL
	addUser(t, base, "dev", store.RoleEditor, "shop")
	token, code := signIn(t, base, "dev", "parola-123")
	if code != http.StatusOK {
		t.Fatalf("sign in: %d", code)
	}
	monitor := func(name, target, ns string) string {
		return monitorJSON(name, "http", target, `"namespace":"`+ns+`"`)
	}

	// A monitor at a target outside the scope (the API server by its .svc
	// name): reason target.
	if code := doAs(t, token, "POST", base+"/api/v1/monitors", monitor("apiserver", "https://kubernetes.default.svc/healthz", "shop"), nil); code != http.StatusForbidden {
		t.Fatalf("out-of-scope target: status = %d, want 403", code)
	}
	// A channel webhook at another team's Service: reason target too.
	hook := `{"name":"hook","type":"webhook","target":"https://hooks.payments.svc/x","namespaces":["shop"]}`
	if code := doAs(t, token, "POST", base+"/api/v1/channels", hook, nil); code != http.StatusForbidden {
		t.Fatalf("out-of-scope channel target: status = %d, want 403", code)
	}
	// A monitor in a namespace the user cannot edit: reason namespace.
	if code := doAs(t, token, "POST", base+"/api/v1/monitors", monitor("other", "https://status.example.com/", "payments"), nil); code != http.StatusForbidden {
		t.Fatalf("out-of-scope namespace: status = %d, want 403", code)
	}
	// A legitimate in-scope create still works and is no denial.
	if code := doAs(t, token, "POST", base+"/api/v1/monitors", monitor("ok", "https://status.example.com/", "shop"), nil); code != http.StatusCreated {
		t.Fatalf("in-scope create: status = %d, want 201", code)
	}

	// Each denial left a deny/target-scope row, by the user who tried.
	denials := auditDenials(t, db)
	if len(denials) != 3 {
		t.Fatalf("deny audit rows = %d (%v), want 3", len(denials), denials)
	}
	var sawTargetSummary, sawNamespaceSummary bool
	for _, d := range denials {
		if d.ObjectType != "target-scope" {
			t.Errorf("deny row object_type = %q, want target-scope", d.ObjectType)
		}
		if d.Username != "dev" {
			t.Errorf("deny row username = %q, want dev", d.Username)
		}
		if strings.HasPrefix(d.Summary, "target ") {
			sawTargetSummary = true
		}
		if strings.HasPrefix(d.Summary, "namespace ") {
			sawNamespaceSummary = true
		}
	}
	if !sawTargetSummary || !sawNamespaceSummary {
		t.Errorf("deny summaries missing a reason: target=%v namespace=%v (%v)", sawTargetSummary, sawNamespaceSummary, denials)
	}

	// The counter rose once per reason: two targets, one namespace.
	got, err := meter.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if got["kubeat_target_scope_denied_total{reason=target}"] != 2 {
		t.Errorf("reason=target = %v, want 2", got["kubeat_target_scope_denied_total{reason=target}"])
	}
	if got["kubeat_target_scope_denied_total{reason=namespace}"] != 1 {
		t.Errorf("reason=namespace = %v, want 1", got["kubeat_target_scope_denied_total{reason=namespace}"])
	}
}

// An administrator and a single-tenant install never raise a scope denial:
// the counter stays at 0 and the audit log gets no deny row, because
// targetInScope lets every target through for a caller who sees all.
func TestScopeDenialNotRaisedForAdmin(t *testing.T) {
	meter := metrics.New("test")
	srv, db := newTestAPIWith(t, func(o *Options) { o.Meter = meter })
	base := srv.URL

	// The service token sees all: a cluster-internal target is created.
	if code := do(t, "POST", base+"/api/v1/monitors", monitorJSON("apiserver", "http", "https://kubernetes.default.svc/healthz", `"namespace":"ops"`), nil); code != http.StatusCreated {
		t.Fatalf("administrator at a cluster target: status = %d, want 201", code)
	}
	if rows := auditDenials(t, db); len(rows) != 0 {
		t.Errorf("administrator left %d deny rows, want 0", len(rows))
	}
	got, err := meter.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if got["kubeat_target_scope_denied_total{reason=target}"] != 0 || got["kubeat_target_scope_denied_total{reason=namespace}"] != 0 {
		t.Errorf("administrator raised the scope-denial counter: %v",
			map[string]float64{
				"target":    got["kubeat_target_scope_denied_total{reason=target}"],
				"namespace": got["kubeat_target_scope_denied_total{reason=namespace}"],
			})
	}
}

// The deny audit summary is written in the install's language, like the
// sign-in phrases: with UPTIME_LANGUAGE=tr a scoped target refusal reads in
// Turkish. The action and object codes stay fixed (deny/target-scope); the
// frontend translates those on its own.
func TestScopeDenialSummaryLocalised(t *testing.T) {
	srv, db := newTestAPIWith(t, func(o *Options) { o.Language = "tr" })
	base := srv.URL
	addUser(t, base, "dev", store.RoleEditor, "shop")
	token, code := signIn(t, base, "dev", "parola-123")
	if code != http.StatusOK {
		t.Fatalf("sign in: %d", code)
	}
	if code := doAs(t, token, "POST", base+"/api/v1/monitors",
		monitorJSON("apiserver", "http", "https://kubernetes.default.svc/healthz", `"namespace":"shop"`), nil); code != http.StatusForbidden {
		t.Fatalf("out-of-scope target: status = %d, want 403", code)
	}
	denials := auditDenials(t, db)
	if len(denials) != 1 {
		t.Fatalf("deny rows = %d, want 1", len(denials))
	}
	if d := denials[0]; d.Action != "deny" || d.ObjectType != "target-scope" || !strings.Contains(d.Summary, "namespace'lerinizin dışında") {
		t.Errorf("tr deny row = %+v, want deny/target-scope with a Turkish summary", d)
	}
}

// corpProxy is the choice http.ProxyFromEnvironment makes with
// HTTPS_PROXY=http://proxy.corp.example:3128 and the given NO_PROXY: the same
// httpproxy code net/http runs for the checker and the senders, without the
// process environment, which net/http reads only once.
func corpProxy(noProxy string) func(*http.Request) (*url.URL, error) {
	proxyFor := (&httpproxy.Config{HTTPSProxy: "http://proxy.corp.example:3128", NoProxy: noProxy}).ProxyFunc()
	return func(r *http.Request) (*url.URL, error) { return proxyFor(r.URL) }
}

// corpNoProxy is the NO_PROXY of a proxied install (docs/pilot.md): the
// cluster's names and Service range, and the company's own zone, which is
// reached directly.
const corpNoProxy = ".svc,.cluster.local,172.30.0.0/16,.corp.example"

// corpResolver answers like the backend's resolver behind such a proxy: the
// cluster's names, two-label short names included (ndots), resolve; a public
// name pointing at a cluster address resolves to it; every other name is
// NXDOMAIN, since only the proxy resolves external names.
func corpResolver(t *testing.T) fakeResolver {
	return fakeResolver{
		"api.payments":        ipList(t, "172.30.44.9"), // another team's Service, via ndots
		"kubernetes.default":  ipList(t, "172.30.0.1"),  // the API server, via ndots
		"hooks.payments":      ipList(t, "172.30.44.10"),
		"172-30-0-1.sslip.io": ipList(t, "172.30.0.1"),
	}
}

// signInScoped adds an editor limited to the namespace shop and signs in.
func signInScoped(t *testing.T, base string) string {
	t.Helper()
	addUser(t, base, "dev", store.RoleEditor, "shop")
	token, code := signIn(t, base, "dev", "parola-123")
	if code != http.StatusOK {
		t.Fatalf("sign in: %d", code)
	}
	return token
}

// With UPTIME_TARGET_SCOPE_TRUST_PROXY on, a scoped user may aim an HTTP,
// keyword, JSON or WebSocket monitor and every webhook-type channel at an
// external name that is NXDOMAIN for the backend's resolver when the request
// goes to the proxy (HTTPS_PROXY, outside NO_PROXY): the proxy resolves it
// outside the cluster (S81, ADR-0003 c). Create, clone and retarget all take
// it, and none of them leaves a deny audit row or counts a scope denial.
func TestScopeTrustProxyAcceptsProxiedNXDOMAIN(t *testing.T) {
	meter := metrics.New("test")
	srv, db := newTestAPIWith(t, func(o *Options) {
		o.Resolver = corpResolver(t)
		o.ScopeTrustProxy = true
		o.Proxy = corpProxy(corpNoProxy)
		o.Meter = meter
	})
	base := srv.URL
	token := signInScoped(t, base)

	var first monitorResponse
	for _, tc := range []struct{ name, typ, target, extra string }{
		{"status", "http", "https://status.vendor.example/health", ""},
		{"keyword", "http", "https://keyword.vendor.example/", `"http":{"keyword":"ok"}`},
		{"json", "http", "https://json.vendor.example/health", `"http":{"json_path":"status","json_operator":"==","json_expected":"ok"}`},
		{"websocket", "websocket", "wss://stream.vendor.example/socket", ""},
	} {
		extra := `"namespace":"shop"`
		if tc.extra != "" {
			extra += "," + tc.extra
		}
		var created monitorResponse
		if code := doAs(t, token, "POST", base+"/api/v1/monitors", monitorJSON(tc.name, tc.typ, tc.target, extra), &created); code != http.StatusCreated {
			t.Errorf("%s monitor at a proxied NXDOMAIN name: status = %d, want 201", tc.name, code)
			continue
		}
		if first.ID == 0 {
			first = created
		}
	}
	if first.ID == 0 {
		t.Fatal("no proxied monitor was created")
	}
	// Its clone, and a retarget to another proxied NXDOMAIN name, pass too.
	if code := doAs(t, token, "POST", base+"/api/v1/monitors/"+itoa(first.ID)+"/clone", `{"name":"status-2"}`, nil); code != http.StatusCreated {
		t.Errorf("cloning the proxied monitor: status = %d, want 201", code)
	}
	if code := doAs(t, token, "PATCH", base+"/api/v1/monitors/"+itoa(first.ID), `{"target":"https://moved.vendor.example/"}`, nil); code != http.StatusOK {
		t.Errorf("retargeting at another proxied NXDOMAIN name: status = %d, want 200", code)
	}

	// Every webhook-type channel is delivered through the same proxy.
	var hook channelResponse
	for _, tc := range []struct{ typ, target, secret string }{
		{"webhook", "https://hooks.vendor.example/kubeat", ""},
		{"slack", "https://hooks.slack.vendor.example/services/T0/B0/x", ""},
		{"teams", "https://teams.vendor.example/workflows/x", ""},
		{"discord", "https://discord.vendor.example/api/webhooks/1/x", ""},
		{"mattermost", "https://chat.vendor.example/hooks/x", ""},
		{"googlechat", "https://chat.google.vendor.example/v1/spaces/x/messages", ""},
		{"gotify", "https://gotify.vendor.example", "app-token"},
		{"ntfy", "https://ntfy.vendor.example/alerts", ""},
	} {
		body := `{"name":"` + tc.typ + `","type":"` + tc.typ + `","target":"` + tc.target + `","secret":"` + tc.secret + `","namespaces":["shop"]}`
		var created channelResponse
		if code := doAs(t, token, "POST", base+"/api/v1/channels", body, &created); code != http.StatusCreated {
			t.Errorf("%s channel at a proxied NXDOMAIN name: status = %d, want 201", tc.typ, code)
			continue
		}
		if tc.typ == "webhook" {
			hook = created
		}
	}
	if hook.ID == 0 {
		t.Fatal("the proxied webhook channel was not created")
	}
	if code := doAs(t, token, "PATCH", base+"/api/v1/channels/"+itoa(hook.ID), `{"target":"https://moved-hooks.vendor.example/kubeat"}`, nil); code != http.StatusOK {
		t.Errorf("retargeting the webhook at another proxied NXDOMAIN name: status = %d, want 200", code)
	}

	// No accepted target is a denial: no audit row, no count.
	if rows := auditDenials(t, db); len(rows) != 0 {
		t.Errorf("proxied NXDOMAIN targets left %d deny rows (%v), want 0", len(rows), rows)
	}
	got, err := meter.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if n := got["kubeat_target_scope_denied_total{reason=target}"]; n != 0 {
		t.Errorf("reason=target = %v, want 0", n)
	}
}

// The proxy exception is for a proxied NXDOMAIN alone. With the setting on,
// every other form stays refused for a scoped user, each with code
// target_scope: a name NO_PROXY sends directly (.svc, .cluster.local,
// 172.30.0.0/16, the company zone); a plain http:// request when only
// HTTPS_PROXY is set; every type that connects directly (TCP, DNS, gRPC,
// ping, the databases); a bare ClusterIP; a short name; a two-label name the
// pod's search domains (ndots) turn into a Service or the API server, though
// it would go to the proxy; the full .svc and FQDN forms and the API server's
// names; and a public name that resolves to a cluster address (S81).
func TestScopeTrustProxyKeepsRefusing(t *testing.T) {
	srv, _ := newTestAPIWith(t, func(o *Options) {
		o.Resolver = corpResolver(t)
		o.ScopeTrustProxy = true
		o.Proxy = corpProxy(corpNoProxy)
	})
	base := srv.URL
	token := signInScoped(t, base)

	for _, tc := range []struct{ name, typ, target, extra string }{
		// NO_PROXY keeps the request away from the proxy: it is dialed here,
		// so an NXDOMAIN here is the answer the check would get.
		{"no-proxy-zone", "http", "https://intranet.corp.example/", ""},
		{"no-proxy-svc", "http", "https://ghost.payments.svc/", ""},
		{"no-proxy-cluster-local", "http", "https://ghost.payments.svc.cluster.local/", ""},
		{"no-proxy-cidr", "http", "https://172.30.12.34/", ""},
		// Only HTTPS_PROXY is set: a plain http:// request is not proxied.
		{"plain-http", "http", "http://plain.vendor.example/", ""},
		{"plain-ws", "websocket", "ws://plain-stream.vendor.example/socket", ""},
		// Types that connect directly, whatever HTTPS_PROXY says.
		{"tcp", "tcp", "status.vendor.example:443", ""},
		{"dns", "dns", "status.vendor.example", ""},
		{"grpc", "grpc", "status.vendor.example:443", ""},
		{"ping", "ping", "status.vendor.example", ""},
		{"postgres", "postgres", "db.vendor.example:5432", `"database":{"username":"probe"}`},
		{"mysql", "mysql", "db.vendor.example:3306", `"database":{"username":"probe"}`},
		{"redis", "redis", "cache.vendor.example:6379", ""},
		// Cluster addresses and names, proxied or not.
		{"bare-clusterip", "http", "https://172.30.0.1/", ""},
		{"short-name", "http", "https://api/", ""},
		{"two-label-service", "http", "https://api.payments/", ""},
		{"two-label-apiserver", "http", "https://kubernetes.default/healthz", ""},
		{"svc", "http", "https://api.payments.svc/", ""},
		{"fqdn", "http", "https://api.payments.svc.cluster.local/", ""},
		{"apiserver-short", "http", "https://kubernetes/healthz", ""},
		{"apiserver-svc", "http", "https://kubernetes.default.svc/healthz", ""},
		{"apiserver-fqdn", "http", "https://kubernetes.default.svc.cluster.local/healthz", ""},
		{"public-name-internal-ip", "http", "https://172-30-0-1.sslip.io/", ""},
		{"websocket-two-label", "websocket", "wss://api.payments/socket", ""},
	} {
		extra := `"namespace":"shop"`
		if tc.extra != "" {
			extra += "," + tc.extra
		}
		var problem errorBody
		if code := doAsError(t, token, "POST", base+"/api/v1/monitors", monitorJSON(tc.name, tc.typ, tc.target, extra), &problem); code != http.StatusForbidden {
			t.Errorf("%s (%s %s): status = %d, want 403 (%q)", tc.name, tc.typ, tc.target, code, problem.Error)
			continue
		}
		if problem.Code != codeTargetScope {
			t.Errorf("%s: code = %q, want %q", tc.name, problem.Code, codeTargetScope)
		}
	}

	// A webhook is held to the same forms.
	for i, target := range []string{
		"https://hooks.corp.example/kubeat",  // NO_PROXY
		"http://hooks.vendor.example/kubeat", // plain http, only HTTPS_PROXY
		"https://hooks.payments/kubeat",      // two-label, resolves into the cluster
		"https://hooks.payments.svc/kubeat",  // another team's Service
		"https://172-30-0-1.sslip.io/kubeat", // public name, cluster address
	} {
		body := `{"name":"hook-` + strconv.Itoa(i) + `","type":"webhook","target":"` + target + `","namespaces":["shop"]}`
		if code := doAs(t, token, "POST", base+"/api/v1/channels", body, nil); code != http.StatusForbidden {
			t.Errorf("webhook at %s: status = %d, want 403", target, code)
		}
	}
}

// The proxy exception covers an NXDOMAIN only: a resolver that fails (a
// SERVFAIL, an outage) or does not answer in time keeps a proxied target
// refused with the setting on (the resolver-error fail-safe, ADR-0003 c).
func TestScopeTrustProxyKeepsResolverFailSafe(t *testing.T) {
	for name, resolver := range map[string]netguard.Resolver{
		"servfail": failingResolver{},
		"timeout":  blockingResolver{},
	} {
		t.Run(name, func(t *testing.T) {
			srv, db := newTestAPIWith(t, func(o *Options) {
				o.Resolver = resolver
				o.ResolveTimeout = 50 * time.Millisecond
				o.ScopeTrustProxy = true
				o.Proxy = corpProxy(corpNoProxy)
			})
			base := srv.URL
			token := signInScoped(t, base)
			var problem errorBody
			if code := doAsError(t, token, "POST", base+"/api/v1/monitors",
				monitorJSON("status", "http", "https://status.vendor.example/", `"namespace":"shop"`), &problem); code != http.StatusForbidden {
				t.Errorf("proxied monitor: status = %d, want 403", code)
			}
			if problem.Code != codeTargetScope {
				t.Errorf("code = %q, want %q", problem.Code, codeTargetScope)
			}
			hook := `{"name":"hook","type":"webhook","target":"https://hooks.vendor.example/x","namespaces":["shop"]}`
			if code := doAs(t, token, "POST", base+"/api/v1/channels", hook, nil); code != http.StatusForbidden {
				t.Errorf("proxied webhook: status = %d, want 403", code)
			}
			if rows := auditDenials(t, db); len(rows) != 2 {
				t.Errorf("deny rows = %d, want 2", len(rows))
			}
		})
	}
}

// With the setting off (the default), a proxied NXDOMAIN name is refused as in
// 0.49 and leaves its deny row; the setting on where nothing is proxied
// changes nothing either. An administrator never resolves and is unaffected.
func TestScopeTrustProxyOffIsUnchanged(t *testing.T) {
	for name, customize := range map[string]func(*Options){
		"setting off, proxy set": func(o *Options) { o.Proxy = corpProxy(corpNoProxy) },
		"setting on, no proxy": func(o *Options) {
			o.ScopeTrustProxy = true
			o.Proxy = func(*http.Request) (*url.URL, error) { return nil, nil }
		},
	} {
		t.Run(name, func(t *testing.T) {
			srv, db := newTestAPIWith(t, func(o *Options) {
				o.Resolver = corpResolver(t)
				customize(o)
			})
			base := srv.URL
			token := signInScoped(t, base)
			if code := doAs(t, token, "POST", base+"/api/v1/monitors",
				monitorJSON("status", "http", "https://status.vendor.example/", `"namespace":"shop"`), nil); code != http.StatusForbidden {
				t.Errorf("proxied NXDOMAIN monitor: status = %d, want 403", code)
			}
			hook := `{"name":"hook","type":"webhook","target":"https://hooks.vendor.example/x","namespaces":["shop"]}`
			if code := doAs(t, token, "POST", base+"/api/v1/channels", hook, nil); code != http.StatusForbidden {
				t.Errorf("proxied NXDOMAIN webhook: status = %d, want 403", code)
			}
			if rows := auditDenials(t, db); len(rows) != 2 {
				t.Errorf("deny rows = %d, want 2", len(rows))
			}
			if code := do(t, "POST", base+"/api/v1/monitors", monitorJSON("admin", "http", "https://status.vendor.example/", `"namespace":"shop"`), nil); code != http.StatusCreated {
				t.Errorf("administrator: status = %d, want 201", code)
			}
		})
	}
}

// monitorRequestURL is the request a proxy may be handed: an HTTP monitor's
// URL, a WebSocket monitor's with ws/wss read as http/https as the checker
// sends the handshake, and nothing for a type that connects directly or for a
// check of each pod.
func TestMonitorRequestURL(t *testing.T) {
	for _, tc := range []struct {
		m    store.Monitor
		want string
	}{
		{store.Monitor{Type: store.MonitorHTTP, Target: "https://status.vendor.example/health"}, "https://status.vendor.example/health"},
		{store.Monitor{Type: store.MonitorHTTP, Target: "http://status.vendor.example/"}, "http://status.vendor.example/"},
		{store.Monitor{Type: store.MonitorWebSocket, Target: "wss://stream.vendor.example/s"}, "https://stream.vendor.example/s"},
		{store.Monitor{Type: store.MonitorWebSocket, Target: "ws://stream.vendor.example/s"}, "http://stream.vendor.example/s"},
		{store.Monitor{Type: store.MonitorWebSocket, Target: "ftp://stream.vendor.example/s"}, ""},
		{store.Monitor{Type: store.MonitorWebSocket, Target: "https://stream.vendor.example/s"}, ""},
		{store.Monitor{Type: store.MonitorHTTP, Target: "ftp://files.vendor.example/"}, ""},
		{store.Monitor{Type: store.MonitorHTTP, Target: "https://api.shop.svc/", EachPod: true}, ""},
		{store.Monitor{Type: store.MonitorTCP, Target: "status.vendor.example:443"}, ""},
		{store.Monitor{Type: store.MonitorGRPC, Target: "status.vendor.example:443"}, ""},
		{store.Monitor{Type: store.MonitorDNS, Target: "status.vendor.example"}, ""},
		{store.Monitor{Type: store.MonitorPing, Target: "status.vendor.example"}, ""},
		{store.Monitor{Type: store.MonitorPostgres, Target: "db.vendor.example:5432"}, ""},
	} {
		got := ""
		if u := monitorRequestURL(tc.m); u != nil {
			got = u.String()
		}
		if got != tc.want {
			t.Errorf("monitorRequestURL(%s %s) = %q, want %q", tc.m.Type, tc.m.Target, got, tc.want)
		}
	}
}
