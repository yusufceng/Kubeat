package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// A workload monitor's pods live in the namespace its target names, which
// may differ from the namespace the monitor is grouped under; a scoped
// editor cannot point one at a namespace they cannot read, and cannot see
// the pods or events of one that an administrator pointed there.
func TestWorkloadMonitorsStayInReadableNamespaces(t *testing.T) {
	srv, _ := newTestAPI(t)
	base := srv.URL
	addUser(t, base, "dev", store.RoleEditor, "shop")
	token, code := signIn(t, base, "dev", "parola-123")
	if code != http.StatusOK {
		t.Fatalf("sign in: %d", code)
	}
	monitor := func(target string) string {
		return `{"name":"` + target + `","type":"workload","target":"` + target + `","namespace":"shop","interval_seconds":30,"timeout_seconds":5}`
	}

	var refused map[string]any
	if code := doAs(t, token, "POST", base+"/api/v1/monitors", monitor("kube-system/deployment/coredns"), &refused); code != http.StatusForbidden {
		t.Errorf("workload in another namespace: status = %d, want 403 (%v)", code, refused)
	}
	var own monitorResponse
	if code := doAs(t, token, "POST", base+"/api/v1/monitors", monitor("shop/deployment/api"), &own); code != http.StatusCreated {
		t.Fatalf("workload in own namespace: status = %d", code)
	}
	// Nor may the target be moved there afterwards.
	if code := doAs(t, token, "PATCH", base+"/api/v1/monitors/"+itoa(own.ID), `{"target":"kube-system/deployment/coredns"}`, &refused); code != http.StatusForbidden {
		t.Errorf("moving the target: status = %d, want 403 (%v)", code, refused)
	}

	// An administrator may group such a monitor under shop; dev then sees
	// the monitor but neither its pods nor its events.
	var admins monitorResponse
	if code := do(t, "POST", base+"/api/v1/monitors", monitor("kube-system/deployment/coredns"), &admins); code != http.StatusCreated {
		t.Fatalf("administrator's monitor: status = %d", code)
	}
	var seen monitorResponse
	if code := doAs(t, token, "GET", base+"/api/v1/monitors/"+itoa(admins.ID), "", &seen); code != http.StatusOK {
		t.Errorf("the monitor itself: status = %d, want 200", code)
	}
	var pods workloadResponse
	if code := doAs(t, token, "GET", base+"/api/v1/monitors/"+itoa(admins.ID)+"/workload", "", &pods); code != http.StatusOK || pods.Available || !strings.Contains(pods.Reason, "outside your namespaces") {
		t.Errorf("pods of another namespace: status %d, %+v", code, pods)
	}
	var window windowResponse
	from := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	if code := doAs(t, token, "GET", base+"/api/v1/monitors/"+itoa(admins.ID)+"/window?from="+from, "", &window); code != http.StatusOK || len(window.Events) != 0 || !strings.Contains(window.EventsReason, "outside your namespaces") {
		t.Errorf("events of another namespace: status %d, events %+v, reason %q", code, window.Events, window.EventsReason)
	}
}
