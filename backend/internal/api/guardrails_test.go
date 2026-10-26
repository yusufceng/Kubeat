package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/k8s"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// post sends body with the service token and returns the status and the
// decoded error, if the answer was one.
func post(t *testing.T, url, body string) (int, string, http.Header) {
	t.Helper()
	req, err := http.NewRequest("POST", url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var problem errorBody
	_ = json.Unmarshal(raw, &problem)
	return resp.StatusCode, problem.Error, resp.Header
}

func guardedMonitor(name, typ, target string, interval int, extra string) string {
	return fmt.Sprintf(`{"name":%q,"type":%q,"target":%q,"interval_seconds":%d,"timeout_seconds":1%s}`, name, typ, target, interval, extra)
}

// An external target may not be checked more often than the external
// minimum, by its interval or by its retry interval; in-cluster targets
// keep the general minimum, and push monitors, which check nothing, are
// held to neither.
func TestExternalMinimumInterval(t *testing.T) {
	srv, _ := newTestAPIWith(t, func(o *Options) { o.MinCheckIntervalExternal = 30 * time.Second })
	base := srv.URL + "/api/v1/monitors"
	wantRule := "interval_seconds must be at least 30 for a target outside the cluster (UPTIME_MIN_CHECK_INTERVAL_EXTERNAL)"

	for name, body := range map[string]string{
		"http":        guardedMonitor("a", "http", "https://example.com/", 10, ""),
		"tcp":         guardedMonitor("b", "tcp", "example.com:443", 10, ""),
		"ping":        guardedMonitor("c", "ping", "example.com", 10, ""),
		"public IP":   guardedMonitor("d", "http", "http://8.8.8.8/", 10, ""),
		"corp domain": guardedMonitor("e", "http", "https://intranet.corp/", 10, ""), // external by name, whatever it resolves to
	} {
		if code, msg, _ := post(t, base, body); code != http.StatusBadRequest || msg != wantRule {
			t.Errorf("%s at 10s: status = %d, error = %q; want 400 %q", name, code, msg, wantRule)
		}
	}
	if code, msg, _ := post(t, base, guardedMonitor("r", "http", "https://example.com/", 60, `,"retry_interval_seconds":10`)); code != http.StatusBadRequest ||
		msg != "retry_interval_seconds must be 0 or at least 30 for a target outside the cluster (UPTIME_MIN_CHECK_INTERVAL_EXTERNAL)" {
		t.Errorf("retry at 10s: status = %d, error = %q", code, msg)
	}
	for name, body := range map[string]string{
		"external at the minimum": guardedMonitor("ok1", "http", "https://example.com/", 30, `,"retry_interval_seconds":30`),
		"service name":            guardedMonitor("ok2", "http", "http://api.shop.svc/healthz", 10, `,"retry_interval_seconds":5`),
		"short service name":      guardedMonitor("ok3", "tcp", "redis:6379", 10, ""),
		"cluster IP":              guardedMonitor("ok4", "tcp", "172.30.0.10:5432", 5, ""),
		"private IPv6":            guardedMonitor("ok5", "http", "http://[fd00::1]:8080/", 5, ""),
		"push":                    guardedMonitor("ok6", "push", "", 10, ""),
	} {
		if code, msg, _ := post(t, base, body); code != http.StatusCreated {
			t.Errorf("%s: status = %d, error = %q; want 201", name, code, msg)
		}
	}

	// The same rule on an update, whether the interval or the target moves.
	var m monitorResponse
	if code := do(t, "POST", base, guardedMonitor("u", "http", "http://api.shop.svc/", 10, ""), &m); code != http.StatusCreated {
		t.Fatalf("create: %d", code)
	}
	id := strconv.FormatInt(m.ID, 10)
	if code, msg, _ := post(t, base+"/"+id+"/clone", `{"name":"copy"}`); code != http.StatusCreated {
		t.Errorf("clone of an in-cluster monitor: %d %q", code, msg)
	}
	if code := do(t, "PATCH", base+"/"+id, `{"target":"https://example.com/"}`, nil); code != http.StatusBadRequest {
		t.Errorf("moving a 10s monitor to an external target: status = %d, want 400", code)
	}
	if code := do(t, "PATCH", base+"/"+id, `{"target":"https://example.com/","interval_seconds":30}`, nil); code != http.StatusOK {
		t.Errorf("moving it with a 30s interval: status = %d, want 200", code)
	}
	if code := do(t, "PATCH", base+"/"+id, `{"interval_seconds":10}`, nil); code != http.StatusBadRequest {
		t.Errorf("shortening an external monitor's interval to 10s: status = %d, want 400", code)
	}
}

// With no external minimum set, or one no longer than the general
// minimum, every target keeps the general rule.
func TestExternalMinimumOff(t *testing.T) {
	srv, _ := newTestAPI(t) // MinCheckIntervalExternal is zero
	if code, msg, _ := post(t, srv.URL+"/api/v1/monitors", guardedMonitor("a", "http", "https://example.com/", 5, "")); code != http.StatusCreated {
		t.Errorf("status = %d, error = %q; want 201", code, msg)
	}
	if code, msg, _ := post(t, srv.URL+"/api/v1/monitors", guardedMonitor("b", "http", "https://example.com/", 4, "")); code != http.StatusBadRequest || msg != "interval_seconds must be at least 5" {
		t.Errorf("under the general minimum: status = %d, error = %q", code, msg)
	}
}

// One external host takes at most MaxMonitorsPerHost monitors, from every
// namespace together and however the host is written; in-cluster hosts
// take any number. A monitor moved onto a full host is refused, one that
// changes its path on the same host is not.
func TestMonitorsPerHost(t *testing.T) {
	srv, _ := newTestAPIWith(t, func(o *Options) { o.MaxMonitorsPerHost = 2 })
	base := srv.URL + "/api/v1/monitors"

	for i, target := range []string{"https://example.com/a", "http://EXAMPLE.com:8080/b"} {
		if code, msg, _ := post(t, base, guardedMonitor("m"+strconv.Itoa(i), "http", target, 30, `,"namespace":"ns`+strconv.Itoa(i)+`"`)); code != http.StatusCreated {
			t.Fatalf("monitor %d on example.com: %d %q", i, code, msg)
		}
	}
	want := "at most 2 monitors may check the host example.com (UPTIME_MAX_MONITORS_PER_HOST); it has 2 already"
	for name, body := range map[string]string{
		"http":   guardedMonitor("x", "http", "https://example.com/c", 30, ""),
		"tcp":    guardedMonitor("y", "tcp", "example.com:443", 30, ""),
		"ping":   guardedMonitor("z", "ping", "example.com.", 30, ""),
		"rooted": guardedMonitor("w", "http", "https://example.com./", 30, ""),
	} {
		if code, msg, _ := post(t, base, body); code != http.StatusBadRequest || msg != want {
			t.Errorf("%s: status = %d, error = %q; want 400 %q", name, code, msg, want)
		}
	}
	for i := 0; i < 4; i++ {
		if code, msg, _ := post(t, base, guardedMonitor("svc"+strconv.Itoa(i), "http", "http://api.shop.svc/"+strconv.Itoa(i), 30, "")); code != http.StatusCreated {
			t.Errorf("in-cluster monitor %d: %d %q", i, code, msg)
		}
	}

	var other monitorResponse
	if code := do(t, "POST", base, guardedMonitor("other", "http", "https://other.example/", 30, ""), &other); code != http.StatusCreated {
		t.Fatalf("other: %d", code)
	}
	id := strconv.FormatInt(other.ID, 10)
	if code := do(t, "PATCH", base+"/"+id, `{"target":"https://example.com/d"}`, nil); code != http.StatusBadRequest {
		t.Errorf("moving onto the full host: status = %d, want 400", code)
	}
	if code := do(t, "PATCH", base+"/"+id, `{"target":"https://other.example/else"}`, nil); code != http.StatusOK {
		t.Errorf("another path on its own host: status = %d, want 200", code)
	}
	// A monitor on the full host may change: it is not one more.
	var list []monitorResponse
	do(t, "GET", base+"?namespace=ns0", "", &list)
	if len(list) != 1 {
		t.Fatalf("ns0 has %d monitors", len(list))
	}
	if code := do(t, "PATCH", base+"/"+strconv.FormatInt(list[0].ID, 10), `{"target":"https://example.com/moved","name":"renamed"}`, nil); code != http.StatusOK {
		t.Errorf("changing a monitor already on the full host: status = %d, want 200", code)
	}
	if code, msg, _ := post(t, base+"/"+strconv.FormatInt(list[0].ID, 10)+"/clone", `{"name":"copy"}`); code != http.StatusBadRequest || msg != want {
		t.Errorf("a copy on the full host: %d %q", code, msg)
	}
}

// The quotas bind new monitors only: those above a limit keep running
// and can be changed, and a copy counts as a new one.
func TestMonitorQuotas(t *testing.T) {
	srv, db := newTestAPIWith(t, func(o *Options) { o.MaxMonitors, o.MaxMonitorsPerNamespace = 4, 2 })
	base := srv.URL + "/api/v1/monitors"

	for i := 0; i < 2; i++ {
		if code, msg, _ := post(t, base, guardedMonitor("shop"+strconv.Itoa(i), "http", "http://api.shop.svc/"+strconv.Itoa(i), 30, `,"namespace":"shop"`)); code != http.StatusCreated {
			t.Fatalf("shop %d: %d %q", i, code, msg)
		}
	}
	wantNS := `the namespace "shop" is at the limit of 2 monitors (UPTIME_MAX_MONITORS_PER_NAMESPACE)`
	if code, msg, _ := post(t, base, guardedMonitor("shop2", "http", "http://api.shop.svc/2", 30, `,"namespace":"shop"`)); code != http.StatusUnprocessableEntity || msg != wantNS {
		t.Errorf("third in shop: status = %d, error = %q; want 422 %q", code, msg, wantNS)
	}
	if code, msg, _ := post(t, base, guardedMonitor("pay0", "http", "http://api.pay.svc/", 30, `,"namespace":"pay"`)); code != http.StatusCreated {
		t.Errorf("first in pay: %d %q", code, msg)
	}
	if code, msg, _ := post(t, base, guardedMonitor("none0", "http", "http://api.none.svc/", 30, "")); code != http.StatusCreated {
		t.Errorf("first without a namespace: %d %q", code, msg)
	}
	wantAll := "the limit of 4 monitors is reached (UPTIME_MAX_MONITORS); delete monitors or raise the limit"
	if code, msg, _ := post(t, base, guardedMonitor("pay1", "http", "http://api.pay.svc/1", 30, `,"namespace":"pay"`)); code != http.StatusUnprocessableEntity || msg != wantAll {
		t.Errorf("fifth: status = %d, error = %q; want 422 %q", code, msg, wantAll)
	}

	// Above the quota (monitors created before it was lowered, say), the
	// monitors there are untouched; copying one is making a new one.
	ctx := context.Background()
	extra, err := db.CreateMonitor(ctx, store.Monitor{Name: "extra", Type: store.MonitorTCP, Target: "db.pay.svc:5432", Interval: time.Minute, Timeout: time.Second, FailureThreshold: 1, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatInt(extra.ID, 10)
	if code := do(t, "PATCH", base+"/"+id, `{"interval_seconds":45,"target":"db2.pay.svc:5432"}`, nil); code != http.StatusOK {
		t.Errorf("changing a monitor above the quota: status = %d, want 200", code)
	}
	if code, msg, _ := post(t, base+"/"+id+"/clone", `{"name":"copy"}`); code != http.StatusUnprocessableEntity || msg != wantAll {
		t.Errorf("a copy above the quota: %d %q", code, msg)
	}
	var list []monitorResponse
	do(t, "GET", base, "", &list)
	if len(list) != 5 {
		t.Errorf("%d monitors, want 5", len(list))
	}
}

// An account creates at most MonitorCreatePerMinute monitors in a minute,
// then gets 429 with Retry-After; the first refusal of the minute is one
// audit entry, and another account is not affected.
func TestCreationRateLimit(t *testing.T) {
	srv, db := newTestAPIWith(t, func(o *Options) { o.MonitorCreatePerMinute = 2 })
	base := srv.URL + "/api/v1/monitors"
	editor := addUser(t, srv.URL, "editor", store.RoleEditor, "shop")
	token, _ := signIn(t, srv.URL, editor.Username, "parola-123")

	for i := 0; i < 2; i++ {
		if code, msg, _ := post(t, base, guardedMonitor("m"+strconv.Itoa(i), "http", "http://api.shop.svc/"+strconv.Itoa(i), 30, "")); code != http.StatusCreated {
			t.Fatalf("monitor %d: %d %q", i, code, msg)
		}
	}
	for i := 2; i < 4; i++ {
		code, msg, header := post(t, base, guardedMonitor("m"+strconv.Itoa(i), "http", "http://api.shop.svc/"+strconv.Itoa(i), 30, ""))
		if code != http.StatusTooManyRequests || !strings.Contains(msg, "UPTIME_MONITOR_CREATE_PER_MINUTE") || !strings.Contains(msg, "2 monitors") {
			t.Errorf("monitor %d: status = %d, error = %q; want 429 naming the limit", i, code, msg)
		}
		if after, err := strconv.Atoi(header.Get("Retry-After")); err != nil || after < 1 || after > 60 {
			t.Errorf("monitor %d: Retry-After = %q, want 1..60", i, header.Get("Retry-After"))
		}
	}
	// A refused creation is not a creation: it does not push the account
	// further into the limit. (Checked indirectly: the count stays 2.)
	audit, err := db.ListAudit(context.Background(), 50)
	if err != nil {
		t.Fatal(err)
	}
	throttled := 0
	for _, e := range audit {
		if e.Action == "monitor_create_throttled" {
			throttled++
			if e.Username != "api-token" || e.ObjectType != "monitor" || !strings.Contains(e.Summary, "2 monitors created within a minute") {
				t.Errorf("audit entry = %+v", e)
			}
		}
	}
	if throttled != 1 {
		t.Errorf("%d throttle entries in the audit log, want 1 for the minute", throttled)
	}

	// The editor's own minute.
	for i := 0; i < 2; i++ {
		if code := doAs(t, token, "POST", base, guardedMonitor("e"+strconv.Itoa(i), "http", "http://api.shop.svc/e"+strconv.Itoa(i), 30, `,"namespace":"shop"`), nil); code != http.StatusCreated {
			t.Errorf("editor's monitor %d: status = %d, want 201", i, code)
		}
	}
	if code := doAs(t, token, "POST", base, guardedMonitor("e2", "http", "http://api.shop.svc/e2", 30, `,"namespace":"shop"`), nil); code != http.StatusTooManyRequests {
		t.Errorf("editor's third: status = %d, want 429", code)
	}
	// An invalid request is refused before it counts, so does a refusal.
	if code := do(t, "POST", base, `{"name":"","type":"http","target":"http://x.shop.svc/","interval_seconds":30,"timeout_seconds":1}`, nil); code != http.StatusBadRequest {
		t.Errorf("invalid request: %d", code)
	}
}

// The limiter itself: the last sixty seconds per account, sliding, so that
// the clock's minute boundary lets no double burst through; a reservation
// released does not count; the first refusal of each burst is the one to
// audit; accounts without a recent creation are forgotten.
func TestCreateLimiter(t *testing.T) {
	l := newCreateLimiter(2)
	t0 := time.Date(2026, 10, 26, 12, 0, 10, 0, time.UTC)
	if ok, _, _ := l.allow("a", t0); !ok {
		t.Fatal("creation 1 refused")
	}
	if ok, _, _ := l.allow("a", t0.Add(30*time.Second)); !ok {
		t.Fatal("creation 2 refused")
	}
	ok, wait, audit := l.allow("a", t0.Add(40*time.Second))
	if ok || wait != 20*time.Second || !audit {
		t.Errorf("third: ok=%v wait=%s audit=%v; want refused, 20s until the oldest is a minute old, audit", ok, wait, audit)
	}
	if ok, _, audit := l.allow("a", t0.Add(59*time.Second)); ok || audit {
		t.Errorf("fourth: ok=%v audit=%v; want refused without a second audit entry", ok, audit)
	}
	if ok, _, _ := l.allow("b", t0); !ok {
		t.Error("another account was held to the first's minute")
	}
	// At +60s the first creation is a minute old and its place is free; the
	// one after it is refused again, and audited again: a new burst.
	if ok, _, _ := l.allow("a", t0.Add(60*time.Second)); !ok {
		t.Error("the window did not slide: no place at +60s")
	}
	if ok, wait, audit := l.allow("a", t0.Add(61*time.Second)); ok || wait != 29*time.Second || !audit {
		t.Errorf("after the window slid: ok=%v wait=%s audit=%v; want refused, 29s, audit", ok, wait, audit)
	}
	if _, kept := l.accounts["b"]; kept {
		t.Error("an account without a creation in the last minute was kept")
	}

	// The clock's minute boundary is no boundary: two creations at :58 and
	// :59 leave no place at :00.
	edge := time.Date(2026, 10, 26, 12, 0, 58, 0, time.UTC)
	l.allow("c", edge)
	l.allow("c", edge.Add(time.Second))
	if ok, _, _ := l.allow("c", edge.Add(2*time.Second)); ok {
		t.Error("the limit doubled across the clock's minute boundary")
	}

	// A reservation given back frees its place.
	l.allow("d", t0)
	l.allow("d", t0)
	if ok, _, _ := l.allow("d", t0); ok {
		t.Fatal("a third creation was allowed")
	}
	l.release("d", t0)
	if ok, _, _ := l.allow("d", t0); !ok {
		t.Error("a released reservation did not free its place")
	}
	l.release("d", t0.Add(time.Hour)) // nothing reserved then: a no-op

	off := newCreateLimiter(0)
	for i := 0; i < 100; i++ {
		if ok, _, _ := off.allow("a", t0); !ok {
			t.Fatal("a limit of zero refused a creation")
		}
	}
	off.release("a", t0)
}

// A creation that did not happen does not count: a name already taken
// (409) leaves the account's place free, so the next creation is not
// refused for it.
func TestConflictIsNotACreation(t *testing.T) {
	srv, _ := newTestAPIWith(t, func(o *Options) { o.MonitorCreatePerMinute = 2 })
	base := srv.URL + "/api/v1/monitors"
	if code, msg, _ := post(t, base, guardedMonitor("m0", "http", "http://api.shop.svc/0", 30, "")); code != http.StatusCreated {
		t.Fatalf("first: %d %q", code, msg)
	}
	if code, _, _ := post(t, base, guardedMonitor("m0", "http", "http://api.shop.svc/0", 30, "")); code != http.StatusConflict {
		t.Fatalf("the same name again: %d, want 409", code)
	}
	if code, msg, _ := post(t, base, guardedMonitor("m1", "http", "http://api.shop.svc/1", 30, "")); code != http.StatusCreated {
		t.Errorf("second creation after a conflict: %d %q; want 201, the conflict did not count", code, msg)
	}
	if code, _, _ := post(t, base, guardedMonitor("m2", "http", "http://api.shop.svc/2", 30, "")); code != http.StatusTooManyRequests {
		t.Errorf("third creation: %d, want 429", code)
	}
}

// A host that is a number in disguise (what inet_aton reads as an address)
// is refused: an address is written as one.
func TestNumericHostsAreRefused(t *testing.T) {
	srv, _ := newTestAPI(t)
	base := srv.URL + "/api/v1/monitors"
	for name, body := range map[string]string{
		"decimal":      guardedMonitor("a", "http", "http://2130706433/", 30, ""),
		"hex":          guardedMonitor("b", "http", "http://0x7f000001/", 30, ""),
		"short dotted": guardedMonitor("c", "tcp", "127.1:80", 30, ""),
		"hex dotted":   guardedMonitor("d", "ping", "0x7f.0.0.1", 30, ""),
		"three parts":  guardedMonitor("e", "dns", "1.2.3", 30, ""),
		"bare 0x":      guardedMonitor("f", "http", "http://0x/", 30, ""),
	} {
		if code, msg, _ := post(t, base, body); code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, error = %q; want 400", name, code, msg)
		}
	}
	for name, body := range map[string]string{
		"address":          guardedMonitor("ok1", "http", "http://203.0.113.1/", 30, ""),
		"name with digits": guardedMonitor("ok2", "http", "http://123.example.com/", 30, ""),
		"digits label":     guardedMonitor("ok3", "tcp", "1password.shop.svc:443", 30, ""),
	} {
		if code, msg, _ := post(t, base, body); code != http.StatusCreated {
			t.Errorf("%s: status = %d, error = %q; want 201", name, code, msg)
		}
	}
	for host, want := range map[string]bool{"2130706433": true, "0x7f000001": true, "127.1": true, "1.2.3": true, "0x7f.1": true, "0x": true,
		"example.com": false, "123.example.com": false, "1password": false, "0xcoffee.example": false, "": false} {
		if got := numericHost(host); got != want {
			t.Errorf("numericHost(%q) = %v, want %v", host, got, want)
		}
	}
}

// A monitor made before the external minimum (or by the syncer) keeps its
// interval through every change that leaves its target and intervals as
// they are: it can be renamed, tagged, paused, resumed and saved as it
// is. Shortening or keeping the interval while moving the target, or
// changing the interval at all, is held to the rule; and so is a copy,
// which is a new monitor.
func TestGrandfatheredExternalMonitor(t *testing.T) {
	srv, db := newTestAPIWith(t, func(o *Options) { o.MinCheckIntervalExternal = 30 * time.Second })
	base := srv.URL + "/api/v1/monitors"
	old, err := db.CreateMonitor(context.Background(), store.Monitor{
		Name: "fast", Type: store.MonitorHTTP, Target: "https://example.com/", Interval: 5 * time.Second, RetryInterval: 5 * time.Second,
		Timeout: time.Second, FailureThreshold: 1, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatInt(old.ID, 10)
	wantRule := "interval_seconds must be at least 30 for a target outside the cluster (UPTIME_MIN_CHECK_INTERVAL_EXTERNAL)"

	for _, change := range []struct{ name, body string }{ // in order: paused, then resumed
		{"rename", `{"name":"renamed"}`},
		{"tag", `{"tags":["prod"]}`},
		{"pause", `{"enabled":false}`},
		{"resume", `{"enabled":true}`},
		{"describe", `{"description":"kept"}`},
		{"save unchanged", `{"target":"https://example.com/","interval_seconds":5,"retry_interval_seconds":5,"timeout_seconds":1}`},
		{"longer timeout", `{"timeout_seconds":3}`},
		{"slow threshold", `{"slow_above_ms":500}`},
	} {
		if code, msg, _ := patch(t, base+"/"+id, change.body); code != http.StatusOK {
			t.Errorf("%s: status = %d, error = %q; want 200", change.name, code, msg)
		}
	}
	current, err := db.GetMonitor(context.Background(), old.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Interval != 5*time.Second || current.RetryInterval != 5*time.Second || !current.Enabled || current.Name != "renamed" {
		t.Errorf("after the changes: %+v; want the old pace kept, enabled, renamed", current)
	}
	for name, body := range map[string]string{
		"shorter interval":    `{"interval_seconds":4}`,
		"longer, still fast":  `{"interval_seconds":10}`,
		"retry only":          `{"retry_interval_seconds":4}`,
		"another host":        `{"target":"https://other.example/"}`,
		"same host, new path": `{"target":"https://example.com/healthz"}`, // a target change holds the rule, path or host
	} {
		if code, msg, _ := patch(t, base+"/"+id, body); code != http.StatusBadRequest || !strings.Contains(msg, "(UPTIME_MIN_CHECK_INTERVAL_EXTERNAL)") {
			t.Errorf("%s: status = %d, error = %q; want 400 naming the external rule", name, code, msg)
		}
	}
	// A retry interval changed on its own is refused by its own name, not
	// the kept interval's; turned off, it is less load, and allowed.
	wantRetry := "retry_interval_seconds must be 0 or at least 30 for a target outside the cluster (UPTIME_MIN_CHECK_INTERVAL_EXTERNAL)"
	if code, msg, _ := patch(t, base+"/"+id, `{"retry_interval_seconds":4}`); code != http.StatusBadRequest || msg != wantRetry {
		t.Errorf("retry only: status = %d, error = %q; want 400 %q", code, msg, wantRetry)
	}
	if code, msg, _ := patch(t, base+"/"+id, `{"retry_interval_seconds":0}`); code != http.StatusOK {
		t.Errorf("retry off: status = %d, error = %q; want 200", code, msg)
	}
	// A copy is a new monitor at the old pace: refused with the same sentence.
	if code, msg, _ := post(t, base+"/"+id+"/clone", `{"name":"copy"}`); code != http.StatusBadRequest || msg != wantRule {
		t.Errorf("clone: status = %d, error = %q; want 400 %q", code, msg, wantRule)
	}
	// Brought to the rule, it can be copied.
	if code := do(t, "PATCH", base+"/"+id, `{"interval_seconds":30,"retry_interval_seconds":30}`, nil); code != http.StatusOK {
		t.Fatalf("to 30s: %d", code)
	}
	if code, msg, _ := post(t, base+"/"+id+"/clone", `{"name":"copy"}`); code != http.StatusCreated {
		t.Errorf("clone at the rule: status = %d, error = %q; want 201", code, msg)
	}
}

// patch sends a PATCH with the service token and returns the status and
// the decoded error, if the answer was one.
func patch(t *testing.T, url, body string) (int, string, http.Header) {
	t.Helper()
	req, err := http.NewRequest("PATCH", url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var problem errorBody
	_ = json.Unmarshal(raw, &problem)
	return resp.StatusCode, problem.Error, resp.Header
}

// The quota on a namespace binds a monitor moved into it as it binds one
// made in it; moving within the quota, and changes that stay put, are
// fine.
func TestNamespaceQuotaOnMove(t *testing.T) {
	srv, _ := newTestAPIWith(t, func(o *Options) { o.MaxMonitorsPerNamespace = 2 })
	base := srv.URL + "/api/v1/monitors"
	for i := 0; i < 2; i++ {
		if code, msg, _ := post(t, base, guardedMonitor("q"+strconv.Itoa(i), "http", "http://api.q.svc/"+strconv.Itoa(i), 30, `,"namespace":"q"`)); code != http.StatusCreated {
			t.Fatalf("q %d: %d %q", i, code, msg)
		}
	}
	var m monitorResponse
	if code := do(t, "POST", base, guardedMonitor("p0", "http", "http://api.p.svc/", 30, `,"namespace":"p"`), &m); code != http.StatusCreated {
		t.Fatalf("p0: %d", code)
	}
	id := strconv.FormatInt(m.ID, 10)
	want := `the namespace "q" is at the limit of 2 monitors (UPTIME_MAX_MONITORS_PER_NAMESPACE)`
	if code, msg, _ := patch(t, base+"/"+id, `{"namespace":"q"}`); code != http.StatusUnprocessableEntity || msg != want {
		t.Errorf("moving into a full namespace: status = %d, error = %q; want 422 %q", code, msg, want)
	}
	if code, msg, _ := patch(t, base+"/"+id, `{"namespace":"r"}`); code != http.StatusOK {
		t.Errorf("moving into an empty namespace: status = %d, error = %q; want 200", code, msg)
	}
	if code, msg, _ := patch(t, base+"/"+id, `{"name":"p0-renamed","namespace":"r"}`); code != http.StatusOK {
		t.Errorf("a change that stays in its namespace: status = %d, error = %q; want 200", code, msg)
	}
}

// An import is held to the same rules, candidate by candidate: an external
// Route candidate to the external minimum, every candidate to the quotas,
// the cap per host and the account's creations a minute; what is held back
// is in the skipped list with the reason.
func TestImportIsHeldToTheGuardrails(t *testing.T) {
	candidates := shopCandidates()
	for i := 0; i < 3; i++ {
		candidates = append(candidates, k8s.Candidate{Kind: k8s.KindRoute, Namespace: "shop", Name: "r" + strconv.Itoa(i), Type: store.MonitorHTTP,
			Target: "https://shop.example.com/" + strconv.Itoa(i), Reason: k8s.ReasonRouteHost})
	}
	newAPI := func(t *testing.T, customize func(*Options)) string {
		t.Helper()
		srv, _ := newTestAPIWith(t, func(o *Options) {
			o.Discovery, o.DiscoveryStatus = &fakeDiscovery{candidates: candidates}, "in-cluster"
			customize(o)
		})
		return srv.URL + "/api/v1/discovery/import"
	}
	importAll := func(t *testing.T, url string, interval int) importResponse {
		t.Helper()
		var result importResponse
		body := fmt.Sprintf(`{"namespace":"shop","sources":["route/store","service/api:80","service/redis:6379","route/r0","route/r1","route/r2"],"interval_seconds":%d,"timeout_seconds":5}`, interval)
		if code := do(t, "POST", url, body, &result); code != http.StatusOK {
			t.Fatalf("import: status = %d", code)
		}
		return result
	}
	skipped := func(result importResponse) map[string]string {
		out := make(map[string]string)
		for _, s := range result.Skipped {
			out[s.Source] = s.Reason
		}
		return out
	}

	t.Run("external minimum", func(t *testing.T) {
		url := newAPI(t, func(o *Options) { o.MinCheckIntervalExternal = 30 * time.Second })
		result := importAll(t, url, 10)
		if len(result.Created) != 2 {
			t.Errorf("created %d, want the two in-cluster services", len(result.Created))
		}
		for _, source := range []string{"route/store", "route/r0", "route/r1", "route/r2"} {
			if reason := skipped(result)[source]; !strings.Contains(reason, "UPTIME_MIN_CHECK_INTERVAL_EXTERNAL") {
				t.Errorf("%s: reason = %q", source, reason)
			}
		}
	})
	t.Run("quota", func(t *testing.T) {
		url := newAPI(t, func(o *Options) { o.MaxMonitors = 3 })
		result := importAll(t, url, 30)
		if len(result.Created) != 3 || len(result.Skipped) != 3 {
			t.Fatalf("created %d, skipped %d; want 3 and 3", len(result.Created), len(result.Skipped))
		}
		for _, s := range result.Skipped {
			if !strings.Contains(s.Reason, "UPTIME_MAX_MONITORS)") {
				t.Errorf("%s: reason = %q", s.Source, s.Reason)
			}
		}
	})
	t.Run("namespace quota", func(t *testing.T) {
		url := newAPI(t, func(o *Options) { o.MaxMonitorsPerNamespace = 2 })
		result := importAll(t, url, 30)
		if len(result.Created) != 2 || !strings.Contains(skipped(result)["service/redis:6379"], "UPTIME_MAX_MONITORS_PER_NAMESPACE") {
			t.Errorf("created %d, skipped %v", len(result.Created), skipped(result))
		}
	})
	t.Run("per host", func(t *testing.T) {
		url := newAPI(t, func(o *Options) { o.MaxMonitorsPerHost = 2 })
		result := importAll(t, url, 30)
		if len(result.Created) != 5 || !strings.Contains(skipped(result)["route/r2"], "shop.example.com (UPTIME_MAX_MONITORS_PER_HOST)") {
			t.Errorf("created %d, skipped %v", len(result.Created), skipped(result))
		}
	})
	t.Run("creations a minute", func(t *testing.T) {
		url := newAPI(t, func(o *Options) { o.MonitorCreatePerMinute = 4 })
		result := importAll(t, url, 30)
		if len(result.Created) != 4 || len(result.Skipped) != 2 {
			t.Fatalf("created %d, skipped %d; want 4 and 2", len(result.Created), len(result.Skipped))
		}
		for _, s := range result.Skipped {
			if !strings.Contains(s.Reason, "UPTIME_MONITOR_CREATE_PER_MINUTE") {
				t.Errorf("%s: reason = %q", s.Source, s.Reason)
			}
		}
	})
}

// The health endpoint tells the UI the shortest intervals, so that a form
// can say so before the server refuses; no separate external rule is 0.
func TestHealthTellsTheIntervalLimits(t *testing.T) {
	srv, _ := newTestAPIWith(t, func(o *Options) { o.MinCheckIntervalExternal = 30 * time.Second })
	var got Health
	do(t, "GET", srv.URL+"/api/v1/health", "", &got)
	if got.Limits.MinCheckIntervalSeconds != 5 || got.Limits.MinCheckIntervalExternalSeconds != 30 {
		t.Errorf("limits = %+v, want 5 and 30", got.Limits)
	}
	srv, _ = newTestAPI(t)
	do(t, "GET", srv.URL+"/api/v1/health", "", &got)
	if got.Limits.MinCheckIntervalSeconds != 5 || got.Limits.MinCheckIntervalExternalSeconds != 0 {
		t.Errorf("without an external rule: limits = %+v, want 5 and 0", got.Limits)
	}
}

type fakeThrottles map[int64]time.Time

func (f fakeThrottles) ThrottledSince(id int64) time.Time { return f[id] }

// A monitor whose check waits for its host's ceiling says since when, in
// its page and in the list; the others say null.
func TestThrottledSinceInResponses(t *testing.T) {
	since := time.Date(2026, 10, 26, 9, 30, 0, 0, time.UTC)
	throttles := fakeThrottles{}
	srv, _ := newTestAPIWith(t, func(o *Options) { o.Throttles = throttles })
	base := srv.URL + "/api/v1/monitors"

	var waiting, free monitorResponse
	do(t, "POST", base, guardedMonitor("waiting", "http", "https://example.com/a", 30, ""), &waiting)
	do(t, "POST", base, guardedMonitor("free", "http", "https://example.com/b", 30, ""), &free)
	throttles[waiting.ID] = since

	var got monitorResponse
	do(t, "GET", base+"/"+strconv.FormatInt(waiting.ID, 10), "", &got)
	if got.ThrottledSince == nil || !got.ThrottledSince.Equal(since) {
		t.Errorf("waiting: throttled_since = %v, want %v", got.ThrottledSince, since)
	}
	var raw []map[string]json.RawMessage
	do(t, "GET", base, "", &raw)
	for _, m := range raw {
		var id int64
		_ = json.Unmarshal(m["id"], &id)
		field, present := m["throttled_since"]
		switch {
		case !present:
			t.Errorf("monitor %d: throttled_since is missing", id)
		case id == free.ID && string(field) != "null":
			t.Errorf("free: throttled_since = %s, want null", field)
		case id == waiting.ID && string(field) == "null":
			t.Error("waiting: throttled_since is null in the list")
		}
	}
	// A paused monitor is not waiting for anything.
	if code := do(t, "PATCH", base+"/"+strconv.FormatInt(waiting.ID, 10), `{"enabled":false}`, &got); code != http.StatusOK || got.ThrottledSince != nil {
		t.Errorf("paused: status = %d, throttled_since = %v", code, got.ThrottledSince)
	}
}
