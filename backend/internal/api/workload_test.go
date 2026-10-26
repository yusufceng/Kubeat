package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/k8s"
	"github.com/yusufceng/Kubeat/backend/internal/prom"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

type fakeWorkloads struct {
	workload  k8s.Workload
	err       error
	eventsErr error
	asked     []string
	// events is how many events to answer with; one when zero.
	events int
}

func (f *fakeWorkloads) Workload(_ context.Context, namespace, source string) (k8s.Workload, error) {
	f.asked = append(f.asked, namespace+" "+source)
	return f.workload, f.err
}

func (f *fakeWorkloads) Events(_ context.Context, _ string, w k8s.Workload, _, _ time.Time, limit int) ([]k8s.Event, error) {
	if f.eventsErr != nil {
		return nil, f.eventsErr
	}
	var out []k8s.Event
	for i := range max(f.events, 1) {
		out = append(out, k8s.Event{At: time.Now().Add(-time.Duration(i) * time.Minute), Type: "Warning", Reason: "BackOff", Object: "Pod/" + w.Pods[0].Name, Message: "restarting", Count: 3})
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

type fakePodMetrics struct {
	usage map[string]prom.Usage
	err   error
	asked []string
}

func (f *fakePodMetrics) PodUsage(_ context.Context, namespace string, pods []string) (map[string]prom.Usage, error) {
	f.asked = append(f.asked, fmt.Sprint(namespace, pods))
	return f.usage, f.err
}

// newWorkloadAPI serves the API with one discovered and one manual monitor.
func newWorkloadAPI(t *testing.T, workloads Workloads, podMetrics PodMetrics) (*httptest.Server, *store.SQLite) {
	t.Helper()
	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, filepath.Join(t.TempDir(), "test.db"), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	base := store.Monitor{Type: store.MonitorTCP, Target: "x:1", Interval: 30 * time.Second, Timeout: 5 * time.Second, FailureThreshold: 3, Enabled: true}
	discovered, manual := base, base
	discovered.Name, discovered.Namespace, discovered.Source = "shop/service/api:80", "shop", "service/api:80"
	manual.Name = "manual"
	for _, m := range []store.Monitor{discovered, manual} {
		if _, err := db.CreateMonitor(ctx, m); err != nil {
			t.Fatal(err)
		}
	}

	srv := httptest.NewServer(NewHandler(Options{
		Store: db, Token: testToken, Workloads: workloads, PodMetrics: podMetrics, DiscoveryStatus: "no cluster", RawRetention: 48 * time.Hour, HistoryRetention: 365 * 24 * time.Hour, EventLimit: 2,
		MaxResults: 100, License: fullLicense{}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}))
	t.Cleanup(func() {
		srv.Close()
		db.Close()
	})
	return srv, db
}

func TestWorkloadEndpoint(t *testing.T) {
	started := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	workloads := &fakeWorkloads{workload: k8s.Workload{Service: "api", Pods: []k8s.Pod{
		{Name: "api-crash", Containers: 1, Restarts: 17, Status: "CrashLoopBackOff", Node: "n1", StartedAt: started},
		{Name: "api-a", Ready: true, ReadyContainers: 1, Containers: 1, Status: "Running", Node: "n1", StartedAt: started},
		{Name: "api-new", Containers: 1, Status: "Pending"},
	}}}
	cpu, mem := 0.25, 104857600.0
	podMetrics := &fakePodMetrics{usage: map[string]prom.Usage{"api-a": {CPUCores: &cpu, MemoryBytes: &mem}}}
	srv, _ := newWorkloadAPI(t, workloads, podMetrics)

	var got workloadResponse
	if code := do(t, "GET", srv.URL+"/api/v1/monitors/1/workload", "", &got); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if !got.Available || got.Service != "api" || got.Ready != 1 || got.Total != 3 || !got.Metrics.Available {
		t.Fatalf("summary = %+v", got)
	}
	if workloads.asked[0] != "shop service/api:80" || podMetrics.asked[0] != "shop[api-crash api-a api-new]" {
		t.Errorf("asked: workloads %v, metrics %v", workloads.asked, podMetrics.asked)
	}
	crash, a, pending := got.Pods[0], got.Pods[1], got.Pods[2]
	if crash.Status != "CrashLoopBackOff" || crash.Restarts != 17 || crash.Ready || crash.CPUCores != nil {
		t.Errorf("crashing pod = %+v", crash)
	}
	if !a.Ready || a.CPUCores == nil || *a.CPUCores != 0.25 || *a.MemoryBytes != 104857600 || a.StartedAt == nil || !a.StartedAt.Equal(started) {
		t.Errorf("healthy pod = %+v", a)
	}
	if pending.StartedAt != nil || pending.Node != "" {
		t.Errorf("pending pod = %+v", pending)
	}

	if len(got.Events) != 1 || got.Events[0].Reason != "BackOff" || got.Events[0].Count != 3 || got.EventsReason != "" {
		t.Errorf("events = %+v (%s)", got.Events, got.EventsReason)
	}

	// The metrics server failing leaves the pod list intact; so does the
	// event list being out of reach.
	podMetrics.err = errors.New("connection refused")
	workloads.eventsErr = errors.New(`events is forbidden`)
	got = workloadResponse{}
	do(t, "GET", srv.URL+"/api/v1/monitors/1/workload", "", &got)
	if !got.Available || got.Total != 3 || got.Metrics.Available || got.Metrics.Reason == "" || got.Pods[1].CPUCores != nil {
		t.Errorf("with failing metrics: available=%v total=%d metrics=%+v", got.Available, got.Total, got.Metrics)
	}
	if len(got.Events) != 0 || got.EventsReason == "" {
		t.Errorf("with failing events = %+v (%s)", got.Events, got.EventsReason)
	}
}

func TestWorkloadUnavailableStates(t *testing.T) {
	// A monitor entered by hand has no pods to show.
	workloads := &fakeWorkloads{}
	srv, _ := newWorkloadAPI(t, workloads, nil)
	var got workloadResponse
	if code := do(t, "GET", srv.URL+"/api/v1/monitors/2/workload", "", &got); code != http.StatusOK || got.Available || got.Reason == "" || got.Pods == nil {
		t.Errorf("manual monitor: status=%d %+v", code, got)
	}
	if len(workloads.asked) != 0 {
		t.Error("the cluster was queried for a manual monitor")
	}

	// No metrics server: pods without usage, and a reason.
	workloads.workload = k8s.Workload{Service: "api", Pods: []k8s.Pod{{Name: "api-a", Ready: true}}}
	got = workloadResponse{}
	do(t, "GET", srv.URL+"/api/v1/monitors/1/workload", "", &got)
	if !got.Available || got.Ready != 1 || got.Metrics.Available || got.Metrics.Reason == "" {
		t.Errorf("without a metrics server: %+v", got)
	}

	// The source disappeared from the cluster.
	workloads.err = fmt.Errorf("%w: service shop/api does not exist", k8s.ErrNoWorkload)
	got = workloadResponse{}
	if code := do(t, "GET", srv.URL+"/api/v1/monitors/1/workload", "", &got); code != http.StatusOK || got.Available || got.Reason == "" {
		t.Errorf("vanished service: status=%d %+v", code, got)
	}

	// The cluster API itself failing is an error, not an empty answer.
	workloads.err = errors.New("pods is forbidden")
	if code := do(t, "GET", srv.URL+"/api/v1/monitors/1/workload", "", nil); code != http.StatusBadGateway {
		t.Errorf("cluster error: status = %d, want 502", code)
	}
	if code := do(t, "GET", srv.URL+"/api/v1/monitors/999/workload", "", nil); code != http.StatusNotFound {
		t.Errorf("unknown monitor: status = %d, want 404", code)
	}

	// No cluster connection at all.
	offline, _ := newWorkloadAPI(t, nil, nil)
	got = workloadResponse{}
	if do(t, "GET", offline.URL+"/api/v1/monitors/1/workload", "", &got); got.Available || got.Reason == "" {
		t.Errorf("no cluster: %+v", got)
	}
}

// A workload monitor entered by hand has no source; its pods are looked up
// through the workload its target names, in that workload's namespace.
func TestWorkloadEndpointOfAWorkloadMonitor(t *testing.T) {
	workloads := &fakeWorkloads{workload: k8s.Workload{Pods: []k8s.Pod{{Name: "web-1", Ready: true, Containers: 1, ReadyContainers: 1, Status: "Running"}}}}
	metrics := &fakePodMetrics{}
	srv, _ := newWorkloadAPI(t, workloads, metrics)

	var m monitorResponse
	body := `{"name":"web-pods","type":"workload","target":"shop-demo/deployment/web","namespace":"Mağaza","interval_seconds":30,"timeout_seconds":5,"failure_threshold":3}`
	if code := do(t, "POST", srv.URL+"/api/v1/monitors", body, &m); code != http.StatusCreated {
		t.Fatalf("create: status = %d", code)
	}
	var resp workloadResponse
	if code := do(t, "GET", srv.URL+"/api/v1/monitors/"+itoa(m.ID)+"/workload", "", &resp); code != http.StatusOK {
		t.Fatalf("workload: status = %d", code)
	}
	if !resp.Available || resp.Total != 1 || resp.Ready != 1 || resp.Service != "" {
		t.Errorf("response = %+v", resp)
	}
	// Asked in the workload's namespace, not in the monitor's group.
	if len(workloads.asked) != 1 || workloads.asked[0] != "shop-demo deployment/web" {
		t.Errorf("cluster was asked for %v", workloads.asked)
	}
	if len(metrics.asked) != 1 || metrics.asked[0] != "shop-demo[web-1]" {
		t.Errorf("metrics were asked for %v", metrics.asked)
	}
}
