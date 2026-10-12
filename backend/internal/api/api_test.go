package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// newTestAPI serves the handler over a real SQLite store.
func newTestAPI(t *testing.T) (*httptest.Server, *store.SQLite) {
	t.Helper()
	db, err := store.OpenSQLite(context.Background(), filepath.Join(t.TempDir(), "test.db"), 5*time.Second)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	srv := httptest.NewServer(NewHandler(Options{
		Version:          "test",
		Store:            db,
		MinCheckInterval: 5 * time.Second,
		StatsWindow:      24 * time.Hour,
		MaxResults:       3,
		Logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
	}))
	t.Cleanup(func() {
		srv.Close()
		db.Close()
	})
	return srv, db
}

// do sends a request and decodes the JSON response into out when non-nil.
func do(t *testing.T, method, url, body string, out any) int {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("%s %s: decode: %v", method, url, err)
		}
	}
	return resp.StatusCode
}

const validMonitor = `{"name":"api","type":"http","target":"https://example.com/healthz","interval_seconds":30,"timeout_seconds":5}`

func TestHealth(t *testing.T) {
	srv, _ := newTestAPI(t)

	var got Health
	if code := do(t, "GET", srv.URL+"/api/v1/health", "", &got); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if got.Status != "ok" || got.Version != "test" || got.Time.IsZero() {
		t.Errorf("unexpected body: %+v", got)
	}
	if code := do(t, "POST", srv.URL+"/api/v1/health", "", nil); code != http.StatusMethodNotAllowed {
		t.Errorf("POST status = %d, want 405", code)
	}
}

func TestMonitorLifecycle(t *testing.T) {
	srv, db := newTestAPI(t)
	base := srv.URL + "/api/v1/monitors"

	var list []monitorResponse
	if code := do(t, "GET", base, "", &list); code != http.StatusOK || list == nil || len(list) != 0 {
		t.Fatalf("empty list: status=%d body=%v (want 200 and [])", code, list)
	}

	var created monitorResponse
	if code := do(t, "POST", base, validMonitor, &created); code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", code)
	}
	if created.ID == 0 || created.Name != "api" || created.IntervalSeconds != 30 || !created.Enabled {
		t.Errorf("created = %+v", created)
	}
	if created.Status != statusPending || created.Last != nil || created.UptimePercent != nil {
		t.Errorf("new monitor should be pending with no statistics: %+v", created)
	}

	if code := do(t, "POST", base, validMonitor, nil); code != http.StatusConflict {
		t.Errorf("duplicate create status = %d, want 409", code)
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	err := db.InsertCheckResults(context.Background(), []store.CheckResult{
		{MonitorID: created.ID, CheckedAt: now.Add(-48 * time.Hour), Up: false, Latency: time.Second, Error: "outside window"},
		{MonitorID: created.ID, CheckedAt: now.Add(-3 * time.Minute), Up: true, Latency: 100 * time.Millisecond, StatusCode: 200},
		{MonitorID: created.ID, CheckedAt: now.Add(-2 * time.Minute), Up: true, Latency: 100 * time.Millisecond, StatusCode: 200},
		{MonitorID: created.ID, CheckedAt: now.Add(-1 * time.Minute), Up: true, Latency: 100 * time.Millisecond, StatusCode: 200},
		{MonitorID: created.ID, CheckedAt: now, Up: false, Latency: 500 * time.Millisecond, StatusCode: 503, Error: "unexpected status 503"},
	})
	if err != nil {
		t.Fatalf("insert results: %v", err)
	}

	var got monitorResponse
	if code := do(t, "GET", base+"/1", "", &got); code != http.StatusOK {
		t.Fatalf("get status = %d, want 200", code)
	}
	if got.Status != statusDown || got.Checks != 4 || got.AvgLatencyMS != 200 || got.WindowSeconds != 86400 {
		t.Errorf("status=%s checks=%d avg=%d window=%d, want down, 4, 200, 86400", got.Status, got.Checks, got.AvgLatencyMS, got.WindowSeconds)
	}
	if got.UptimePercent == nil || *got.UptimePercent != 75 {
		t.Errorf("uptime_percent = %v, want 75", got.UptimePercent)
	}
	if got.Last == nil || !got.Last.CheckedAt.Equal(now) || got.Last.StatusCode == nil || *got.Last.StatusCode != 503 ||
		got.Last.Error == nil || *got.Last.Error != "unexpected status 503" {
		t.Errorf("last_result = %+v", got.Last)
	}

	// A wider window also counts the old failure: 3 of 5 up.
	if do(t, "GET", base+"/1?window=72h", "", &got); got.Checks != 5 || *got.UptimePercent != 60 {
		t.Errorf("72h window: checks=%d uptime=%v, want 5 and 60", got.Checks, *got.UptimePercent)
	}
	if do(t, "GET", base, "", &list); len(list) != 1 || list[0].Status != statusDown {
		t.Errorf("list = %+v", list)
	}

	var results []resultResponse
	if code := do(t, "GET", base+"/1/results", "", &results); code != http.StatusOK {
		t.Fatalf("results status = %d, want 200", code)
	}
	if len(results) != 3 {
		t.Fatalf("results capped at MaxResults: len = %d, want 3", len(results))
	}
	if results[0].Up || !results[1].Up || results[1].Error != nil || results[1].StatusCode == nil {
		t.Errorf("results not newest first or wrong shape: %+v", results)
	}
	if do(t, "GET", base+"/1/results?limit=1", "", &results); len(results) != 1 {
		t.Errorf("limit=1 returned %d results", len(results))
	}
	if do(t, "GET", base+"/1/results?limit=500", "", &results); len(results) != 3 {
		t.Errorf("limit above the cap returned %d results, want 3", len(results))
	}

	if code := do(t, "DELETE", base+"/1", "", nil); code != http.StatusNoContent {
		t.Errorf("delete status = %d, want 204", code)
	}
	for _, path := range []string{"/1", "/1/results"} {
		if code := do(t, "GET", base+path, "", nil); code != http.StatusNotFound {
			t.Errorf("GET %s after delete: status = %d, want 404", path, code)
		}
	}
	if code := do(t, "DELETE", base+"/1", "", nil); code != http.StatusNotFound {
		t.Errorf("second delete status = %d, want 404", code)
	}
}

