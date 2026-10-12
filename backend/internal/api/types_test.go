package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// monitorJSON returns the body that creates a monitor, with extra fields.
func monitorJSON(name, typ, target, extra string) string {
	body := `{"name":"` + name + `","type":"` + typ + `","target":"` + target + `","interval_seconds":60,"timeout_seconds":5`
	if extra != "" {
		body += "," + extra
	}
	return body + "}"
}

func TestMonitorTypes(t *testing.T) {
	srv, db := newTestAPI(t)
	base := srv.URL + "/api/v1/monitors"
	ctx := context.Background()

	create := func(body string) monitorResponse {
		t.Helper()
		var m monitorResponse
		if code := do(t, "POST", base, body, &m); code != http.StatusCreated {
			t.Fatalf("create %s: status = %d", body, code)
		}
		return m
	}

	// Every type has the target it checks and, in responses, only its own options.
	tcp := create(monitorJSON("ldaps", "tcp", "ldap.corp:636", `"tcp":{"tls":true,"tls_skip_verify":true}`))
	if tcp.TCP == nil || !tcp.TCP.TLS || !tcp.TCP.TLSSkipVerify || tcp.HTTP != nil || tcp.DNS != nil || tcp.GRPC != nil || tcp.WebSocket != nil {
		t.Errorf("tcp options = %+v", tcp.checkOptions)
	}
	// Skipping verification means nothing without TLS.
	if plain := create(monitorJSON("plain-tcp", "tcp", "db:5432", `"tcp":{"tls_skip_verify":true}`)); plain.TCP.TLSSkipVerify {
		t.Errorf("tls_skip_verify kept without tls: %+v", plain.TCP)
	}

	dns := create(monitorJSON("cluster-dns", "dns", "kubernetes.default.svc.cluster.local", `"dns":{"record_type":"a","resolver":"10.96.0.10","expected":"10.96.0.1"}`))
	if dns.DNS == nil || dns.DNS.RecordType != "A" || dns.DNS.Resolver != "10.96.0.10" || dns.DNS.Expected != "10.96.0.1" {
		t.Errorf("dns options = %+v", dns.DNS)
	}
	if plain := create(monitorJSON("plain-dns", "dns", "example.com", "")); plain.DNS == nil || plain.DNS.RecordType != "A" {
		t.Errorf("default dns options = %+v", plain.DNS)
	}
	create(monitorJSON("srv", "dns", "_https._tcp.example.com", `"dns":{"record_type":"SRV","resolver":"ns1.example.com:5353"}`))

	grpc := create(monitorJSON("cart", "grpc", "cart.shop.svc:9090", `"grpc":{"service":"shop.Cart","tls":true}`))
	if grpc.GRPC == nil || grpc.GRPC.Service != "shop.Cart" || !grpc.GRPC.TLS {
		t.Errorf("grpc options = %+v", grpc.GRPC)
	}
	ws := create(monitorJSON("live", "websocket", "wss://live.example.com/socket", `"websocket":{"subprotocol":"graphql-ws"}`))
	if ws.WebSocket == nil || ws.WebSocket.Subprotocol != "graphql-ws" {
		t.Errorf("websocket options = %+v", ws.WebSocket)
	}
	ping := create(monitorJSON("gateway", "ping", "10.0.0.1", `"description":"çıkış ağ geçidi","retry_interval_seconds":10,"inverted":true`))
	if ping.Description != "çıkış ağ geçidi" || ping.RetryIntervalSeconds != 10 || !ping.Inverted || ping.PushToken != "" {
		t.Errorf("ping monitor = %+v", ping)
	}

	// An HTTP monitor with everything a request can carry.
	api := create(monitorJSON("orders", "http", "https://orders.example.com/health", `"http":{
		"method":"post","body":"{\"deep\":true}","content_type":"application/json",
		"auth":"basic","auth_user":"probe","auth_secret":"s3cret","max_redirects":0,
		"keyword":"error","invert_keyword":true,"json_path":"checks[0].state","json_operator":"!=","json_expected":"down"}`))
	o := api.HTTP
	if o.Method != "POST" || o.Body != `{"deep":true}` || o.ContentType != "application/json" || o.Auth != "basic" || o.AuthUser != "probe" ||
		o.AuthSecret != maskedValue || *o.MaxRedirects != 0 || !o.InvertKeyword || o.JSONPath != "checks[0].state" || o.JSONOperator != "!=" || o.JSONExpected != "down" {
		t.Errorf("http options = %+v", o)
	}
	if stored, _ := db.GetMonitor(ctx, api.ID); stored.HTTP.AuthSecret != "s3cret" {
		t.Errorf("stored secret = %q", stored.HTTP.AuthSecret)
	}
	// Redirects are followed unless the request says otherwise.
	if plain := create(monitorJSON("plain-http", "http", "http://x/", `"http":{"keyword":"ok"}`)); *plain.HTTP.MaxRedirects != store.DefaultMaxRedirects || plain.HTTP.JSONOperator != "" {
		t.Errorf("default http options = %+v", plain.HTTP)
	}
	if bare := create(monitorJSON("bare-http", "http", "http://y/", "")); *bare.HTTP.MaxRedirects != store.DefaultMaxRedirects {
		t.Errorf("http monitor without options: max_redirects = %d", *bare.HTTP.MaxRedirects)
	}

	// An update keeps a masked secret, but only for the same kind of credential.
	var updated monitorResponse
	patch := `{"http":{"method":"GET","auth":"basic","auth_user":"probe2","auth_secret":"` + maskedValue + `"}}`
	if code := do(t, "PATCH", base+"/"+itoa(api.ID), patch, &updated); code != http.StatusOK || updated.HTTP.AuthUser != "probe2" || updated.HTTP.Body != "" {
		t.Errorf("update keeping the secret: status = %d, options = %+v", code, updated.HTTP)
	}
	if stored, _ := db.GetMonitor(ctx, api.ID); stored.HTTP.AuthSecret != "s3cret" || stored.HTTP.Method != "GET" {
		t.Errorf("stored after update = %+v", stored.HTTP)
	}
	patch = `{"http":{"auth":"bearer","auth_secret":"` + maskedValue + `"}}`
	if code := do(t, "PATCH", base+"/"+itoa(api.ID), patch, nil); code != http.StatusBadRequest {
		t.Errorf("a basic password reused as a bearer token: status = %d, want 400", code)
	}
	// The shared settings of any monitor can be changed.
	if code := do(t, "PATCH", base+"/"+itoa(ping.ID), `{"description":"","retry_interval_seconds":0,"inverted":false}`, &updated); code != http.StatusOK ||
		updated.Description != "" || updated.RetryIntervalSeconds != 0 || updated.Inverted {
		t.Errorf("update of the shared settings: status = %d, monitor = %+v", code, updated)
	}

	invalid := map[string]string{
		"unknown type":            monitorJSON("x", "icmp", "10.0.0.1", ""),
		"ping with a port":        monitorJSON("x", "ping", "10.0.0.1:80", ""),
		"ping with a scheme":      monitorJSON("x", "ping", "http://host", ""),
		"dns with a space":        monitorJSON("x", "dns", "exa mple.com", ""),
		"dns record type":         monitorJSON("x", "dns", "example.com", `"dns":{"record_type":"LOC"}`),
		"dns resolver":            monitorJSON("x", "dns", "example.com", `"dns":{"resolver":"http://8.8.8.8"}`),
		"dns resolver port":       monitorJSON("x", "dns", "example.com", `"dns":{"resolver":"8.8.8.8:99999"}`),
		"grpc without a port":     monitorJSON("x", "grpc", "cart.shop.svc", ""),
		"websocket over http":     monitorJSON("x", "websocket", "https://live.example.com", ""),
		"websocket subprotocol":   monitorJSON("x", "websocket", "ws://live/s", `"websocket":{"subprotocol":"a b"}`),
		"push with a target":      monitorJSON("x", "push", "http://x", ""),
		"push with a retry":       monitorJSON("x", "push", "", `"retry_interval_seconds":10`),
		"options of another type": monitorJSON("x", "ping", "10.0.0.1", `"tcp":{"tls":true}`),
		"retry below the minimum": monitorJSON("x", "tcp", "x:1", `"retry_interval_seconds":2`),
		"retry above interval":    monitorJSON("x", "tcp", "x:1", `"retry_interval_seconds":61`),
		"long description":        monitorJSON("x", "tcp", "x:1", `"description":"`+strings.Repeat("a", 501)+`"`),
	}
	for name, body := range invalid {
		if code := do(t, "POST", base, body, nil); code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, code)
		}
	}
}

