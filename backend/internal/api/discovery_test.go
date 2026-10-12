package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/k8s"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

type fakeDiscovery struct {
	candidates []k8s.Candidate
	err        error
}

func (f *fakeDiscovery) Namespaces(context.Context) ([]string, error) {
	return []string{"billing", "shop"}, f.err
}

func (f *fakeDiscovery) Candidates(_ context.Context, namespace string) ([]k8s.Candidate, error) {
	var out []k8s.Candidate
	for _, c := range f.candidates {
		if c.Namespace == namespace {
			out = append(out, c)
		}
	}
	return out, f.err
}

func newDiscoveryAPI(t *testing.T, d Discovery) *httptest.Server {
	t.Helper()
	db, err := store.OpenSQLite(context.Background(), filepath.Join(t.TempDir(), "test.db"), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{
		Store: db, MinCheckInterval: 5 * time.Second, StatsWindow: 24 * time.Hour, MaxResults: 100,
		DefaultFailureThreshold: 3, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		DiscoveryStatus: "no cluster",
	}
	if d != nil {
		opts.Discovery, opts.DiscoveryStatus = d, "in-cluster"
	}
	srv := httptest.NewServer(NewHandler(opts))
	t.Cleanup(func() {
		srv.Close()
		db.Close()
	})
	return srv
}

func shopCandidates() []k8s.Candidate {
	return []k8s.Candidate{
		{Kind: k8s.KindRoute, Namespace: "shop", Name: "store", Type: store.MonitorHTTP, Target: "https://store.apps.example.com/", Reason: k8s.ReasonRouteHost},
		{Kind: k8s.KindService, Namespace: "shop", Name: "api", Port: 80, Type: store.MonitorHTTP, Target: "http://api.shop.svc:80/healthz", Reason: k8s.ReasonReadinessProbe},
		{Kind: k8s.KindService, Namespace: "shop", Name: "redis", Port: 6379, Type: store.MonitorTCP, Target: "redis.shop.svc:6379", Reason: k8s.ReasonTCPFallback},
		{Kind: k8s.KindService, Namespace: "billing", Name: "api", Port: 80, Type: store.MonitorTCP, Target: "api.billing.svc:80", Reason: k8s.ReasonTCPFallback},
	}
}

func TestDiscoveryUnavailable(t *testing.T) {
	srv := newDiscoveryAPI(t, nil)

	var status discoveryStatusResponse
	if code := do(t, "GET", srv.URL+"/api/v1/discovery", "", &status); code != http.StatusOK || status.Enabled || status.Detail != "no cluster" {
		t.Errorf("status = %d %+v", code, status)
	}
	for _, req := range [][2]string{
		{"GET", "/api/v1/discovery/namespaces"},
		{"GET", "/api/v1/discovery/candidates?namespace=shop"},
		{"POST", "/api/v1/discovery/import"},
	} {
		if code := do(t, req[0], srv.URL+req[1], "{}", nil); code != http.StatusServiceUnavailable {
			t.Errorf("%s %s: status = %d, want 503", req[0], req[1], code)
		}
	}
}

func TestDiscoveryAndImport(t *testing.T) {
	srv := newDiscoveryAPI(t, &fakeDiscovery{candidates: shopCandidates()})
	base := srv.URL + "/api/v1"

	var status discoveryStatusResponse
	if do(t, "GET", base+"/discovery", "", &status); !status.Enabled || status.Detail != "in-cluster" {
		t.Errorf("status = %+v", status)
	}
	var namespaces []string
	if do(t, "GET", base+"/discovery/namespaces", "", &namespaces); len(namespaces) != 2 {
		t.Errorf("namespaces = %v", namespaces)
	}
	if code := do(t, "GET", base+"/discovery/candidates", "", nil); code != http.StatusBadRequest {
		t.Errorf("candidates without namespace: status = %d, want 400", code)
	}

	var cands []candidateResponse
	do(t, "GET", base+"/discovery/candidates?namespace=shop", "", &cands)
	if len(cands) != 3 || cands[0].Source != "route/store" || cands[1].Source != "service/api:80" || cands[1].Reason != "readiness-probe" {
		t.Fatalf("candidates = %+v", cands)
	}
	for _, c := range cands {
		if c.MonitorID != nil {
			t.Errorf("%s reported as monitored before any import", c.Source)
		}
	}

	body := `{"namespace":"shop","sources":["service/api:80","route/store","service/api:80","service/gone:1"],"interval_seconds":30,"timeout_seconds":5}`
	var result importResponse
	if code := do(t, "POST", base+"/discovery/import", body, &result); code != http.StatusOK {
		t.Fatalf("import status = %d, want 200", code)
	}
	if len(result.Created) != 2 || len(result.Skipped) != 1 || result.Skipped[0].Source != "service/gone:1" {
		t.Fatalf("import result = %+v", result)
	}
	api := result.Created[0]
	if api.Name != "shop/service/api:80" || api.Namespace != "shop" || api.Source != "service/api:80" ||
		api.Type != "http" || api.Target != "http://api.shop.svc:80/healthz" || api.IntervalSeconds != 30 || api.FailureThreshold != 3 {
		t.Errorf("created monitor = %+v", api)
	}

	// Importing again skips what is already there and adds the rest.
	body = `{"namespace":"shop","sources":["service/api:80","service/redis:6379"],"interval_seconds":60,"timeout_seconds":5,"failure_threshold":2}`
	result = importResponse{}
	do(t, "POST", base+"/discovery/import", body, &result)
	if len(result.Created) != 1 || result.Created[0].Type != "tcp" || result.Created[0].FailureThreshold != 2 ||
		len(result.Skipped) != 1 || result.Skipped[0].Reason != "already monitored" {
		t.Errorf("second import = %+v", result)
	}

	do(t, "GET", base+"/discovery/candidates?namespace=shop", "", &cands)
	for _, c := range cands {
		if c.MonitorID == nil {
			t.Errorf("%s not reported as monitored after import", c.Source)
		}
	}
	// The same source name in another namespace is unaffected.
	do(t, "GET", base+"/discovery/candidates?namespace=billing", "", &cands)
	if len(cands) != 1 || cands[0].MonitorID != nil {
		t.Errorf("billing candidates = %+v", cands)
	}

	var monitors []monitorResponse
	if do(t, "GET", base+"/monitors", "", &monitors); len(monitors) != 3 {
		t.Errorf("monitors after imports = %d, want 3", len(monitors))
	}
}

func TestImportValidation(t *testing.T) {
	srv := newDiscoveryAPI(t, &fakeDiscovery{candidates: shopCandidates()})

	for name, body := range map[string]string{
		"no namespace":   `{"sources":["route/store"],"interval_seconds":30,"timeout_seconds":5}`,
		"no sources":     `{"namespace":"shop","sources":[],"interval_seconds":30,"timeout_seconds":5}`,
		"short interval": `{"namespace":"shop","sources":["route/store"],"interval_seconds":1,"timeout_seconds":1}`,
		"long timeout":   `{"namespace":"shop","sources":["route/store"],"interval_seconds":30,"timeout_seconds":60}`,
		"unknown field":  `{"namespace":"shop","sources":["route/store"],"interval_seconds":30,"timeout_seconds":5,"target":"http://evil"}`,
	} {
		if code := do(t, "POST", srv.URL+"/api/v1/discovery/import", body, nil); code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, code)
		}
	}
	var monitors []monitorResponse
	if do(t, "GET", srv.URL+"/api/v1/monitors", "", &monitors); len(monitors) != 0 {
		t.Errorf("rejected imports created monitors: %+v", monitors)
	}
}

func TestClusterErrorIsBadGateway(t *testing.T) {
	srv := newDiscoveryAPI(t, &fakeDiscovery{err: errors.New(`services is forbidden: User "system:serviceaccount:kubeat:kubeat" cannot list`)})

	var body errorBody
	if code := do(t, "GET", srv.URL+"/api/v1/discovery/namespaces", "", &body); code != http.StatusBadGateway || body.Error == "" {
		t.Errorf("status = %d body = %+v, want 502 with the reason", code, body)
	}
	if code := do(t, "GET", srv.URL+"/api/v1/discovery/candidates?namespace=shop", "", nil); code != http.StatusBadGateway {
		t.Errorf("candidates: status = %d, want 502", code)
	}
}
