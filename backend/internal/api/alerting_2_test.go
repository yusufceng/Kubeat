package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

func TestChannelDelayAndRepeat(t *testing.T) {
	srv, db := newTestAPI(t)
	base := srv.URL + "/api/v1/channels"

	var ch channelResponse
	code := do(t, http.MethodPost, base, `{"name":"pager","type":"pagerduty","secret":"key","delay_seconds":600,"repeat_seconds":3600}`, &ch)
	if code != http.StatusCreated || ch.DelaySeconds != 600 || ch.RepeatSeconds != 3600 {
		t.Fatalf("create = %d %+v", code, ch)
	}
	stored, err := db.GetChannel(context.Background(), ch.ID)
	if err != nil || stored.Delay != 10*time.Minute || stored.Repeat != time.Hour {
		t.Errorf("stored = %+v, %v", stored, err)
	}
	var plain channelResponse
	if do(t, http.MethodPost, base, `{"name":"chat","type":"slack","target":"https://hooks.example.com/x"}`, &plain); plain.DelaySeconds != 0 || plain.RepeatSeconds != 0 {
		t.Errorf("defaults = %+v", plain)
	}

	for name, body := range map[string]string{
		"negative delay":   `{"name":"x","type":"slack","target":"https://hooks.example.com/x","delay_seconds":-1}`,
		"delay too long":   `{"name":"x","type":"slack","target":"https://hooks.example.com/x","delay_seconds":90000}`,
		"repeat too short": `{"name":"x","type":"slack","target":"https://hooks.example.com/x","repeat_seconds":30}`,
		"repeat too long":  `{"name":"x","type":"slack","target":"https://hooks.example.com/x","repeat_seconds":700000}`,
	} {
		var problem errorBody
		if code := do(t, http.MethodPost, base, body, &problem); code != http.StatusBadRequest {
			t.Errorf("%s: status = %d (%s), want 400", name, code, problem.Error)
		}
	}

	// One may change without the other; the same bounds apply.
	if code := do(t, http.MethodPatch, base+"/"+itoa(ch.ID), `{"repeat_seconds":0}`, &ch); code != http.StatusOK || ch.DelaySeconds != 600 || ch.RepeatSeconds != 0 {
		t.Errorf("update = %d %+v", code, ch)
	}
	if code := do(t, http.MethodPatch, base+"/"+itoa(ch.ID), `{"delay_seconds":100000}`, nil); code != http.StatusBadRequest {
		t.Errorf("update out of bounds: status = %d, want 400", code)
	}

	// Where a monitor's alarm goes says how each channel times it.
	m, err := db.CreateMonitor(context.Background(), store.Monitor{Name: "api", Type: store.MonitorTCP, Target: "a:1",
		Interval: time.Minute, Timeout: time.Second, FailureThreshold: 1, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	var where []monitorChannelResponse
	if code := do(t, http.MethodGet, srv.URL+"/api/v1/monitors/"+itoa(m.ID)+"/channels", "", &where); code != http.StatusOK || len(where) != 2 {
		t.Fatalf("monitor channels = %d %+v", code, where)
	}
	if where[0].Name != "pager" || where[0].DelaySeconds != 600 || where[1].DelaySeconds != 0 {
		t.Errorf("monitor channels = %+v", where)
	}
}

// hook is a webhook endpoint that remembers the bodies it was sent and
// answers with a status of the test's choosing.
type hook struct {
	*httptest.Server
	mu     sync.Mutex
	bodies []map[string]any
	status int
}

func newHook(t *testing.T) *hook {
	t.Helper()
	h := &hook{status: http.StatusOK}
	h.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		h.mu.Lock()
		h.bodies = append(h.bodies, body)
		status := h.status
		h.mu.Unlock()
		w.WriteHeader(status)
	}))
	t.Cleanup(h.Close)
	return h
}

func (h *hook) texts() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, len(h.bodies))
	for i, b := range h.bodies {
		out[i], _ = b["text"].(string)
	}
	return out
}

