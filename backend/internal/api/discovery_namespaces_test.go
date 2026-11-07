package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/yusufceng/Kubeat/backend/internal/discovery"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// The namespaces the sync could not read are told like the objects it
// skipped: each reader is told of their own namespaces, with the reason;
// only an administrator gets the cluster's words, everyone else the kind of
// failure, and what one reader is told leaves the syncer's status alone.
func TestDiscoveryStatusTellsTheSkippedNamespaces(t *testing.T) {
	shopErr := fmt.Errorf("list services: %w", rbacRefusal("list", "services", "", "shop"))
	financeErr := fmt.Errorf("list services: %w", rbacRefusal("list", "services", "", "finance"))
	srv, _ := newTestAPIWith(t, func(o *Options) {
		o.Discovery, o.DiscoveryStatus = &fakeDiscovery{}, "in-cluster"
		o.Sync = fakeSync{status: discovery.Status{IntervalSeconds: 60, Skipped: []discovery.Skip{},
			SkippedNamespaces: []discovery.NamespaceSkip{
				{Namespace: "finance", Reason: "forbidden", Error: financeErr.Error(), Cause: financeErr},
				{Namespace: "shop", Reason: "forbidden", Error: shopErr.Error(), Cause: shopErr},
			},
		}}
	})
	addUser(t, srv.URL, "viewer", store.RoleViewer, "shop")
	addUser(t, srv.URL, "seer", store.RoleViewer, "*")
	url := srv.URL + "/api/v1/discovery"

	told := func(st *discovery.Status) string {
		var out []string
		for _, n := range st.SkippedNamespaces {
			out = append(out, n.Namespace+" "+n.Reason+": "+n.Error)
		}
		return strings.Join(out, "\n")
	}
	for _, tc := range []struct{ user, want string }{
		{"viewer", "shop forbidden: Kubeat's service account may not list services in shop; ask an administrator"},
		{"seer", "finance forbidden: Kubeat's service account may not list services in finance; ask an administrator\n" +
			"shop forbidden: Kubeat's service account may not list services in shop; ask an administrator"},
	} {
		token, _ := signIn(t, srv.URL, tc.user, "parola-123")
		code, body := bodyAs(t, token, url)
		var status discoveryStatusResponse
		if err := json.Unmarshal([]byte(body), &status); err != nil || code != http.StatusOK || status.Sync == nil {
			t.Fatalf("%s: status = %d, body %s", tc.user, code, body)
		}
		if got := told(status.Sync); got != tc.want {
			t.Errorf("%s is told\n%s\nwant\n%s", tc.user, got, tc.want)
		}
		if strings.Contains(body, "system:serviceaccount") || (tc.user == "viewer" && strings.Contains(body, "finance")) {
			t.Errorf("%s reads %s", tc.user, body)
		}
	}

	var status discoveryStatusResponse
	if code := do(t, http.MethodGet, url, "", &status); code != http.StatusOK || status.Sync == nil {
		t.Fatalf("admin: status = %d", code)
	}
	if got, want := told(status.Sync), "finance forbidden: "+financeErr.Error()+"\nshop forbidden: "+shopErr.Error(); got != want {
		t.Errorf("the administrator is told\n%s\nwant\n%s", got, want)
	}
}
