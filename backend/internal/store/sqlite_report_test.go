package store

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestReport(t *testing.T) {
	ctx := context.Background()
	db, err := OpenSQLite(ctx, filepath.Join(t.TempDir(), "test.db"), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	api, err := db.CreateMonitor(ctx, Monitor{Name: "api", Type: MonitorTCP, Target: "a:1", Namespace: "shop", Interval: time.Minute, Timeout: time.Second, FailureThreshold: 1, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	quiet, err := db.CreateMonitor(ctx, Monitor{Name: "quiet", Type: MonitorTCP, Target: "q:1", Interval: time.Minute, Timeout: time.Second, FailureThreshold: 1, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}

	day := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	from, to := day, day.Add(24*time.Hour)
	for _, stmt := range []string{
		`INSERT INTO check_rollups (monitor_id, bucket_start, checks, up_checks, up_latency_sum_ms, up_latency_max_ms) VALUES
		 (1, ` + ms(day.Add(-time.Hour)) + `, 60, 60, 600, 20),` + // the hour before: not counted
			`(1, ` + ms(day) + `, 60, 50, 500, 20),` +
			`(1, ` + ms(day.Add(12*time.Hour)) + `, 60, 60, 1200, 40),` +
			`(1, ` + ms(to) + `, 60, 0, 0, 0)`, // the hour after: not counted
		`INSERT INTO incidents (monitor_id, started_at, resolved_at, cause, kind) VALUES
		 (1, ` + ms(day.Add(-30*time.Minute)) + `, ` + ms(day.Add(30*time.Minute)) + `, 'x', 'down'),` + // straddles the start: 30m inside
			`(1, ` + ms(day.Add(5*time.Hour)) + `, ` + ms(day.Add(6*time.Hour)) + `, 'x', 'down'),` + // 1h
			`(1, ` + ms(day.Add(23*time.Hour)) + `, NULL, 'x', 'down'),` + // open: 1h inside
			`(1, ` + ms(day.Add(2*time.Hour)) + `, ` + ms(day.Add(3*time.Hour)) + `, 'x', 'cert'),` + // not an outage
			`(2, ` + ms(to.Add(time.Hour)) + `, NULL, 'x', 'down')`, // after the period
	} {
		if _, err := db.write.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}

	got, err := db.Report(ctx, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("Report = %d monitors, want 2", len(got))
	}
	a := got[0]
	if a.ID != api.ID || a.Checks != 120 || a.UpChecks != 110 || a.AvgLatency != 15*time.Millisecond {
		t.Errorf("checks = %+v", a)
	}
	if a.Outages != 3 || a.Downtime != 150*time.Minute || a.LongestOutage != time.Hour || !a.OpenOutage {
		t.Errorf("outages = %d downtime=%s longest=%s open=%v", a.Outages, a.Downtime, a.LongestOutage, a.OpenOutage)
	}
	if q := got[1]; q.ID != quiet.ID || q.Checks != 0 || q.Outages != 0 || q.OpenOutage {
		t.Errorf("quiet = %+v", q)
	}
}

func ms(t time.Time) string { return strconv.FormatInt(t.UnixMilli(), 10) }
