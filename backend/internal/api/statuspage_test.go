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

func TestStatusPageCRUD(t *testing.T) {
	srv, _ := newTestAPI(t)
	base := srv.URL + "/api/v1/status-pages"
	var web, api monitorResponse
	do(t, "POST", srv.URL+"/api/v1/monitors", monitorJSON("web", "http", "http://web/", ""), &web)
	do(t, "POST", srv.URL+"/api/v1/monitors", monitorJSON("api", "http", "http://api/", ""), &api)

	var page statusPageResponse
	body := `{"slug":"shop","title":" Mağaza ","description":"Müşteriye dönük","monitors":[{"monitor_id":2,"display_name":" API "},{"monitor_id":1}]}`
	if code := do(t, "POST", base, body, &page); code != http.StatusCreated {
		t.Fatalf("create: status = %d", code)
	}
	if page.Slug != "shop" || page.Title != "Mağaza" || !page.Published || len(page.Monitors) != 2 ||
		page.Monitors[0] != (statusPageMonitor{MonitorID: 2, DisplayName: "API"}) || page.Monitors[1].MonitorID != 1 {
		t.Errorf("created = %+v", page)
	}
	var list []statusPageResponse
	if do(t, "GET", base, "", &list); len(list) != 1 || list[0].ID != page.ID {
		t.Errorf("list = %+v", list)
	}

	// An update changes what it names and leaves the rest.
	var updated statusPageResponse
	if code := do(t, "PATCH", base+"/1", `{"announcement":"Cumartesi 02:00 planlı bakım","published":false}`, &updated); code != http.StatusOK ||
		updated.Announcement == "" || updated.Published || updated.Title != "Mağaza" || len(updated.Monitors) != 2 {
		t.Errorf("update: status = %d, page = %+v", code, updated)
	}
	if code := do(t, "PATCH", base+"/1", `{"monitors":[{"monitor_id":1}]}`, &updated); code != http.StatusOK || len(updated.Monitors) != 1 {
		t.Errorf("new monitors: status = %d, page = %+v", code, updated)
	}

	for name, tc := range map[string]struct {
		method, url, body string
		want              int
	}{
		"no title":        {"POST", base, `{"slug":"a"}`, http.StatusBadRequest},
		"slug with space": {"POST", base, `{"slug":"my page","title":"x"}`, http.StatusBadRequest},
		"slug upper case": {"POST", base, `{"slug":"Shop","title":"x"}`, http.StatusBadRequest},
		"slug as a path":  {"POST", base, `{"slug":"a/b","title":"x"}`, http.StatusBadRequest},
		"unknown monitor": {"POST", base, `{"slug":"a","title":"x","monitors":[{"monitor_id":99}]}`, http.StatusBadRequest},
		"monitor twice":   {"POST", base, `{"slug":"a","title":"x","monitors":[{"monitor_id":1},{"monitor_id":1}]}`, http.StatusBadRequest},
		"slug taken":      {"POST", base, `{"slug":"shop","title":"x"}`, http.StatusConflict},
		"update unknown":  {"PATCH", base + "/99", `{"title":"x"}`, http.StatusNotFound},
		"update bad slug": {"PATCH", base + "/1", `{"slug":""}`, http.StatusBadRequest},
		"get unknown":     {"GET", base + "/99", "", http.StatusNotFound},
	} {
		if code := do(t, tc.method, tc.url, tc.body, nil); code != tc.want {
			t.Errorf("%s: status = %d, want %d", name, code, tc.want)
		}
	}

	if code := do(t, "DELETE", base+"/1", "", nil); code != http.StatusNoContent {
		t.Errorf("delete: status = %d", code)
	}
	if code := do(t, "DELETE", base+"/1", "", nil); code != http.StatusNotFound {
		t.Errorf("second delete: status = %d", code)
	}
}

