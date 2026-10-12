package store

import (
	"context"
	"testing"
	"time"
)

func TestRollupAndSeries(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	api, _ := s.CreateMonitor(ctx, newMonitor("api"))
	web, _ := s.CreateMonitor(ctx, newMonitor("web"))

	h0 := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)
	at := func(d time.Duration, up bool, latency time.Duration) CheckResult {
		r := CheckResult{MonitorID: api.ID, CheckedAt: h0.Add(d), Up: up, Latency: latency}
		if !up {
			r.Error = "timeout"
		}
		return r
	}
	err := s.InsertCheckResults(ctx, []CheckResult{
		// 10:00 hour: three up (100, 200, 300 ms), one failed after 5 s.
		at(1*time.Minute, true, 100*time.Millisecond),
		at(20*time.Minute, true, 200*time.Millisecond),
		at(40*time.Minute, true, 300*time.Millisecond),
		at(59*time.Minute, false, 5*time.Second),
		// 11:00 hour: only failures.
		at(61*time.Minute, false, 5*time.Second),
		at(90*time.Minute, false, 5*time.Second),
		// 13:00 hour (12:00 is empty): one up.
		at(3*time.Hour+5*time.Minute, true, 50*time.Millisecond),
		{MonitorID: web.ID, CheckedAt: h0.Add(time.Minute), Up: true, Latency: 900 * time.Millisecond},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.RollupCheckResults(ctx, h0.Add(time.Minute), h0.Add(4*time.Hour)); err == nil {
		t.Error("a start off the hour boundary was accepted")
	}
	if err := s.RollupCheckResults(ctx, h0, h0.Add(4*time.Hour)); err != nil {
		t.Fatalf("rollup: %v", err)
	}

	hourly, err := s.Series(ctx, api.ID, SeriesRollup, h0, h0.Add(4*time.Hour), time.Hour)
	if err != nil {
		t.Fatalf("series: %v", err)
	}
	want := []SeriesPoint{
		// Latency ignores the failed check: avg of 100, 200, 300.
		{Start: h0, Checks: 4, UpChecks: 3, AvgLatency: 200 * time.Millisecond, MaxLatency: 300 * time.Millisecond},
		{Start: h0.Add(time.Hour), Checks: 2, UpChecks: 0},
		{Start: h0.Add(3 * time.Hour), Checks: 1, UpChecks: 1, AvgLatency: 50 * time.Millisecond, MaxLatency: 50 * time.Millisecond},
	}
	if len(hourly) != len(want) {
		t.Fatalf("hourly = %+v, want %+v", hourly, want)
	}
	for i := range want {
		if hourly[i] != want[i] {
			t.Errorf("hourly[%d] = %+v, want %+v", i, hourly[i], want[i])
		}
	}

	// Raw and rollup sources agree on the same buckets.
	raw, err := s.Series(ctx, api.ID, SeriesRaw, h0, h0.Add(4*time.Hour), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != len(hourly) {
		t.Fatalf("raw = %+v, hourly = %+v", raw, hourly)
	}
	for i := range raw {
		if raw[i] != hourly[i] {
			t.Errorf("raw[%d] = %+v, rollup = %+v", i, raw[i], hourly[i])
		}
	}

	// Coarser buckets merge hours: 10:00-12:00 has 6 checks, 3 up.
	twoHourly, _ := s.Series(ctx, api.ID, SeriesRollup, h0, h0.Add(4*time.Hour), 2*time.Hour)
	if len(twoHourly) != 2 || twoHourly[0].Checks != 6 || twoHourly[0].UpChecks != 3 ||
		twoHourly[0].AvgLatency != 200*time.Millisecond || twoHourly[1].Checks != 1 {
		t.Errorf("two-hourly = %+v", twoHourly)
	}
	// Finer buckets from raw data.
	fine, _ := s.Series(ctx, api.ID, SeriesRaw, h0, h0.Add(time.Hour), 30*time.Minute)
	if len(fine) != 2 || fine[0].Checks != 2 || fine[1].Checks != 2 || fine[1].UpChecks != 1 || !fine[1].Start.Equal(h0.Add(30*time.Minute)) {
		t.Errorf("30-minute series = %+v", fine)
	}
	// The range is half-open and scoped to the monitor.
	if only, _ := s.Series(ctx, api.ID, SeriesRollup, h0.Add(time.Hour), h0.Add(3*time.Hour), time.Hour); len(only) != 1 {
		t.Errorf("11:00-13:00 = %+v, want only the 11:00 bucket", only)
	}
	if other, _ := s.Series(ctx, web.ID, SeriesRollup, h0, h0.Add(4*time.Hour), time.Hour); len(other) != 1 || other[0].Checks != 1 {
		t.Errorf("other monitor = %+v", other)
	}
	if _, err := s.Series(ctx, api.ID, SeriesRollup, h0, h0.Add(time.Hour), 30*time.Minute); err == nil {
		t.Error("sub-hour bucket on rollups was accepted")
	}

	// Rolling up again after more results arrive replaces, not doubles.
	if err := s.InsertCheckResults(ctx, []CheckResult{at(3*time.Hour+30*time.Minute, true, 150*time.Millisecond)}); err != nil {
		t.Fatal(err)
	}
	if err := s.RollupCheckResults(ctx, h0.Add(3*time.Hour), h0.Add(4*time.Hour)); err != nil {
		t.Fatal(err)
	}
	again, _ := s.Series(ctx, api.ID, SeriesRollup, h0, h0.Add(4*time.Hour), time.Hour)
	if len(again) != 3 || again[2].Checks != 2 || again[2].AvgLatency != 100*time.Millisecond || again[0] != want[0] {
		t.Errorf("after re-rollup = %+v", again)
	}

	// Summaries survive raw retention, and have their own.
	if _, err := s.DeleteCheckResultsBefore(ctx, h0.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if kept, _ := s.Series(ctx, api.ID, SeriesRollup, h0, h0.Add(4*time.Hour), time.Hour); len(kept) != 3 {
		t.Errorf("rollups after raw retention = %+v", kept)
	}
	n, err := s.DeleteRollupsBefore(ctx, h0.Add(time.Hour))
	if err != nil || n != 2 { // 10:00 of both monitors
		t.Errorf("DeleteRollupsBefore = %d, %v; want 2", n, err)
	}
	if err := s.DeleteMonitor(ctx, api.ID); err != nil {
		t.Fatal(err)
	}
	if left, _ := s.Series(ctx, api.ID, SeriesRollup, h0, h0.Add(4*time.Hour), time.Hour); len(left) != 0 {
		t.Errorf("rollups survived monitor delete: %+v", left)
	}
}