func itoa(id int64) string { return strconv.FormatInt(id, 10) }

func TestPushMonitor(t *testing.T) {
	srv, db := newTestAPI(t)
	base := srv.URL + "/api/v1/monitors"
	ctx := context.Background()

	var job, other monitorResponse
	if code := do(t, "POST", base, monitorJSON("nightly-backup", "push", "", ""), &job); code != http.StatusCreated {
		t.Fatalf("create: status = %d", code)
	}
	do(t, "POST", base, monitorJSON("hourly-sync", "push", "", ""), &other)
	if len(job.PushToken) != 32 || job.PushToken == other.PushToken || job.Target != "" {
		t.Fatalf("push monitors = %+v, %+v", job, other)
	}
	// The token comes back when the monitor is read, so the UI can show its URL.
	var read monitorResponse
	if do(t, "GET", base+"/1", "", &read); read.PushToken != job.PushToken {
		t.Errorf("token when read = %q", read.PushToken)
	}

	push := srv.URL + "/api/push/" + job.PushToken
	var answer pushResponse
	if code := do(t, "GET", push, "", &answer); code != http.StatusOK || !answer.OK {
		t.Fatalf("bare push: status = %d, answer = %+v", code, answer)
	}
	report, err := db.LastPush(ctx, job.ID)
	if err != nil || !report.Up || report.Message != "" || report.Latency != 0 || time.Since(report.ReceivedAt) > time.Minute {
		t.Errorf("report = %+v, %v", report, err)
	}

	if code := do(t, "POST", push+"?status=down&msg=disk+dolu&ping=1500.4", "", &answer); code != http.StatusOK {
		t.Fatalf("push with details: status = %d", code)
	}
	report, _ = db.LastPush(ctx, job.ID)
	if report.Up || report.Message != "disk dolu" || report.Latency != 1500*time.Millisecond { // stored in whole milliseconds
		t.Errorf("report with details = %+v", report)
	}
	// The other monitor heard nothing.
	if _, err := db.LastPush(ctx, other.ID); err == nil {
		t.Error("a push reached a monitor it was not for")
	}

	for name, query := range map[string]string{
		"unknown status":    "?status=degraded",
		"negative ping":     "?ping=-1",
		"ping not a number": "?ping=fast",
		"long message":      "?msg=" + strings.Repeat("a", 251),
	} {
		if code := do(t, "GET", push+query, "", &answer); code != http.StatusBadRequest || answer.OK || answer.Error == "" {
			t.Errorf("%s: status = %d, answer = %+v", name, code, answer)
		}
	}
	if code := do(t, "GET", srv.URL+"/api/push/0123456789abcdef0123456789abcdef", "", &answer); code != http.StatusNotFound || answer.OK {
		t.Errorf("unknown token: status = %d, answer = %+v", code, answer)
	}
	// Nothing invalid replaced the last good report.
	if report, _ := db.LastPush(ctx, job.ID); report.Message != "disk dolu" {
		t.Errorf("report after invalid pushes = %+v", report)
	}

	// The token survives an update and is not taken from one.
	var updated monitorResponse
	if code := do(t, "PATCH", base+"/1", `{"interval_seconds":120}`, &updated); code != http.StatusOK || updated.PushToken != job.PushToken {
		t.Errorf("update: status = %d, token = %q", code, updated.PushToken)
	}
}

