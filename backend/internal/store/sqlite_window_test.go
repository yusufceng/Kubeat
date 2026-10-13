package store

import (
	"context"
	"testing"
	"time"
)

func TestWindowQueries(t *testing.T) {
	db := openTest(t)
	ctx := context.Background()
	m, err := db.CreateMonitor(ctx, Monitor{Name: "api", Type: MonitorHTTP, Target: "http://api", Interval: 10 * time.Second, Timeout: time.Second, FailureThreshold: 2, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	other, err := db.CreateMonitor(ctx, Monitor{Name: "other", Type: MonitorHTTP, Target: "http://other", Interval: 10 * time.Second, Timeout: time.Second, FailureThreshold: 2, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 10, 13, 11, 0, 0, 0, time.UTC)
	var results []CheckResult
	for i := range 12 { // 11:00:00 .. 11:01:50, every 10 s
		r := CheckResult{MonitorID: m.ID, CheckedAt: base.Add(time.Duration(i) * 10 * time.Second), Up: true, Latency: time.Duration(10+i) * time.Millisecond}
		if i >= 4 && i <= 7 { // four failures in the middle
			r.Up, r.Latency, r.Error = false, 2*time.Second, "context deadline exceeded"
		}
		results = append(results, r)
	}
	results = append(results, CheckResult{MonitorID: other.ID, CheckedAt: base.Add(50 * time.Second), Up: false, Error: "elsewhere"})
	if err := db.InsertCheckResults(ctx, results); err != nil {
		t.Fatal(err)
	}

	// The window [11:00:30, 11:01:20) holds checks 3..7: five, four failed.
	from, to := base.Add(30*time.Second), base.Add(80*time.Second)
	sum, err := db.SummarizeChecks(ctx, m.ID, from, to)
	if err != nil || sum.Total != 5 || sum.Failed != 4 || sum.AvgLatency != 13*time.Millisecond || sum.MaxLatency != 13*time.Millisecond {
		t.Fatalf("summary = %+v, %v", sum, err)
	}
	failed, err := db.ListFailedChecks(ctx, m.ID, from, to, 3)
	if err != nil || len(failed) != 3 || !failed[0].CheckedAt.Equal(base.Add(70*time.Second)) || failed[0].Error != "context deadline exceeded" {
		t.Fatalf("failed checks = %+v, %v", failed, err)
	}
	// A window with no successful check has no latency to report.
	if sum, _ := db.SummarizeChecks(ctx, m.ID, base.Add(40*time.Second), base.Add(80*time.Second)); sum.Total != 4 || sum.Failed != 4 || sum.AvgLatency != 0 {
		t.Errorf("all-failed summary = %+v", sum)
	}
	if sum, _ := db.SummarizeChecks(ctx, m.ID, base.Add(time.Hour), base.Add(2*time.Hour)); sum.Total != 0 {
		t.Errorf("empty summary = %+v", sum)
	}

	// Incidents: one before the window, one overlapping its start, one
	// inside, one still open from inside it, one after.
	open := func(start time.Time, end *time.Time) {
		i, err := db.OpenIncident(ctx, Incident{MonitorID: m.ID, Kind: IncidentDown, StartedAt: start, Cause: "x"})
		if err != nil {
			t.Fatal(err)
		}
		if end != nil {
			if err := db.ResolveIncident(ctx, i.ID, *end); err != nil {
				t.Fatal(err)
			}
		}
	}
	at := func(s int) *time.Time { v := base.Add(time.Duration(s) * time.Second); return &v }
	open(base.Add(-time.Minute), at(10))   // ended before
	open(base.Add(20*time.Second), at(40)) // overlaps the start
	open(base.Add(50*time.Second), at(60)) // inside
	open(base.Add(70*time.Second), at(85)) // overlaps the end
	open(base.Add(90*time.Second), nil)    // after, still open
	if _, err := db.OpenIncident(ctx, Incident{MonitorID: other.ID, Kind: IncidentDown, StartedAt: base.Add(55 * time.Second), Cause: "y"}); err != nil {
		t.Fatal(err)
	}
	got, err := db.ListIncidentsBetween(ctx, m.ID, from, to, 10)
	if err != nil {
		t.Fatal(err)
	}
	var starts []int
	for _, i := range got {
		starts = append(starts, int(i.StartedAt.Sub(base)/time.Second))
	}
	if len(starts) != 3 || starts[0] != 70 || starts[1] != 50 || starts[2] != 20 {
		t.Errorf("incidents between (seconds from base, newest first) = %v, want [70 50 20]", starts)
	}
	if got, _ := db.ListIncidentsBetween(ctx, m.ID, from, to, 1); len(got) != 1 || got[0].StartedAt.Sub(base) != 70*time.Second {
		t.Errorf("limited to one = %+v", got)
	}
}
