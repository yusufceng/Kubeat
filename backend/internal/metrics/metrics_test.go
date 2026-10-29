package metrics

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestNilMetricsRecordNothing(t *testing.T) {
	var m *Metrics
	// None of these may panic.
	m.CheckStarted()
	m.CheckDone("http", true, time.Second)
	m.SchedulerTick(time.Now())
	m.DispatchSkipped(3)
	m.Results(5, 1)
	m.Notification("slack", "down", true)
	m.NotificationAttempt("slack", false)
	m.NotificationDropped()
	m.Heartbeat(HeartbeatSent, time.Now())
	m.TargetThrottled("example.com")
	m.Backup(true, time.Now())
	m.Backup(false, time.Now())
	m.LastBackup(time.Now())
	m.License(time.Now())
	m.WatchHealth(func() error { return nil })
}

// kubeat_self_healthy is read from the judgement at every scrape: it follows
// a change at once, without anything recording it, and has no labels.
func TestSelfHealthyIsReadAtScrape(t *testing.T) {
	m := New("test")
	var problem error
	m.WatchHealth(func() error { return problem })

	for _, tt := range []struct {
		problem error
		want    float64
	}{
		{nil, 1},
		{errors.New("check results came in the last 1m0s and none of them could be stored"), 0},
		{nil, 1},
	} {
		problem = tt.problem
		got, err := m.Gather()
		if err != nil {
			t.Fatal(err)
		}
		if value, ok := got["kubeat_self_healthy"]; !ok || value != tt.want {
			t.Errorf("with problem %v: kubeat_self_healthy = %v (present: %v), want %v", tt.problem, value, ok, tt.want)
		}
	}
}

// The labelled counters the alert rules read with increase() have their
// series from the start, at 0: a series that first appears holding 1 is no
// increase to increase(), so the first failure would go unseen.
func TestAlertedCountersStartAtZero(t *testing.T) {
	got, err := New("test").Gather()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"kubeat_backups_total{result=ok}":     true,
		"kubeat_backups_total{result=failed}": true,
	}
	for _, channelType := range store.ChannelTypes {
		for _, result := range []string{"sent", "failed"} {
			want[fmt.Sprintf("kubeat_notification_attempts_total{channel_type=%s}{result=%s}", channelType, result)] = true
		}
	}
	for key := range want {
		if value, ok := got[key]; !ok || value != 0 {
			t.Errorf("%s = %v (present: %v), want 0", key, value, ok)
		}
	}
	for key := range got {
		if (strings.HasPrefix(key, "kubeat_notification_attempts_total") || strings.HasPrefix(key, "kubeat_backups_total")) && !want[key] {
			t.Errorf("unexpected series %s", key)
		}
	}
}

// A failed backup stays the newest backup event until one succeeds, which is
// what KubeatBackupFailing compares; a copy found at start is no attempt and
// leaves the failure time alone.
func TestBackupFailureTime(t *testing.T) {
	m := New("test")
	start := time.Unix(1_800_000_000, 0)
	m.LastBackup(start.Add(-24 * time.Hour))
	m.Backup(false, start)
	m.Backup(false, start.Add(time.Minute))

	got, err := m.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if got["kubeat_backup_last_failure_timestamp_seconds"] != 1_800_000_060 ||
		got["kubeat_backup_last_success_timestamp_seconds"] != 1_800_000_000-86400 ||
		got["kubeat_backups_total{result=failed}"] != 2 || got["kubeat_backups_total{result=ok}"] != 0 {
		t.Errorf("after two failures: failure = %v, success = %v, failed = %v, ok = %v",
			got["kubeat_backup_last_failure_timestamp_seconds"], got["kubeat_backup_last_success_timestamp_seconds"],
			got["kubeat_backups_total{result=failed}"], got["kubeat_backups_total{result=ok}"])
	}

	m.Backup(true, start.Add(2*time.Minute))
	got, err = m.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if got["kubeat_backup_last_success_timestamp_seconds"] <= got["kubeat_backup_last_failure_timestamp_seconds"] {
		t.Errorf("after a success: success = %v is not newer than failure = %v",
			got["kubeat_backup_last_success_timestamp_seconds"], got["kubeat_backup_last_failure_timestamp_seconds"])
	}
}

