package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/k8s"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

func TestWindowEndpoint(t *testing.T) {
	workloads := &fakeWorkloads{workload: k8s.Workload{Service: "api", Pods: []k8s.Pod{{Name: "api-7d9f-a", Ready: true}}}}
	srv, db := newWorkloadAPI(t, workloads, &fakePodMetrics{})

	ctx := context.Background()
	now := time.Now().UTC()
	base := now.Add(-10 * time.Minute).Truncate(time.Second)
	var results []store.CheckResult
	for i := range 30 { // five minutes of checks every 10 s; 10..19 fail
		r := store.CheckResult{MonitorID: 1, CheckedAt: base.Add(time.Duration(i) * 10 * time.Second), Up: true, Latency: 20 * time.Millisecond}
		if i >= 10 && i < 20 {
			r.Up, r.Error = false, "unexpected status 503"
		}
		results = append(results, r)
	}
	if err := db.InsertCheckResults(ctx, results); err != nil {
		t.Fatal(err)
	}
	inc, err := db.OpenIncident(ctx, store.Incident{MonitorID: 1, Kind: store.IncidentDown, StartedAt: base.Add(110 * time.Second), Cause: "unexpected status 503"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ResolveIncident(ctx, inc.ID, base.Add(200*time.Second)); err != nil {
		t.Fatal(err)
	}

	from, to := base.Add(90*time.Second), base.Add(210*time.Second)
	url := srv.URL + "/api/v1/monitors/1/window?from=" + from.Format(time.RFC3339) + "&to=" + to.Format(time.RFC3339)
	var got windowResponse
	if code := do(t, "GET", url, "", &got); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	// Checks 9..20 are in the window: twelve, ten failed.
	if got.Checks == nil || got.Checks.Total != 12 || got.Checks.Failed != 10 || got.Checks.AvgLatencyMS != 20 || got.Checks.MaxLatencyMS != 20 {
		t.Fatalf("checks = %+v", got.Checks)
	}
	if len(got.Failures) != 10 || got.FailuresTruncated || got.Failures[0].Error == nil || *got.Failures[0].Error != "unexpected status 503" {
		t.Errorf("failures = %d, truncated %v, first %+v", len(got.Failures), got.FailuresTruncated, got.Failures[0])
	}
	if len(got.Incidents) != 1 || got.Incidents[0].ID != inc.ID || got.Incidents[0].ResolvedAt == nil {
		t.Errorf("incidents = %+v", got.Incidents)
	}
	if len(got.Events) != 1 || got.Events[0].Object != "Pod/api-7d9f-a" || got.EventsReason != "" || got.EventsTruncated {
		t.Errorf("events = %+v, truncated %v, reason %q", got.Events, got.EventsTruncated, got.EventsReason)
	}
	if !got.From.Equal(from) || !got.To.Equal(to) {
		t.Errorf("window = %s – %s", got.From, got.To)
	}

	// More events than the limit: the newest ones, and a word that there were more.
	workloads.events = 3
	if code := do(t, "GET", url, "", &got); code != http.StatusOK || len(got.Events) != 2 || !got.EventsTruncated {
		t.Errorf("many events: status %d, %d events, truncated %v", code, len(got.Events), got.EventsTruncated)
	}
	workloads.events = 0

	// "to" defaults to now and is never in the future.
	if code := do(t, "GET", srv.URL+"/api/v1/monitors/1/window?from="+from.Format(time.RFC3339)+"&to="+now.Add(time.Hour).Format(time.RFC3339), "", &got); code != http.StatusOK || got.To.After(time.Now()) {
		t.Errorf("future to: status %d, to %s", code, got.To)
	}

	// A manual monitor has no cluster object to ask about.
	if code := do(t, "GET", srv.URL+"/api/v1/monitors/2/window?from="+from.Format(time.RFC3339), "", &got); code != http.StatusOK || len(got.Events) != 0 || got.EventsReason == "" {
		t.Errorf("manual monitor: status %d, events %+v, reason %q", code, got.Events, got.EventsReason)
	}

	// Bad input.
	for _, q := range []string{"", "?from=yesterday", "?from=" + to.Format(time.RFC3339) + "&to=" + from.Format(time.RFC3339)} {
		var body map[string]any
		if code := do(t, "GET", srv.URL+"/api/v1/monitors/1/window"+q, "", &body); code != http.StatusBadRequest {
			t.Errorf("%q: status = %d, want 400", q, code)
		}
	}
	if code := do(t, "GET", srv.URL+"/api/v1/monitors/99/window?from="+from.Format(time.RFC3339), "", &got); code != http.StatusNotFound {
		t.Errorf("unknown monitor: status = %d, want 404", code)
	}
}
