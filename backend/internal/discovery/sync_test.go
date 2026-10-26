package discovery

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/k8s"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

type fakeCluster struct {
	candidates []k8s.Candidate
	err        error
}

func (f *fakeCluster) Candidates(_ context.Context, namespace string) ([]k8s.Candidate, error) {
	if namespace != "" {
		panic("the syncer reads the whole cluster at once")
	}
	return f.candidates, f.err
}

func monitored(extra map[string]string) map[string]string {
	a := map[string]string{AnnotationMonitor: "true"}
	for k, v := range extra {
		a[k] = v
	}
	return a
}

func serviceCandidate(name string, port int32, portName string, a map[string]string) k8s.Candidate {
	return k8s.Candidate{
		Kind: k8s.KindService, Namespace: "shop", Name: name, Port: port, PortName: portName, Annotations: a,
		Type: store.MonitorTCP, Target: name + ".shop.svc:" + itoa(port), Reason: k8s.ReasonTCPFallback,
	}
}

func itoa(n int32) string { return strconv.Itoa(int(n)) }

func newSyncer(t *testing.T, cluster *fakeCluster) (*Syncer, *store.SQLite) {
	t.Helper()
	db, err := store.OpenSQLite(context.Background(), filepath.Join(t.TempDir(), "test.db"), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return New(Options{
		Cluster: cluster, Store: db, Interval: time.Minute,
		DefaultInterval: 30 * time.Second, DefaultTimeout: 5 * time.Second, DefaultFailureThreshold: 3, MinInterval: 5 * time.Second,
	}), db
}

func names(t *testing.T, db *store.SQLite) map[string]store.Monitor {
	t.Helper()
	list, err := db.ListMonitors(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[string]store.Monitor)
	for _, m := range list {
		out[m.Name] = m
	}
	return out
}

func TestSyncCreatesUpdatesAndRemovesManagedMonitors(t *testing.T) {
	ctx := context.Background()
	cluster := &fakeCluster{candidates: []k8s.Candidate{
		serviceCandidate("api", 8080, "http", monitored(map[string]string{AnnotationInterval: "20s", AnnotationTimeout: "3s", AnnotationFailureThreshold: "2"})),
		serviceCandidate("plain", 80, "", nil), // not annotated
		{Kind: k8s.KindDeployment, Namespace: "shop", Name: "api", Annotations: monitored(nil),
			Type: store.MonitorWorkload, Target: k8s.WorkloadTarget("shop", k8s.KindDeployment, "api"), Reason: k8s.ReasonWorkload},
	}}
	s, db := newSyncer(t, cluster)

	if err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	st := s.Status()
	if st.Created != 2 || st.Managed != 2 || st.Error != "" || len(st.Skipped) != 0 {
		t.Fatalf("first run: %+v", st)
	}
	got := names(t, db)
	api, ok := got["shop/service/api:8080"]
	if !ok {
		t.Fatalf("api monitor missing from %v", got)
	}
	if !api.Managed || api.Interval != 20*time.Second || api.Timeout != 3*time.Second || api.FailureThreshold != 2 || api.Namespace != "shop" || !api.Enabled {
		t.Errorf("api monitor = %+v", api)
	}
	if _, ok := got["shop/plain"]; ok {
		t.Error("the unannotated service was monitored")
	}
	if _, ok := got["shop/deployment/api"]; !ok {
		t.Error("the annotated deployment was not monitored")
	}

	// The same cluster again: nothing to do.
	if err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if st := s.Status(); st.Created != 0 || st.Updated != 0 || st.Removed != 0 || st.Managed != 2 {
		t.Errorf("second run: %+v", st)
	}

	// A probe appears (type change) and the interval annotation changes;
	// the deployment loses its annotation.
	cluster.candidates[0].Type, cluster.candidates[0].Target, cluster.candidates[0].Reason = store.MonitorHTTP, "http://api.shop.svc:8080/healthz", k8s.ReasonReadinessProbe
	cluster.candidates[2].Annotations = nil
	if err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if st := s.Status(); st.Updated != 1 || st.Removed != 1 || st.Managed != 1 {
		t.Errorf("third run: %+v", st)
	}
	got = names(t, db)
	if api = got["shop/service/api:8080"]; api.Type != store.MonitorHTTP || api.Target != "http://api.shop.svc:8080/healthz" || api.Interval != 20*time.Second {
		t.Errorf("after type change: %+v", api)
	}
	if _, ok := got["shop/deployment/api"]; ok {
		t.Error("the deployment stayed monitored without its annotation")
	}

	// Only the target changes: the monitor is updated in place, keeping its id.
	cluster.candidates[0].Target = "http://api.shop.svc:8080/ready"
	if err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if st := s.Status(); st.Updated != 1 || st.Created != 0 {
		t.Errorf("fourth run: %+v", st)
	}
	if after := names(t, db)["shop/service/api:8080"]; after.ID != api.ID || after.Target != "http://api.shop.svc:8080/ready" {
		t.Errorf("after target change: %+v (was id %d)", after, api.ID)
	}

	audit, err := db.ListAudit(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(audit) == 0 || audit[0].Username != Actor || audit[0].ObjectType != "monitor" {
		t.Errorf("audit = %+v", audit)
	}
}

// An object that leads its monitor to another server, a Route to another
// host or an Ingress without TLS, no longer takes the secrets stored with
// the monitor along, nor its request body: they are dropped, and the audit
// log names them. A new path on the same server keeps them, and so does TLS
// added to an Ingress: the same host, over HTTPS.
func TestSyncDropsSecretsWhenTheTargetMovesToAnotherServer(t *testing.T) {
	ctx := context.Background()
	cluster := &fakeCluster{candidates: []k8s.Candidate{
		{Kind: k8s.KindRoute, Namespace: "shop", Name: "web", Annotations: monitored(nil),
			Type: store.MonitorHTTP, Target: "https://web.shop.example/", Reason: k8s.ReasonRouteHost},
		{Kind: k8s.KindIngress, Namespace: "shop", Name: "site", Annotations: monitored(nil),
			Type: store.MonitorHTTP, Target: "https://site.shop.example/", Reason: k8s.ReasonIngressRule},
		{Kind: k8s.KindIngress, Namespace: "shop", Name: "plain", Annotations: monitored(nil),
			Type: store.MonitorHTTP, Target: "http://plain.shop.example/", Reason: k8s.ReasonIngressRule},
	}}
	s, db := newSyncer(t, cluster)
	if err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	web, site, plain := "shop/route/web", "shop/ingress/site@site.shop.example/", "shop/ingress/plain@plain.shop.example/"
	// Their editors give the monitors credentials, and a body to log in with.
	body := `{"username":"probe","password":"S3cr3t"}`
	for _, name := range []string{web, site, plain} {
		m := names(t, db)[name]
		m.HTTP.Auth, m.HTTP.AuthSecret, m.HTTP.Headers = store.HTTPAuthBearer, "BEARER-TOKEN", map[string]string{"X-Api-Key": "API-KEY"}
		m.HTTP.Method, m.HTTP.Body, m.HTTP.ContentType = "POST", body, "application/json"
		if _, err := db.UpdateMonitor(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	kept := func(name string) bool {
		m := names(t, db)[name]
		return m.HTTP.AuthSecret == "BEARER-TOKEN" && m.HTTP.Headers["X-Api-Key"] == "API-KEY" && m.HTTP.Body == body
	}

	cluster.candidates[0].Target = "https://web.shop.example/healthz"
	if err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if !kept(web) || !kept(site) || !kept(plain) {
		t.Errorf("a new path on the same server: %+v", names(t, db)[web].HTTP)
	}

	cluster.candidates[0].Target = "https://capture.attacker.example/"
	cluster.candidates[1].Target = "http://site.shop.example/"
	cluster.candidates[2].Target = "https://plain.shop.example/"
	if err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if st := s.Status(); st.Updated != 3 || st.Removed != 0 {
		t.Errorf("status = %+v", st)
	}
	for name, target := range map[string]string{web: "https://capture.attacker.example/", site: "http://site.shop.example/"} {
		m := names(t, db)[name]
		if m.Target != target || m.HTTP.AuthSecret != "" || len(m.HTTP.Headers) != 0 || m.HTTP.Body != "" || m.HTTP.ContentType != "" ||
			m.HTTP.Auth != store.HTTPAuthBearer || m.HTTP.Method != "POST" {
			t.Errorf("%s: target %s, http %+v", name, m.Target, m.HTTP)
		}
	}
	if m := names(t, db)[plain]; m.Target != "https://plain.shop.example/" || !kept(plain) {
		t.Errorf("TLS added: target %s, http %+v", m.Target, m.HTTP)
	}
	audit, err := db.ListAudit(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	dropped := 0
	for _, e := range audit {
		if e.Username == Actor && e.Action == "update" && strings.Contains(e.Summary, "dropped") {
			if !strings.HasSuffix(e.Summary, "; dropped http.auth_secret, http.body, http.headers.X-Api-Key, which go only to the server they were entered for") {
				t.Errorf("audit: %q", e.Summary)
			}
			dropped++
		}
	}
	if dropped != 2 {
		t.Errorf("audit = %+v", audit)
	}
}

func TestSyncLeavesMonitorsMadeByHandAlone(t *testing.T) {
	ctx := context.Background()
	cluster := &fakeCluster{candidates: []k8s.Candidate{serviceCandidate("api", 8080, "", monitored(nil))}}
	s, db := newSyncer(t, cluster)
	byHand, err := db.CreateMonitor(ctx, store.Monitor{
		Name: "my api", Type: store.MonitorTCP, Target: "api.shop.svc:8080", Namespace: "shop", Source: "service/api:8080",
		Interval: time.Minute, Timeout: 5 * time.Second, FailureThreshold: 3, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	st := s.Status()
	if st.Created != 0 || len(st.Skipped) != 1 || st.Skipped[0].Reason != "already monitored by hand" {
		t.Errorf("status = %+v", st)
	}
	// Dropping the annotation does not remove what the syncer did not make.
	cluster.candidates[0].Annotations = nil
	if err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if got := names(t, db)["my api"]; got.ID != byHand.ID || got.Managed {
		t.Errorf("hand-made monitor = %+v", got)
	}
}

func TestSyncHonoursPortAndPath(t *testing.T) {
	ctx := context.Background()
	a := monitored(map[string]string{AnnotationPort: "web", AnnotationPath: "/healthz"})
	cluster := &fakeCluster{candidates: []k8s.Candidate{
		serviceCandidate("api", 8080, "web", a),
		serviceCandidate("api", 9090, "metrics", a),
	}}
	s, db := newSyncer(t, cluster)
	if err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	got := names(t, db)
	if len(got) != 1 {
		t.Fatalf("monitors = %v, want only the web port", got)
	}
	m := got["shop/service/api:8080"]
	if m.Type != store.MonitorHTTP || m.Target != "http://api.shop.svc:8080/healthz" {
		t.Errorf("monitor = %+v", m)
	}

	// Each pod, asked for on the service.
	cluster.candidates[0].Annotations = monitored(map[string]string{AnnotationPort: "web", AnnotationEachPod: "true"})
	if err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if m := names(t, db)["shop/service/api:8080"]; !m.EachPod || m.Type != store.MonitorTCP {
		t.Errorf("each pod = %+v", m)
	}

	// A probe-derived HTTPS check keeps its scheme and gets the path.
	cluster.candidates[0].Type, cluster.candidates[0].Target = store.MonitorHTTP, "https://api.shop.svc:8080/livez"
	cluster.candidates[0].Annotations = monitored(map[string]string{AnnotationPort: "8080", AnnotationPath: "/ready"})
	if err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if m := names(t, db)["shop/service/api:8080"]; m.Target != "https://api.shop.svc:8080/ready" {
		t.Errorf("target = %q", m.Target)
	}
}

func TestSyncReportsBadAnnotations(t *testing.T) {
	cluster := &fakeCluster{candidates: []k8s.Candidate{
		serviceCandidate("fast", 80, "", monitored(map[string]string{AnnotationInterval: "1s"})),
		serviceCandidate("slow", 80, "", monitored(map[string]string{AnnotationTimeout: "2m"})),
		serviceCandidate("odd", 80, "", monitored(map[string]string{AnnotationInterval: "soon"})),
		serviceCandidate("zero", 80, "", monitored(map[string]string{AnnotationFailureThreshold: "0"})),
		serviceCandidate("rel", 80, "", monitored(map[string]string{AnnotationPath: "healthz"})),
		{Kind: k8s.KindIngress, Namespace: "shop", Name: "web", Annotations: monitored(map[string]string{AnnotationPath: "/x"}),
			Type: store.MonitorHTTP, Target: "https://shop.example.com/", Reason: k8s.ReasonIngressRule},
		{Kind: k8s.KindDeployment, Namespace: "shop", Name: "api", Annotations: monitored(map[string]string{AnnotationEachPod: "true"}),
			Type: store.MonitorWorkload, Target: k8s.WorkloadTarget("shop", k8s.KindDeployment, "api"), Reason: k8s.ReasonWorkload},
		serviceCandidate("ok", 80, "", monitored(map[string]string{AnnotationMonitor: " True "})),
	}}
	s, db := newSyncer(t, cluster)
	if err := s.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := s.Status()
	if st.Created != 1 || len(st.Skipped) != 7 {
		t.Errorf("status = %+v", st)
	}
	if got := names(t, db); len(got) != 1 || got["shop/service/ok:80"].Name == "" {
		t.Errorf("monitors = %v", got)
	}
}

// Each namespace's share of the counts is kept, so that a reader who sees
// only some namespaces can be told about those alone.
func TestSyncCountsEachNamespace(t *testing.T) {
	ctx := context.Background()
	ledger := serviceCandidate("ledger", 80, "", monitored(nil))
	ledger.Namespace, ledger.Target = "billing", "ledger.billing.svc:80"
	legacy := serviceCandidate("legacy", 81, "", monitored(map[string]string{AnnotationInterval: "1s"}))
	legacy.Namespace, legacy.Target = "billing", "legacy.billing.svc:81"
	cluster := &fakeCluster{candidates: []k8s.Candidate{
		serviceCandidate("api", 80, "", monitored(nil)),
		serviceCandidate("web", 81, "", monitored(nil)),
		ledger, legacy,
	}}
	s, _ := newSyncer(t, cluster)
	if err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	st := s.Status()
	if st.Managed != 3 || st.Created != 3 || len(st.Skipped) != 1 ||
		st.ByNamespace["shop"] != (Counts{Managed: 2, Created: 2}) || st.ByNamespace["billing"] != (Counts{Managed: 1, Created: 1}) {
		t.Fatalf("first run: %+v", st)
	}

	shop := st.Within(func(ns string) bool { return ns == "shop" })
	if shop.Managed != 2 || shop.Created != 2 || len(shop.Skipped) != 0 || len(shop.ByNamespace) != 1 {
		t.Errorf("within shop: %+v", shop)
	}
	billing := st.Within(func(ns string) bool { return ns == "billing" })
	if billing.Managed != 1 || billing.Created != 1 || len(billing.Skipped) != 1 || billing.Skipped[0].Source != "service/legacy:81" {
		t.Errorf("within billing: %+v", billing)
	}
	if none := st.Within(func(string) bool { return false }); none.Managed != 0 || none.Created != 0 || none.Skipped == nil || len(none.Skipped) != 0 {
		t.Errorf("within nothing: %+v", none)
	}
	// What a reader is told leaves what the syncer keeps alone.
	if again := s.Status(); again.Managed != 3 || len(again.Skipped) != 1 || len(again.ByNamespace) != 2 {
		t.Errorf("the syncer's own status after Within: %+v", again)
	}

	// A removal counts in the namespace the monitor was in.
	cluster.candidates = cluster.candidates[:1]
	if err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	st = s.Status()
	if st.Managed != 1 || st.Removed != 2 ||
		st.ByNamespace["shop"] != (Counts{Managed: 1, Removed: 1}) || st.ByNamespace["billing"] != (Counts{Removed: 1}) {
		t.Errorf("after removals: %+v", st)
	}

	// A failed run keeps the counts of the last good one, and the error.
	cluster.err = errors.New("the server is currently unable to handle the request")
	if err := s.Sync(ctx); err == nil {
		t.Fatal("expected the cluster error")
	}
	if st := s.Status(); !errors.Is(st.Cause, cluster.err) || st.Error == "" || st.ByNamespace["billing"] != (Counts{Removed: 1}) {
		t.Errorf("after a failed run: %+v", st)
	}
}

func TestSyncChangesNothingWhenTheClusterIsAway(t *testing.T) {
	ctx := context.Background()
	cluster := &fakeCluster{candidates: []k8s.Candidate{serviceCandidate("api", 8080, "", monitored(nil))}}
	s, db := newSyncer(t, cluster)
	if err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	cluster.err = errors.New("the server is currently unable to handle the request")
	if err := s.Sync(ctx); err == nil {
		t.Fatal("expected the cluster error")
	}
	st := s.Status()
	if st.Error == "" || st.Managed != 1 {
		t.Errorf("status = %+v", st)
	}
	if got := names(t, db); len(got) != 1 {
		t.Errorf("monitors after a failed read = %v", got)
	}
}

func TestSyncLeavesADeletedMonitorDeleted(t *testing.T) {
	cluster := &fakeCluster{candidates: []k8s.Candidate{serviceCandidate("api", 80, "http", monitored(nil))}}
	s, db := newSyncer(t, cluster)
	ctx := context.Background()
	if err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	m, ok := names(t, db)["shop/service/api:80"]
	if !ok {
		t.Fatal("the annotated service was not monitored")
	}

	// Someone deletes it in Kubeat: the annotation still asks, the
	// dismissal says no.
	if err := db.DismissSource(ctx, store.DismissedSource{Namespace: "shop", Source: m.Source, At: time.Now(), By: "ayse"}); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteMonitor(ctx, m.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if len(names(t, db)) != 0 {
		t.Fatal("the deleted monitor came back")
	}
	if st := s.Status(); len(st.Skipped) != 1 || !st.Skipped[0].Dismissed || st.Skipped[0].Source != "service/api:80" || st.Created != 0 {
		t.Errorf("status = %+v", st)
	}

	// Restoring it brings it back at the next sync.
	if err := db.RestoreSource(ctx, "shop", m.Source); err != nil {
		t.Fatal(err)
	}
	if err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok := names(t, db)["shop/service/api:80"]; !ok {
		t.Fatal("the restored monitor was not created again")
	}

	// A dismissal ends with the annotation: annotating again starts afresh.
	m = names(t, db)["shop/service/api:80"]
	if err := db.DismissSource(ctx, store.DismissedSource{Namespace: "shop", Source: m.Source, At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteMonitor(ctx, m.ID); err != nil {
		t.Fatal(err)
	}
	cluster.candidates = []k8s.Candidate{serviceCandidate("api", 80, "http", nil)} // annotation removed
	if err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if left, _ := db.ListDismissedSources(ctx); len(left) != 0 {
		t.Errorf("dismissal outlived its annotation: %+v", left)
	}
	cluster.candidates = []k8s.Candidate{serviceCandidate("api", 80, "http", monitored(nil))}
	if err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok := names(t, db)["shop/service/api:80"]; !ok {
		t.Fatal("annotating again did not create the monitor")
	}
}

func ingressCandidate(name, host, path string, a map[string]string) k8s.Candidate {
	return k8s.Candidate{
		Kind: k8s.KindIngress, Namespace: "shop", Name: name, Annotations: a,
		Type: store.MonitorHTTP, Target: "https://" + host + path, Reason: k8s.ReasonIngressRule,
	}
}

func skipped(st Status) map[string]string {
	out := make(map[string]string)
	for _, s := range st.Skipped {
		out[s.Source] = s.Reason
	}
	return out
}

// A managed monitor of an external host made before the external minimum,
// at an interval now under it, is kept as it is, history and all, like
// the API keeps such a monitor: the rule binds new monitors. The run says
// so in the skipped list; a new candidate under the minimum is still
// refused, and the kept monitor goes when its annotation does.
func TestSyncKeepsAnExistingFastExternalMonitor(t *testing.T) {
	ctx := context.Background()
	cluster := &fakeCluster{candidates: []k8s.Candidate{
		ingressCandidate("fast", "victim.example", "/", monitored(map[string]string{AnnotationInterval: "10s", AnnotationTimeout: "2s"})),
	}}
	s, db := newSyncer(t, cluster)
	// Before the upgrade: no external minimum; the monitor is made at 10s.
	if err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	before := names(t, db)["shop/ingress/fast@victim.example/"]
	if before.ID == 0 || before.Interval != 10*time.Second {
		t.Fatalf("before the rule: %+v", before)
	}
	if err := db.InsertCheckResults(ctx, []store.CheckResult{{MonitorID: before.ID, Up: true, CheckedAt: time.Now()}}); err != nil {
		t.Fatal(err)
	}

	// The upgrade: the external minimum is on, and another fast Ingress
	// is annotated.
	s.opts.MinIntervalExternal = 30 * time.Second
	cluster.candidates = append(cluster.candidates,
		ingressCandidate("new", "victim.example", "/new", monitored(map[string]string{AnnotationInterval: "10s", AnnotationTimeout: "2s"})))
	for run := 1; run <= 2; run++ {
		if err := s.Sync(ctx); err != nil {
			t.Fatal(err)
		}
		st := s.Status()
		after, ok := names(t, db)["shop/ingress/fast@victim.example/"]
		if !ok || after.ID != before.ID || after.Interval != 10*time.Second || !after.Managed {
			t.Fatalf("run %d: the monitor made before the rule = %+v, %v; want kept as it was (id %d, 10s, managed)", run, after, ok, before.ID)
		}
		if st.Removed != 0 || st.Created != 0 || st.Updated != 0 || st.Managed != 1 {
			t.Errorf("run %d: %+v; want nothing removed, one managed", run, st)
		}
		rule := "kubeat.io/interval: must be at least 30s for a target outside the cluster (UPTIME_MIN_CHECK_INTERVAL_EXTERNAL)"
		want := map[string]string{
			"ingress/fast@victim.example/":   rule + "; the monitor made before the rule is kept at 10s",
			"ingress/new@victim.example/new": rule,
		}
		if got := skipped(st); fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("run %d skipped:\n got  %v\n want %v", run, got, want)
		}
		if _, made := names(t, db)["shop/ingress/new@victim.example/new"]; made {
			t.Errorf("run %d: the new fast candidate was made", run)
		}
	}
	results, err := db.ListCheckResults(ctx, before.ID, 10)
	if err != nil || len(results) != 1 {
		t.Errorf("the kept monitor's history: %d results, err = %v; want the one", len(results), err)
	}

	// The annotation taken off: the kept monitor goes like any other.
	cluster.candidates[0].Annotations = nil
	if err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if _, still := names(t, db)["shop/ingress/fast@victim.example/"]; still || s.Status().Removed != 1 {
		t.Errorf("after the annotation went: still there = %v, status %+v", still, s.Status())
	}
}

// The discovery default interval is for in-cluster targets and may be
// under the external minimum: an external Ingress without an interval of
// its own is then skipped with the rule's sentence, the one it already
// has is kept, and in-cluster candidates are untouched.
func TestSyncHoldsTheDefaultIntervalToTheExternalMinimum(t *testing.T) {
	ctx := context.Background()
	cluster := &fakeCluster{candidates: []k8s.Candidate{
		ingressCandidate("web", "shop.example.com", "/", monitored(nil)),
		serviceCandidate("api", 8080, "http", monitored(nil)),
	}}
	s, db := newSyncer(t, cluster)
	s.opts.DefaultInterval = 15 * time.Second
	if err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if got := names(t, db); len(got) != 2 {
		t.Fatalf("before the rule: %v", got)
	}

	s.opts.MinIntervalExternal = 30 * time.Second
	cluster.candidates = append(cluster.candidates,
		ingressCandidate("docs", "docs.example.com", "/", monitored(nil)),
		serviceCandidate("cache", 6379, "", monitored(nil)))
	if err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	st := s.Status()
	got := names(t, db)
	if web, ok := got["shop/ingress/web@shop.example.com/"]; !ok || web.Interval != 15*time.Second {
		t.Errorf("the Ingress monitor at the default interval: %+v, %v; want kept at 15s", web, ok)
	}
	if _, ok := got["shop/ingress/docs@docs.example.com/"]; ok {
		t.Error("a new external Ingress at the default interval was made")
	}
	if _, ok := got["shop/service/cache:6379"]; !ok {
		t.Error("a new in-cluster service at the default interval was not made")
	}
	want := map[string]string{
		"ingress/web@shop.example.com/":  "kubeat.io/interval: must be at least 30s for a target outside the cluster (UPTIME_MIN_CHECK_INTERVAL_EXTERNAL); the monitor made before the rule is kept at 15s",
		"ingress/docs@docs.example.com/": "kubeat.io/interval: must be at least 30s for a target outside the cluster (UPTIME_MIN_CHECK_INTERVAL_EXTERNAL)",
	}
	if got := skipped(st); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("skipped:\n got  %v\n want %v", got, want)
	}
	if st.Removed != 0 || st.Created != 1 || st.Managed != 3 {
		t.Errorf("status: %+v; want nothing removed, one created, three managed", st)
	}
}

// The syncer is held to the limits on load like the API, in the API's
// words: an external Ingress host may not be checked faster than the
// external minimum; the quotas and the cap per host turn candidates away,
// the same ones on every run, since the candidates are taken in order; a
// candidate turned away is in the status as skipped. The account's
// creations a minute do not apply: the syncer is not an account.
func TestSyncIsHeldToTheLoadLimits(t *testing.T) {
	ctx := context.Background()
	cluster := &fakeCluster{candidates: []k8s.Candidate{
		ingressCandidate("fast", "victim.example", "/", monitored(map[string]string{AnnotationInterval: "5s", AnnotationTimeout: "2s"})),
		ingressCandidate("web", "victim.example", "/a", monitored(nil)),
		ingressCandidate("web", "victim.example", "/b", monitored(nil)),
		serviceCandidate("api", 8080, "http", monitored(nil)),
		serviceCandidate("db", 5432, "", monitored(nil)),
		serviceCandidate("cache", 6379, "", monitored(nil)),
	}}
	s, db := newSyncer(t, cluster)
	s.opts.MinIntervalExternal, s.opts.MaxMonitorsPerHost, s.opts.MaxMonitorsPerNamespace = 30*time.Second, 1, 3

	if err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	st := s.Status()
	want := map[string]string{
		"ingress/fast@victim.example/": "kubeat.io/interval: must be at least 30s for a target outside the cluster (UPTIME_MIN_CHECK_INTERVAL_EXTERNAL)",
		"ingress/web@victim.example/b": "at most 1 monitors may check the host victim.example (UPTIME_MAX_MONITORS_PER_HOST); it has 1 already",
		"service/db:5432":              `the namespace "shop" is at the limit of 3 monitors (UPTIME_MAX_MONITORS_PER_NAMESPACE)`,
	}
	if got := skipped(st); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("skipped:\n got  %v\n want %v", got, want)
	}
	if st.Created != 3 || st.Managed != 3 {
		t.Errorf("first run: created %d, managed %d; want 3 and 3", st.Created, st.Managed)
	}
	got := names(t, db)
	for _, name := range []string{"shop/ingress/web@victim.example/a", "shop/service/api:8080", "shop/service/cache:6379"} {
		if _, ok := got[name]; !ok {
			t.Errorf("%s was not created; have %v", name, got)
		}
	}

	// The same cluster again: the same candidates are turned away, nothing
	// flaps. (The reason given may be another rule that now binds too: the
	// namespace, full since the first run, is checked before the host.)
	if err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	st = s.Status()
	got2 := skipped(st)
	if len(got2) != len(want) {
		t.Errorf("second run skipped %v, want the same candidates as the first: %v", got2, want)
	}
	for source := range want {
		if _, ok := got2[source]; !ok {
			t.Errorf("second run no longer skips %s: %v", source, got2)
		}
	}
	if st.Created != 0 || st.Updated != 0 || st.Removed != 0 || st.Managed != 3 {
		t.Errorf("second run: %+v", st)
	}

	// An object moving its monitor onto a full host is turned away too,
	// and its monitor stays as it was.
	cluster.candidates[3].Annotations[AnnotationPath] = "/healthz"
	cluster.candidates[3].Type, cluster.candidates[3].Target = store.MonitorHTTP, "https://victim.example/api"
	if err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	st = s.Status()
	if reason := skipped(st)["service/api:8080"]; !strings.Contains(reason, "UPTIME_MAX_MONITORS_PER_HOST") {
		t.Errorf("a managed monitor moved onto a full host: skipped = %v", skipped(st))
	}
	if api := names(t, db)["shop/service/api:8080"]; api.Target != "api.shop.svc:8080" || api.Type != store.MonitorTCP {
		t.Errorf("the monitor was changed although its new target was refused: %+v", api)
	}
	if st.Managed != 3 {
		t.Errorf("managed = %d after a refused change, want 3", st.Managed)
	}

	// With the limits off, everything but the too-fast one is made.
	s.opts.MaxMonitorsPerHost, s.opts.MaxMonitorsPerNamespace = 0, 0
	if err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	st = s.Status()
	if len(st.Skipped) != 1 || st.Skipped[0].Source != "ingress/fast@victim.example/" {
		t.Errorf("with the quotas off, skipped = %v", st.Skipped)
	}
	if st.Managed != 5 {
		t.Errorf("managed = %d with the quotas off, want 5", st.Managed)
	}
}

// An annotation edited into something invalid keeps the monitor made from
// it earlier: the candidate is reported, nothing is removed, until the
// annotation itself goes.
func TestSyncKeepsAMonitorWhoseAnnotationBroke(t *testing.T) {
	ctx := context.Background()
	cluster := &fakeCluster{candidates: []k8s.Candidate{
		ingressCandidate("web", "shop.example.com", "/", monitored(map[string]string{AnnotationInterval: "60s"})),
	}}
	s, db := newSyncer(t, cluster)
	if err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	before := names(t, db)["shop/ingress/web@shop.example.com/"]
	if before.ID == 0 {
		t.Fatal("the monitor was not made")
	}
	// kubeat.io/path applies to a Service only: the candidate is refused.
	cluster.candidates[0].Annotations[AnnotationPath] = "/healthz"
	for run := 1; run <= 2; run++ {
		if err := s.Sync(ctx); err != nil {
			t.Fatal(err)
		}
		st := s.Status()
		after, ok := names(t, db)["shop/ingress/web@shop.example.com/"]
		if !ok || after.ID != before.ID || !after.Managed || st.Removed != 0 {
			t.Fatalf("run %d: monitor %+v %v, status %+v; want it kept and nothing removed", run, after, ok, st)
		}
		if got := skipped(st); len(got) != 1 || !strings.HasSuffix(got["ingress/web@shop.example.com/"], "; the monitor made earlier is kept as it is") {
			t.Errorf("run %d skipped: %v", run, got)
		}
	}
	cluster.candidates[0].Annotations = nil
	if err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if _, still := names(t, db)["shop/ingress/web@shop.example.com/"]; still || s.Status().Removed != 1 {
		t.Errorf("after the annotation went: still there = %v, status %+v", still, s.Status())
	}
}
