package api

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

func TestReportGroupsByNamespaceAndScope(t *testing.T) {
	srv, db := newTestAPI(t)
	ctx := context.Background()
	for _, m := range []store.Monitor{
		{Name: "shop-api", Type: store.MonitorTCP, Target: "a:1", Namespace: "shop"},
		{Name: "billing-db", Type: store.MonitorTCP, Target: "b:1", Namespace: "billing"},
		{Name: "loose", Type: store.MonitorTCP, Target: "c:1"},
	} {
		m.Interval, m.Timeout, m.FailureThreshold, m.Enabled = time.Minute, time.Second, 1, true
		if _, err := db.CreateMonitor(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	// A resolved outage of 2h for shop-api, three days ago.
	i, err := db.OpenIncident(ctx, store.Incident{MonitorID: 1, StartedAt: time.Now().Add(-72 * time.Hour), Cause: "refused"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ResolveIncident(ctx, i.ID, time.Now().Add(-70*time.Hour)); err != nil {
		t.Fatal(err)
	}

	var got reportResponse
	if code := do(t, http.MethodGet, srv.URL+"/api/v1/reports?days=7", "", &got); code != http.StatusOK {
		t.Fatalf("GET = %d", code)
	}
	if got.Days != 7 || len(got.Namespaces) != 3 || got.Namespaces[0].Namespace != "" || got.Namespaces[1].Namespace != "billing" || got.Namespaces[2].Namespace != "shop" {
		t.Fatalf("report = %+v", got)
	}
	shop := got.Namespaces[2]
	if shop.Outages != 1 || shop.DowntimeSeconds != 7200 || shop.Uptime != nil || len(shop.Monitors) != 1 || shop.Monitors[0].Outages != 1 || shop.Monitors[0].LongestOutageSeconds != 7200 {
		t.Errorf("shop = %+v", shop)
	}

	// Only one namespace, and a one-day report does not see the old outage.
	if code := do(t, http.MethodGet, srv.URL+"/api/v1/reports?days=1&namespace=shop", "", &got); code != http.StatusOK || len(got.Namespaces) != 1 || got.Namespaces[0].Outages != 0 {
		t.Errorf("one day of shop = %d %+v", code, got)
	}
	if code := do(t, http.MethodGet, srv.URL+"/api/v1/reports?days=0", "", &got); code != http.StatusBadRequest {
		t.Errorf("days=0 = %d, want 400", code)
	}

	// CSV: a header and one row per monitor.
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/reports?days=7&format=csv", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	if res.StatusCode != http.StatusOK || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/csv") || len(lines) != 4 || !strings.HasPrefix(lines[0], "namespace,monitor,") {
		t.Errorf("csv = %d %q\n%s", res.StatusCode, res.Header.Get("Content-Type"), body)
	}
	if !strings.Contains(string(body), "shop,shop-api,tcp,a:1,true,0,0,,0,1,7200,7200,false") {
		t.Errorf("csv row missing:\n%s", body)
	}

	// A viewer of billing sees only billing.
	addUser(t, srv.URL, "v", store.RoleViewer, "billing")
	token, code := signIn(t, srv.URL, "v", "parola-123")
	if code != http.StatusOK {
		t.Fatalf("sign in = %d", code)
	}
	if code := doAs(t, token, http.MethodGet, srv.URL+"/api/v1/reports", "", &got); code != http.StatusOK || len(got.Namespaces) != 1 || got.Namespaces[0].Namespace != "billing" {
		t.Errorf("viewer's report = %d %+v", code, got)
	}
}