func TestCloneMonitor(t *testing.T) {
	srv, db := newTestAPI(t)
	base := srv.URL + "/api/v1/monitors"
	ctx := context.Background()

	original, err := db.CreateMonitor(ctx, store.Monitor{
		Name: "shop/service/api:443", Namespace: "shop", Source: "service/api:443", Description: "sipariş API'si",
		Type: store.MonitorHTTP, Target: "https://api.shop.svc/healthz", Interval: time.Minute, Timeout: 5 * time.Second,
		RetryInterval: 20 * time.Second, FailureThreshold: 2, Enabled: false,
		HTTP: store.HTTPOptions{Method: "POST", ExpectedStatus: "200", Auth: store.HTTPAuthBearer, AuthSecret: "t0ken",
			Headers: map[string]string{"X-Key": "k3y"}, MaxRedirects: 2},
	})
	if err != nil {
		t.Fatal(err)
	}

	var clone monitorResponse
	if code := do(t, "POST", base+"/1/clone", `{"name":" api (kopya) "}`, &clone); code != http.StatusCreated {
		t.Fatalf("clone: status = %d", code)
	}
	if clone.ID == original.ID || clone.Name != "api (kopya)" || clone.Namespace != "shop" || clone.Source != "" || !clone.Enabled ||
		clone.Description != original.Description || clone.RetryIntervalSeconds != 20 || clone.FailureThreshold != 2 {
		t.Errorf("clone = %+v", clone)
	}
	// The secrets came along, which a client could not have done itself.
	stored, _ := db.GetMonitor(ctx, clone.ID)
	if stored.HTTP.AuthSecret != "t0ken" || stored.HTTP.Headers["X-Key"] != "k3y" || stored.HTTP.Method != "POST" || stored.HTTP.MaxRedirects != 2 {
		t.Errorf("cloned options = %+v", stored.HTTP)
	}

	// A cloned push monitor reports to a URL of its own.
	var job, jobClone monitorResponse
	do(t, "POST", base, monitorJSON("job", "push", "", ""), &job)
	do(t, "POST", base+"/"+itoa(job.ID)+"/clone", `{"name":"job-2"}`, &jobClone)
	if len(jobClone.PushToken) != 32 || jobClone.PushToken == job.PushToken {
		t.Errorf("tokens = %q, %q", job.PushToken, jobClone.PushToken)
	}

	for name, tc := range map[string]struct {
		url, body string
		want      int
	}{
		"name taken":      {base + "/1/clone", `{"name":"job"}`, http.StatusConflict},
		"no name":         {base + "/1/clone", `{"name":"  "}`, http.StatusBadRequest},
		"unknown monitor": {base + "/999/clone", `{"name":"z"}`, http.StatusNotFound},
		"unknown field":   {base + "/1/clone", `{"name":"z","target":"http://evil"}`, http.StatusBadRequest},
	} {
		if code := do(t, "POST", tc.url, tc.body, nil); code != tc.want {
			t.Errorf("%s: status = %d, want %d", name, code, tc.want)
		}
	}
}

