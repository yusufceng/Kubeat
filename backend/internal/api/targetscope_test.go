package api

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/metrics"
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
