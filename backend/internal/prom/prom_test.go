package prom

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
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
