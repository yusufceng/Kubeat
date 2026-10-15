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

	"github.com/yusufceng/Kubeat/backend/internal/alert"
	"github.com/yusufceng/Kubeat/backend/internal/auth"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// testToken is the service token every test request carries.
const testToken = "test-token"

// newTestAPI serves the handler over a real SQLite store.
func newTestAPI(t *testing.T) (*httptest.Server, *store.SQLite) {
	t.Helper()
	return newTestAPIWith(t, nil)
}

// newTestAPIWith is newTestAPI with the options changed by customize first.
func newTestAPIWith(t *testing.T, customize func(*Options)) (*httptest.Server, *store.SQLite) {
	t.Helper()
	db, err := store.OpenSQLite(context.Background(), filepath.Join(t.TempDir(), "test.db"), 5*time.Second)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	// No target policy: the channels these tests deliver to are local servers.
	notifier := alert.New(db, alert.NewRouter(nil, "kubeat/test", alert.SMTPConfig{}, alert.Endpoints{}), alert.Options{
		Attempts: 1, SendTimeout: 2 * time.Second, Logger: logger,
	})
	opts := Options{
		Version:           "test",
		Store:             db,
		Token:             testToken,
		SessionTTL:        time.Hour,
		PasswordMinLength: 8,
		Throttle:          &auth.Throttle{MaxFailures: 3, Lockout: time.Minute},
		MinCheckInterval:  5 * time.Second,
		StatsWindow:       24 * time.Hour,
		MaxResults:        3,
		Notifier:          notifier,
		Logger:            logger,

		StatusPageDays:           3,
		StatusPageIncidentWindow: 48 * time.Hour,

		DefaultFailureThreshold: 3,
		RawRetention:            48 * time.Hour,
		HistoryRetention:        365 * 24 * time.Hour,
		SeriesPoints:            120,
	}
	if customize != nil {
		customize(&opts)
	}
	srv := httptest.NewServer(NewHandler(opts))
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
	req.Header.Set("Authorization", "Bearer "+testToken)
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
	if got.Status != "ok" || got.Version != "test" || got.Revision != Revision || got.Time.IsZero() {
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
	// Statistics come from the hourly summaries, which the server refreshes
	// in the background; here the test does it.
	if err := db.RollupCheckResults(context.Background(), now.Add(-72*time.Hour).Truncate(time.Hour), now.Add(time.Second)); err != nil {
		t.Fatalf("rollup: %v", err)
	}

	var got monitorResponse
	if code := do(t, "GET", base+"/1", "", &got); code != http.StatusOK {
		t.Fatalf("get status = %d, want 200", code)
	}
	// The average is over the three successful checks; the failed one's 500 ms
	// is not a response time.
	if got.Status != statusDown || got.Checks != 4 || got.AvgLatencyMS != 100 || got.WindowSeconds != 86400 {
		t.Errorf("status=%s checks=%d avg=%d window=%d, want down, 4, 100, 86400", got.Status, got.Checks, got.AvgLatencyMS, got.WindowSeconds)
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

func TestUpdateMonitor(t *testing.T) {
	srv, db := newTestAPI(t)
	base := srv.URL + "/api/v1/monitors"
	do(t, "POST", base, validMonitor, nil)
	do(t, "POST", base, strings.Replace(validMonitor, `"api"`, `"web"`, 1), nil)

	var got monitorResponse
	body := `{"target":"https://example.com/ready","interval_seconds":60,"failure_threshold":5}`
	if code := do(t, "PATCH", base+"/1", body, &got); code != http.StatusOK {
		t.Fatalf("update status = %d, want 200", code)
	}
	if got.Target != "https://example.com/ready" || got.IntervalSeconds != 60 || got.FailureThreshold != 5 {
		t.Errorf("changed fields = %+v", got)
	}
	// Fields left out keep their values.
	if got.Name != "api" || got.TimeoutSeconds != 5 || !got.Enabled || got.Type != "http" {
		t.Errorf("untouched fields changed: %+v", got)
	}

	tests := map[string]struct {
		body string
		want int
	}{
		"name taken":         {`{"name":"web"}`, http.StatusConflict},
		"empty name":         {`{"name":" "}`, http.StatusBadRequest},
		"bad target":         {`{"target":"example.com"}`, http.StatusBadRequest},
		"short interval":     {`{"interval_seconds":1}`, http.StatusBadRequest},
		"timeout > interval": {`{"timeout_seconds":61}`, http.StatusBadRequest},
		"zero threshold":     {`{"failure_threshold":0}`, http.StatusBadRequest},
		"type is immutable":  {`{"type":"tcp"}`, http.StatusBadRequest},
		"not JSON":           {`{`, http.StatusBadRequest},
	}
	for name, tt := range tests {
		if code := do(t, "PATCH", base+"/1", tt.body, nil); code != tt.want {
			t.Errorf("%s: status = %d, want %d", name, code, tt.want)
		}
	}
	if code := do(t, "PATCH", base+"/999", `{"name":"x"}`, nil); code != http.StatusNotFound {
		t.Errorf("unknown monitor: status = %d, want 404", code)
	}
	if do(t, "GET", base+"/1", "", &got); got.Target != "https://example.com/ready" || got.Name != "api" {
		t.Errorf("rejected updates changed the monitor: %+v", got)
	}

	// Pausing closes the open incident; resuming does not reopen it.
	ctx := context.Background()
	if _, err := db.OpenIncident(ctx, store.Incident{MonitorID: 1, StartedAt: time.Now().Add(-time.Minute), Cause: "timeout"}); err != nil {
		t.Fatal(err)
	}
	if code := do(t, "PATCH", base+"/1", `{"enabled":false}`, &got); code != http.StatusOK {
		t.Fatalf("pause status = %d", code)
	}
	if got.Enabled || got.Status != statusPaused || got.OpenIncident != nil {
		t.Errorf("paused monitor = enabled:%v status:%s incident:%+v", got.Enabled, got.Status, got.OpenIncident)
	}
	incidents, err := db.ListIncidents(ctx, 1, 10)
	if err != nil || len(incidents) != 1 || incidents[0].Open() {
		t.Errorf("incident after pause = %+v, %v", incidents, err)
	}

	// A certificate warning is closed by pausing as well.
	if _, err := db.OpenIncident(ctx, store.Incident{MonitorID: 2, Kind: store.IncidentCert, StartedAt: time.Now(), Cause: "expires"}); err != nil {
		t.Fatal(err)
	}
	var web monitorResponse
	if do(t, "GET", base+"/2", "", &web); !web.CertWarning || web.OpenIncident != nil {
		t.Errorf("monitor with a cert incident: warning=%v incident=%+v", web.CertWarning, web.OpenIncident)
	}
	if do(t, "PATCH", base+"/2", `{"enabled":false}`, &web); web.CertWarning {
		t.Error("cert warning survived pausing the monitor")
	}

	if do(t, "PATCH", base+"/1", `{"enabled":true}`, &got); !got.Enabled || got.Status == statusPaused || got.OpenIncident != nil {
		t.Errorf("resumed monitor = enabled:%v status:%s incident:%+v", got.Enabled, got.Status, got.OpenIncident)
	}
}

func TestHTTPOptionsAPI(t *testing.T) {
	srv, db := newTestAPI(t)
	base := srv.URL + "/api/v1/monitors"

	// Defaults for an HTTP monitor, nothing for a TCP one.
	var plain, tcp monitorResponse
	do(t, "POST", base, validMonitor, &plain)
	if plain.HTTP == nil || plain.HTTP.Method != "GET" || plain.HTTP.ExpectedStatus != "200-399" || plain.HTTP.Keyword != "" || len(plain.HTTP.Headers) != 0 {
		t.Errorf("default http options = %+v", plain.HTTP)
	}
	do(t, "POST", base, `{"name":"db","type":"tcp","target":"db:5432","interval_seconds":30,"timeout_seconds":5}`, &tcp)
	if tcp.HTTP != nil {
		t.Errorf("tcp monitor has http options: %+v", tcp.HTTP)
	}

	body := `{"name":"secure","type":"http","target":"https://x/health","interval_seconds":30,"timeout_seconds":5,
		"http":{"method":"get","expected_status":"200, 401","keyword":"ok","headers":{"authorization":"Bearer s3cret","x-tenant":"a"},"tls_skip_verify":true}}`
	var created monitorResponse
	if code := do(t, "POST", base, body, &created); code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", code)
	}
	o := created.HTTP
	if o.Method != "GET" || o.ExpectedStatus != "200, 401" || o.Keyword != "ok" || !o.TLSSkipVerify {
		t.Errorf("created options = %+v", o)
	}
	// Header names are canonical and values never leave the server.
	if len(o.Headers) != 2 || o.Headers["Authorization"] != maskedValue || o.Headers["X-Tenant"] != maskedValue {
		t.Errorf("headers in response = %v, want canonical names with masked values", o.Headers)
	}
	stored, _ := db.GetMonitor(context.Background(), created.ID)
	if stored.HTTP.Headers["Authorization"] != "Bearer s3cret" {
		t.Errorf("stored headers = %v", stored.HTTP.Headers)
	}

	// An update keeps masked values, changes given ones and drops the rest.
	patch := `{"http":{"method":"GET","expected_status":"204","headers":{"Authorization":"` + maskedValue + `","X-New":"n"}}}`
	var updated monitorResponse
	if code := do(t, "PATCH", base+"/3", patch, &updated); code != http.StatusOK {
		t.Fatalf("update status = %d, want 200", code)
	}
	if updated.HTTP.ExpectedStatus != "204" || updated.HTTP.Keyword != "" || updated.HTTP.TLSSkipVerify {
		t.Errorf("updated options = %+v (keyword and tls flag should be reset)", updated.HTTP)
	}
	stored, _ = db.GetMonitor(context.Background(), created.ID)
	if len(stored.HTTP.Headers) != 2 || stored.HTTP.Headers["Authorization"] != "Bearer s3cret" || stored.HTTP.Headers["X-New"] != "n" {
		t.Errorf("stored headers after update = %v", stored.HTTP.Headers)
	}
	// An update without "http" leaves the options alone.
	do(t, "PATCH", base+"/3", `{"interval_seconds":60}`, &updated)
	if updated.HTTP.ExpectedStatus != "204" || len(updated.HTTP.Headers) != 2 {
		t.Errorf("options changed by an unrelated update: %+v", updated.HTTP)
	}

	invalid := map[string]string{
		"bad method":           `{"method":"TRACE"}`,
		"json path with HEAD":  `{"method":"HEAD","json_path":"status"}`,
		"bad json path":        `{"json_path":"a..b"}`,
		"bad json operator":    `{"json_path":"a","json_operator":"~"}`,
		"too many redirects":   `{"max_redirects":21}`,
		"negative redirects":   `{"max_redirects":-1}`,
		"unknown auth":         `{"auth":"digest"}`,
		"basic without a user": `{"auth":"basic","auth_secret":"p"}`,
		"bearer without token": `{"auth":"bearer"}`,
		"mask without secret":  `{"auth":"bearer","auth_secret":"` + maskedValue + `"}`,
		"line break in type":   `{"body":"x","content_type":"a\r\nX: y"}`,
		"bad status":           `{"expected_status":"2xx"}`,
		"keyword with HEAD":    `{"method":"HEAD","keyword":"ok"}`,
		"bad header name":      `{"headers":{"X Bad":"v"}}`,
		"line break in value":  `{"headers":{"X-A":"a\r\nX-B: b"}}`,
		"mask without a value": `{"headers":{"X-Unknown":"` + maskedValue + `"}}`,
		"duplicate header":     `{"headers":{"x-a":"1","X-A":"2"}}`,
	}
	for name, opts := range invalid {
		if code := do(t, "PATCH", base+"/3", `{"http":`+opts+`}`, nil); code != http.StatusBadRequest {
			t.Errorf("update, %s: status = %d, want 400", name, code)
		}
		create := `{"name":"n","type":"http","target":"http://x","interval_seconds":30,"timeout_seconds":5,"http":` + opts + `}`
		if code := do(t, "POST", base, create, nil); code != http.StatusBadRequest {
			t.Errorf("create, %s: status = %d, want 400", name, code)
		}
	}
	for _, target := range []string{"POST " + base, "PATCH " + base + "/2"} {
		method, url, _ := strings.Cut(target, " ")
		body := `{"http":{"method":"GET"}}`
		if method == "POST" {
			body = `{"name":"t","type":"tcp","target":"x:1","interval_seconds":30,"timeout_seconds":5,"http":{"method":"GET"}}`
		}
		if code := do(t, method, url, body, nil); code != http.StatusBadRequest {
			t.Errorf("%s with http options on a tcp monitor: status = %d, want 400", method, code)
		}
	}
}

func TestAPIToken(t *testing.T) {
	db, err := store.OpenSQLite(context.Background(), filepath.Join(t.TempDir(), "test.db"), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	metrics := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("kubeat_build_info 1\n")) })
	srv := httptest.NewServer(NewHandler(Options{
		Store: db, Token: "s3cret-token", Metrics: metrics, MaxResults: 100, StatsWindow: time.Hour, SessionTTL: time.Hour,
		MinCheckInterval: 5 * time.Second, DefaultFailureThreshold: 3,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}))
	t.Cleanup(func() {
		srv.Close()
		db.Close()
	})

	request := func(method, path, auth string) int {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(validMonitor))
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	// Probes and scrapes need no token.
	for _, path := range []string{"/api/v1/health", "/metrics"} {
		if code := request("GET", path, ""); code != http.StatusOK {
			t.Errorf("GET %s without a token: status = %d, want 200", path, code)
		}
	}
	// Everything else does, reads and writes alike.
	for _, target := range []string{"GET /api/v1/monitors", "POST /api/v1/monitors", "GET /api/v1/channels", "DELETE /api/v1/monitors/1", "GET /api/v1/discovery", "GET /api/v1/nope"} {
		method, path, _ := strings.Cut(target, " ")
		for name, auth := range map[string]string{
			"no header":    "",
			"wrong token":  "Bearer wrong",
			"prefix":       "Bearer s3cret",
			"longer":       "Bearer s3cret-token-and-more",
			"wrong scheme": "Basic s3cret-token",
			"token alone":  "s3cret-token",
		} {
			if code := request(method, path, auth); code != http.StatusUnauthorized {
				t.Errorf("%s with %s: status = %d, want 401", target, name, code)
			}
		}
	}
	var list []monitorResponse
	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/monitors", nil)
	req.Header.Set("Authorization", "Bearer s3cret-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("with the token: status = %d, want 200", resp.StatusCode)
	}
	// None of the rejected POSTs created anything.
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil || len(list) != 0 {
		t.Errorf("monitors after rejected requests = %v, %v", list, err)
	}
	if code := request("POST", "/api/v1/monitors", "Bearer s3cret-token"); code != http.StatusCreated {
		t.Errorf("POST with the token: status = %d, want 201", code)
	}
}

