package api

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/metrics"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// scopeAudit is an API whose target-scope refusals a test reads: on a fake
// clock, with UPTIME_LOGIN_AUDIT_PER_MINUTE at perMinute, the meter, the
// backend's log and the caps' end-of-minute warnings, which run only when
// the test runs them.
type scopeAudit struct {
	srv   *httptest.Server
	db    *store.SQLite
	clock *fakeClock
	logs  *syncBuffer
	meter *metrics.Metrics

	mu    sync.Mutex
	later []func()
}

func newScopeAudit(t *testing.T, perMinute int, customize func(*Options)) *scopeAudit {
	t.Helper()
	a := &scopeAudit{
		clock: &fakeClock{at: time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)},
		logs:  &syncBuffer{},
		meter: metrics.New("test"),
	}
	a.srv, a.db = newTestAPIWith(t, func(o *Options) {
		o.now = a.clock.now
		o.auditLater = func(_ time.Duration, f func()) {
			a.mu.Lock()
			defer a.mu.Unlock()
			a.later = append(a.later, f)
		}
		o.LoginAuditPerMinute = perMinute
		o.Meter = a.meter
		o.Logger = slog.New(slog.NewTextHandler(a.logs, nil))
		if customize != nil {
			customize(o)
		}
	})
	return a
}

// counted is kubeat_target_scope_denied_total for reason, the API's
// refusals being all a user's.
func (a *scopeAudit) counted(t *testing.T, reason string) float64 {
	t.Helper()
	got, err := a.meter.Gather()
	if err != nil {
		t.Fatal(err)
	}
	return got[scopeDeniedKey(reason)]
}

// warnings are the log lines of refused requests.
func (a *scopeAudit) warnings() []string {
	var out []string
	for line := range strings.Lines(a.logs.String()) {
		if strings.Contains(line, "level=WARN") && strings.Contains(line, "a request was refused by the target scope") {
			out = append(out, line)
		}
	}
	return out
}