// The host label is bounded: past MaxThrottledHosts hosts, new ones are
// counted together as "other", while the hosts already labelled keep
// their label.
func TestTargetThrottledBoundsHosts(t *testing.T) {
	m := New("test")
	for i := 0; i < MaxThrottledHosts; i++ {
		m.TargetThrottled(fmt.Sprintf("host-%d.example", i))
	}
	m.TargetThrottled("host-0.example")
	m.TargetThrottled("late.example")
	m.TargetThrottled("later.example")

	got, err := m.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if got["kubeat_target_throttled_total{host=host-0.example}"] != 2 {
		t.Errorf("host-0 = %v, want 2", got["kubeat_target_throttled_total{host=host-0.example}"])
	}
	if got["kubeat_target_throttled_total{host=other}"] != 2 {
		t.Errorf("other = %v, want 2", got["kubeat_target_throttled_total{host=other}"])
	}
	if _, ok := got["kubeat_target_throttled_total{host=late.example}"]; ok {
		t.Error("a host past the bound got a label of its own")
	}
}

func TestEventCounters(t *testing.T) {
	m := New("1.2.3")
	now := time.Unix(1_800_000_000, 500_000_000)

	m.CheckStarted()
	m.CheckStarted()
	m.CheckDone("http", true, 120*time.Millisecond)
	m.CheckDone("tcp", false, time.Second)
	m.CheckStarted()
	m.SchedulerTick(now)
	m.DispatchSkipped(4)
	m.DispatchSkipped(0)
	m.Results(10, 2)
	m.Notification("slack", "down", true)
	m.Notification("email", "down", false)
	m.NotificationDropped()
	m.Heartbeat(HeartbeatWithheld, now)
	m.Heartbeat(HeartbeatSent, now)
	m.LastBackup(now)

	got, err := m.Gather()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]float64{
		"kubeat_backup_last_success_timestamp_seconds":                              1_800_000_000.5,
		"kubeat_build_info{version=1.2.3}":                                          1,
		"kubeat_checks_total{result=up}{type=http}":                                 1,
		"kubeat_checks_total{result=down}{type=tcp}":                                1,
		"kubeat_check_duration_seconds{type=http}_count":                            1,
		"kubeat_checks_in_flight":                                                   1,
		"kubeat_scheduler_last_tick_timestamp_seconds":                              1_800_000_000.5,
		"kubeat_scheduler_dispatch_skipped_total":                                   4,
		"kubeat_results_written_total":                                              10,
		"kubeat_results_dropped_total":                                              2,
		"kubeat_notifications_total{channel_type=slack}{event=down}{result=sent}":   1,
		"kubeat_notifications_total{channel_type=email}{event=down}{result=failed}": 1,
		"kubeat_notifications_dropped_total":                                        1,
		"kubeat_heartbeats_total{result=withheld}":                                  1,
		"kubeat_heartbeats_total{result=sent}":                                      1,
		"kubeat_heartbeat_last_success_timestamp_seconds":                           1_800_000_000.5,
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("%s = %v, want %v", key, got[key], value)
		}
	}
	// A backup found at start is not a backup attempt, nor a failed one.
	for key := range got {
		if strings.HasPrefix(key, "kubeat_backups_total") && got[key] != 0 {
			t.Errorf("LastBackup counted an attempt: %s = %v", key, got[key])
		}
	}
	if got["kubeat_backup_last_failure_timestamp_seconds"] != 0 {
		t.Errorf("LastBackup recorded a failure at %v", got["kubeat_backup_last_failure_timestamp_seconds"])
	}
	if _, ok := got["go_goroutines"]; !ok {
		t.Error("Go runtime metrics are missing")
	}
}

