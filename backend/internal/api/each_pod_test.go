package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

func TestEachPodNeedsAServicePort(t *testing.T) {
	srv, db := newTestAPI(t)
	ctx := context.Background()

	// By hand: there is no service behind it.
	var problem errorBody
	code := do(t, http.MethodPost, srv.URL+"/api/v1/monitors",
		`{"name":"x","type":"http","target":"http://x/","interval_seconds":30,"timeout_seconds":5,"each_pod":true}`, &problem)
	if code != http.StatusBadRequest || problem.Error == "" {
		t.Errorf("create by hand = %d %s, want 400", code, problem.Error)
	}

	fromService, err := db.CreateMonitor(ctx, store.Monitor{
		Name: "shop/service/api:80", Type: store.MonitorHTTP, Target: "http://api.shop.svc/healthz", Namespace: "shop", Source: "service/api:80",
		Interval: 30 * time.Second, Timeout: 5 * time.Second, FailureThreshold: 3, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var got monitorResponse
	if code := do(t, http.MethodPatch, srv.URL+"/api/v1/monitors/"+itoa(fromService.ID), `{"each_pod":true}`, &got); code != http.StatusOK || !got.EachPod {
		t.Errorf("enable on a service monitor = %d each_pod=%v", code, got.EachPod)
	}
	if code := do(t, http.MethodGet, srv.URL+"/api/v1/monitors/"+itoa(fromService.ID), "", &got); code != http.StatusOK || !got.EachPod {
		t.Errorf("read back = %d each_pod=%v", code, got.EachPod)
	}

	fromWorkload, err := db.CreateMonitor(ctx, store.Monitor{
		Name: "shop/deployment/api", Type: store.MonitorWorkload, Target: "shop/deployment/api", Namespace: "shop", Source: "deployment/api",
		Interval: 30 * time.Second, Timeout: 5 * time.Second, FailureThreshold: 3, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if code := do(t, http.MethodPatch, srv.URL+"/api/v1/monitors/"+itoa(fromWorkload.ID), `{"each_pod":true}`, &problem); code != http.StatusBadRequest {
		t.Errorf("enable on a workload monitor = %d, want 400", code)
	}
}
