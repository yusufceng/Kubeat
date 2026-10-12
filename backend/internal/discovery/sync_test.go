package discovery

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
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
		serviceCandidate("ok", 80, "", monitored(map[string]string{AnnotationMonitor: " True "})),
	}}
	s, db := newSyncer(t, cluster)
	if err := s.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := s.Status()
	if st.Created != 1 || len(st.Skipped) != 6 {
		t.Errorf("status = %+v", st)
	}
	if got := names(t, db); len(got) != 1 || got["shop/service/ok:80"].Name == "" {
		t.Errorf("monitors = %v", got)
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