func TestPerMonitorMetrics(t *testing.T) {
	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, filepath.Join(t.TempDir(), "test.db"), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	create := func(name, ns string, typ store.MonitorType, enabled bool) store.Monitor {
		m, err := db.CreateMonitor(ctx, store.Monitor{Name: name, Namespace: ns, Type: typ, Target: "x:1",
			Interval: 30 * time.Second, Timeout: 5 * time.Second, FailureThreshold: 3, Enabled: enabled})
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	api := create("shop/api", "shop", store.MonitorHTTP, true)
	db2 := create("db", "", store.MonitorTCP, true)
	create("fresh", "", store.MonitorTCP, true)
	create("paused", "", store.MonitorTCP, false)

	checked := time.Unix(1_800_000_000, 0)
	expiry := checked.Add(30 * 24 * time.Hour)
	err = db.InsertCheckResults(ctx, []store.CheckResult{
		{MonitorID: api.ID, CheckedAt: checked, Up: true, Latency: 250 * time.Millisecond, StatusCode: 200, CertExpiresAt: expiry},
		{MonitorID: db2.ID, CheckedAt: checked, Up: false, Latency: 2 * time.Second, Error: "refused"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.OpenIncident(ctx, store.Incident{MonitorID: db2.ID, StartedAt: checked, Cause: "refused"}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetMonitorSlowSince(ctx, api.ID, checked); err != nil {
		t.Fatal(err)
	}

	m := New("test")
	m.WatchMonitors(db, 5*time.Second, discard)
	got, err := m.Gather()
	if err != nil {
		t.Fatal(err)
	}
	apiLabels := "{monitor=shop/api}{monitor_namespace=shop}{type=http}"
	dbLabels := "{monitor=db}{monitor_namespace=}{type=tcp}"
	want := map[string]float64{
		"kubeat_monitor_scrape_success":                                             1,
		"kubeat_monitors{state=enabled}":                                            3,
		"kubeat_monitors{state=paused}":                                             1,
		"kubeat_monitor_up" + apiLabels:                                             1,
		"kubeat_monitor_response_seconds" + apiLabels:                               0.25,
		"kubeat_monitor_last_check_timestamp_seconds" + apiLabels:                   1_800_000_000,
		"kubeat_monitor_cert_expiry_timestamp_seconds" + apiLabels:                  float64(expiry.Unix()),
		"kubeat_monitor_incident_open" + apiLabels:                                  0,
		"kubeat_monitor_slow" + apiLabels:                                           1,
		"kubeat_monitor_up" + dbLabels:                                              0,
		"kubeat_monitor_incident_open" + dbLabels:                                   1,
		"kubeat_monitor_slow" + dbLabels:                                            0,
		"kubeat_monitor_incident_open{monitor=fresh}{monitor_namespace=}{type=tcp}": 0,
	}
	for key, value := range want {
		v, ok := got[key]
		if !ok || v != value {
			t.Errorf("%s = %v (present: %v), want %v", key, v, ok, value)
		}
	}
	// No "up" for monitors without a result or that are paused, and no
	// certificate metric where no certificate was seen.
	for key := range got {
		if strings.HasPrefix(key, "kubeat_monitor_up") && (strings.Contains(key, "fresh") || strings.Contains(key, "paused")) {
			t.Errorf("unexpected series %s", key)
		}
		if strings.HasPrefix(key, "kubeat_monitor_cert_expiry") && !strings.Contains(key, "shop/api") {
			t.Errorf("unexpected series %s", key)
		}
	}

	// The HTTP handler serves the same data in the exposition format.
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, `kubeat_monitor_up{monitor="shop/api",monitor_namespace="shop",type="http"} 1`) ||
		!strings.Contains(body, "# HELP kubeat_checks_in_flight") {
		t.Errorf("handler: status %d, body lacks expected lines", rec.Code)
	}
}

type failingStore struct{}

func (failingStore) ListMonitorStatuses(context.Context, time.Time) ([]store.MonitorStatus, error) {
	return nil, errors.New("database is locked")
}

func TestScrapeSurvivesStoreFailure(t *testing.T) {
	m := New("test")
	m.WatchMonitors(failingStore{}, time.Second, discard)

	got, err := m.Gather()
	if err != nil {
		t.Fatalf("a store failure broke the whole scrape: %v", err)
	}
	if got["kubeat_monitor_scrape_success"] != 0 {
		t.Errorf("kubeat_monitor_scrape_success = %v, want 0", got["kubeat_monitor_scrape_success"])
	}
	if _, ok := got["kubeat_build_info{version=test}"]; !ok {
		t.Error("other metrics are missing after a store failure")
	}
}
