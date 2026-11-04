package api

import (
	"net/http"
	"testing"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

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
