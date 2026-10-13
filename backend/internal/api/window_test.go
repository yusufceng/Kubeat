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

func TestGridEndpoint(t *testing.T) {
	srv, db := newWorkloadAPI(t, &fakeWorkloads{}, &fakePodMetrics{})
	ctx := context.Background()
	// Checks every 30 s over the last 20 minutes; the middle five minutes
	// fail with one error, one later check with another.
	base := time.Now().UTC().Add(-20 * time.Minute).Truncate(5 * time.Minute)
	var results []store.CheckResult
	for i := range 30 { // 15 minutes of checks
		at := base.Add(time.Duration(i) * 30 * time.Second)
		r := store.CheckResult{MonitorID: 1, CheckedAt: at, Up: true, Latency: 12 * time.Millisecond}
		switch {
		case i >= 10 && i < 20: // the second five-minute bucket
			r.Up, r.Error = false, "the workload is scaled to zero"
		case i == 25:
			r.Up, r.Error = false, "timeout"
		}
		results = append(results, r)
	}
	if err := db.InsertCheckResults(ctx, results); err != nil {
		t.Fatal(err)
	}

	var got gridResponse
	if code := do(t, "GET", srv.URL+"/api/v1/monitors/1/grid?range=24h", "", &got); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	// The monitor's interval is 30 s, so the day view has five-minute cells.
	if got.BucketSeconds != 300 || got.RangeSeconds != 86400 || len(got.Cells) != 3 {
		t.Fatalf("grid = bucket %d, range %d, %d cells: %+v", got.BucketSeconds, got.RangeSeconds, len(got.Cells), got.Cells)
	}
	first, second, third := got.Cells[0], got.Cells[1], got.Cells[2]
	if first.Checks != 10 || first.Failed != 0 || first.AvgLatencyMS == nil || *first.AvgLatencyMS != 12 || first.Error != "" {
		t.Errorf("healthy cell = %+v", first)
	}
	if second.Checks != 10 || second.Failed != 10 || second.AvgLatencyMS != nil || second.Error != "the workload is scaled to zero" || !second.Start.Equal(base.Add(5*time.Minute)) {
		t.Errorf("failed cell = %+v", second)
	}
	if third.Checks != 10 || third.Failed != 1 || third.Error != "timeout" {
		t.Errorf("partial cell = %+v", third)
	}
	if len(got.Errors) != 2 || got.Errors[0].Error != "the workload is scaled to zero" || got.Errors[0].Count != 10 || got.Errors[1].Count != 1 ||
		!got.Errors[0].FirstAt.Equal(base.Add(5*time.Minute)) || !got.Errors[0].LastAt.Equal(base.Add(9*time.Minute+30*time.Second)) {
		t.Errorf("errors = %+v", got.Errors)
	}

	// Beyond a day the cells are hours, and the range is cut to what the
	// raw results cover (48h here).
	if code := do(t, "GET", srv.URL+"/api/v1/monitors/1/grid?range=168h", "", &got); code != http.StatusOK || got.BucketSeconds != 3600 || got.RangeSeconds != 48*3600 {
		t.Errorf("a week: status %d, bucket %d, range %d", code, got.BucketSeconds, got.RangeSeconds)
	}
	var problem map[string]any
	if code := do(t, "GET", srv.URL+"/api/v1/monitors/1/grid", "", &problem); code != http.StatusBadRequest {
		t.Errorf("without a range: status = %d, want 400", code)
	}
	if code := do(t, "GET", srv.URL+"/api/v1/monitors/99/grid?range=24h", "", &problem); code != http.StatusNotFound {
		t.Errorf("unknown monitor: status = %d, want 404", code)
	}
}

func TestGridBucketFollowsTheInterval(t *testing.T) {
	for _, c := range []struct{ span, interval, want time.Duration }{
		{24 * time.Hour, 30 * time.Second, 5 * time.Minute},
		{24 * time.Hour, 5 * time.Minute, 5 * time.Minute},
		{24 * time.Hour, 6 * time.Minute, 10 * time.Minute},
		{24 * time.Hour, 25 * time.Minute, 30 * time.Minute},
		{24 * time.Hour, 2 * time.Hour, time.Hour},
		{7 * 24 * time.Hour, 30 * time.Second, time.Hour},
	} {
		if got := gridBucket(c.span, c.interval); got != c.want {
			t.Errorf("gridBucket(%s, %s) = %s, want %s", c.span, c.interval, got, c.want)
		}
	}
}
