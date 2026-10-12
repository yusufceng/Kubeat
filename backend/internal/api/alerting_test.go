package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

func TestFailureThreshold(t *testing.T) {
	srv, _ := newTestAPI(t)
	base := srv.URL + "/api/v1/monitors"

	var m monitorResponse
	do(t, "POST", base, validMonitor, &m)
	if m.FailureThreshold != 3 {
		t.Errorf("default failure_threshold = %d, want 3", m.FailureThreshold)
	}

	custom := `{"name":"b","type":"http","target":"http://x","interval_seconds":30,"timeout_seconds":5,"failure_threshold":5}`
	if code := do(t, "POST", base, custom, &m); code != http.StatusCreated || m.FailureThreshold != 5 {
		t.Errorf("custom threshold: status=%d failure_threshold=%d, want 201 and 5", code, m.FailureThreshold)
	}

	negative := `{"name":"c","type":"http","target":"http://x","interval_seconds":30,"timeout_seconds":5,"failure_threshold":-1}`
	if code := do(t, "POST", base, negative, nil); code != http.StatusBadRequest {
		t.Errorf("negative threshold: status = %d, want 400", code)
	}
}

func TestIncidentsEndpoint(t *testing.T) {
	srv, db := newTestAPI(t)
	ctx := context.Background()
	var api, web monitorResponse
	do(t, "POST", srv.URL+"/api/v1/monitors", validMonitor, &api)
	do(t, "POST", srv.URL+"/api/v1/monitors", strings.Replace(validMonitor, `"api"`, `"web"`, 1), &web)

	var list []incidentResponse
	if code := do(t, "GET", srv.URL+"/api/v1/incidents", "", &list); code != http.StatusOK || list == nil || len(list) != 0 {
		t.Fatalf("empty list: status=%d body=%v (want 200 and [])", code, list)
	}

	base := time.Now().UTC().Truncate(time.Millisecond).Add(-time.Hour)
	first, err := db.OpenIncident(ctx, store.Incident{MonitorID: api.ID, StartedAt: base, Cause: "timeout"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ResolveIncident(ctx, first.ID, base.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.OpenIncident(ctx, store.Incident{MonitorID: web.ID, StartedAt: base.Add(30 * time.Minute), Cause: "refused"}); err != nil {
		t.Fatal(err)
	}

	do(t, "GET", srv.URL+"/api/v1/incidents", "", &list)
	if len(list) != 2 || list[0].MonitorName != "web" || list[0].ResolvedAt != nil || list[0].Cause != "refused" {
		t.Fatalf("list = %+v, want open web incident first", list)
	}
	if list[1].ResolvedAt == nil || !list[1].ResolvedAt.Equal(base.Add(10*time.Minute)) || !list[1].StartedAt.Equal(base) {
		t.Errorf("resolved incident = %+v", list[1])
	}

	if do(t, "GET", srv.URL+"/api/v1/incidents?monitor_id=1", "", &list); len(list) != 1 || list[0].MonitorID != api.ID {
		t.Errorf("filtered list = %+v", list)
	}
	if do(t, "GET", srv.URL+"/api/v1/incidents?limit=1", "", &list); len(list) != 1 {
		t.Errorf("limit=1 returned %d incidents", len(list))
	}
	for _, q := range []string{"monitor_id=abc", "monitor_id=0", "limit=0"} {
		if code := do(t, "GET", srv.URL+"/api/v1/incidents?"+q, "", nil); code != http.StatusBadRequest {
			t.Errorf("?%s: status = %d, want 400", q, code)
		}
	}

	// The monitor itself reports its open incident.
	var m monitorResponse
	if do(t, "GET", srv.URL+"/api/v1/monitors/2", "", &m); m.OpenIncident == nil || m.OpenIncident.Cause != "refused" {
		t.Errorf("monitor with open incident: %+v", m.OpenIncident)
	}
	if do(t, "GET", srv.URL+"/api/v1/monitors/1", "", &m); m.OpenIncident != nil {
		t.Errorf("monitor with resolved incident still reports one: %+v", m.OpenIncident)
	}
}

func TestChannelsEndpoint(t *testing.T) {
	srv, _ := newTestAPI(t)
	base := srv.URL + "/api/v1/channels"

	var hits atomic.Int32
	status := http.StatusOK
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(status)
	}))
	defer hook.Close()
	secretURL := hook.URL + "/services/T000/SECRET-TOKEN"

	var list []channelResponse
	if code := do(t, "GET", base, "", &list); code != http.StatusOK || list == nil || len(list) != 0 {
		t.Fatalf("empty list: status=%d body=%v", code, list)
	}

	var created channelResponse
	body := `{"name":" ops ","type":"webhook","target":"` + secretURL + `"}`
	if code := do(t, "POST", base, body, &created); code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", code)
	}
	if created.Name != "ops" || !created.Enabled || strings.Contains(created.Target, "SECRET") || !strings.HasSuffix(created.Target, "/…") {
		t.Errorf("created = %+v (URL must be masked)", created)
	}
	if do(t, "GET", base, "", &list); len(list) != 1 || strings.Contains(list[0].Target, "SECRET") {
		t.Errorf("list leaks the webhook URL: %+v", list)
	}

	if code := do(t, "POST", base, body, nil); code != http.StatusConflict {
		t.Errorf("duplicate name: status = %d, want 409", code)
	}
	for name, bad := range map[string]string{
		"empty name":      `{"name":"","type":"webhook","target":"https://x"}`,
		"wrong type":      `{"name":"a","type":"pagerduty","target":"https://x"}`,
		"not a URL":       `{"name":"a","type":"webhook","target":"hooks.example.com"}`,
		"slack not a URL": `{"name":"a","type":"slack","target":"#ops"}`,
		"unknown key":     `{"name":"a","type":"webhook","target":"https://x","extra":1}`,
		// The test API has no SMTP server configured.
		"email without smtp": `{"name":"a","type":"email","target":"ops@example.com"}`,
	} {
		if code := do(t, "POST", base, bad, nil); code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, code)
		}
	}

	var result testChannelResponse
	if code := do(t, "POST", base+"/1/test", "", &result); code != http.StatusOK || !result.Delivered || result.Error != "" {
		t.Errorf("test delivery: status=%d result=%+v", code, result)
	}
	if hits.Load() != 1 {
		t.Errorf("webhook received %d requests, want 1", hits.Load())
	}

	status = http.StatusForbidden
	result = testChannelResponse{}
	if do(t, "POST", base+"/1/test", "", &result); result.Delivered || !strings.Contains(result.Error, "403") {
		t.Errorf("rejected test delivery: %+v", result)
	}

	hook.Close() // now unreachable: the transport error embeds the URL
	result = testChannelResponse{}
	do(t, "POST", base+"/1/test", "", &result)
	if result.Delivered || result.Error == "" || strings.Contains(result.Error, "SECRET") {
		t.Errorf("unreachable webhook: %+v (error must not leak the URL)", result)
	}

	if code := do(t, "POST", base+"/999/test", "", nil); code != http.StatusNotFound {
		t.Errorf("test unknown channel: status = %d, want 404", code)
	}
	if code := do(t, "DELETE", base+"/1", "", nil); code != http.StatusNoContent {
		t.Errorf("delete status = %d, want 204", code)
	}
	if code := do(t, "DELETE", base+"/1", "", nil); code != http.StatusNotFound {
		t.Errorf("second delete status = %d, want 404", code)
	}
}

