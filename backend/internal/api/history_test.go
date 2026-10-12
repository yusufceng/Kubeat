package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

func TestBucketFor(t *testing.T) {
	tests := []struct {
		span time.Duration
		want time.Duration
	}{
		{time.Hour, 30 * time.Second},          // 120 buckets exactly
		{24 * time.Hour, 15 * time.Minute},     // 12m needed, next round size
		{7 * 24 * time.Hour, 2 * time.Hour},    // 84m needed
		{30 * 24 * time.Hour, 6 * time.Hour},   // 6h exactly
		{365 * 24 * time.Hour, 96 * time.Hour}, // past the list: whole days
		{time.Minute, time.Second},
	}
	for _, tt := range tests {
		got := bucketFor(tt.span, 120)
		if got != tt.want {
			t.Errorf("bucketFor(%s) = %s, want %s", tt.span, got, tt.want)
		}
		if n := tt.span / got; n > 120 {
			t.Errorf("bucketFor(%s) = %s gives %d buckets, more than 120", tt.span, got, n)
		}
	}
}

// seedHistory stores results for monitor 1: recent ones and, 10 days back,
// older ones that exist only as hourly summaries.
func seedHistory(t *testing.T, db *store.SQLite, now time.Time) {
	t.Helper()
	ctx := context.Background()
	old := now.Add(-10 * 24 * time.Hour).Truncate(time.Hour)
	results := []store.CheckResult{
		// 10 days ago: 4 checks, 1 up.
		{MonitorID: 1, CheckedAt: old.Add(1 * time.Minute), Up: true, Latency: 400 * time.Millisecond},
		{MonitorID: 1, CheckedAt: old.Add(2 * time.Minute), Up: false, Latency: 5 * time.Second, Error: "timeout"},
		{MonitorID: 1, CheckedAt: old.Add(3 * time.Minute), Up: false, Latency: 5 * time.Second, Error: "timeout"},
		{MonitorID: 1, CheckedAt: old.Add(4 * time.Minute), Up: false, Latency: 5 * time.Second, Error: "timeout"},
		// Last hour: 4 checks, 3 up with 100/200/300 ms.
		{MonitorID: 1, CheckedAt: now.Add(-30 * time.Minute), Up: true, Latency: 100 * time.Millisecond},
		{MonitorID: 1, CheckedAt: now.Add(-20 * time.Minute), Up: true, Latency: 200 * time.Millisecond},
		{MonitorID: 1, CheckedAt: now.Add(-10 * time.Minute), Up: true, Latency: 300 * time.Millisecond},
		{MonitorID: 1, CheckedAt: now.Add(-5 * time.Minute), Up: false, Latency: 5 * time.Second, Error: "timeout"},
	}
	if err := db.InsertCheckResults(ctx, results); err != nil {
		t.Fatal(err)
	}
	if err := db.RollupCheckResults(ctx, old, now); err != nil {
		t.Fatal(err)
	}
	// Raw retention in the test API is 48h: drop what it would have pruned.
	if _, err := db.DeleteCheckResultsBefore(ctx, now.Add(-48*time.Hour)); err != nil {
		t.Fatal(err)
	}
}

