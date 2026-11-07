package api

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/discovery"
	"github.com/yusufceng/Kubeat/backend/internal/k8s"
	"github.com/yusufceng/Kubeat/backend/internal/metrics"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

func storedScope(t *testing.T, db *store.SQLite, id int64) *store.TargetScope {
	t.Helper()
	m, err := db.GetMonitor(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return m.Scope
}

// Whoever sets where a monitor's checks connect leaves the record of the
// scope they are held to: a scoped user's namespaces, sorted, or every
// namespace for one who sees all; a copy takes its maker's. Pausing,
// renaming or tagging keeps the record; a change of the target or of the
// type's options writes the changer's.
func TestScopeRecordFollowsWhoSetsTheTarget(t *testing.T) {
	srv, db := newTestAPI(t)
	base := srv.URL
	addUser(t, base, "dev", store.RoleEditor, "shop", "billing")
	token, code := signIn(t, base, "dev", "parola-123")
	if code != http.StatusOK {
		t.Fatalf("sign in: %d", code)
	}
	scoped := &store.TargetScope{Namespaces: []string{"billing", "shop"}}
	all := &store.TargetScope{Namespaces: []string{store.AllNamespaces}}

	var mine monitorResponse
	if code := doAs(t, token, "POST", base+"/api/v1/monitors", monitorJSON("mine", "http", "https://api.shop.svc/", `"namespace":"shop"`), &mine); code != http.StatusCreated {
		t.Fatalf("create: %d", code)
	}
	if got := storedScope(t, db, mine.ID); !reflect.DeepEqual(got, scoped) {
		t.Errorf("a scoped user's monitor: Scope = %+v, want %+v", got, scoped)
	}
	var admins monitorResponse
	if code := do(t, "POST", base+"/api/v1/monitors", monitorJSON("admins", "http", "https://status.example.com/", `"namespace":"shop"`), &admins); code != http.StatusCreated {
		t.Fatalf("create: %d", code)
	}
	if got := storedScope(t, db, admins.ID); !reflect.DeepEqual(got, all) {
		t.Errorf("an administrator's monitor: Scope = %+v, want %+v", got, all)
	}

	// The scoped user pauses, renames and tags the administrator's monitor
	// in their namespace: its checks stay the administrator's.
	for _, change := range []string{`{"enabled":false}`, `{"name":"admins-2"}`, `{"tags":["x"]}`} {
		if code := doAs(t, token, "PATCH", base+"/api/v1/monitors/"+itoa(admins.ID), change, nil); code != http.StatusOK {
			t.Fatalf("%s: %d", change, code)
		}
	}
	if got := storedScope(t, db, admins.ID); !reflect.DeepEqual(got, all) {
		t.Errorf("after a pause, a rename and tags: Scope = %+v, want %+v", got, all)
	}
	// Changing the options of its type makes the checks theirs.
	if code := doAs(t, token, "PATCH", base+"/api/v1/monitors/"+itoa(admins.ID), `{"http":{"keyword":"ok"}}`, nil); code != http.StatusOK {
		t.Fatalf("options: %d", code)
	}
	if got := storedScope(t, db, admins.ID); !reflect.DeepEqual(got, scoped) {
		t.Errorf("after the scoped user changed the options: Scope = %+v, want %+v", got, scoped)
	}
	// An administrator saving it as it is keeps it theirs
	// (TestScopeRecordSurvivesAFormSave); changing how it is checked takes
	// it back.
	if code := do(t, "PATCH", base+"/api/v1/monitors/"+itoa(admins.ID), `{"target":"https://status.example.com/"}`, nil); code != http.StatusOK {
		t.Fatalf("re-save: %d", code)
	}
	if got := storedScope(t, db, admins.ID); !reflect.DeepEqual(got, scoped) {
		t.Errorf("after the administrator saved it unchanged: Scope = %+v, want %+v", got, scoped)
	}
	if code := do(t, "PATCH", base+"/api/v1/monitors/"+itoa(admins.ID), `{"http":{"keyword":""}}`, nil); code != http.StatusOK {
		t.Fatalf("change: %d", code)
	}
	if got := storedScope(t, db, admins.ID); !reflect.DeepEqual(got, all) {
		t.Errorf("after the administrator changed the options: Scope = %+v, want %+v", got, all)
	}

	// A copy is its maker's.
	var copied monitorResponse
	if code := doAs(t, token, "POST", base+"/api/v1/monitors/"+itoa(admins.ID)+"/clone", `{"name":"copy"}`, &copied); code != http.StatusCreated {
		t.Fatalf("clone: %d", code)
	}
	if got := storedScope(t, db, copied.ID); !reflect.DeepEqual(got, scoped) {
		t.Errorf("a scoped user's copy: Scope = %+v, want %+v", got, scoped)
	}
}

// A scoped user may not take over the checks of an administrator's monitor
// whose target is outside their namespaces: changing its options is a new
// setting of where it connects, held to their scope like a new target, so it
// is refused rather than the monitor going down.
func TestScopeRecordOptionsOfAnOutOfScopeMonitor(t *testing.T) {
	srv, db := newTestAPI(t)
	base := srv.URL
	token := signInScoped(t, base)
	var admins monitorResponse
	if code := do(t, "POST", base+"/api/v1/monitors", monitorJSON("apiserver", "http", "https://kubernetes.default.svc/healthz", `"namespace":"shop"`), &admins); code != http.StatusCreated {
		t.Fatalf("create: %d", code)
	}
	var problem errorBody
	if code := doAsError(t, token, "PATCH", base+"/api/v1/monitors/"+itoa(admins.ID), `{"http":{"keyword":"ok"}}`, &problem); code != http.StatusForbidden || problem.Code != codeTargetScope {
		t.Errorf("options of an out-of-scope monitor: %d %+v, want 403 target_scope", code, problem)
	}
	if code := doAs(t, token, "PATCH", base+"/api/v1/monitors/"+itoa(admins.ID), `{"enabled":false}`, nil); code != http.StatusOK {
		t.Errorf("pausing it: %d", code)
	}
	if got := storedScope(t, db, admins.ID); got == nil || !got.All() {
		t.Errorf("Scope = %+v, want the administrator's", got)
	}
}

// Every endpoint a check connects to is held to the scope when it is set, as
// the target is, in every form a host is written in, with an audit row and
// the target reason of the counter.
func TestScopeRecordEndpointsAreHeldWhenSet(t *testing.T) {
	meter := metrics.New("test")
	resolver := companyResolver(t)
	resolver["status.example.com"] = ipList(t, "203.0.113.10")
	srv, db := newTestAPIWith(t, func(o *Options) {
		o.Meter = meter
		o.Resolver = resolver
	})
	base := srv.URL
	token := signInScoped(t, base)
	dns := func(name, resolver string) string {
		return monitorJSON(name, "dns", "status.example.com", `"namespace":"shop","dns":{"record_type":"A","resolver":"`+resolver+`"}`)
	}

	refused := []string{
		"172.30.0.10",                 // a bare ClusterIP
		"[fd00::10]:53",               // over IPv6
		"dns",                         // a short name
		"api.payments",                // two labels: ndots lands it on a Service
		"kube-dns.kube-system.svc:53", // another namespace's Service
		"kube-dns.kube-system.svc.cluster.local:53", // its full name
		"kubernetes.default.svc",                    // the API server
		"kubernetes.default",                        // its two-label name
		"172-30-0-1.sslip.io",                       // a public name pointing into the cluster
		"gone.example.com",                          // not provably outside the cluster
	}
	for _, resolver := range refused {
		var problem errorBody
		if code := doAsError(t, token, "POST", base+"/api/v1/monitors", dns("d-"+resolver, resolver), &problem); code != http.StatusForbidden || problem.Code != codeTargetScope || problem.Error != scopeRefused {
			t.Errorf("resolver %s: %d %+v, want 403 with the generic refusal", resolver, code, problem)
		}
	}
	if rows := auditDenials(t, db); len(rows) != len(refused) {
		t.Errorf("deny rows = %d, want %d", len(rows), len(refused))
	}
	got, err := meter.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if n := got["kubeat_target_scope_denied_total{reason=target}"]; n != float64(len(refused)) {
		t.Errorf("reason=target = %v, want %d", n, len(refused))
	}

	// In scope: one's own namespace's Service, a public resolver.
	var own monitorResponse
	if code := doAs(t, token, "POST", base+"/api/v1/monitors", dns("own", "dns.shop.svc:53"), &own); code != http.StatusCreated {
		t.Fatalf("one's own resolver: %d", code)
	}
	if code := doAs(t, token, "POST", base+"/api/v1/monitors", dns("public", "8.8.8.8"), nil); code != http.StatusCreated {
		t.Errorf("a public resolver: %d", code)
	}
	// Set again, on an edit and on a copy, it is held alike.
	var problem errorBody
	if code := doAsError(t, token, "PATCH", base+"/api/v1/monitors/"+itoa(own.ID), `{"dns":{"record_type":"A","resolver":"10.96.0.10"}}`, &problem); code != http.StatusForbidden {
		t.Errorf("an edit to a ClusterIP resolver: %d", code)
	}
	var admins monitorResponse
	if code := do(t, "POST", base+"/api/v1/monitors", dns("admins", "10.96.0.10"), &admins); code != http.StatusCreated {
		t.Fatalf("an administrator's resolver at a ClusterIP: %d", code)
	}
	if code := doAsError(t, token, "POST", base+"/api/v1/monitors/"+itoa(admins.ID)+"/clone", `{"name":"copy"}`, &problem); code != http.StatusForbidden {
		t.Errorf("a scoped user's copy of it: %d", code)
	}
}

// An imported monitor's target is the object's: its record says so, and
// holds the importer's namespaces.
func TestScopeRecordOfAnImport(t *testing.T) {
	route := k8s.Candidate{Kind: k8s.KindRoute, Namespace: "shop", Name: "orders", Type: store.MonitorHTTP,
		Target: "https://orders.apps.corp.example/", Reason: k8s.ReasonRouteHost}
	srv, db := newTestAPIWith(t, func(o *Options) {
		o.Resolver = companyResolver(t)
		o.Discovery, o.DiscoveryStatus = &fakeDiscovery{candidates: []k8s.Candidate{route}}, "in-cluster"
	})
	token := signInScoped(t, srv.URL)
	var result importResponse
	body := `{"namespace":"shop","sources":["` + route.Source() + `"],"interval_seconds":60,"timeout_seconds":5,"failure_threshold":3}`
	if code := doAs(t, token, "POST", srv.URL+"/api/v1/discovery/import", body, &result); code != http.StatusOK || len(result.Created) != 1 {
		t.Fatalf("import: %d %+v", code, result)
	}
	want := &store.TargetScope{Namespaces: []string{"shop"}, Discovered: true}
	if got := storedScope(t, db, result.Created[0].ID); !reflect.DeepEqual(got, want) {
		t.Errorf("Scope = %+v, want %+v", got, want)
	}
	// Renaming it keeps it the object's.
	if code := doAs(t, token, "PATCH", srv.URL+"/api/v1/monitors/"+itoa(result.Created[0].ID), `{"name":"orders"}`, nil); code != http.StatusOK {
		t.Fatalf("rename: %d", code)
	}
	if got := storedScope(t, db, result.Created[0].ID); !reflect.DeepEqual(got, want) {
		t.Errorf("after a rename Scope = %+v, want %+v", got, want)
	}
}

// formSave saves monitor id as the monitor form does: every field the form
// has, the options of the monitor's type among them, sent back as the
// monitor has them (secrets masked), with change on top. It is sent with
// token, or as the administrator for "".
func formSave(t *testing.T, base, token string, id int64, change map[string]any) int {
	t.Helper()
	var m map[string]any
	if code := do(t, http.MethodGet, base+"/api/v1/monitors/"+itoa(id), "", &m); code != http.StatusOK {
		t.Fatalf("GET monitor %d: %d", id, code)
	}
	form := map[string]any{}
	for _, field := range []string{"description", "target", "interval_seconds", "timeout_seconds", "failure_threshold",
		"retry_interval_seconds", "inverted", "slow_above_ms", "tags", "http", "tcp", "dns", "grpc", "websocket", "database", "workload"} {
		if value := m[field]; value != nil {
			form[field] = value
		}
	}
	if m["source"] == "" {
		form["name"], form["namespace"] = m["name"], m["namespace"]
	} else {
		form["each_pod"] = m["each_pod"]
	}
	for field, value := range change {
		form[field] = value
	}
	body, err := json.Marshal(form)
	if err != nil {
		t.Fatal(err)
	}
	url := base + "/api/v1/monitors/" + itoa(id)
	if token == "" {
		return do(t, http.MethodPatch, url, string(body), nil)
	}
	return doAs(t, token, http.MethodPatch, url, string(body), nil)
}

// The record is the scope of whoever last changed how the checks run. The
// form sends every field on every save, so a save through it that changes
// only the name, the tags or the interval leaves the record as it is: an
// administrator who tidies up a scoped user's monitor does not lift its
// scope, nor does a scoped user who renames an administrator's narrow it.
// A monitor without a record takes an administrator's on such a save, the
// way back after an upgrade, and nobody else's.
func TestScopeRecordSurvivesAFormSave(t *testing.T) {
	srv, db := newTestAPI(t)
	base := srv.URL
	token := signInScoped(t, base)
	shop := &store.TargetScope{Namespaces: []string{"shop"}}
	all := &store.TargetScope{Namespaces: []string{store.AllNamespaces}}
	target := func(name string) string {
		return monitorJSON(name, "http", "https://status.example.com/", `"namespace":"shop","http":{"method":"GET","expected_status":"200-399","max_redirects":5,"headers":{"X-Team":"shop"},"auth":"bearer","auth_secret":"s3cret"}`)
	}

	var mine monitorResponse
	if code := doAs(t, token, "POST", base+"/api/v1/monitors", target("mine"), &mine); code != http.StatusCreated {
		t.Fatalf("create: %d", code)
	}
	if code := formSave(t, base, "", mine.ID, map[string]any{"tags": []string{"payments"}, "interval_seconds": 120}); code != http.StatusOK {
		t.Fatalf("the administrator's save: %d", code)
	}
	if got := storedScope(t, db, mine.ID); !reflect.DeepEqual(got, shop) {
		t.Errorf("a scoped user's monitor after an administrator's form save: Scope = %+v, want %+v", got, shop)
	}
	if m, err := db.GetMonitor(context.Background(), mine.ID); err != nil || m.HTTP.AuthSecret != "s3cret" || m.Interval != 2*time.Minute {
		t.Errorf("the save did not keep the secret or take the interval: %+v, %v", m, err)
	}

	var admins monitorResponse
	if code := do(t, "POST", base+"/api/v1/monitors", target("admins"), &admins); code != http.StatusCreated {
		t.Fatalf("create: %d", code)
	}
	if code := formSave(t, base, token, admins.ID, map[string]any{"name": "renamed"}); code != http.StatusOK {
		t.Fatalf("the scoped user's save: %d", code)
	}
	if got := storedScope(t, db, admins.ID); !reflect.DeepEqual(got, all) {
		t.Errorf("an administrator's monitor after a scoped user's form save: Scope = %+v, want %+v", got, all)
	}

	// Without a record, the monitor is held to its namespace: a scoped
	// user's save leaves it so, an administrator's gives it every
	// namespace.
	old, err := db.CreateMonitor(context.Background(), store.Monitor{
		Name: "old", Type: store.MonitorHTTP, Target: "https://status.example.com/", Namespace: "shop", Enabled: true,
		Interval: time.Minute, Timeout: 5 * time.Second, FailureThreshold: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if code := formSave(t, base, token, old.ID, map[string]any{"name": "old-renamed"}); code != http.StatusOK {
		t.Fatalf("the scoped user's save of one without a record: %d", code)
	}
	if got := storedScope(t, db, old.ID); got != nil {
		t.Errorf("after the scoped user's save: Scope = %+v, want none", got)
	}
	if code := formSave(t, base, "", old.ID, nil); code != http.StatusOK {
		t.Fatalf("the administrator's save of one without a record: %d", code)
	}
	if got := storedScope(t, db, old.ID); !reflect.DeepEqual(got, all) {
		t.Errorf("after the administrator's save: Scope = %+v, want %+v", got, all)
	}
}

// An annotation monitor keeps its object's record while the sync manages
// it, whoever changes its options: the object sets where it connects.
func TestScopeRecordOfAnAnnotationMonitor(t *testing.T) {
	srv, db := newTestAPIWith(t, func(o *Options) {
		o.Sync = fakeSync{status: discovery.Status{IntervalSeconds: 60, Skipped: []discovery.Skip{}}}
	})
	m, err := db.CreateMonitor(context.Background(), store.Monitor{
		Name: "shop/ingress/web", Type: store.MonitorHTTP, Target: "https://web.example.com/", Namespace: "shop",
		Source: "ingress/web", Managed: true, Enabled: true, Scope: store.ObjectScope("shop"),
		Interval: time.Minute, Timeout: 5 * time.Second, FailureThreshold: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if code := do(t, "PATCH", srv.URL+"/api/v1/monitors/"+itoa(m.ID), `{"http":{"keyword":"ok"}}`, nil); code != http.StatusOK {
		t.Fatalf("options: %d", code)
	}
	if got := storedScope(t, db, m.ID); !reflect.DeepEqual(got, store.ObjectScope("shop")) {
		t.Errorf("after an administrator changed its options: Scope = %+v, want the object's", got)
	}
}
