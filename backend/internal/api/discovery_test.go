package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/discovery"
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
		Store: db, Token: testToken, MinCheckInterval: 5 * time.Second, StatsWindow: 24 * time.Hour, MaxResults: 100,
		DefaultFailureThreshold: 3, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		DiscoveryStatus: "no cluster", License: fullLicense{},
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

// The sync runs over the whole cluster; a user who sees some namespaces is
// told about those alone: their skipped objects and their counts.
func TestDiscoveryStatusShowsOnlyTheUsersNamespaces(t *testing.T) {
	ran := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	srv, _ := newTestAPIWith(t, func(o *Options) {
		o.Discovery, o.DiscoveryStatus = &fakeDiscovery{}, "in-cluster"
		o.Sync = fakeSync{status: discovery.Status{
			IntervalSeconds: 60, LastRunAt: ran, Managed: 7, Created: 3, Updated: 1, Removed: 2,
			Skipped: []discovery.Skip{
				{Namespace: "shop", Source: "service/api:80", Reason: "already monitored by hand"},
				{Namespace: "finance", Source: "service/payments-db:5432", Reason: "kubeat.io/interval: must be at least 5s"},
				{Namespace: "kube-system", Source: "route/secret-admin-console", Reason: "deleted in Kubeat", Dismissed: true},
			},
			ByNamespace: map[string]discovery.Counts{
				"shop":    {Managed: 2, Created: 1},
				"finance": {Managed: 4, Created: 2, Removed: 2},
				"payroll": {Managed: 1, Updated: 1},
			},
		}}
	})
	addUser(t, srv.URL, "viewer", store.RoleViewer, "shop")
	addUser(t, srv.URL, "editor", store.RoleEditor, "shop", "payroll")
	addUser(t, srv.URL, "seer", store.RoleViewer, "*")
	url := srv.URL + "/api/v1/discovery"

	for name, tc := range map[string]struct {
		user                               string
		skipped                            string
		managed, created, updated, removed int
	}{
		"viewer of shop":          {"viewer", "shop/service/api:80", 2, 1, 0, 0},
		"editor of shop, payroll": {"editor", "shop/service/api:80", 3, 1, 1, 0},
		"viewer of every one":     {"seer", "shop/service/api:80,finance/service/payments-db:5432,kube-system/route/secret-admin-console", 7, 3, 1, 2},
	} {
		token, _ := signIn(t, srv.URL, tc.user, "parola-123")
		code, body := bodyAs(t, token, url)
		var status discoveryStatusResponse
		if err := json.Unmarshal([]byte(body), &status); err != nil || code != http.StatusOK || status.Sync == nil {
			t.Fatalf("%s: status = %d, body %s", name, code, body)
		}
		st := status.Sync
		var skipped []string
		for _, s := range st.Skipped {
			skipped = append(skipped, s.Namespace+"/"+s.Source)
		}
		if got := strings.Join(skipped, ","); got != tc.skipped {
			t.Errorf("%s: skipped %q, want %q", name, got, tc.skipped)
		}
		if st.Managed != tc.managed || st.Created != tc.created || st.Updated != tc.updated || st.Removed != tc.removed {
			t.Errorf("%s: counts %d/%d/%d/%d, want %d/%d/%d/%d", name,
				st.Managed, st.Created, st.Updated, st.Removed, tc.managed, tc.created, tc.updated, tc.removed)
		}
		if !st.LastRunAt.Equal(ran) || st.IntervalSeconds != 60 {
			t.Errorf("%s: run %s every %ds", name, st.LastRunAt, st.IntervalSeconds)
		}
		if tc.user != "seer" && (strings.Contains(body, "finance") || strings.Contains(body, "kube-system")) {
			t.Errorf("%s reads other namespaces: %s", name, body)
		}
	}

	// An administrator sees the whole run.
	var status discoveryStatusResponse
	do(t, "GET", url, "", &status)
	if status.Detail != "in-cluster" || status.Sync == nil || status.Sync.Managed != 7 || len(status.Sync.Skipped) != 3 {
		t.Errorf("admin: %+v", status)
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

// A service with a gRPC probe becomes a gRPC monitor that asks about the
// service the probe names, and an HTTP one follows redirects like any other.
func TestImportKeepsTheProbe(t *testing.T) {
	srv := newDiscoveryAPI(t, &fakeDiscovery{candidates: []k8s.Candidate{
		{Kind: k8s.KindService, Namespace: "shop", Name: "cart", Port: 9000, Type: store.MonitorGRPC,
			Target: "cart.shop.svc:9000", Reason: k8s.ReasonReadinessProbe, GRPCService: "shop.Cart"},
		{Kind: k8s.KindService, Namespace: "shop", Name: "web", Port: 80, Type: store.MonitorHTTP,
			Target: "http://web.shop.svc:80/healthz", Reason: k8s.ReasonReadinessProbe},
	}})

	var candidates []candidateResponse
	do(t, "GET", srv.URL+"/api/v1/discovery/candidates?namespace=shop", "", &candidates)
	if len(candidates) != 2 || candidates[0].Type != "grpc" || candidates[0].Target != "cart.shop.svc:9000" {
		t.Fatalf("candidates = %+v", candidates)
	}

	var result importResponse
	body := `{"namespace":"shop","sources":["service/cart:9000","service/web:80"],"interval_seconds":30,"timeout_seconds":5}`
	if code := do(t, "POST", srv.URL+"/api/v1/discovery/import", body, &result); code != http.StatusOK || len(result.Created) != 2 {
		t.Fatalf("import: status = %d, result = %+v", code, result)
	}
	cart, web := result.Created[0], result.Created[1]
	if cart.Type != "grpc" || cart.GRPC == nil || cart.GRPC.Service != "shop.Cart" || cart.HTTP != nil {
		t.Errorf("cart = %+v", cart)
	}
	if web.HTTP == nil || *web.HTTP.MaxRedirects != store.DefaultMaxRedirects || web.GRPC != nil {
		t.Errorf("web = %+v", web)
	}
}