func TestMonitorSeries(t *testing.T) {
	srv, db := newTestAPI(t)
	base := srv.URL + "/api/v1/monitors"
	do(t, "POST", base, validMonitor, nil)
	now := time.Now().UTC()
	seedHistory(t, db, now)

	// Short range: fine buckets from raw results.
	var hour seriesResponse
	if code := do(t, "GET", base+"/1/series?range=1h", "", &hour); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if hour.RangeSeconds != 3600 || hour.BucketSeconds != 30 || len(hour.Points) != 4 {
		t.Fatalf("1h series = range %d bucket %d points %d, want 3600, 30, 4", hour.RangeSeconds, hour.BucketSeconds, len(hour.Points))
	}
	first, last := hour.Points[0], hour.Points[3]
	if first.Checks != 1 || first.UpChecks != 1 || first.AvgLatencyMS == nil || *first.AvgLatencyMS != 100 || *first.MaxLatencyMS != 100 {
		t.Errorf("first point = %+v", first)
	}
	if last.Checks != 1 || last.UpChecks != 0 || last.AvgLatencyMS != nil || last.MaxLatencyMS != nil {
		t.Errorf("failed-only point must have null latency: %+v", last)
	}
	if !hour.From.Before(first.Start.Add(time.Second)) || !hour.To.After(last.Start) || hour.To.Sub(hour.From) < time.Hour {
		t.Errorf("from/to do not cover the points: %s .. %s", hour.From, hour.To)
	}

	// Long range: hourly summaries, including what raw retention removed.
	var month seriesResponse
	do(t, "GET", base+"/1/series?range=720h", "", &month)
	if month.BucketSeconds != 6*3600 {
		t.Errorf("30d bucket = %ds, want 6h", month.BucketSeconds)
	}
	checks, up := 0, 0
	for _, p := range month.Points {
		checks += p.Checks
		up += p.UpChecks
	}
	if checks != 8 || up != 4 {
		t.Errorf("30d totals = %d checks, %d up; want 8 and 4", checks, up)
	}
	if old := month.Points[0]; old.Checks != 4 || old.UpChecks != 1 || *old.AvgLatencyMS != 400 {
		t.Errorf("oldest bucket = %+v", old)
	}

	for query, want := range map[string]int{
		"":              http.StatusBadRequest,
		"range=abc":     http.StatusBadRequest,
		"range=-1h":     http.StatusBadRequest,
		"range=100000h": http.StatusBadRequest, // beyond history retention
		"range=24h":     http.StatusOK,
		"range=1h&x=1":  http.StatusOK,
	} {
		if code := do(t, "GET", base+"/1/series?"+query, "", nil); code != want {
			t.Errorf("?%s: status = %d, want %d", query, code, want)
		}
	}
	if code := do(t, "GET", base+"/999/series?range=1h", "", nil); code != http.StatusNotFound {
		t.Errorf("unknown monitor: status = %d, want 404", code)
	}

	var empty seriesResponse
	do(t, "POST", base, `{"name":"new","type":"tcp","target":"x:1","interval_seconds":30,"timeout_seconds":5}`, nil)
	if do(t, "GET", base+"/2/series?range=1h", "", &empty); empty.Points == nil || len(empty.Points) != 0 {
		t.Errorf("monitor without results: points = %v, want []", empty.Points)
	}
}

func TestMonitorUptime(t *testing.T) {
	srv, db := newTestAPI(t)
	base := srv.URL + "/api/v1/monitors"
	do(t, "POST", base, validMonitor, nil)
	seedHistory(t, db, time.Now().UTC())

	var got []uptimeWindow
	if code := do(t, "GET", base+"/1/uptime?windows=15m,24h,720h", "", &got); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if len(got) != 3 {
		t.Fatalf("windows = %+v", got)
	}
	// Last 15 minutes: the 300 ms success and the failure.
	if w := got[0]; w.WindowSeconds != 900 || w.Checks != 2 || w.UpChecks != 1 || *w.UptimePercent != 50 {
		t.Errorf("15m = %+v", w)
	}
	if w := got[1]; w.Checks != 4 || *w.UptimePercent != 75 {
		t.Errorf("24h = %+v", w)
	}
	// 30 days reaches the summarized results from 10 days ago: 4 of 8 up.
	if w := got[2]; w.WindowSeconds != 720*3600 || w.Checks != 8 || *w.UptimePercent != 50 {
		t.Errorf("30d = %+v", w)
	}

	do(t, "POST", base, `{"name":"new","type":"tcp","target":"x:1","interval_seconds":30,"timeout_seconds":5}`, nil)
	if do(t, "GET", base+"/2/uptime?windows=24h", "", &got); len(got) != 1 || got[0].Checks != 0 || got[0].UptimePercent != nil {
		t.Errorf("monitor without results = %+v, want null percent", got)
	}

	for query, want := range map[string]int{
		"":                http.StatusBadRequest,
		"windows=":        http.StatusBadRequest,
		"windows=24h,abc": http.StatusBadRequest,
		"windows=100000h": http.StatusBadRequest,
		"windows=1h,1h,1h,1h,1h,1h,1h,1h,1h,1h,1h": http.StatusBadRequest,
	} {
		if code := do(t, "GET", base+"/1/uptime?"+query, "", nil); code != want {
			t.Errorf("?%s: status = %d, want %d", query, code, want)
		}
	}
	if code := do(t, "GET", base+"/999/uptime?windows=24h", "", nil); code != http.StatusNotFound {
		t.Errorf("unknown monitor: status = %d, want 404", code)
	}
}