func TestStatusPageView(t *testing.T) {
	srv, db := newTestAPI(t) // three days of history, incidents of the last two
	ctx := context.Background()
	mk := func(name, namespace, source string, enabled bool) store.Monitor {
		m, err := db.CreateMonitor(ctx, store.Monitor{Name: name, Namespace: namespace, Source: source, Type: store.MonitorTCP,
			Target: "internal.corp:1", Interval: time.Minute, Timeout: time.Second, FailureThreshold: 1, Enabled: enabled})
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	web := mk("shop/service/web:80", "shop", "service/web:80", true)
	api := mk("api", "", "", true)
	paused := mk("batch", "", "", false)
	fresh := mk("fresh", "", "", true)
	hidden := mk("hidden", "", "", true)

	now := time.Now().UTC()
	today := now.Truncate(24 * time.Hour)
	var results []store.CheckResult
	add := func(m store.Monitor, at time.Time, up bool) {
		results = append(results, store.CheckResult{MonitorID: m.ID, CheckedAt: at, Up: up, Latency: time.Millisecond})
	}
	// web: two days ago 1 of 2 up, yesterday nothing, today up.
	add(web, today.Add(-47*time.Hour), true)
	add(web, today.Add(-46*time.Hour), false)
	add(web, today, true)
	add(api, today, false)
	add(hidden, today, false)
	if err := db.InsertCheckResults(ctx, results); err != nil {
		t.Fatal(err)
	}
	if err := db.RollupCheckResults(ctx, today.Add(-72*time.Hour), now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	open := func(m store.Monitor, start time.Time, cause string) store.Incident {
		i, err := db.OpenIncident(ctx, store.Incident{MonitorID: m.ID, Kind: store.IncidentDown, StartedAt: start, Cause: cause})
		if err != nil {
			t.Fatal(err)
		}
		return i
	}
	old := open(web, now.Add(-72*time.Hour), "too long ago")
	db.ResolveIncident(ctx, old.ID, now.Add(-71*time.Hour))
	recent := open(web, now.Add(-46*time.Hour), "dial tcp 10.0.0.7:80: refused")
	db.ResolveIncident(ctx, recent.ID, now.Add(-45*time.Hour))
	open(api, now.Add(-time.Minute), "dial tcp internal.corp:1: refused")
	open(hidden, now.Add(-time.Minute), "not on the page")

	body := `{"slug":"shop","title":"Mağaza","announcement":"Bakım var","monitors":[
		{"monitor_id":1,"display_name":"Web sitesi"},{"monitor_id":2},{"monitor_id":3},{"monitor_id":4}]}`
	if code := do(t, "POST", srv.URL+"/api/v1/status-pages", body, nil); code != http.StatusCreated {
		t.Fatalf("create: status = %d", code)
	}

	var view statusPageView
	url := srv.URL + "/api/v1/status-pages/by-slug/shop/view"
	if code := do(t, "GET", url, "", &view); code != http.StatusOK {
		t.Fatalf("view: status = %d", code)
	}
	if !view.From.Equal(today.Add(-48 * time.Hour)) {
		t.Errorf("from = %s, want the start of the day before yesterday", view.From)
	}
	if view.Title != "Mağaza" || view.Announcement != "Bakım var" || view.Days != 3 || view.State != publicDegraded || len(view.Monitors) != 4 {
		t.Fatalf("view = %+v", view)
	}
	w := view.Monitors[0]
	if w.Name != "Web sitesi" || w.State != publicUp || len(w.History) != 3 || w.History[0] == nil || *w.History[0] != 50 ||
		w.History[1] != nil || w.History[2] == nil || *w.History[2] != 100 || w.UptimePercent == nil || int(*w.UptimePercent) != 66 {
		t.Errorf("web = %+v", w)
	}
	if a := view.Monitors[1]; a.Name != "api" || a.State != publicDown || *a.UptimePercent != 0 {
		t.Errorf("api = %+v", a)
	}
	if p := view.Monitors[2]; p.State != publicPaused || p.UptimePercent != nil {
		t.Errorf("paused = %+v", p)
	}
	if f := view.Monitors[3]; f.State != publicPending {
		t.Errorf("fresh = %+v", f)
	}
	_ = paused
	_ = fresh
	// The open outage and the recent one, newest first; nothing older, and
	// nothing of a monitor the page does not show.
	if len(view.Incidents) != 2 || view.Incidents[0].MonitorName != "api" || view.Incidents[0].ResolvedAt != nil ||
		view.Incidents[1].MonitorName != "Web sitesi" || view.Incidents[1].ResolvedAt == nil {
		t.Errorf("incidents = %+v", view.Incidents)
	}

	// Nothing internal is in what the public gets.
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	for _, internal := range []string{"internal.corp", "10.0.0.7", "refused", "hidden", "service/web", "shop/"} {
		if strings.Contains(string(raw), internal) {
			t.Errorf("the public view contains %q", internal)
		}
	}

	// A discovered monitor without a display name shows the object's name.
	do(t, "PATCH", srv.URL+"/api/v1/status-pages/1", `{"monitors":[{"monitor_id":1}]}`, nil)
	do(t, "GET", url, "", &view)
	if view.Monitors[0].Name != "web:80" || view.State != publicUp {
		t.Errorf("after the update: name = %q, state = %q", view.Monitors[0].Name, view.State)
	}
	// An unpublished page is not served, nor is an unknown one.
	do(t, "PATCH", srv.URL+"/api/v1/status-pages/1", `{"published":false}`, nil)
	for _, slug := range []string{"shop", "nope"} {
		if code := do(t, "GET", srv.URL+"/api/v1/status-pages/by-slug/"+slug+"/view", "", nil); code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", slug, code)
		}
	}
}

func TestBadges(t *testing.T) {
	srv, db := newTestAPI(t)
	ctx := context.Background()
	var shown, private monitorResponse
	do(t, "POST", srv.URL+"/api/v1/monitors", monitorJSON("shown", "tcp", "x:1", ""), &shown)
	do(t, "POST", srv.URL+"/api/v1/monitors", monitorJSON("private", "tcp", "x:1", ""), &private)
	do(t, "POST", srv.URL+"/api/v1/status-pages", `{"slug":"p","title":"p","monitors":[{"monitor_id":1}]}`, nil)

	hour := time.Now().UTC().Truncate(time.Hour)
	results := []store.CheckResult{
		{MonitorID: 1, CheckedAt: hour, Up: true}, {MonitorID: 1, CheckedAt: hour.Add(time.Second), Up: true},
		{MonitorID: 1, CheckedAt: hour.Add(2 * time.Second), Up: true}, {MonitorID: 1, CheckedAt: hour.Add(3 * time.Second), Up: false},
	}
	if err := db.InsertCheckResults(ctx, results); err != nil {
		t.Fatal(err)
	}
	db.RollupCheckResults(ctx, hour, hour.Add(time.Hour))

	get := func(path string) (int, string, string) {
		resp, err := http.Get(srv.URL + "/api/v1/badges/" + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, resp.Header.Get("Content-Type"), string(body)
	}

	// The latest check failed but no incident is open: not "down" yet.
	code, contentType, svg := get("1/status")
	if code != http.StatusOK || !strings.HasPrefix(contentType, "image/svg+xml") || !strings.Contains(svg, ">up<") || !strings.Contains(svg, badgeGreen) {
		t.Errorf("status badge: %d %s %s", code, contentType, svg)
	}
	if _, _, svg := get("1/uptime"); !strings.Contains(svg, ">75.00%<") || !strings.Contains(svg, "uptime 24h") || !strings.Contains(svg, badgeRed) {
		t.Errorf("uptime badge: %s", svg)
	}
	if _, err := db.OpenIncident(ctx, store.Incident{MonitorID: 1, StartedAt: hour, Cause: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, _, svg := get("1/status"); !strings.Contains(svg, ">down<") || !strings.Contains(svg, badgeRed) {
		t.Errorf("status badge during an outage: %s", svg)
	}
	// A label of one's own cannot inject markup.
	if _, _, svg := get("1/status?label=" + "%3Cscript%3Ex"); strings.Contains(svg, "<script") || !strings.Contains(svg, "&lt;script&gt;x") {
		t.Errorf("custom label: %s", svg)
	}

	// No badge for what no published page shows, or for anything unknown.
	for _, path := range []string{"2/status", "2/uptime", "99/status", "1/latency", "x/status"} {
		if code, _, _ := get(path); code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", path, code)
		}
	}
	do(t, "PATCH", srv.URL+"/api/v1/status-pages/1", `{"published":false}`, nil)
	if code, _, _ := get("1/status"); code != http.StatusNotFound {
		t.Errorf("badge of an unpublished page: status = %d, want 404", code)
	}
}
