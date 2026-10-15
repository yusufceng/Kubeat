package prom

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeProm answers instant queries: CPU for queries containing "cpu",
// memory otherwise.
type fakeProm struct {
	mu      sync.Mutex
	queries []string
	auth    []string
	cpu     map[string]float64
	memory  map[string]float64
	fail    bool
	rawBody string
}

func (f *fakeProm) handler(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	query := r.Form.Get("query")
	f.mu.Lock()
	f.queries = append(f.queries, query)
	f.auth = append(f.auth, r.Header.Get("Authorization"))
	f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	if f.fail {
		w.WriteHeader(http.StatusUnprocessableEntity)
		w.Write([]byte(`{"status":"error","errorType":"bad_data","error":"parse error"}`))
		return
	}
	if f.rawBody != "" {
		w.Write([]byte(f.rawBody))
		return
	}
	values := f.memory
	if strings.Contains(query, "cpu") {
		values = f.cpu
	}
	var result []map[string]any
	for pod, v := range values {
		result = append(result, map[string]any{
			"metric": map[string]string{"pod": pod},
			"value":  []any{1.8e9, strings.TrimRight(strings.TrimRight(jsonNumber(v), "0"), ".")},
		})
	}
	json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "vector", "result": result}})
}

func jsonNumber(v float64) string {
	b, _ := json.Marshal(v)
	if strings.Contains(string(b), ".") {
		return string(b) + "0"
	}
	return string(b) + ".0"
}

func newClient(t *testing.T, srv *httptest.Server, edit func(*Options)) *Client {
	t.Helper()
	opts := Options{URL: srv.URL, Timeout: 5 * time.Second, RateWindow: 5 * time.Minute}
	if edit != nil {
		edit(&opts)
	}
	c, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestPodUsage(t *testing.T) {
	fake := &fakeProm{
		cpu:    map[string]float64{"web-1": 0.25, "web-2": 1.5},
		memory: map[string]float64{"web-1": 104857600, "cache.0": 2048},
	}
	srv := httptest.NewServer(http.HandlerFunc(fake.handler))
	defer srv.Close()

	got, err := newClient(t, srv, nil).PodUsage(context.Background(), "shop", []string{"web-1", "web-2", "cache.0", "gone"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("usage = %v, want entries for the three known pods", got)
	}
	if u := got["web-1"]; u.CPUCores == nil || *u.CPUCores != 0.25 || u.MemoryBytes == nil || *u.MemoryBytes != 104857600 {
		t.Errorf("web-1 = %+v", u)
	}
	// A pod can have one metric and not the other.
	if u := got["web-2"]; u.CPUCores == nil || *u.CPUCores != 1.5 || u.MemoryBytes != nil {
		t.Errorf("web-2 = %+v, want CPU only", u)
	}
	if u := got["cache.0"]; u.CPUCores != nil || u.MemoryBytes == nil {
		t.Errorf("cache.0 = %+v, want memory only", u)
	}

	cpuQuery, memQuery := fake.queries[0], fake.queries[1]
	if !strings.Contains(cpuQuery, `namespace="shop"`) || !strings.Contains(cpuQuery, "[300s]") ||
		!strings.Contains(cpuQuery, `pod=~"web-1|web-2|cache\\.0|gone"`) {
		t.Errorf("cpu query = %s", cpuQuery)
	}
	if !strings.Contains(memQuery, "container_memory_working_set_bytes") || strings.Contains(memQuery, "%") {
		t.Errorf("memory query = %s", memQuery)
	}
	if fake.auth[0] != "" {
		t.Errorf("Authorization sent without a token file: %q", fake.auth[0])
	}
}

func TestBearerTokenIsReadPerRequest(t *testing.T) {
	fake := &fakeProm{}
	srv := httptest.NewServer(http.HandlerFunc(fake.handler))
	defer srv.Close()
	tokenFile := filepath.Join(t.TempDir(), "token")
	os.WriteFile(tokenFile, []byte("first-token\n"), 0o600)
	c := newClient(t, srv, func(o *Options) { o.TokenFile = tokenFile })

	if _, err := c.PodUsage(context.Background(), "shop", []string{"web-1"}); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(tokenFile, []byte("rotated-token"), 0o600) // as kubelet does
	if _, err := c.PodUsage(context.Background(), "shop", []string{"web-1"}); err != nil {
		t.Fatal(err)
	}
	if fake.auth[0] != "Bearer first-token" || fake.auth[2] != "Bearer rotated-token" {
		t.Errorf("Authorization headers = %q", fake.auth)
	}

	os.Remove(tokenFile)
	if _, err := c.PodUsage(context.Background(), "shop", []string{"web-1"}); err == nil {
		t.Error("a missing token file was ignored")
	}
}

// The token goes to the configured server only. A redirect to another
// server is followed without it: the token is the pod's service account
// token, and must not reach whoever the server, or someone answering in its
// place, sends the client to.
func TestBearerTokenStaysWithTheConfiguredServer(t *testing.T) {
	other := &fakeProm{memory: map[string]float64{"web-1": 1}}
	elsewhere := httptest.NewServer(http.HandlerFunc(other.handler))
	defer elsewhere.Close()
	var (
		mu   sync.Mutex
		seen []string
	)
	configured := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get("Authorization"))
		mu.Unlock()
		http.Redirect(w, r, elsewhere.URL+r.URL.Path+"?"+r.URL.RawQuery, http.StatusTemporaryRedirect)
	}))
	defer configured.Close()
	tokenFile := filepath.Join(t.TempDir(), "token")
	os.WriteFile(tokenFile, []byte("sa-token"), 0o600)

	c := newClient(t, configured, func(o *Options) { o.TokenFile = tokenFile })
	if _, err := c.PodUsage(context.Background(), "shop", []string{"web-1"}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	other.mu.Lock()
	defer other.mu.Unlock()
	if len(seen) != 2 || seen[0] != "Bearer sa-token" || seen[1] != "Bearer sa-token" {
		t.Errorf("the configured server got %q, want the token on both queries", seen)
	}
	if len(other.auth) != 2 {
		t.Fatalf("the other server got %d requests, want the two redirected queries", len(other.auth))
	}
	for _, auth := range other.auth {
		if auth != "" {
			t.Errorf("the token followed the redirect to another server: %q", auth)
		}
	}
}

