package api

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"testing"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// fakeResolver answers the dial-time scope check from a fixed table; an
// unknown name does not resolve. Used as the test API's resolver so a scope
// check never touches the real network.
type fakeResolver map[string][]netip.Addr

func (f fakeResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if addrs, ok := f[host]; ok {
		return addrs, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
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
	// A name that does not resolve at all is not a cluster target either.
	if code := doAs(t, token, "POST", base+"/api/v1/monitors", monitor("unresolved", "https://nowhere.example.net/"), nil); code != http.StatusCreated {
		t.Errorf("a name that does not resolve: status = %d", code)
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
