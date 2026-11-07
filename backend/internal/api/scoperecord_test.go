package api

import (
	"cmp"
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"strings"
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
// renaming or tagging keeps the record; a change of the target writes the
// changer's, a change of the type's options narrows it to theirs.
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
	// An administrator saving it as it is, or changing what it sends or
	// reads, keeps it theirs (TestScopeRecordWidenedOnlyWhereTheChecksConnect);
	// naming where it connects takes it back.
	if code := do(t, "PATCH", base+"/api/v1/monitors/"+itoa(admins.ID), `{"target":"https://status.example.com/"}`, nil); code != http.StatusOK {
		t.Fatalf("re-save: %d", code)
	}
	if got := storedScope(t, db, admins.ID); !reflect.DeepEqual(got, scoped) {
		t.Errorf("after the administrator saved it unchanged: Scope = %+v, want %+v", got, scoped)
	}
	if code := do(t, "PATCH", base+"/api/v1/monitors/"+itoa(admins.ID), `{"http":{"keyword":""}}`, nil); code != http.StatusOK {
		t.Fatalf("change: %d", code)
	}
	if got := storedScope(t, db, admins.ID); !reflect.DeepEqual(got, scoped) {
		t.Errorf("after the administrator changed the options: Scope = %+v, want %+v", got, scoped)
	}
	if code := do(t, "PATCH", base+"/api/v1/monitors/"+itoa(admins.ID), `{"target":"https://status.example.com/health"}`, nil); code != http.StatusOK {
		t.Fatalf("retarget: %d", code)
	}
	if got := storedScope(t, db, admins.ID); !reflect.DeepEqual(got, all) {
		t.Errorf("after the administrator changed the target: Scope = %+v, want %+v", got, all)
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
	// gone.example.com does not resolve: a fault of the resolver, not an
	// attempt, for the operator.
	if n := got["kubeat_target_scope_denied_total{reason=target}{source=user}"]; n != float64(len(refused)-1) {
		t.Errorf("reason=target = %v, want %d", n, len(refused)-1)
	}
	if n := got["kubeat_target_scope_denied_total{reason=resolve_error}{source=user}"]; n != 1 {
		t.Errorf("reason=resolve_error = %v, want 1", n)
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

// formContentTypes are the content types the monitor form offers.
var formContentTypes = []string{"application/json", "application/x-www-form-urlencoded", "application/xml", "text/plain"}

// textarea is s as a browser submits a textarea that holds it: every line
// break as CRLF.
func textarea(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.ReplaceAll(s, "\n", "\r\n")
}

// formSave saves monitor id as the monitor form does. The form
// (frontend/app/ui/monitor-form.tsx) is filled from the monitor as the API
// returns it, secrets masked, and its action (saveMonitorAction,
// frontend/app/actions.ts) sends every field it has, with change on top, as
// the browser and the action write them: trimmed where the action trims, a
// textarea's line breaks as CRLF, a control's default where the monitor has
// no value, nothing where its value is none of the control's options. It is
// sent with token, or as the administrator for "". Only the types the tests
// save are written out.
func formSave(t *testing.T, base, token string, id int64, change map[string]any) int {
	t.Helper()
	var m monitorResponse
	if code := do(t, http.MethodGet, base+"/api/v1/monitors/"+itoa(id), "", &m); code != http.StatusOK {
		t.Fatalf("GET monitor %d: %d", id, code)
	}
	tags := m.Tags
	if tags == nil {
		tags = []string{}
	}
	form := map[string]any{
		"description": strings.TrimSpace(m.Description), "target": strings.TrimSpace(m.Target),
		"interval_seconds": m.IntervalSeconds, "timeout_seconds": m.TimeoutSeconds, "failure_threshold": m.FailureThreshold,
		"retry_interval_seconds": m.RetryIntervalSeconds, "inverted": m.Inverted, "slow_above_ms": m.SlowAboveMS, "tags": tags,
	}
	switch {
	case m.Source == "":
		form["name"], form["namespace"] = m.Name, m.Namespace
	case strings.HasPrefix(m.Source, "service/") && m.EachPod && !m.Managed:
		form["each_pod"] = true // an unticked or disabled box is not sent
	}
	switch m.Type {
	case string(store.MonitorHTTP):
		h := m.HTTP
		var lines []string
		for name, value := range h.Headers {
			lines = append(lines, name+": "+value)
		}
		headers := map[string]string{}
		for _, line := range strings.Split(textarea(strings.Join(lines, "\n")), "\n") {
			if strings.TrimSpace(line) != "" {
				name, value, _ := strings.Cut(line, ":")
				headers[strings.TrimSpace(name)] = strings.TrimSpace(value)
			}
		}
		contentType := h.ContentType
		if contentType == "" {
			contentType = "application/json"
		}
		if !slices.Contains(formContentTypes, contentType) {
			contentType = ""
		}
		jsonPath, operator, expected := strings.TrimSpace(h.JSONPath), "", ""
		if jsonPath != "" {
			operator, expected = cmp.Or(h.JSONOperator, "=="), strings.TrimSpace(h.JSONExpected)
		}
		authUser, authSecret := "", ""
		if h.Auth == string(store.HTTPAuthBasic) {
			authUser = strings.TrimSpace(h.AuthUser)
		}
		if h.Auth != "" {
			authSecret = h.AuthSecret
		}
		redirects := store.DefaultMaxRedirects
		if h.MaxRedirects != nil {
			redirects = *h.MaxRedirects
		}
		form["http"] = map[string]any{
			"method": cmp.Or(h.Method, "GET"), "expected_status": strings.TrimSpace(cmp.Or(h.ExpectedStatus, "200-399")),
			"max_redirects": redirects, "keyword": h.Keyword, "invert_keyword": h.InvertKeyword,
			"json_path": jsonPath, "json_operator": operator, "json_expected": expected,
			"headers": headers, "body": textarea(h.Body), "content_type": contentType,
			"auth": h.Auth, "auth_user": authUser, "auth_secret": authSecret, "tls_skip_verify": h.TLSSkipVerify,
		}
	case string(store.MonitorDNS):
		form["dns"] = map[string]any{
			"record_type": cmp.Or(m.DNS.RecordType, "A"), "resolver": strings.TrimSpace(m.DNS.Resolver),
			"expected": strings.TrimSpace(m.DNS.Expected),
		}
	default:
		t.Fatalf("formSave does not write out a %s monitor", m.Type)
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

// The form sends every field on every save, so a save through it that
// changes only the name, the tags or the interval of a monitor whose
// options it writes back as they are leaves the record as it is: an
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

// A save by someone whose scope is wider than the record's keeps the record
// whatever else of the check it changes, through the API or through the
// form, with what the form writes of the options of a monitor set up
// through the API (formSave). The record is widened only by a change of
// where the checks connect: the target, the server or site an HTTP request
// names, a DNS check's resolver.
func TestScopeRecordWidenedOnlyWhereTheChecksConnect(t *testing.T) {
	srv, db := newTestAPI(t)
	base := srv.URL
	token := signInScoped(t, base)
	shop := &store.TargetScope{Namespaces: []string{"shop"}}
	all := &store.TargetScope{Namespaces: []string{store.AllNamespaces}}
	stored := func(id int64) store.Monitor {
		t.Helper()
		m, err := db.GetMonitor(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	create := func(name, typ, target, options string) int64 {
		t.Helper()
		var m monitorResponse
		if code := doAs(t, token, "POST", base+"/api/v1/monitors", monitorJSON(name, typ, target, `"namespace":"shop",`+options), &m); code != http.StatusCreated {
			t.Fatalf("create %s: %d", name, code)
		}
		if got := storedScope(t, db, m.ID); !reflect.DeepEqual(got, shop) {
			t.Fatalf("%s: Scope = %+v, want %+v", name, got, shop)
		}
		return m.ID
	}

	for _, c := range []struct {
		name, options string
		// rewritten reports whether the form wrote the options back
		// otherwise than the API stored them, as the case needs.
		rewritten func(store.HTTPOptions) bool
	}{
		{"body-without-content-type", `"http":{"method":"POST","body":"a=1"}`,
			func(o store.HTTPOptions) bool { return o.ContentType == "application/json" }},
		{"body-of-lines", `"http":{"method":"POST","body":"{\n  \"ok\": true\n}","content_type":"application/json"}`,
			func(o store.HTTPOptions) bool { return o.Body == "{\r\n  \"ok\": true\r\n}" }},
		{"padded-expected-value", `"http":{"json_path":"status","json_expected":" ok "}`,
			func(o store.HTTPOptions) bool { return o.JSONExpected == "ok" }},
	} {
		id := create(c.name, "http", "https://status.example.com/", c.options)
		if code := formSave(t, base, "", id, map[string]any{"tags": []string{"payments"}}); code != http.StatusOK {
			t.Fatalf("%s: the administrator's form save: %d", c.name, code)
		}
		m := stored(id)
		if !c.rewritten(m.HTTP) || !slices.Equal(m.Tags, []string{"payments"}) {
			t.Fatalf("%s: the form save did not write what the form sends: %+v, tags %v", c.name, m.HTTP, m.Tags)
		}
		if !reflect.DeepEqual(m.Scope, shop) {
			t.Errorf("%s: after the administrator's form save Scope = %+v, want %+v", c.name, m.Scope, shop)
		}
	}

	// A user of more namespaces than the record holds keeps it alike.
	addUser(t, base, "wide", store.RoleEditor, "billing", "shop")
	wide, code := signIn(t, base, "wide", "parola-123")
	if code != http.StatusOK {
		t.Fatalf("sign in: %d", code)
	}
	id := create("by-a-wider-user", "http", "https://status.example.com/", `"http":{"method":"POST","body":"a=1"}`)
	if code := formSave(t, base, wide, id, map[string]any{"name": "by-a-wider-user-2"}); code != http.StatusOK {
		t.Fatalf("the wider user's form save: %d", code)
	}
	if got := stored(id).Scope; !reflect.DeepEqual(got, shop) {
		t.Errorf("after a wider user's form save Scope = %+v, want %+v", got, shop)
	}

	// Options that name no host keep it; the server or site a request
	// names, and the target, widen it.
	id = create("http", "http", "https://status.example.com/", `"http":{"method":"GET"}`)
	for _, change := range []string{
		`{"http":{"method":"POST","body":"a=1","keyword":"ok","max_redirects":0,"headers":{"X-Team":"shop"}}}`,
		`{"http":{"method":"GET","max_redirects":20,"tls_skip_verify":true}}`,
		`{"inverted":true}`,
	} {
		if code := do(t, "PATCH", base+"/api/v1/monitors/"+itoa(id), change, nil); code != http.StatusOK {
			t.Fatalf("%s: %d", change, code)
		}
		if got := stored(id).Scope; !reflect.DeepEqual(got, shop) {
			t.Errorf("after the administrator's %s Scope = %+v, want %+v", change, got, shop)
		}
	}
	if code := do(t, "PATCH", base+"/api/v1/monitors/"+itoa(id), `{"http":{"headers":{"Host":"www.example.com"}}}`, nil); code != http.StatusOK {
		t.Fatalf("Host: %d", code)
	}
	if got := stored(id).Scope; !reflect.DeepEqual(got, all) {
		t.Errorf("after the administrator named another site Scope = %+v, want %+v", got, all)
	}
	id = create("target", "http", "https://status.example.com/", `"http":{"method":"GET"}`)
	if code := do(t, "PATCH", base+"/api/v1/monitors/"+itoa(id), `{"target":"https://status.example.com/health"}`, nil); code != http.StatusOK {
		t.Fatalf("target: %d", code)
	}
	if got := stored(id).Scope; !reflect.DeepEqual(got, all) {
		t.Errorf("after the administrator changed the target Scope = %+v, want %+v", got, all)
	}

	// A DNS check: what it asks keeps the record, the resolver it asks
	// widens it.
	id = create("dns", "dns", "status.example.com", `"dns":{"record_type":"A","resolver":"8.8.8.8"}`)
	if code := formSave(t, base, "", id, map[string]any{"dns": map[string]any{"record_type": "AAAA", "resolver": "8.8.8.8", "expected": "2001:db8::1"}}); code != http.StatusOK {
		t.Fatalf("record type: %d", code)
	}
	if got := stored(id).Scope; !reflect.DeepEqual(got, shop) {
		t.Errorf("after the administrator changed what is asked Scope = %+v, want %+v", got, shop)
	}
	if code := formSave(t, base, "", id, map[string]any{"dns": map[string]any{"record_type": "A", "resolver": "1.1.1.1"}}); code != http.StatusOK {
		t.Fatalf("resolver: %d", code)
	}
	if got := stored(id).Scope; !reflect.DeepEqual(got, all) {
		t.Errorf("after the administrator changed the resolver Scope = %+v, want %+v", got, all)
	}
}

// Anyone whose scope the record does not hold narrows it by any change of
// how the checks run, the form's save of what it writes otherwise among
// them, to the namespaces both hold: a scoped user who changes what an
// administrator's monitor sends or reads does not keep the administrator's
// scope for it.
func TestScopeRecordNarrowedByAnyChangeOfTheCheck(t *testing.T) {
	srv, db := newTestAPI(t)
	base := srv.URL
	token := signInScoped(t, base)
	shop := &store.TargetScope{Namespaces: []string{"shop"}}

	var admins monitorResponse
	if code := do(t, "POST", base+"/api/v1/monitors", monitorJSON("admins", "http", "https://status.example.com/", `"namespace":"shop","http":{"method":"POST","body":"a=1"}`), &admins); code != http.StatusCreated {
		t.Fatalf("create: %d", code)
	}
	if code := formSave(t, base, token, admins.ID, map[string]any{"name": "admins-2"}); code != http.StatusOK {
		t.Fatalf("the scoped user's form save: %d", code)
	}
	if got := storedScope(t, db, admins.ID); !reflect.DeepEqual(got, shop) {
		t.Errorf("an administrator's monitor after a scoped user's form save that changed an option: Scope = %+v, want %+v", got, shop)
	}

	// Of two users with scopes neither of which holds the other's, the
	// second holds the checks to the namespaces both have.
	addUser(t, base, "billing", store.RoleEditor, "billing", "shop")
	addUser(t, base, "payments", store.RoleEditor, "payments", "shop")
	first, _ := signIn(t, base, "billing", "parola-123")
	second, _ := signIn(t, base, "payments", "parola-123")
	var theirs monitorResponse
	if code := doAs(t, first, "POST", base+"/api/v1/monitors", monitorJSON("theirs", "http", "https://status.example.com/", `"namespace":"shop"`), &theirs); code != http.StatusCreated {
		t.Fatalf("create: %d", code)
	}
	if code := doAs(t, second, "PATCH", base+"/api/v1/monitors/"+itoa(theirs.ID), `{"http":{"keyword":"ok"}}`, nil); code != http.StatusOK {
		t.Fatalf("options: %d", code)
	}
	if got := storedScope(t, db, theirs.ID); !reflect.DeepEqual(got, shop) {
		t.Errorf("after a user of other namespaces changed the options: Scope = %+v, want %+v", got, shop)
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