// The same server with its default port written out is still the
// configured one.
func TestOrigin(t *testing.T) {
	same := [][2]string{
		{"https://thanos-querier.openshift-monitoring.svc:9091", "https://THANOS-querier.openshift-monitoring.svc:9091/api/v1/query"},
		{"https://thanos", "https://thanos:443/api"},
		{"http://prometheus", "HTTP://prometheus:80/api"},
		{"http://[fd00::1]:9090", "http://[fd00::1]:9090/api"},
	}
	for _, pair := range same {
		a, _ := url.Parse(pair[0])
		b, _ := url.Parse(pair[1])
		if origin(a) != origin(b) {
			t.Errorf("%s and %s differ: %s, %s", pair[0], pair[1], origin(a), origin(b))
		}
	}
	different := [][2]string{
		{"https://thanos:9091", "http://thanos:9091"},
		{"https://thanos:9091", "https://thanos:9092"},
		{"https://thanos", "https://thanos.evil.example"},
		{"http://prometheus", "http://prometheus:8080"},
	}
	for _, pair := range different {
		a, _ := url.Parse(pair[0])
		b, _ := url.Parse(pair[1])
		if origin(a) == origin(b) {
			t.Errorf("%s and %s agree: %s", pair[0], pair[1], origin(a))
		}
	}
}

func TestCustomQueries(t *testing.T) {
	fake := &fakeProm{}
	srv := httptest.NewServer(http.HandlerFunc(fake.handler))
	defer srv.Close()
	c := newClient(t, srv, func(o *Options) {
		o.CPUQuery = `my_cpu{ns="%NAMESPACE%",p=~"%PODS%"}[%WINDOW%]`
		o.MemoryQuery = `my_mem{ns="%NAMESPACE%"}`
		o.RateWindow = 90 * time.Second
	})
	if _, err := c.PodUsage(context.Background(), "billing", []string{"a"}); err != nil {
		t.Fatal(err)
	}
	if fake.queries[0] != `my_cpu{ns="billing",p=~"a"}[90s]` || fake.queries[1] != `my_mem{ns="billing"}` {
		t.Errorf("queries = %q", fake.queries)
	}
}