func TestCreateMonitorValidation(t *testing.T) {
	srv, _ := newTestAPI(t)

	tests := map[string]string{
		"not JSON":           `{`,
		"unknown field":      `{"name":"a","type":"http","target":"http://x","interval_seconds":30,"timeout_seconds":5,"extra":1}`,
		"empty name":         `{"name":"  ","type":"http","target":"http://x","interval_seconds":30,"timeout_seconds":5}`,
		"unknown type":       `{"name":"a","type":"icmp","target":"x","interval_seconds":30,"timeout_seconds":5}`,
		"http without host":  `{"name":"a","type":"http","target":"example.com","interval_seconds":30,"timeout_seconds":5}`,
		"http wrong scheme":  `{"name":"a","type":"http","target":"ftp://example.com","interval_seconds":30,"timeout_seconds":5}`,
		"tcp without port":   `{"name":"a","type":"tcp","target":"example.com","interval_seconds":30,"timeout_seconds":5}`,
		"tcp bad port":       `{"name":"a","type":"tcp","target":"example.com:99999","interval_seconds":30,"timeout_seconds":5}`,
		"interval too short": `{"name":"a","type":"http","target":"http://x","interval_seconds":4,"timeout_seconds":1}`,
		"zero timeout":       `{"name":"a","type":"http","target":"http://x","interval_seconds":30,"timeout_seconds":0}`,
		"timeout > interval": `{"name":"a","type":"http","target":"http://x","interval_seconds":30,"timeout_seconds":31}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			var got errorBody
			if code := do(t, "POST", srv.URL+"/api/v1/monitors", body, &got); code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", code)
			}
			if got.Error == "" {
				t.Error("no error message in body")
			}
		})
	}

	var list []monitorResponse
	if do(t, "GET", srv.URL+"/api/v1/monitors", "", &list); len(list) != 0 {
		t.Errorf("rejected requests created monitors: %+v", list)
	}

	var created monitorResponse
	tcp := `{"name":" db ","type":"tcp","target":"db.internal:5432","interval_seconds":5,"timeout_seconds":5}`
	if code := do(t, "POST", srv.URL+"/api/v1/monitors", tcp, &created); code != http.StatusCreated || created.Name != "db" {
		t.Errorf("valid tcp monitor: status=%d name=%q, want 201 and trimmed name", code, created.Name)
	}
}

func TestBadQueryAndPath(t *testing.T) {
	srv, _ := newTestAPI(t)
	do(t, "POST", srv.URL+"/api/v1/monitors", validMonitor, nil)

	tests := map[string]int{
		"/api/v1/monitors?window=abc":          http.StatusBadRequest,
		"/api/v1/monitors?window=-1h":          http.StatusBadRequest,
		"/api/v1/monitors/1?window=0s":         http.StatusBadRequest,
		"/api/v1/monitors/1/results?limit=0":   http.StatusBadRequest,
		"/api/v1/monitors/1/results?limit=abc": http.StatusBadRequest,
		"/api/v1/monitors/abc":                 http.StatusNotFound,
		"/api/v1/monitors/0":                   http.StatusNotFound,
		"/api/v1/monitors/999":                 http.StatusNotFound,
		"/api/v1/monitors/999/results":         http.StatusNotFound,
	}
	for path, want := range tests {
		if code := do(t, "GET", srv.URL+path, "", nil); code != want {
			t.Errorf("GET %s: status = %d, want %d", path, code, want)
		}
	}
}
