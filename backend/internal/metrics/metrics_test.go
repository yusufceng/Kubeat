package metrics

import (
	"context"
	"errors"
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
	m.NotificationDropped()
	m.Heartbeat(HeartbeatSent, time.Now())
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

	got, err := m.Gather()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]float64{
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
		"kubeat_monitor_up" + dbLabels:                                              0,
		"kubeat_monitor_incident_open" + dbLabels:                                   1,
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
