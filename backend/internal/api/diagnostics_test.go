package api

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/explain"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// The results, the window's failures and the monitor's last result carry
// a failed check's diagnostics, the explanation with its English text;
// those who may not read the monitor get none of it.
func TestResultsCarryDiagnostics(t *testing.T) {
	srv, db := newTestAPI(t)
	base := srv.URL + "/api/v1/monitors"
	var created monitorResponse
	if code := do(t, "POST", base, monitorJSON("api", "http", "https://api.shop.svc/healthz", `"namespace":"shop"`), &created); code != http.StatusCreated {
		t.Fatalf("create: status = %d", code)
	}
	id := itoa(created.ID)

	connect := 0.25
	failure := &store.Diagnostics{
		Explanation: &store.Explanation{Code: explain.DNSServiceNotFound, Params: map[string]string{"service": "api", "namespace": "shop"}},
		Timing:      &store.Timing{Connect: &connect, Total: 2.5},
		RemoteAddr:  "10.128.2.15:443",
		TLS:         &store.TLSDetails{Version: "TLS 1.3", SANs: []string{"api.shop.svc"}, MoreSANs: 2, NotAfter: time.Date(2027, 1, 2, 3, 4, 5, 0, time.UTC)},
		StatusLine:  "HTTP/1.1 503 Service Unavailable",
		Headers:     map[string]string{"server": "envoy"},
	}
	now := time.Now().UTC().Truncate(time.Second)
	if err := db.InsertCheckResults(context.Background(), []store.CheckResult{
		{MonitorID: created.ID, CheckedAt: now.Add(-3 * time.Minute), Up: true, Latency: 20 * time.Millisecond},
		{MonitorID: created.ID, CheckedAt: now.Add(-2 * time.Minute), Up: false, Error: "unexpected status 503", StatusCode: 503, Diagnostics: failure},
		{MonitorID: created.ID, CheckedAt: now.Add(-time.Minute), Up: false, Error: "timeout", Diagnostics: &store.Diagnostics{Explanation: &store.Explanation{Code: explain.ConnectTimeout}}},
	}); err != nil {
		t.Fatal(err)
	}

	// The shape, as a client reads it: every field present, null for what
	// the check did not learn.
	var raw []map[string]any
	if code := do(t, "GET", base+"/"+id+"/results", "", &raw); code != http.StatusOK || len(raw) != 3 {
		t.Fatalf("results: status %d, %d results", code, len(raw))
	}
	if d, present := raw[2]["diagnostics"]; !present || d != nil {
		t.Errorf("a successful result: diagnostics = %v (present %v)", d, present)
	}
	d := raw[1]["diagnostics"].(map[string]any)
	if keys := slices.Sorted(maps.Keys(d)); !slices.Equal(keys, []string{"explanation", "headers", "pod", "redirects", "remote_addr", "status_line", "timing", "tls"}) {
		t.Errorf("diagnostics fields = %v", keys)
	}
	if timing := d["timing"].(map[string]any); timing["dns_ms"] != nil || timing["connect_ms"] != 0.25 || timing["total_ms"] != 2.5 {
		t.Errorf("timing = %v", timing)
	}
	if tls := d["tls"].(map[string]any); tls["subject"] != nil || tls["version"] != "TLS 1.3" || tls["more_sans"] != 2.0 || tls["not_after"] != "2027-01-02T03:04:05Z" {
		t.Errorf("tls = %v", tls)
	}
	newest := raw[0]["diagnostics"].(map[string]any)
	if newest["timing"] != nil || newest["tls"] != nil || newest["remote_addr"] != nil || newest["headers"] == nil {
		t.Errorf("diagnostics with only an explanation = %v", newest)
	}
	if e := newest["explanation"].(map[string]any); e["code"] != explain.ConnectTimeout || e["params"] == nil {
		t.Errorf("explanation without values = %v", e)
	}

	var results []resultResponse
	do(t, "GET", base+"/"+id+"/results", "", &results)
	e := results[1].Diagnostics.Explanation
	if e == nil || e.Code != explain.DNSServiceNotFound || e.Params["namespace"] != "shop" ||
		e.Text != "There is no Service api in the namespace shop: check both names in the address." {
		t.Errorf("explanation = %+v", e)
	}
	if got := results[1].Diagnostics; got.Headers["server"] != "envoy" || *got.StatusLine != "HTTP/1.1 503 Service Unavailable" || got.Pod != nil {
		t.Errorf("diagnostics = %+v", got)
	}

	var window windowResponse
	from := now.Add(-time.Hour).Format(time.RFC3339)
	if code := do(t, "GET", base+"/"+id+"/window?from="+from, "", &window); code != http.StatusOK || len(window.Failures) != 2 ||
		window.Failures[1].Diagnostics == nil || window.Failures[1].Diagnostics.Explanation.Code != explain.DNSServiceNotFound {
		t.Errorf("window: status %d, failures %+v", code, window.Failures)
	}
	var monitor monitorResponse
	if code := do(t, "GET", base+"/"+id, "", &monitor); code != http.StatusOK || monitor.Last == nil || monitor.Last.Diagnostics == nil ||
		monitor.Last.Diagnostics.Explanation.Code != explain.ConnectTimeout {
		t.Errorf("last result: status %d, %+v", code, monitor.Last)
	}

	// A viewer of the namespace reads them as the rest of the results; one
	// of another namespace finds no such monitor.
	addUser(t, srv.URL, "shop-viewer", store.RoleViewer, "shop")
	addUser(t, srv.URL, "billing-viewer", store.RoleViewer, "billing")
	inside, _ := signIn(t, srv.URL, "shop-viewer", "parola-123")
	outside, _ := signIn(t, srv.URL, "billing-viewer", "parola-123")
	results = nil
	if code := doAs(t, inside, "GET", base+"/"+id+"/results", "", &results); code != http.StatusOK || len(results) != 3 || results[1].Diagnostics == nil {
		t.Errorf("viewer of the namespace: status %d, %+v", code, results)
	}
	for _, path := range []string{"/results", "/window?from=" + from, ""} {
		var body json.RawMessage
		if code := doAs(t, outside, "GET", base+"/"+id+path, "", &body); code != http.StatusNotFound {
			t.Errorf("viewer of another namespace, %q: status %d, %s", path, code, body)
		}
	}
}