func TestPodUsageRejectsNamesThatCouldAlterTheQuery(t *testing.T) {
	fake := &fakeProm{}
	srv := httptest.NewServer(http.HandlerFunc(fake.handler))
	defer srv.Close()
	c := newClient(t, srv, nil)
	ctx := context.Background()

	for _, pod := range []string{`a"} or vector(1) #`, "a|.*", "A", "", "a b", `a\`} {
		if _, err := c.PodUsage(ctx, "shop", []string{pod}); err == nil {
			t.Errorf("pod name %q was accepted", pod)
		}
	}
	if _, err := c.PodUsage(ctx, `shop"}`, []string{"a"}); err == nil {
		t.Error("a namespace with a quote was accepted")
	}
	if len(fake.queries) != 0 {
		t.Errorf("queries were sent for invalid names: %q", fake.queries)
	}
	// No pods, no queries.
	if got, err := c.PodUsage(ctx, "shop", nil); err != nil || len(got) != 0 || len(fake.queries) != 0 {
		t.Errorf("empty pod list: %v, %v, %d queries", got, err, len(fake.queries))
	}
}

func TestErrors(t *testing.T) {
	ctx := context.Background()

	failing := &fakeProm{fail: true}
	srv := httptest.NewServer(http.HandlerFunc(failing.handler))
	defer srv.Close()
	if _, err := newClient(t, srv, nil).PodUsage(ctx, "shop", []string{"a"}); err == nil || !strings.Contains(err.Error(), "cpu query") {
		t.Errorf("server error: err = %v", err)
	}

	noPodLabel := &fakeProm{rawBody: `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"instance":"x"},"value":[1800000000,"1"]}]}}`}
	srv2 := httptest.NewServer(http.HandlerFunc(noPodLabel.handler))
	defer srv2.Close()
	if _, err := newClient(t, srv2, nil).PodUsage(ctx, "shop", []string{"a"}); err == nil || !strings.Contains(err.Error(), `"pod" label`) {
		t.Errorf("series without pod label: err = %v", err)
	}

	scalar := &fakeProm{rawBody: `{"status":"success","data":{"resultType":"scalar","result":[1800000000,"1"]}}`}
	srv3 := httptest.NewServer(http.HandlerFunc(scalar.handler))
	defer srv3.Close()
	if _, err := newClient(t, srv3, nil).PodUsage(ctx, "shop", []string{"a"}); err == nil {
		t.Error("a scalar result was accepted")
	}

	if _, err := New(Options{URL: "http://x", CAFile: "/nonexistent/ca.crt"}); err == nil {
		t.Error("a missing CA file was accepted")
	}
	empty := filepath.Join(t.TempDir(), "ca.crt")
	os.WriteFile(empty, []byte("not a certificate"), 0o600)
	if _, err := New(Options{URL: "http://x", CAFile: empty}); err == nil {
		t.Error("a CA file without certificates was accepted")
	}
}

func TestTLSWithCAFile(t *testing.T) {
	fake := &fakeProm{memory: map[string]float64{"a": 1}}
	srv := httptest.NewTLSServer(http.HandlerFunc(fake.handler))
	defer srv.Close()
	ctx := context.Background()

	// Without the CA the server's certificate is not trusted.
	if _, err := newClient(t, srv, nil).PodUsage(ctx, "shop", []string{"a"}); err == nil {
		t.Error("an untrusted certificate was accepted")
	}
	caFile := filepath.Join(t.TempDir(), "ca.crt")
	os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0o600)
	if _, err := newClient(t, srv, func(o *Options) { o.CAFile = caFile }).PodUsage(ctx, "shop", []string{"a"}); err != nil {
		t.Errorf("with the CA file: %v", err)
	}
	if _, err := newClient(t, srv, func(o *Options) { o.InsecureSkipVerify = true }).PodUsage(ctx, "shop", []string{"a"}); err != nil {
		t.Errorf("with verification off: %v", err)
	}
}

func TestQueriesNameTheirNamespace(t *testing.T) {
	// A tenancy-enforcing proxy (OpenShift's thanos-querier on 9092) wants
	// the namespace as a URL parameter on every query.
	var seen []string
	fake := &fakeProm{cpu: map[string]float64{"web-1": 1}, memory: map[string]float64{"web-1": 1}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query().Get("namespace"))
		fake.handler(w, r)
	}))
	defer srv.Close()

	if _, err := newClient(t, srv, nil).PodUsage(context.Background(), "shop", []string{"web-1"}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[0] != "shop" || seen[1] != "shop" {
		t.Errorf("namespace parameters = %v, want shop on both queries", seen)
	}
}