func TestMonitorTestNotification(t *testing.T) {
	srv, db := newTestAPI(t)
	base := srv.URL
	ctx := context.Background()
	shop, err := db.CreateMonitor(ctx, store.Monitor{Name: "cart", Type: store.MonitorTCP, Target: "cart:80", Namespace: "shop",
		Interval: time.Minute, Timeout: time.Second, FailureThreshold: 1, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	test := func(token string, id int64, out any) int {
		return doAs(t, token, http.MethodPost, base+"/api/v1/monitors/"+itoa(id)+"/test-notification", "", out)
	}

	// Nobody would hear it.
	var problem errorBody
	if code := doAsError(t, testToken, http.MethodPost, base+"/api/v1/monitors/"+itoa(shop.ID)+"/test-notification", "", &problem); code != http.StatusUnprocessableEntity {
		t.Fatalf("without channels: status = %d (%s), want 422", code, problem.Error)
	}
	if code := test(testToken, 999, nil); code != http.StatusNotFound {
		t.Errorf("unknown monitor: status = %d, want 404", code)
	}

	// Two channels hear it: one for everyone, one the shop team's. A third
	// is outside the monitor's scope, a fourth disabled.
	everyone, team := newHook(t), newHook(t)
	for _, c := range []store.Channel{
		{Name: "everyone", Type: store.ChannelWebhook, Target: everyone.URL, Enabled: true},
		{Name: "shop-team", Type: store.ChannelSlack, Target: team.URL + "/services/T0/SECRET", Namespaces: []string{"shop"}, Enabled: true},
		{Name: "billing", Type: store.ChannelWebhook, Target: everyone.URL, Namespaces: []string{"billing"}, Enabled: true},
		{Name: "off", Type: store.ChannelWebhook, Target: everyone.URL, Enabled: false},
	} {
		if _, err := db.CreateChannel(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	var results []monitorTestResponse
	if code := test(testToken, shop.ID, &results); code != http.StatusOK || len(results) != 2 {
		t.Fatalf("test: status = %d, results = %+v", code, results)
	}
	for _, r := range results {
		if !r.Delivered || r.Error != "" || r.ChannelID == 0 {
			t.Errorf("result = %+v", r)
		}
	}
	if results[0].Name != "everyone" || results[1].Name != "shop-team" || results[1].Type != "slack" {
		t.Errorf("results = %+v", results)
	}
	// The message names the monitor.
	if texts := everyone.texts(); len(texts) != 1 || !strings.Contains(texts[0], "cart") || !strings.Contains(texts[0], "test") {
		t.Errorf("webhook got %q", texts)
	}
	if texts := team.texts(); len(texts) != 1 || !strings.Contains(texts[0], "cart") {
		t.Errorf("slack got %q", texts)
	}
	everyone.mu.Lock()
	monitor, _ := everyone.bodies[0]["monitor"].(map[string]any)
	everyone.mu.Unlock()
	if monitor == nil || monitor["name"] != "cart" || everyone.bodies[0]["event"] != "test" {
		t.Errorf("webhook body = %v", everyone.bodies[0])
	}

	// A failure is reported, with its reason only to whoever may manage
	// the channel: the shop editor manages the shop team's channel, not
	// the one for everyone.
	everyone.mu.Lock()
	everyone.status = http.StatusForbidden
	everyone.mu.Unlock()
	team.Close() // unreachable: the transport error carries the URL
	addUser(t, base, "dev", store.RoleEditor, "shop")
	addUser(t, base, "reader", store.RoleViewer, "shop")
	dev, _ := signIn(t, base, "dev", "parola-123")
	reader, _ := signIn(t, base, "reader", "parola-123")

	results = nil
	if code := test(dev, shop.ID, &results); code != http.StatusOK || len(results) != 2 {
		t.Fatalf("editor's test: status = %d, results = %+v", code, results)
	}
	if r := results[0]; r.Delivered || r.Error != "" {
		t.Errorf("the channel for everyone, to an editor: %+v (no reason)", r)
	}
	if r := results[1]; r.Delivered || r.Error == "" || strings.Contains(r.Error, "SECRET") {
		t.Errorf("the team's channel, to its editor: %+v (a redacted reason)", r)
	}
	results = nil
	if code := test(testToken, shop.ID, &results); code != http.StatusOK || results[0].Error == "" || !strings.Contains(results[0].Error, "403") {
		t.Errorf("the channel for everyone, to an admin: %d %+v", code, results)
	}
	// The channel's own status shows the attempt.
	if ch, _ := db.GetChannel(ctx, 1); ch.LastOK || ch.LastAttemptAt.IsZero() {
		t.Errorf("channel status after a failed test = %+v", ch)
	}

	// Viewers may not; editors may not outside their namespaces.
	if code := test(reader, shop.ID, nil); code != http.StatusForbidden {
		t.Errorf("viewer: status = %d, want 403", code)
	}
	other, err := db.CreateMonitor(ctx, store.Monitor{Name: "ledger", Type: store.MonitorTCP, Target: "l:80", Namespace: "billing",
		Interval: time.Minute, Timeout: time.Second, FailureThreshold: 1, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if code := test(dev, other.ID, nil); code != http.StatusNotFound {
		t.Errorf("editor outside the namespace: status = %d, want 404", code)
	}

	// Audited, with the channels it went to.
	audit, err := db.ListAudit(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	var tests []store.AuditEntry
	for _, a := range audit {
		if a.Action == "test" {
			tests = append(tests, a)
		}
	}
	if len(tests) != 3 || tests[0].ObjectType != "monitor" || tests[0].ObjectID != shop.ID || !strings.Contains(tests[0].Summary, "everyone, shop-team") || tests[1].Username != "dev" {
		t.Errorf("audit = %+v", tests)
	}
}

func TestSlowThresholdOption(t *testing.T) {
	srv, db := newTestAPI(t)
	base := srv.URL + "/api/v1/monitors"
	ctx := context.Background()

	var m monitorResponse
	body := `{"name":"api","type":"http","target":"https://example.com","interval_seconds":30,"timeout_seconds":5,"slow_above_ms":1500}`
	if code := do(t, http.MethodPost, base, body, &m); code != http.StatusCreated || m.SlowAboveMS != 1500 || m.SlowSince != nil || m.Status != statusPending {
		t.Fatalf("create = %d %+v", code, m)
	}
	for name, bad := range map[string]string{
		"negative":          `{"name":"b","type":"http","target":"https://example.com","interval_seconds":30,"timeout_seconds":5,"slow_above_ms":-1}`,
		"not below timeout": `{"name":"b","type":"http","target":"https://example.com","interval_seconds":30,"timeout_seconds":5,"slow_above_ms":5000}`,
	} {
		if code := do(t, http.MethodPost, base, bad, nil); code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, code)
		}
	}
	if code := do(t, http.MethodPatch, base+"/"+itoa(m.ID), `{"slow_above_ms":800}`, &m); code != http.StatusOK || m.SlowAboveMS != 800 {
		t.Errorf("update = %d %+v", code, m)
	}
	if code := do(t, http.MethodPatch, base+"/"+itoa(m.ID), `{"timeout_seconds":1,"slow_above_ms":1000}`, nil); code != http.StatusBadRequest {
		t.Errorf("update with a threshold at the timeout: status = %d, want 400", code)
	}

	// The engine's state shows as "degraded" while the monitor answers.
	now := time.Now().UTC().Truncate(time.Millisecond)
	if err := db.InsertCheckResults(ctx, []store.CheckResult{{MonitorID: m.ID, CheckedAt: now, Up: true, Latency: 900 * time.Millisecond}}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetMonitorSlowSince(ctx, m.ID, now); err != nil {
		t.Fatal(err)
	}
	if do(t, http.MethodGet, base+"/"+itoa(m.ID), "", &m); m.Status != statusDegraded || m.SlowSince == nil || !m.SlowSince.Equal(now) {
		t.Errorf("slow monitor = status %q, slow_since %v", m.Status, m.SlowSince)
	}
	var list []monitorResponse
	if do(t, http.MethodGet, base, "", &list); len(list) != 1 || list[0].Status != statusDegraded {
		t.Errorf("list = %+v", list)
	}
	// A failing monitor is down, slow or not; a paused one is paused.
	if err := db.InsertCheckResults(ctx, []store.CheckResult{{MonitorID: m.ID, CheckedAt: now.Add(time.Second), Up: false, Error: "refused"}}); err != nil {
		t.Fatal(err)
	}
	if do(t, http.MethodGet, base+"/"+itoa(m.ID), "", &m); m.Status != statusDown || m.SlowSince == nil {
		t.Errorf("failing slow monitor = status %q, slow_since %v", m.Status, m.SlowSince)
	}
	if do(t, http.MethodPatch, base+"/"+itoa(m.ID), `{"enabled":false}`, &m); m.Status != statusPaused || m.SlowSince != nil {
		t.Errorf("paused = status %q, slow_since %v (cleared on pause)", m.Status, m.SlowSince)
	}
	// Turning the threshold off clears the state too.
	if err := db.SetMonitorSlowSince(ctx, m.ID, now); err != nil {
		t.Fatal(err)
	}
	if do(t, http.MethodPatch, base+"/"+itoa(m.ID), `{"slow_above_ms":0}`, &m); m.SlowAboveMS != 0 || m.SlowSince != nil {
		t.Errorf("threshold off = %+v", m)
	}
}

func TestStatsForOneMonitor(t *testing.T) {
	srv, db := newTestAPI(t)
	base := srv.URL + "/api/v1/stats"
	ctx := context.Background()
	var ids []int64
	for _, ns := range []string{"shop", "billing"} {
		m, err := db.CreateMonitor(ctx, store.Monitor{Name: ns + "-api", Type: store.MonitorTCP, Target: "a:1", Namespace: ns,
			Interval: time.Minute, Timeout: time.Second, FailureThreshold: 1, Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, m.ID)
	}
	if _, err := db.OpenIncident(ctx, store.Incident{MonitorID: ids[1], StartedAt: time.Now().Add(-time.Hour), Cause: "refused"}); err != nil {
		t.Fatal(err)
	}

	var all, one statsResponse
	if code := do(t, http.MethodGet, base, "", &all); code != http.StatusOK || all.Monitors != 2 || all.OpenOutages != 1 {
		t.Fatalf("all = %d %+v", code, all)
	}
	if code := do(t, http.MethodGet, base+"?monitor_id="+itoa(ids[0]), "", &one); code != http.StatusOK || one.Monitors != 1 || one.OpenOutages != 0 {
		t.Errorf("shop's monitor = %d %+v", code, one)
	}
	if code := do(t, http.MethodGet, base+"?monitor_id="+itoa(ids[1]), "", &one); code != http.StatusOK || one.Monitors != 1 || one.OpenOutages != 1 || one.OpenIncidents != 1 {
		t.Errorf("billing's monitor = %d %+v", code, one)
	}
	if code := do(t, http.MethodGet, base+"?monitor_id=999", "", nil); code != http.StatusNotFound {
		t.Errorf("unknown monitor: status = %d, want 404", code)
	}
	if code := do(t, http.MethodGet, base+"?monitor_id=x", "", nil); code != http.StatusBadRequest {
		t.Errorf("bad id: status = %d, want 400", code)
	}
	// Outside a user's namespaces the monitor is not there.
	addUser(t, srv.URL, "dev", store.RoleEditor, "shop")
	dev, _ := signIn(t, srv.URL, "dev", "parola-123")
	if code := doAs(t, dev, http.MethodGet, base+"?monitor_id="+itoa(ids[1]), "", nil); code != http.StatusNotFound {
		t.Errorf("another namespace's monitor: status = %d, want 404", code)
	}
	if code := doAs(t, dev, http.MethodGet, base+"?monitor_id="+itoa(ids[0]), "", &one); code != http.StatusOK || one.Monitors != 1 {
		t.Errorf("own monitor: %d %+v", code, one)
	}
}
