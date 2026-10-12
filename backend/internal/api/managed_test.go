package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/discovery"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

type fakeSync struct{ status discovery.Status }

func (f fakeSync) Status() discovery.Status { return f.status }

func TestManagedMonitorsFollowTheirAnnotations(t *testing.T) {
	ran := time.Date(2026, 10, 12, 20, 0, 0, 0, time.UTC)
	srv, db := newTestAPIWith(t, func(o *Options) {
		o.Sync = fakeSync{status: discovery.Status{IntervalSeconds: 60, LastRunAt: ran, Managed: 1, Skipped: []discovery.Skip{}}}
	})
	m, err := db.CreateMonitor(context.Background(), store.Monitor{
		Name: "shop/service/api:80", Type: store.MonitorHTTP, Target: "http://api.shop.svc/healthz",
		Namespace: "shop", Source: "service/api:80", Managed: true, Enabled: true,
		Interval: 30 * time.Second, Timeout: 5 * time.Second, FailureThreshold: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	url := srv.URL + "/api/v1/monitors/" + itoa(m.ID)

	var got monitorResponse
	if code := do(t, http.MethodGet, url, "", &got); code != http.StatusOK || !got.Managed {
		t.Fatalf("GET = %d managed=%v", code, got.Managed)
	}

	// What discovery owns cannot be changed here; the rest can.
	var problem errorBody
	if code := do(t, http.MethodPatch, url, `{"target":"http://elsewhere/"}`, &problem); code != http.StatusConflict {
		t.Errorf("PATCH target = %d %s, want 409", code, problem.Error)
	}
	if code := do(t, http.MethodPatch, url, `{"interval_seconds":10}`, &problem); code != http.StatusConflict {
		t.Errorf("PATCH interval = %d, want 409", code)
	}
	if code := do(t, http.MethodPatch, url, `{"interval_seconds":30,"tags":["prod"],"description":"ours","enabled":false}`, &got); code != http.StatusOK || !got.Managed || got.Enabled || len(got.Tags) != 1 {
		t.Errorf("PATCH tags = %d %+v", code, got)
	}
	if code := do(t, http.MethodDelete, url, "", &problem); code != http.StatusConflict {
		t.Errorf("DELETE = %d, want 409", code)
	}

	var status discoveryStatusResponse
	if code := do(t, http.MethodGet, srv.URL+"/api/v1/discovery", "", &status); code != http.StatusOK || status.Sync == nil || status.Sync.Managed != 1 || !status.Sync.LastRunAt.Equal(ran) {
		t.Errorf("discovery status = %d %+v", code, status)
	}
}

func TestManagedMonitorsAreOrdinaryWithoutTheSyncer(t *testing.T) {
	srv, db := newTestAPI(t)
	m, err := db.CreateMonitor(context.Background(), store.Monitor{
		Name: "shop/service/api:80", Type: store.MonitorTCP, Target: "api.shop.svc:80",
		Namespace: "shop", Source: "service/api:80", Managed: true, Enabled: true,
		Interval: 30 * time.Second, Timeout: 5 * time.Second, FailureThreshold: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	var status discoveryStatusResponse
	if code := do(t, http.MethodGet, srv.URL+"/api/v1/discovery", "", &status); code != http.StatusOK || status.Sync != nil {
		t.Errorf("discovery status = %d %+v, want no sync", code, status)
	}
	if code := do(t, http.MethodDelete, srv.URL+"/api/v1/monitors/"+itoa(m.ID), "", nil); code != http.StatusNoContent {
		t.Errorf("DELETE = %d, want 204", code)
	}
}