func TestChannelTypes(t *testing.T) {
	db, err := store.OpenSQLite(context.Background(), filepath.Join(t.TempDir(), "test.db"), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewHandler(Options{
		Store: db, EmailEnabled: true, MaxResults: 100,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}))
	t.Cleanup(func() {
		srv.Close()
		db.Close()
	})
	base := srv.URL + "/api/v1/channels"

	var c channelResponse
	for _, typ := range []string{"slack", "teams"} {
		body := `{"name":"` + typ + `","type":"` + typ + `","target":"https://hooks.example.com/services/SECRET"}`
		if code := do(t, "POST", base, body, &c); code != http.StatusCreated || c.Type != typ || strings.Contains(c.Target, "SECRET") {
			t.Errorf("%s channel: status=%d %+v", typ, code, c)
		}
	}

	// Addresses are normalized and, unlike URLs, shown as they are.
	body := `{"name":"mail","type":"email","target":" Ops <ops@example.com> ,oncall@example.com "}`
	if code := do(t, "POST", base, body, &c); code != http.StatusCreated || c.Target != "ops@example.com, oncall@example.com" {
		t.Errorf("email channel: status=%d target=%q", code, c.Target)
	}

	many := strings.Repeat("a@example.com,", maxRecipients+1)
	for name, target := range map[string]string{"invalid address": "ops@", "empty": " ", "too many": many} {
		body := `{"name":"x","type":"email","target":"` + target + `"}`
		if code := do(t, "POST", base, body, nil); code != http.StatusBadRequest {
			t.Errorf("email with %s: status = %d, want 400", name, code)
		}
	}

	var list []channelResponse
	if do(t, "GET", base, "", &list); len(list) != 3 {
		t.Errorf("channels = %+v", list)
	}
}