// Of a minute's target-scope refusals of requests, no more than
// UPTIME_LOGIN_AUDIT_PER_MINUTE leave a deny row and a WARN line; the
// counter counts every one, and once the minute is over one warning says
// how many rows it left out. The next minute writes again, the failed
// sign-ins have a cap of their own, and every refusal is the same 403.
func TestScopeDenialRowsCappedPerMinute(t *testing.T) {
	a := newScopeAudit(t, 2, nil)
	base := a.srv.URL
	token := signInScoped(t, base)
	refuse := func(name string) {
		t.Helper()
		var problem errorBody
		code := doAsError(t, token, "POST", base+"/api/v1/monitors",
			monitorJSON(name, "http", "https://kubernetes.default.svc/healthz", `"namespace":"shop"`), &problem)
		if code != http.StatusForbidden || problem != (errorBody{Error: scopeRefused, Code: codeTargetScope}) {
			t.Errorf("%s: %d %+v, want 403 with the generic refusal", name, code, problem)
		}
	}
	for _, name := range []string{"a", "b", "c", "d"} {
		refuse(name)
	}
	if n := a.counted(t, metrics.ScopeDeniedTarget); n != 4 {
		t.Errorf("reason=target = %v, want 4: every refusal counts", n)
	}
	if rows := auditDenials(t, a.db); len(rows) != 2 {
		t.Errorf("deny rows = %d, want 2, the minute's cap", len(rows))
	}
	if lines := a.warnings(); len(lines) != 2 {
		t.Errorf("WARN lines = %d, want 2: a line goes with its row\n%s", len(lines), a.logs)
	}
	if strings.Contains(a.logs.String(), ScopeDenialsDropped) {
		t.Errorf("warned of the rows left out before the minute was over: %s", a.logs)
	}

	// A failed sign-in of a made-up name in the same minute is written: the
	// sign-ins have a cap of their own.
	if _, code := signIn(t, base, "ghost", "parola-123"); code != http.StatusUnauthorized {
		t.Fatalf("made-up name: %d, want 401", code)
	}
	entries, err := a.db.ListAudit(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	failed := 0
	for _, e := range entries {
		if e.Action == "login_failed" {
			failed++
		}
	}
	if failed != 1 {
		t.Errorf("login_failed rows = %d, want 1", failed)
	}

	// The minute is over: one warning says how many rows it left out.
	a.mu.Lock()
	later := a.later
	a.mu.Unlock()
	if len(later) != 1 {
		t.Fatalf("%d warnings were scheduled, want 1", len(later))
	}
	later[0]()
	if log := a.logs.String(); strings.Count(log, ScopeDenialsDropped) != 1 || !strings.Contains(log, "dropped=2") || !strings.Contains(log, "limit=2") {
		t.Errorf("warning of the minute: %s", log)
	}

	// The next minute writes again.
	a.clock.advance(time.Minute)
	refuse("e")
	if rows := auditDenials(t, a.db); len(rows) != 3 {
		t.Errorf("deny rows the next minute = %d, want 3", len(rows))
	}
	if lines := a.warnings(); len(lines) != 3 {
		t.Errorf("WARN lines the next minute = %d, want 3", len(lines))
	}
	if n := a.counted(t, metrics.ScopeDeniedTarget); n != 5 {
		t.Errorf("reason=target = %v, want 5", n)
	}
}

// A refused request's WARN line names the user, the namespace, the reason
// and the target as the scope read it, so that a central log tells a
// person's attempt from a check's; it never carries an address a name
// resolved to, and neither does the response, which is the 403 it was.
func TestScopeDenialWarnLine(t *testing.T) {
	a := newScopeAudit(t, 0, func(o *Options) {
		o.Resolver = answeringResolver{
			"api.payments":         ipList(t, "172.30.44.9"), // another team's Service, via ndots
			"servfail.example.com": servfail,
		}
	})
	base := a.srv.URL
	token := signInScoped(t, base)

	for _, tc := range []struct {
		name, path, body string
		problem          errorBody
		line             string
	}{
		{
			"spelled", "/api/v1/monitors", monitorJSON("apiserver", "http", "https://Kubernetes.Default.svc./healthz", `"namespace":"shop"`),
			errorBody{Error: scopeRefused, Code: codeTargetScope},
			"user=dev namespace=shop reason=target host=kubernetes.default.svc\n",
		},
		{
			"resolved into the cluster", "/api/v1/monitors", monitorJSON("payments", "http", "https://api.payments/", `"namespace":"shop"`),
			errorBody{Error: scopeRefused, Code: codeTargetScope},
			"user=dev namespace=shop reason=target host=api.payments\n",
		},
		{
			"unresolved", "/api/v1/monitors", monitorJSON("vendor", "http", "https://servfail.example.com/", `"namespace":"shop"`),
			errorBody{Error: scopeRefused, Code: codeTargetScope},
			"user=dev namespace=shop reason=resolve_error host=servfail.example.com\n",
		},
		{
			"namespace", "/api/v1/monitors", monitorJSON("other", "http", "https://status.example.com/", `"namespace":"payments"`),
			errorBody{Error: "you may change monitors only in: shop"},
			"user=dev namespace=payments reason=namespace host=\"\"\n",
		},
		{
			"channel", "/api/v1/channels", `{"name":"hook","type":"webhook","target":"https://hooks.payments.svc/x","namespaces":["shop"]}`,
			errorBody{Error: scopeRefused, Code: codeTargetScope},
			"user=dev namespace=shop reason=target host=hooks.payments.svc\n",
		},
		{
			"channel of every namespace", "/api/v1/channels", `{"name":"all","type":"webhook","target":"https://hooks.example.com/x"}`,
			errorBody{Error: "you may not do this"},
			"user=dev namespace=* reason=namespace host=\"\"\n",
		},
	} {
		var problem errorBody
		if code := doAsError(t, token, "POST", base+tc.path, tc.body, &problem); code != http.StatusForbidden || problem != tc.problem {
			t.Errorf("%s: %d %+v, want 403 %+v", tc.name, code, problem, tc.problem)
		}
		if lines := a.warnings(); len(lines) == 0 || !strings.HasSuffix(lines[len(lines)-1], tc.line) {
			t.Errorf("%s: WARN line = %q, want one ending %q", tc.name, lines, tc.line)
		}
	}
	if lines := a.warnings(); len(lines) != 6 {
		t.Errorf("WARN lines = %d, want one a refusal", len(lines))
	}
	if strings.Contains(a.logs.String(), "172.30.44.9") {
		t.Errorf("the log carries the address a name resolved to: %s", a.logs)
	}
}

// Every way a scoped user is refused a monitor or a window outside their
// namespaces leaves a deny row, a WARN line and a count — an import from
// discovery and its restore, a maintenance window's creation and deletion,
// a workload monitor's target, set or changed — and the refusal is the 403
// it was. The refusals of the list README "Kapsam ihlali denetimi" names
// ("Denetlenmeyen red yolları") are a role's, not a choice of target: a
// viewer of the namespace renewing a push token, deleting a monitor,
// sending a test notification or writing, changing or deleting an incident
// note. They leave nothing, and their 403 is unchanged too.
func TestScopeDenialPaths(t *testing.T) {
	a := newScopeAudit(t, 0, func(o *Options) {
		o.Discovery, o.DiscoveryStatus = &fakeDiscovery{}, "in-cluster"
	})
	base := a.srv.URL
	ctx := context.Background()
	monitor := func(body string) monitorResponse {
		t.Helper()
		var m monitorResponse
		if code := do(t, "POST", base+"/api/v1/monitors", body, &m); code != http.StatusCreated {
			t.Fatalf("create %s: %d", body, code)
		}
		return m
	}
	web := monitor(monitorJSON("web", "http", "https://web.example.com/", `"namespace":"shop"`))
	job := monitor(`{"name":"job","type":"push","namespace":"shop","interval_seconds":60,"timeout_seconds":5}`)
	pods := monitor(monitorJSON("pods", "workload", "shop/deployment/web", `"namespace":"shop"`))
	pay := monitor(monitorJSON("pay", "http", "https://pay.example.com/", `"namespace":"payments"`))
	window := func(body string) maintenanceResponse {
		t.Helper()
		var m maintenanceResponse
		if code := do(t, "POST", base+"/api/v1/maintenance", body, &m); code != http.StatusCreated {
			t.Fatalf("create window %s: %d", body, code)
		}
		return m
	}
	everywhere := window(`{"name":"freeze","duration_seconds":3600}`)
	shopWindow := window(`{"name":"deploy","namespace":"shop","duration_seconds":3600}`)
	incident, err := a.db.OpenIncident(ctx, store.Incident{MonitorID: web.ID, StartedAt: time.Now().Add(-time.Minute), Cause: "timeout"})
	if err != nil {
		t.Fatal(err)
	}
	var note noteResponse
	if code := do(t, "POST", base+"/api/v1/incidents/"+itoa(incident.ID)+"/notes", `{"text":"looking"}`, &note); code != http.StatusCreated {
		t.Fatalf("note: %d", code)
	}

	dev := signInScoped(t, base)
	addUser(t, base, "watcher", store.RoleViewer, "shop")
	watcher, code := signIn(t, base, "watcher", "parola-123")
	if code != http.StatusOK {
		t.Fatalf("sign in watcher: %d", code)
	}
	const editorRefusal, viewerRefusal = "you may change monitors only in: shop", "you may not do this"

	for _, tc := range []struct {
		name, token, method, path, body string
		refusal                         string
		// summary is the deny row written, "" for none; user its author.
		summary, user, reason string
	}{
		{"import", dev, "POST", "/api/v1/discovery/import", `{"namespace":"payments","sources":["service/api"]}`,
			editorRefusal, "namespace payments is outside the namespaces you may edit", "dev", metrics.ScopeDeniedNamespace},
		{"restore", dev, "POST", "/api/v1/discovery/restore", `{"namespace":"payments","source":"service/api"}`,
			editorRefusal, "namespace payments is outside the namespaces you may edit", "dev", metrics.ScopeDeniedNamespace},
		{"window of a namespace", dev, "POST", "/api/v1/maintenance", `{"name":"x","namespace":"payments","duration_seconds":60}`,
			editorRefusal, "namespace payments is outside the namespaces you may edit", "dev", metrics.ScopeDeniedNamespace},
		{"window of a monitor", dev, "POST", "/api/v1/maintenance", `{"name":"x","monitor_id":` + itoa(pay.ID) + `,"duration_seconds":60}`,
			editorRefusal, "namespace payments is outside the namespaces you may edit", "dev", metrics.ScopeDeniedNamespace},
		{"window of every namespace", dev, "POST", "/api/v1/maintenance", `{"name":"x","duration_seconds":60}`,
			editorRefusal, "namespace * is outside the namespaces you may edit", "dev", metrics.ScopeDeniedNamespace},
		{"deleting a window of every namespace", dev, "DELETE", "/api/v1/maintenance/" + itoa(everywhere.ID), "",
			editorRefusal, "namespace * is outside the namespaces you may edit", "dev", metrics.ScopeDeniedNamespace},
		{"deleting a window as a viewer", watcher, "DELETE", "/api/v1/maintenance/" + itoa(shopWindow.ID), "",
			viewerRefusal, "namespace shop is outside the namespaces you may edit", "watcher", metrics.ScopeDeniedNamespace},
		{"workload elsewhere", dev, "POST", "/api/v1/monitors", monitorJSON("theirs", "workload", "payments/deployment/api", `"namespace":"shop"`),
			`the workload's namespace "payments" is outside your namespaces`, "target payments/deployment/api is outside your namespaces", "dev", metrics.ScopeDeniedTarget},
		{"workload moved elsewhere", dev, "PATCH", "/api/v1/monitors/" + itoa(pods.ID), `{"target":"payments/deployment/api"}`,
			`the workload's namespace "payments" is outside your namespaces`, "target payments/deployment/api is outside your namespaces", "dev", metrics.ScopeDeniedTarget},

		{"push token", watcher, "POST", "/api/v1/monitors/" + itoa(job.ID) + "/push-token", "", viewerRefusal, "", "", ""},
		{"deleting a monitor", watcher, "DELETE", "/api/v1/monitors/" + itoa(web.ID), "", viewerRefusal, "", "", ""},
		{"test notification", watcher, "POST", "/api/v1/monitors/" + itoa(web.ID) + "/test-notification", "", viewerRefusal, "", "", ""},
		{"incident note", watcher, "POST", "/api/v1/incidents/" + itoa(incident.ID) + "/notes", `{"text":"me too"}`, viewerRefusal, "", "", ""},
		{"changing an incident note", watcher, "PATCH", "/api/v1/incidents/" + itoa(incident.ID) + "/notes/" + itoa(note.ID), `{"text":"edited"}`, viewerRefusal, "", "", ""},
		{"deleting an incident note", watcher, "DELETE", "/api/v1/incidents/" + itoa(incident.ID) + "/notes/" + itoa(note.ID), "", viewerRefusal, "", "", ""},
	} {
		rowsBefore, linesBefore := len(auditDenials(t, a.db)), len(a.warnings())
		countedBefore := map[string]float64{}
		for _, reason := range []string{metrics.ScopeDeniedTarget, metrics.ScopeDeniedNamespace, metrics.ScopeDeniedResolveError} {
			countedBefore[reason] = a.counted(t, reason)
		}

		var problem errorBody
		if code := doAsError(t, tc.token, tc.method, base+tc.path, tc.body, &problem); code != http.StatusForbidden || problem != (errorBody{Error: tc.refusal}) {
			t.Errorf("%s: %d %+v, want 403 %q", tc.name, code, problem, tc.refusal)
		}

		rows, lines := auditDenials(t, a.db), a.warnings()
		wantRows := rowsBefore
		if tc.summary != "" {
			wantRows++
		}
		if len(rows) != wantRows || len(lines) != linesBefore+wantRows-rowsBefore {
			t.Errorf("%s: deny rows %d → %d, WARN lines %d → %d; want %d more of each", tc.name, rowsBefore, len(rows), linesBefore, len(lines), wantRows-rowsBefore)
			continue
		}
		if tc.summary != "" {
			// The audit log lists the newest first.
			if row := rows[0]; row.Summary != tc.summary || row.Username != tc.user || row.ObjectType != "target-scope" {
				t.Errorf("%s: deny row = %+v, want %q by %s", tc.name, row, tc.summary, tc.user)
			}
			if line := lines[len(lines)-1]; !strings.Contains(line, "user="+tc.user+" ") || !strings.Contains(line, "reason="+tc.reason+" ") {
				t.Errorf("%s: WARN line = %q", tc.name, line)
			}
		}
		for reason, before := range countedBefore {
			want := before
			if reason == tc.reason {
				want++
			}
			if n := a.counted(t, reason); n != want {
				t.Errorf("%s: reason=%s = %v, want %v", tc.name, reason, n, want)
			}
		}
	}
}