func TestMonitorListFiltersAndHistory(t *testing.T) {
	srv, db := newTestAPI(t)
	ctx := context.Background()
	base := srv.URL + "/api/v1/monitors"

	create := func(name, namespace string) store.Monitor {
		m, err := db.CreateMonitor(ctx, store.Monitor{Name: name, Namespace: namespace, Source: "service/" + name, Type: store.MonitorTCP,
			Target: "x:1", Interval: 30 * time.Second, Timeout: 5 * time.Second, FailureThreshold: 3, Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	shopA, shopB, billing := create("a", "shop"), create("b", "shop"), create("c", "billing")
	do(t, "POST", base, validMonitor, nil) // a monitor without a namespace, id 4

	now := time.Now().UTC().Truncate(time.Millisecond)
	var batch []store.CheckResult
	for i := 0; i < 5; i++ {
		batch = append(batch, store.CheckResult{MonitorID: shopA.ID, CheckedAt: now.Add(-time.Duration(i) * time.Minute), Up: i != 0,
			Latency: time.Duration(10*(i+1)) * time.Millisecond, Error: map[bool]string{true: "refused"}[i == 0]})
	}
	if err := db.InsertCheckResults(ctx, batch); err != nil {
		t.Fatal(err)
	}

	names := func(list []monitorResponse) string {
		var out []string
		for _, m := range list {
			out = append(out, m.Name)
		}
		return strings.Join(out, ",")
	}
	var list []monitorResponse
	for query, want := range map[string]string{
		"":                      "a,b,c,api",
		"?namespace=shop":       "a,b",
		"?namespace=":           "api", // present but empty: monitors without a namespace
		"?namespace=nope":       "",
		"?ids=1,3":              "a,c",
		"?ids=2&namespace=shop": "b",
		"?ids=3&namespace=shop": "",
	} {
		list = nil
		if code := do(t, "GET", base+query, "", &list); code != http.StatusOK || names(list) != want {
			t.Errorf("GET %q: status=%d names=%q, want %q", query, code, names(list), want)
		}
		for _, m := range list {
			if m.History != nil {
				t.Errorf("GET %q: history present without being asked for", query)
			}
		}
	}

	// History: newest first, capped at MaxResults (3 in the test API), in the
	// compact form.
	list = nil
	do(t, "GET", base+"?namespace=shop&history=50", "", &list)
	a, b := list[0], list[1]
	if len(a.History) != 3 || a.History[0].T != now.UnixMilli() || a.History[0].Up || a.History[0].Error != "refused" || a.History[0].MS != 10 {
		t.Errorf("history of a = %+v", a.History)
	}
	if !a.History[1].Up || a.History[1].Error != "" || a.History[1].MS != 20 {
		t.Errorf("second point of a = %+v", a.History[1])
	}
	// A monitor without results simply has no history field.
	if len(b.History) != 0 {
		t.Errorf("history of a monitor without results = %v", b.History)
	}
	_ = shopB
	_ = billing

	many := strings.TrimSuffix(strings.Repeat("1,", maxListIDs+1), ",")
	for _, bad := range []string{"?history=0", "?history=x", "?ids=a", "?ids=0", "?ids=1,,2", "?ids=" + many} {
		if code := do(t, "GET", base+bad, "", nil); code != http.StatusBadRequest {
			t.Errorf("GET %.30q: status = %d, want 400", bad, code)
		}
	}
}