// A push needs no API token: its own token is the credential.
func TestPushBypassesTheAPIToken(t *testing.T) {
	srv, db := newTestAPI(t)
	job, err := db.CreateMonitor(context.Background(), store.Monitor{
		Name: "job", Type: store.MonitorPush, Interval: time.Minute, Timeout: 5 * time.Second, FailureThreshold: 1, Enabled: true, PushToken: "tok",
	})
	if err != nil {
		t.Fatal(err)
	}
	guarded := requireToken("s3cret", srv.Config.Handler)

	for path, want := range map[string]int{
		"/api/push/tok":     http.StatusOK,
		"/api/push/unknown": http.StatusNotFound,
		"/api/v1/monitors":  http.StatusUnauthorized,
		// Nothing else hides behind the prefix.
		"/api/push/../v1/monitors": http.StatusUnauthorized,
	} {
		req, _ := http.NewRequest("GET", "http://kubeat"+path, nil)
		rec := newRecorder()
		guarded.ServeHTTP(rec, req)
		if rec.status != want {
			t.Errorf("%s: status = %d, want %d", path, rec.status, want)
		}
	}
	if _, err := db.LastPush(context.Background(), job.ID); err != nil {
		t.Errorf("the push was not recorded: %v", err)
	}
}

type recorder struct {
	header http.Header
	status int
}

func newRecorder() *recorder { return &recorder{header: http.Header{}, status: http.StatusOK} }

func (r *recorder) Header() http.Header         { return r.header }
func (r *recorder) Write(b []byte) (int, error) { return len(b), nil }
func (r *recorder) WriteHeader(status int)      { r.status = status }
