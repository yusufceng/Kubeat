package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// public sends a request as a visitor without a session, with the given
// headers, and decodes whatever JSON comes back, errors included.
func public(t *testing.T, method, url, body string, headers map[string]string, out any) int {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("%s %s: decode %q: %v", method, url, raw, err)
		}
	}
	return resp.StatusCode
}

func TestStatusPageCRUD(t *testing.T) {
	srv, _ := newTestAPI(t)
	base := srv.URL + "/api/v1/status-pages"
	var web, api monitorResponse
	do(t, "POST", srv.URL+"/api/v1/monitors", monitorJSON("web", "http", "http://web/", ""), &web)
	do(t, "POST", srv.URL+"/api/v1/monitors", monitorJSON("api", "http", "http://api/", ""), &api)

	var page statusPageResponse
	body := `{"slug":"shop","title":" Mağaza ","description":"Müşteriye dönük","sections":[{"name":" Vitrin "}],
		"monitors":[{"monitor_id":2,"display_name":" API ","section":"Vitrin"},{"monitor_id":1}],
		"rules":[{"tag":" public ","section":"Vitrin"}],"noindex":true,"language":"tr"}`
	if code := do(t, "POST", base, body, &page); code != http.StatusCreated {
		t.Fatalf("create: status = %d", code)
	}
	if page.Slug != "shop" || page.Title != "Mağaza" || !page.Published || len(page.Monitors) != 2 ||
		page.Monitors[0] != (statusPageMonitor{MonitorID: 2, DisplayName: "API", Section: "Vitrin"}) || page.Monitors[1].MonitorID != 1 ||
		len(page.Sections) != 1 || page.Sections[0].Name != "Vitrin" || len(page.Rules) != 1 || page.Rules[0] != (statusPageRule{Tag: "public", Section: "Vitrin"}) {
		t.Errorf("created = %+v", page)
	}
	// The settings: their defaults, and what was given.
	if !page.NoIndex || page.HidePaused || page.Language != "tr" || !page.DetailPages || page.HasPassword {
		t.Errorf("settings of the created page = %+v", page)
	}
	var list []statusPageResponse
	if do(t, "GET", base, "", &list); len(list) != 1 || list[0].ID != page.ID {
		t.Errorf("list = %+v", list)
	}

	// An update changes what it names and leaves the rest.
	var updated statusPageResponse
	if code := do(t, "PATCH", base+"/1", `{"announcement":"Cumartesi 02:00 planlı bakım","published":false,"hide_paused":true,"detail_pages":false,"language":""}`, &updated); code != http.StatusOK ||
		updated.Announcement == "" || updated.Published || updated.Title != "Mağaza" || len(updated.Monitors) != 2 || len(updated.Rules) != 1 ||
		!updated.HidePaused || updated.DetailPages || updated.Language != "" || !updated.NoIndex {
		t.Errorf("update: status = %d, page = %+v", code, updated)
	}
	if code := do(t, "PATCH", base+"/1", `{"monitors":[{"monitor_id":1}]}`, &updated); code != http.StatusOK || len(updated.Monitors) != 1 || len(updated.Sections) != 1 {
		t.Errorf("new monitors: status = %d, page = %+v", code, updated)
	}
	if code := do(t, "PATCH", base+"/1", `{"rules":[],"sections":[]}`, &updated); code != http.StatusOK || len(updated.Rules) != 0 || len(updated.Sections) != 0 {
		t.Errorf("no rules and sections: status = %d, page = %+v", code, updated)
	}

	for name, tc := range map[string]struct {
		method, url, body string
		want              int
	}{
		"no title":            {"POST", base, `{"slug":"a"}`, http.StatusBadRequest},
		"slug with space":     {"POST", base, `{"slug":"my page","title":"x"}`, http.StatusBadRequest},
		"slug upper case":     {"POST", base, `{"slug":"Shop","title":"x"}`, http.StatusBadRequest},
		"slug as a path":      {"POST", base, `{"slug":"a/b","title":"x"}`, http.StatusBadRequest},
		"unknown monitor":     {"POST", base, `{"slug":"a","title":"x","monitors":[{"monitor_id":99}]}`, http.StatusBadRequest},
		"monitor twice":       {"POST", base, `{"slug":"a","title":"x","monitors":[{"monitor_id":1},{"monitor_id":1}]}`, http.StatusBadRequest},
		"unknown section":     {"POST", base, `{"slug":"a","title":"x","monitors":[{"monitor_id":1,"section":"nope"}]}`, http.StatusBadRequest},
		"rule section":        {"POST", base, `{"slug":"a","title":"x","rules":[{"tag":"x","section":"nope"}]}`, http.StatusBadRequest},
		"section twice":       {"POST", base, `{"slug":"a","title":"x","sections":[{"name":"a"},{"name":"a"}]}`, http.StatusBadRequest},
		"nameless section":    {"POST", base, `{"slug":"a","title":"x","sections":[{"name":" "}]}`, http.StatusBadRequest},
		"rule with a bad tag": {"POST", base, `{"slug":"a","title":"x","rules":[{"tag":"a,b"}]}`, http.StatusBadRequest},
		"rule with a bad ns":  {"POST", base, `{"slug":"a","title":"x","rules":[{"namespace":"a/b"}]}`, http.StatusBadRequest},
		"unknown language":    {"POST", base, `{"slug":"a","title":"x","language":"de"}`, http.StatusBadRequest},
		"weak password":       {"POST", base, `{"slug":"a","title":"x","password":"password"}`, http.StatusBadRequest},
		"slug taken":          {"POST", base, `{"slug":"shop","title":"x"}`, http.StatusConflict},
		"update unknown":      {"PATCH", base + "/99", `{"title":"x"}`, http.StatusNotFound},
		"update bad slug":     {"PATCH", base + "/1", `{"slug":""}`, http.StatusBadRequest},
		"get unknown":         {"GET", base + "/99", "", http.StatusNotFound},
	} {
		if code := do(t, tc.method, tc.url, tc.body, nil); code != tc.want {
			t.Errorf("%s: status = %d, want %d", name, code, tc.want)
		}
	}
	// A section cannot go while a monitor or a rule is in it.
	if code := do(t, "PATCH", base+"/1", `{"sections":[{"name":"A"}],"monitors":[{"monitor_id":1,"section":"A"}],"rules":[{"tag":"x","section":"A"}]}`, nil); code != http.StatusOK {
		t.Errorf("a section with a monitor and a rule: status = %d", code)
	}
	for _, body := range []string{`{"sections":[]}`, `{"sections":[{"name":"B"}]}`} {
		if code := do(t, "PATCH", base+"/1", body, nil); code != http.StatusBadRequest {
			t.Errorf("sections %s while A is in use: status = %d, want 400", body, code)
		}
	}

	if code := do(t, "DELETE", base+"/1", "", nil); code != http.StatusNoContent {
		t.Errorf("delete: status = %d", code)
	}
	if code := do(t, "DELETE", base+"/1", "", nil); code != http.StatusNotFound {
		t.Errorf("second delete: status = %d", code)
	}
}

// A page that shows no monitor and has no rule belongs to no namespace, so
// to no editor: only admins see and manage it, made so or emptied when its
// monitors were deleted. A rule in an editor's namespace is theirs; one
// beyond it is not.
func TestStatusPageWithoutMonitorsIsTheAdmins(t *testing.T) {
	srv, _ := newTestAPI(t)
	base := srv.URL + "/api/v1/status-pages"
	addUser(t, srv.URL, "shopper", store.RoleEditor, "shop")
	addUser(t, srv.URL, "biller", store.RoleEditor, "billing")
	shop, _ := signIn(t, srv.URL, "shopper", "parola-123")
	billing, _ := signIn(t, srv.URL, "biller", "parola-123")
	var cart monitorResponse
	do(t, "POST", srv.URL+"/api/v1/monitors", monitorJSON("cart", "tcp", "cart:80", `"namespace":"shop"`), &cart)

	slugs := func(token string) string {
		t.Helper()
		var list []statusPageResponse
		if code := doAs(t, token, "GET", base, "", &list); code != http.StatusOK {
			t.Fatalf("list: status = %d", code)
		}
		var out []string
		for _, p := range list {
			out = append(out, p.Slug)
		}
		return strings.Join(out, ",")
	}
	// What an editor may not manage does not exist for them.
	notTheirs := func(who, token string, page int64) {
		t.Helper()
		for _, req := range [][2]string{{"GET", ""}, {"PATCH", `{"announcement":"Parolanızı sıfırlayın"}`}, {"DELETE", ""}} {
			if code := doAs(t, token, req[0], base+"/"+itoa(page), req[1], nil); code != http.StatusNotFound {
				t.Errorf("%s: %s page %d: status = %d, want 404", who, req[0], page, code)
			}
		}
		for _, req := range [][2]string{{"GET", ""}, {"POST", `{"kind":"notice","title":"x"}`}} {
			if code := doAs(t, token, req[0], base+"/"+itoa(page)+"/updates", req[1], nil); code != http.StatusNotFound {
				t.Errorf("%s: %s updates of page %d: status = %d, want 404", who, req[0], page, code)
			}
		}
	}

	// An announcement-only page of the admins.
	var company statusPageResponse
	if code := do(t, "POST", base, `{"slug":"company","title":"Şirket","announcement":"Bakım yok"}`, &company); code != http.StatusCreated {
		t.Fatalf("admin creates a page without monitors: status = %d", code)
	}
	if got := slugs(shop); got != "" {
		t.Errorf("editor's pages = %q", got)
	}
	notTheirs("shop editor", shop, company.ID)

	// Editors make no such page, nor empty one of theirs, nor one reaching
	// beyond their namespaces.
	for name, body := range map[string]string{
		"without monitors":        `{"slug":"mine","title":"x"}`,
		"with none of them":       `{"slug":"mine","title":"x","monitors":[]}`,
		"in another's scope":      `{"slug":"mine","title":"x","monitors":[{"monitor_id":` + itoa(cart.ID) + `}]}`,
		"rule for another's":      `{"slug":"mine","title":"x","rules":[{"namespace":"shop"}]}`,
		"rule for any namespace":  `{"slug":"mine","title":"x","rules":[{"tag":"public"}]}`,
		"rule for every monitor":  `{"slug":"mine","title":"x","rules":[{}]}`,
		"monitor and a wide rule": `{"slug":"mine","title":"x","monitors":[{"monitor_id":` + itoa(cart.ID) + `}],"rules":[{"namespace":"billing"}]}`,
	} {
		token := billing
		if strings.Contains(name, "wide") {
			token = shop
		}
		if code := doAs(t, token, "POST", base, body, nil); code != http.StatusBadRequest {
			t.Errorf("editor creates a page %s: status = %d, want 400", name, code)
		}
	}
	var own, ruled statusPageResponse
	if code := doAs(t, shop, "POST", base, `{"slug":"shop","title":"Mağaza","monitors":[{"monitor_id":`+itoa(cart.ID)+`}]}`, &own); code != http.StatusCreated {
		t.Fatalf("editor creates a page: status = %d", code)
	}
	if code := doAs(t, shop, "POST", base, `{"slug":"shop-all","title":"Mağaza (hepsi)","rules":[{"namespace":"shop"}]}`, &ruled); code != http.StatusCreated {
		t.Fatalf("editor creates a page of a rule: status = %d", code)
	}
	if code := doAs(t, shop, "PATCH", base+"/"+itoa(own.ID), `{"monitors":[]}`, nil); code != http.StatusBadRequest {
		t.Errorf("editor empties their page: status = %d, want 400", code)
	}
	if code := doAs(t, shop, "PATCH", base+"/"+itoa(own.ID), `{"title":"Mağaza servisleri"}`, nil); code != http.StatusOK {
		t.Errorf("editor renames their page: status = %d", code)
	}
	if got := slugs(shop); got != "shop-all,shop" { // by title
		t.Errorf("shop editor's pages = %q", got)
	}
	notTheirs("billing editor", billing, own.ID)
	notTheirs("billing editor", billing, ruled.ID)
	// An admin widening the rule takes the page from the editor.
	if code := do(t, "PATCH", base+"/"+itoa(ruled.ID), `{"rules":[{"tag":"public"}]}`, nil); code != http.StatusOK {
		t.Fatalf("admin widens the rule: status = %d", code)
	}
	notTheirs("shop editor", shop, ruled.ID)

	// Its only monitor deleted, the page shows nothing and is the admins'.
	if code := doAs(t, shop, "DELETE", srv.URL+"/api/v1/monitors/"+itoa(cart.ID), "", nil); code != http.StatusNoContent {
		t.Fatalf("delete the monitor: status = %d", code)
	}
	for who, token := range map[string]string{"shop editor": shop, "billing editor": billing} {
		if got := slugs(token); got != "" {
			t.Errorf("%s's pages after the monitor went = %q", who, got)
		}
		notTheirs(who, token, own.ID)
	}
	if got := slugs(testToken); got != "shop-all,shop,company" {
		t.Errorf("admin's pages = %q", got)
	}
	var page statusPageResponse
	if code := do(t, "PATCH", base+"/"+itoa(own.ID), `{"published":false}`, &page); code != http.StatusOK || page.Published || len(page.Monitors) != 0 {
		t.Errorf("admin unpublishes the emptied page: status = %d, page = %+v", code, page)
	}
	if code := do(t, "DELETE", base+"/"+itoa(company.ID), "", nil); code != http.StatusNoContent {
		t.Errorf("admin deletes their page: status = %d", code)
	}
}

// mkMonitor creates a monitor straight in the store, as discovery would.
func mkMonitor(t *testing.T, db *store.SQLite, name, namespace, source string, enabled bool, tags ...string) store.Monitor {
	t.Helper()
	m, err := db.CreateMonitor(context.Background(), store.Monitor{Name: name, Namespace: namespace, Source: source, Type: store.MonitorTCP,
		Target: "internal.corp:1", Interval: time.Minute, Timeout: time.Second, FailureThreshold: 1, Enabled: enabled, Tags: tags})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestStatusPageView(t *testing.T) {
	srv, db := newTestAPI(t) // three days of history, incidents of the last two
	ctx := context.Background()
	web := mkMonitor(t, db, "shop/service/web:80", "shop", "service/web:80", true)
	api := mkMonitor(t, db, "api", "", "", true)
	paused := mkMonitor(t, db, "batch", "", "", false)
	fresh := mkMonitor(t, db, "fresh", "", "", true)
	hidden := mkMonitor(t, db, "hidden", "", "", true)

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
	if code := public(t, "GET", url, "", nil, &view); code != http.StatusOK {
		t.Fatalf("view: status = %d", code)
	}
	if !view.From.Equal(today.Add(-48 * time.Hour)) {
		t.Errorf("from = %s, want the start of the day before yesterday", view.From)
	}
	if view.Title != "Mağaza" || view.Announcement != "Bakım var" || view.Days != 3 || view.State != publicDegraded ||
		len(view.Sections) != 1 || view.Sections[0].Name != "" || len(view.Sections[0].Monitors) != 4 ||
		view.Language != "en" || view.NoIndex || !view.DetailPages {
		t.Fatalf("view = %+v", view)
	}
	monitors := view.Sections[0].Monitors
	w := monitors[0]
	if w.Name != "Web sitesi" || w.State != publicUp || len(w.History) != 3 || w.History[0] == nil || *w.History[0] != 50 ||
		w.History[1] != nil || w.History[2] == nil || *w.History[2] != 100 || w.UptimePercent == nil || int(*w.UptimePercent) != 66 {
		t.Errorf("web = %+v", w)
	}
	// Uptime over a day, the page's three days, a week and a month: today's
	// check alone, then all three.
	seconds := func(u []uptimeView) []int {
		var out []int
		for _, w := range u {
			out = append(out, w.WindowSeconds)
		}
		return out
	}
	if got := seconds(w.Uptime); len(got) != 4 || got[0] != 86400 || got[1] != 3*86400 || got[2] != 7*86400 || got[3] != 30*86400 {
		t.Errorf("uptime windows = %v", got)
	}
	if w.Uptime[0].UptimePercent == nil || *w.Uptime[0].UptimePercent != 100 || w.Uptime[1].UptimePercent == nil || int(*w.Uptime[1].UptimePercent) != 66 {
		t.Errorf("web uptime = %+v, %+v", w.Uptime[0], w.Uptime[1])
	}
	if a := monitors[1]; a.Name != "api" || a.State != publicDown || *a.UptimePercent != 0 || *a.Uptime[0].UptimePercent != 0 {
		t.Errorf("api = %+v", a)
	}
	if p := monitors[2]; p.State != publicPaused || p.UptimePercent != nil || p.Uptime[0].UptimePercent != nil {
		t.Errorf("paused = %+v", p)
	}
	if f := monitors[3]; f.State != publicPending {
		t.Errorf("fresh = %+v", f)
	}
	_ = paused
	_ = fresh
	// The page's uptime is the mean of the monitors that have checks: web
	// at 100 and api at 0 over the day.
	if got := seconds(view.Uptime); len(got) != 4 || view.Uptime[0].UptimePercent == nil || *view.Uptime[0].UptimePercent != 50 {
		t.Errorf("page uptime = %+v", view.Uptime)
	}
	// The open outage and the recent one, newest first; nothing older, and
	// nothing of a monitor the page does not show.
	if len(view.Incidents) != 2 || view.Incidents[0].MonitorName != "api" || view.Incidents[0].MonitorID != api.ID || view.Incidents[0].ResolvedAt != nil ||
		view.Incidents[1].MonitorName != "Web sitesi" || view.Incidents[1].ResolvedAt == nil {
		t.Errorf("incidents = %+v", view.Incidents)
	}
	if len(view.Updates) != 0 || len(view.Maintenance) != 0 {
		t.Errorf("updates = %+v, maintenance = %+v, want none", view.Updates, view.Maintenance)
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

	// With hide_paused the paused monitor is left out; the page's own
	// language wins over the instance's.
	do(t, "PATCH", srv.URL+"/api/v1/status-pages/1", `{"hide_paused":true,"language":"tr","noindex":true}`, nil)
	public(t, "GET", url, "", nil, &view)
	if len(view.Sections[0].Monitors) != 3 || view.Sections[0].Monitors[2].Name != "fresh" || view.Language != "tr" || !view.NoIndex {
		t.Errorf("with hide_paused: monitors = %+v, language = %q", view.Sections[0].Monitors, view.Language)
	}

	// A discovered monitor without a display name shows the object's name.
	do(t, "PATCH", srv.URL+"/api/v1/status-pages/1", `{"monitors":[{"monitor_id":1}]}`, nil)
	public(t, "GET", url, "", nil, &view)
	if view.Sections[0].Monitors[0].Name != "web:80" || view.State != publicUp {
		t.Errorf("after the update: name = %q, state = %q", view.Sections[0].Monitors[0].Name, view.State)
	}
	// An unpublished page is not served, nor is an unknown one.
	do(t, "PATCH", srv.URL+"/api/v1/status-pages/1", `{"published":false}`, nil)
	for _, slug := range []string{"shop", "nope"} {
		if code := public(t, "GET", srv.URL+"/api/v1/status-pages/by-slug/"+slug+"/view", "", nil, nil); code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", slug, code)
		}
	}
}

// Sections group the monitors; rules add those they match after the
// explicit ones of their section, by name, once each, and are evaluated
// when the page is served, so a monitor found later appears by itself.
func TestStatusPageSectionsAndRules(t *testing.T) {
	srv, db := newTestAPI(t)
	a := mkMonitor(t, db, "shop/service/a:80", "shop", "service/a:80", true, "prod")
	b := mkMonitor(t, db, "shop/service/b:80", "shop", "service/b:80", true, "prod", "public")
	c := mkMonitor(t, db, "billing-c", "billing", "", true, "prod")
	d := mkMonitor(t, db, "docs", "", "", true, "public")
	e := mkMonitor(t, db, "explicit", "", "", true)
	stopped := mkMonitor(t, db, "shop/service/stopped:80", "shop", "service/stopped:80", false)

	base := srv.URL + "/api/v1/status-pages"
	body := `{"slug":"all","title":"Hepsi","sections":[{"name":"Mağaza"},{"name":"Diğer"}],
		"monitors":[{"monitor_id":` + itoa(e.ID) + `},{"monitor_id":` + itoa(a.ID) + `,"display_name":"A servisi","section":"Mağaza"}],
		"rules":[{"namespace":"shop","section":"Mağaza"},{"tag":"public","section":"Diğer"},{}]}`
	var page statusPageResponse
	if code := do(t, "POST", base, body, &page); code != http.StatusCreated {
		t.Fatalf("create: status = %d", code)
	}

	// What the editors see: the top group with the explicit monitor and
	// what the catch-all rule had left (c), the shop section with its
	// explicit monitor and then the shop rule's, the other section with
	// what carries the tag and was not taken already.
	names := func(sections []previewSection) string {
		var out []string
		for _, s := range sections {
			var names []string
			for _, m := range s.Monitors {
				name := m.Name
				if !m.Explicit {
					name += "*"
				}
				if !m.Enabled {
					name += "(paused)"
				}
				names = append(names, name)
			}
			out = append(out, s.Name+"["+strings.Join(names, " ")+"]")
		}
		return strings.Join(out, " ")
	}
	var preview statusPagePreview
	if code := do(t, "GET", base+"/"+itoa(page.ID)+"/monitors", "", &preview); code != http.StatusOK {
		t.Fatalf("preview: status = %d", code)
	}
	if got := names(preview.Sections); got != "[explicit billing-c*] Mağaza[A servisi b:80* stopped:80*(paused)] Diğer[docs*]" {
		t.Errorf("preview = %q", got)
	}
	if m := preview.Sections[1].Monitors[1]; m.MonitorID != b.ID || m.MonitorName != "shop/service/b:80" || m.Namespace != "shop" {
		t.Errorf("matched monitor = %+v", m)
	}
	_, _, _ = c, d, stopped

	// The public sees the same groups, and a monitor discovered after the
	// page was made, by name among the others.
	viewNames := func() string {
		t.Helper()
		var view statusPageView
		if code := public(t, "GET", srv.URL+"/api/v1/status-pages/by-slug/all/view", "", nil, &view); code != http.StatusOK {
			t.Fatalf("view: status = %d", code)
		}
		var out []string
		for _, s := range view.Sections {
			var names []string
			for _, m := range s.Monitors {
				names = append(names, m.Name)
			}
			out = append(out, s.Name+"["+strings.Join(names, " ")+"]")
		}
		return strings.Join(out, " ")
	}
	if got := viewNames(); got != "[explicit billing-c] Mağaza[A servisi b:80 stopped:80] Diğer[docs]" {
		t.Errorf("view = %q", got)
	}
	mkMonitor(t, db, "shop/service/aa:80", "shop", "service/aa:80", true)
	if got := viewNames(); got != "[explicit billing-c] Mağaza[A servisi aa:80 b:80 stopped:80] Diğer[docs]" {
		t.Errorf("view with a new monitor = %q", got)
	}
	// Paused monitors can be hidden; an empty section is left out of the
	// view.
	do(t, "PATCH", base+"/"+itoa(page.ID), `{"hide_paused":true,"rules":[{"namespace":"shop","section":"Mağaza"}]}`, nil)
	if got := viewNames(); got != "[explicit] Mağaza[A servisi aa:80 b:80]" {
		t.Errorf("view without paused and with one rule = %q", got)
	}
	do(t, "GET", base+"/"+itoa(page.ID)+"/monitors", "", &preview)
	if got := names(preview.Sections); got != "[explicit] Mağaza[A servisi aa:80* b:80*] Diğer[]" {
		t.Errorf("preview without paused = %q", got)
	}
	// Editors preview only their own pages.
	addUser(t, srv.URL, "viewer", store.RoleViewer, "*")
	viewer, _ := signIn(t, srv.URL, "viewer", "parola-123")
	if code := doAs(t, viewer, "GET", base+"/"+itoa(page.ID)+"/monitors", "", nil); code != http.StatusNotFound {
		t.Errorf("viewer previews: status = %d, want 404", code)
	}
}

// A discovered monitor the operator renamed goes by that name on a page,
// as in the UI; only the name discovery gave is shortened to the object's
// own. The incidents and notes of the page carry the same name.
func TestStatusPageShowsRenamedDiscoveredMonitorsByName(t *testing.T) {
	srv, db := newTestAPI(t)
	ctx := context.Background()
	renamed := mkMonitor(t, db, "Ödeme servisi", "shop", "service/payments:8080", true)
	mkMonitor(t, db, "shop/service/api:80", "shop", "service/api:80", true)
	if _, err := db.OpenIncident(ctx, store.Incident{MonitorID: renamed.ID, StartedAt: time.Now().Add(-time.Hour), OpenedAt: time.Now().Add(-time.Hour), Cause: "refused"}); err != nil {
		t.Fatal(err)
	}
	var page statusPageResponse
	if code := do(t, "POST", srv.URL+"/api/v1/status-pages", `{"slug":"shop","title":"Mağaza","rules":[{"namespace":"shop"}]}`, &page); code != http.StatusCreated {
		t.Fatalf("create: status = %d", code)
	}

	var view statusPageView
	if code := public(t, "GET", srv.URL+"/api/v1/status-pages/by-slug/shop/view", "", nil, &view); code != http.StatusOK {
		t.Fatalf("view: status = %d", code)
	}
	var names []string
	for _, m := range view.Sections[0].Monitors {
		names = append(names, m.Name)
	}
	if got := strings.Join(names, " "); got != "api:80 Ödeme servisi" {
		t.Errorf("view names = %q, want the object's name for the derived one and the operator's for the renamed", got)
	}
	if len(view.Incidents) != 1 || view.Incidents[0].MonitorName != "Ödeme servisi" {
		t.Errorf("view incidents = %+v, want one of %q", view.Incidents, "Ödeme servisi")
	}
	var preview statusPagePreview
	if code := do(t, "GET", srv.URL+"/api/v1/status-pages/"+itoa(page.ID)+"/monitors", "", &preview); code != http.StatusOK {
		t.Fatalf("preview: status = %d", code)
	}
	names = nil
	for _, m := range preview.Sections[0].Monitors {
		names = append(names, m.Name)
	}
	if got := strings.Join(names, " "); got != "api:80 Ödeme servisi" {
		t.Errorf("preview names = %q", got)
	}
}

func TestStatusPageUpdates(t *testing.T) {
	srv, db := newTestAPI(t) // MaxResults 3, four updates on the page
	ctx := context.Background()
	web := mkMonitor(t, db, "shop/service/web:80", "shop", "service/web:80", true)
	hidden := mkMonitor(t, db, "hidden", "billing", "", true)
	addUser(t, srv.URL, "shopper", store.RoleEditor, "shop")
	shop, _ := signIn(t, srv.URL, "shopper", "parola-123")
	base := srv.URL + "/api/v1/status-pages"
	var page, other statusPageResponse
	do(t, "POST", base, `{"slug":"shop","title":"Mağaza","monitors":[{"monitor_id":`+itoa(web.ID)+`,"display_name":"Web sitesi"}]}`, &page)
	do(t, "POST", base, `{"slug":"other","title":"Diğer","monitors":[{"monitor_id":`+itoa(hidden.ID)+`}]}`, &other)
	updates := base + "/" + itoa(page.ID) + "/updates"
	incident, _ := db.OpenIncident(ctx, store.Incident{MonitorID: web.ID, StartedAt: time.Now().Add(-time.Hour), Cause: "dial tcp internal.corp:1: refused"})
	elsewhere, _ := db.OpenIncident(ctx, store.Incident{MonitorID: hidden.ID, StartedAt: time.Now().Add(-time.Hour), Cause: "x"})

	var first updateResponse
	if code := doAs(t, shop, "POST", updates, `{"kind":"investigating","title":" Ödeme yavaş ","body":"Bakıyoruz.\r\nBir saat içinde haber vereceğiz.","incident_id":`+itoa(incident.ID)+`}`, &first); code != http.StatusCreated {
		t.Fatalf("create: status = %d", code)
	}
	if first.Kind != "investigating" || first.Title != "Ödeme yavaş" || first.Body != "Bakıyoruz.\nBir saat içinde haber vereceğiz." ||
		first.IncidentID == nil || *first.IncidentID != incident.ID || first.CreatedBy != "shopper" || first.PageID != page.ID {
		t.Errorf("created = %+v", first)
	}
	for name, body := range map[string]string{
		"unknown kind":      `{"kind":"shrug","title":"x"}`,
		"no title":          `{"kind":"notice","title":" "}`,
		"unknown incident":  `{"kind":"notice","title":"x","incident_id":999}`,
		"other's incident":  `{"kind":"notice","title":"x","incident_id":` + itoa(elsewhere.ID) + `}`,
		"control character": `{"kind":"notice","title":"x","body":"a\u0007b"}`,
	} {
		if code := doAs(t, shop, "POST", updates, body, nil); code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, code)
		}
	}
	var second, third updateResponse
	do(t, "POST", updates, `{"kind":"identified","title":"Veritabanı","incident_id":`+itoa(incident.ID)+`}`, &second)
	do(t, "POST", updates, `{"kind":"notice","title":"Yeni sürüm"}`, &third)
	var list []updateResponse
	if code := doAs(t, shop, "GET", updates, "", &list); code != http.StatusOK || len(list) != 3 || list[0].ID != third.ID || list[2].ID != first.ID {
		t.Errorf("list: status = %d, updates = %+v (want newest first)", code, list)
	}
	if code := doAs(t, shop, "GET", updates+"?limit=1", "", &list); code != http.StatusOK || len(list) != 1 {
		t.Errorf("limited list: status = %d, updates = %+v", code, list)
	}

	var changed updateResponse
	if code := doAs(t, shop, "PATCH", updates+"/"+itoa(first.ID), `{"kind":"resolved","body":"","incident_id":null}`, &changed); code != http.StatusOK ||
		changed.Kind != "resolved" || changed.Title != "Ödeme yavaş" || changed.Body != "" || changed.IncidentID == nil {
		t.Errorf("change: status = %d, update = %+v (a null incident_id is no change)", code, changed)
	}
	if code := doAs(t, shop, "PATCH", updates+"/"+itoa(first.ID), `{"incident_id":0}`, &changed); code != http.StatusOK || changed.IncidentID != nil {
		t.Errorf("detach: status = %d, update = %+v", code, changed)
	}
	// An update belongs to its page; other pages and other people get 404.
	addUser(t, srv.URL, "biller", store.RoleEditor, "billing")
	billing, _ := signIn(t, srv.URL, "biller", "parola-123")
	for name, tc := range map[string]struct {
		token, method, url string
		want               int
	}{
		"other page":         {testToken, "PATCH", base + "/" + itoa(other.ID) + "/updates/" + itoa(first.ID), http.StatusNotFound},
		"unknown update":     {testToken, "DELETE", updates + "/999", http.StatusNotFound},
		"not their page":     {billing, "GET", updates, http.StatusNotFound},
		"not their update":   {billing, "DELETE", updates + "/" + itoa(first.ID), http.StatusNotFound},
		"their own page":     {billing, "GET", base + "/" + itoa(other.ID) + "/updates", http.StatusOK},
		"editor deletes own": {shop, "DELETE", updates + "/" + itoa(third.ID), http.StatusNoContent},
	} {
		body := ""
		if tc.method == "PATCH" {
			body = `{"title":"x"}`
		}
		if code := doAs(t, tc.token, tc.method, tc.url, body, nil); code != tc.want {
			t.Errorf("%s: status = %d, want %d", name, code, tc.want)
		}
	}
	audit, _ := db.ListAudit(ctx, 100)
	actions := map[string]int{}
	for _, e := range audit {
		if e.ObjectType == "status_page_update" {
			actions[e.Action]++
		}
	}
	if actions["create"] != 3 || actions["update"] != 2 || actions["delete"] != 1 {
		t.Errorf("audit entries about updates = %v, want 3 creates, 2 updates and a delete", actions)
	}

	// The view merges the notes flagged for status pages, as "note" titled
	// with the monitor's name on the page, newest first, up to the limit.
	db.CreateIncidentNote(ctx, store.IncidentNote{IncidentID: incident.ID, Author: "ayse", Text: "İç not"})
	db.CreateIncidentNote(ctx, store.IncidentNote{IncidentID: incident.ID, Author: "ayse", Text: "Rolled back", ShowOnStatusPage: true})
	db.CreateIncidentNote(ctx, store.IncidentNote{IncidentID: elsewhere.ID, Author: "ayse", Text: "Başka sayfanın", ShowOnStatusPage: true})
	do(t, "POST", updates, `{"kind":"monitoring","title":"İzliyoruz","incident_id":`+itoa(incident.ID)+`}`, nil)
	do(t, "POST", updates, `{"kind":"maintenance","title":"Gece bakımı"}`, nil)
	var view statusPageView
	url := srv.URL + "/api/v1/status-pages/by-slug/shop/view"
	if code := public(t, "GET", url, "", nil, &view); code != http.StatusOK {
		t.Fatalf("view: status = %d", code)
	}
	if len(view.Updates) != 1 || view.Updates[0].Day != time.Now().UTC().Format(time.DateOnly) || len(view.Updates[0].Updates) != 4 {
		t.Fatalf("updates = %+v (want one day with four entries)", view.Updates)
	}
	var kinds, titles []string
	for _, u := range view.Updates[0].Updates {
		kinds = append(kinds, u.Kind)
		titles = append(titles, u.Title)
	}
	if got := strings.Join(kinds, ","); got != "maintenance,monitoring,note,identified" {
		t.Errorf("kinds = %s", got)
	}
	if got := strings.Join(titles, ","); got != "Gece bakımı,İzliyoruz,Web sitesi,Veritabanı" {
		t.Errorf("titles = %s", got)
	}
	if u := view.Updates[0].Updates[1]; u.MonitorName != "Web sitesi" {
		t.Errorf("an update about an incident names its monitor: %+v", u)
	}
	if u := view.Updates[0].Updates[2]; u.Body != "Rolled back" || u.MonitorName != "Web sitesi" {
		t.Errorf("note = %+v", u)
	}
	resp, _ := http.Get(url)
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	for _, internal := range []string{"İç not", "Başka sayfanın", "internal.corp", "shopper", "ayse"} {
		if strings.Contains(string(raw), internal) {
			t.Errorf("the public view contains %q", internal)
		}
	}
}

func TestStatusPageMaintenance(t *testing.T) {
	srv, db := newTestAPI(t) // a week ahead
	ctx := context.Background()
	web := mkMonitor(t, db, "web", "shop", "", true)
	api := mkMonitor(t, db, "api", "billing", "", true)
	other := mkMonitor(t, db, "other", "billing", "", true)
	do(t, "POST", srv.URL+"/api/v1/status-pages", `{"slug":"p","title":"p","monitors":[{"monitor_id":`+itoa(web.ID)+`,"display_name":"Web"},{"monitor_id":`+itoa(api.ID)+`}]}`, nil)

	now := time.Now().UTC().Truncate(time.Second)
	add := func(name string, start, end time.Time, namespace string, monitorID int64, repeat store.Repeat) {
		t.Helper()
		_, err := db.CreateMaintenance(ctx, store.Maintenance{Name: name, StartsAt: start, EndsAt: end, Namespace: namespace, MonitorID: monitorID, Repeat: repeat, Timezone: "UTC"})
		if err != nil {
			t.Fatal(err)
		}
	}
	add("Ödeme bakımı", now.Add(-time.Hour), now.Add(time.Hour), "billing", 0, "")                               // in progress, api
	add("Veri merkezi", now.Add(2*time.Hour), now.Add(3*time.Hour), "", 0, "")                                   // upcoming, everything
	add("Diğer servis", now.Add(time.Hour), now.Add(2*time.Hour), "", other.ID, "")                              // a monitor not on the page
	add("Çok sonra", now.Add(8*24*time.Hour), now.Add(8*24*time.Hour+time.Hour), "", 0, "")                      // beyond the week
	add("Geçti", now.Add(-3*time.Hour), now.Add(-2*time.Hour), "", 0, "")                                        // over
	add("Gece", now.Add(-20*time.Hour), now.Add(-19*time.Hour), "", web.ID, store.RepeatDaily)                   // daily, next in 4 hours
	add("Ay sonu", now.Add(10*24*time.Hour), now.Add(10*24*time.Hour+time.Hour), "shop", 0, store.RepeatMonthly) // first in ten days

	var view statusPageView
	if code := public(t, "GET", srv.URL+"/api/v1/status-pages/by-slug/p/view", "", nil, &view); code != http.StatusOK {
		t.Fatalf("view: status = %d", code)
	}
	var got []string
	for _, m := range view.Maintenance {
		got = append(got, m.Name+" "+m.StartsAt.Sub(now).String()+" "+strings.Join(m.Monitors, "+"))
	}
	want := []string{"Ödeme bakımı -1h0m0s api", "Veri merkezi 2h0m0s Web+api", "Gece 4h0m0s Web"}
	if strings.Join(got, "; ") != strings.Join(want, "; ") {
		t.Errorf("maintenance = %q\nwant        %q", got, want)
	}
	if m := view.Maintenance[0]; !m.EndsAt.Equal(now.Add(time.Hour)) {
		t.Errorf("end of the window in progress = %s", m.EndsAt)
	}
	// The monitor in maintenance says so.
	if a := view.Sections[0].Monitors[1]; a.State != publicMaintenance || view.State != publicMaintenance {
		t.Errorf("api = %+v, page state = %s", a, view.State)
	}
}

func TestStatusPageDetail(t *testing.T) {
	srv, db := newTestAPI(t) // three days, a day of series
	ctx := context.Background()
	web := mkMonitor(t, db, "shop/service/web:80", "shop", "service/web:80", true)
	other := mkMonitor(t, db, "other", "", "", true)
	do(t, "POST", srv.URL+"/api/v1/status-pages", `{"slug":"p","title":"Mağaza","monitors":[{"monitor_id":`+itoa(web.ID)+`,"display_name":"Web sitesi"}]}`, nil)

	now := time.Now().UTC().Truncate(time.Millisecond)
	today := now.Truncate(24 * time.Hour)
	var results []store.CheckResult
	for i := 1; i <= 6; i++ {
		results = append(results, store.CheckResult{MonitorID: web.ID, CheckedAt: now.Add(-time.Duration(i) * 10 * time.Minute), Up: i != 3,
			Latency: time.Duration(100*i) * time.Millisecond, Error: map[bool]string{true: "dial tcp internal.corp:1: refused"}[i == 3]})
	}
	if err := db.InsertCheckResults(ctx, results); err != nil {
		t.Fatal(err)
	}
	if err := db.RollupCheckResults(ctx, today.Add(-72*time.Hour), now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	old, _ := db.OpenIncident(ctx, store.Incident{MonitorID: web.ID, StartedAt: today.Add(-80 * time.Hour), Cause: "old"})
	db.ResolveIncident(ctx, old.ID, today.Add(-79*time.Hour))
	recent, _ := db.OpenIncident(ctx, store.Incident{MonitorID: web.ID, StartedAt: now.Add(-40 * time.Minute), Cause: "dial tcp 10.0.0.7:80: refused"})
	db.ResolveIncident(ctx, recent.ID, now.Add(-20*time.Minute))
	db.OpenIncident(ctx, store.Incident{MonitorID: web.ID, StartedAt: now.Add(-5 * time.Minute), Cause: "going on"})

	url := srv.URL + "/api/v1/status-pages/by-slug/p/monitors/" + itoa(web.ID)
	var detail statusPageDetail
	if code := public(t, "GET", url, "", nil, &detail); code != http.StatusOK {
		t.Fatalf("detail: status = %d", code)
	}
	m := detail.Monitor
	if detail.Slug != "p" || detail.Title != "Mağaza" || detail.Language != "en" || m.ID != web.ID || m.Name != "Web sitesi" || m.State != publicDown ||
		len(m.History) != 3 || m.UptimePercent == nil || int(*m.UptimePercent) != 83 || len(m.Uptime) != 4 || detail.Days != 3 {
		t.Errorf("detail = %+v", detail)
	}
	if detail.Series.RangeSeconds != 86400 || len(detail.Series.Points) == 0 || detail.Series.Points[0].AvgLatencyMS == nil {
		t.Errorf("series = %+v", detail.Series)
	}
	// The outages of the last days, newest first: the open one and the
	// recent one, not the one before the page's days.
	if len(detail.Incidents) != 2 || detail.Incidents[0].ResolvedAt != nil || detail.Incidents[0].DurationSeconds < 290 ||
		detail.Incidents[1].ResolvedAt == nil || detail.Incidents[1].DurationSeconds != 1200 {
		t.Errorf("incidents = %+v", detail.Incidents)
	}
	resp, _ := http.Get(url)
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	for _, internal := range []string{"internal.corp", "10.0.0.7", "refused", "going on", "service/web", "shop/"} {
		if strings.Contains(string(raw), internal) {
			t.Errorf("the public detail contains %q", internal)
		}
	}

	// Nothing for a monitor the page does not show, for an unknown one,
	// or when the page has no detail pages.
	for _, path := range []string{itoa(other.ID), "999", "x"} {
		if code := public(t, "GET", srv.URL+"/api/v1/status-pages/by-slug/p/monitors/"+path, "", nil, nil); code != http.StatusNotFound {
			t.Errorf("monitor %s: status = %d, want 404", path, code)
		}
	}
	do(t, "PATCH", srv.URL+"/api/v1/status-pages/1", `{"detail_pages":false}`, nil)
	if code := public(t, "GET", url, "", nil, nil); code != http.StatusNotFound {
		t.Errorf("detail pages off: status = %d, want 404", code)
	}
}

func TestStatusPagePassword(t *testing.T) {
	srv, db := newTestAPI(t) // three failures lock a key
	web := mkMonitor(t, db, "web", "", "", true)
	base := srv.URL + "/api/v1/status-pages"
	var page statusPageResponse
	if code := do(t, "POST", base, `{"slug":"gizli","title":"Gizli sayfa","monitors":[{"monitor_id":`+itoa(web.ID)+`}],"password":"gizli-parola-1"}`, &page); code != http.StatusCreated || !page.HasPassword {
		t.Fatalf("create: status = %d, page = %+v", code, page)
	}
	do(t, "POST", base, `{"slug":"baska","title":"Başka","monitors":[{"monitor_id":`+itoa(web.ID)+`}],"password":"baska-parola-1"}`, nil)
	view := srv.URL + "/api/v1/status-pages/by-slug/gizli/view"
	detail := srv.URL + "/api/v1/status-pages/by-slug/gizli/monitors/" + itoa(web.ID)
	badge := srv.URL + "/api/v1/badges/" + itoa(web.ID) + "/status"
	unlock := srv.URL + "/api/v1/status-pages/by-slug/gizli/unlock"

	// Without the token: locked, with the title alone.
	var lock lockedResponse
	if code := public(t, "GET", view, "", nil, &lock); code != http.StatusUnauthorized || !lock.Locked || lock.Title != "Gizli sayfa" {
		t.Fatalf("view without a token: status = %d, body = %+v", code, lock)
	}
	for name, url := range map[string]string{"detail": detail, "badge": badge} {
		if code := public(t, "GET", url, "", nil, nil); code != http.StatusUnauthorized {
			t.Errorf("%s without a token: status = %d, want 401", name, code)
		}
	}
	raw, _ := json.Marshal(lock)
	if strings.Contains(string(raw), "web") {
		t.Errorf("the locked answer names a monitor: %s", raw)
	}

	// The password opens it, for the session's length, by either header.
	var session unlockResponse
	if code := public(t, "POST", unlock, `{"password":"yanlis"}`, nil, nil); code != http.StatusUnauthorized {
		t.Errorf("wrong password: status = %d, want 401", code)
	}
	if code := public(t, "POST", unlock, `{"password":"gizli-parola-1"}`, nil, &session); code != http.StatusOK || session.Token == "" ||
		session.ExpiresAt.Before(time.Now().Add(50*time.Minute)) || session.ExpiresAt.After(time.Now().Add(70*time.Minute)) {
		t.Fatalf("unlock: status = %d, session = %+v", code, session)
	}
	bearer := map[string]string{"Authorization": "Bearer " + session.Token}
	header := map[string]string{"X-Status-Page-Token": session.Token}
	var got statusPageView
	if code := public(t, "GET", view, "", bearer, &got); code != http.StatusOK || got.Title != "Gizli sayfa" || len(got.Sections) != 1 {
		t.Errorf("view with the bearer token: status = %d, view = %+v", code, got)
	}
	for name, url := range map[string]string{"view": view, "detail": detail, "badge": badge} {
		if code := public(t, "GET", url, "", header, nil); code != http.StatusOK {
			t.Errorf("%s with the header token: status = %d, want 200", name, code)
		}
	}
	// With both headers, whichever holds the page's token opens it: a
	// client signed in to Kubeat sends its own token as the bearer.
	for name, headers := range map[string]map[string]string{
		"the page token beside a Kubeat token":            {"Authorization": "Bearer " + testToken, "X-Status-Page-Token": session.Token},
		"the page token as the bearer beside a stale one": {"Authorization": "Bearer " + session.Token, "X-Status-Page-Token": "gizli.99999999999.xx"},
	} {
		for what, url := range map[string]string{"view": view, "detail": detail, "badge": badge} {
			if code := public(t, "GET", url, "", headers, nil); code != http.StatusOK {
				t.Errorf("%s with %s: status = %d, want 200", what, name, code)
			}
		}
	}
	// Another page's token, a forged one and a token of the old password
	// do not open it.
	var other unlockResponse
	public(t, "POST", srv.URL+"/api/v1/status-pages/by-slug/baska/unlock", `{"password":"baska-parola-1"}`, nil, &other)
	forged := strings.Replace(other.Token, "baska.", "gizli.", 1)
	for name, token := range map[string]string{"another page's": other.Token, "forged": forged, "garbage": "gizli.99999999999.xx", "empty": ""} {
		if code := public(t, "GET", view, "", map[string]string{"X-Status-Page-Token": token}, nil); code != http.StatusUnauthorized {
			t.Errorf("view with %s token: status = %d, want 401", name, code)
		}
	}
	if code := do(t, "PATCH", base+"/"+itoa(page.ID), `{"password":"yeni-parola-2"}`, nil); code != http.StatusOK {
		t.Fatalf("change the password: status = %d", code)
	}
	if code := public(t, "GET", view, "", bearer, nil); code != http.StatusUnauthorized {
		t.Errorf("view with the old password's token: status = %d, want 401", code)
	}

	// Guessing is throttled per page, like sign-ins per name.
	codes := []int{}
	for _, password := range []string{"a", "b", "c", "yeni-parola-2"} {
		codes = append(codes, public(t, "POST", unlock, `{"password":"`+password+`"}`, nil, nil))
	}
	if got := codes; got[0] != 401 || got[1] != 401 || got[2] != 401 || got[3] != http.StatusTooManyRequests {
		t.Errorf("answers to three guesses and the right password = %v, want 401 401 401 429", got)
	}
	// Other pages are not held up by it.
	if code := public(t, "POST", srv.URL+"/api/v1/status-pages/by-slug/baska/unlock", `{"password":"baska-parola-1"}`, nil, nil); code != http.StatusOK {
		t.Errorf("unlock another page while one is locked: status = %d", code)
	}
	for name, tc := range map[string]struct {
		url, body string
		want      int
	}{
		"unknown page": {srv.URL + "/api/v1/status-pages/by-slug/nope/unlock", `{"password":"x"}`, http.StatusNotFound},
		"bad slug":     {srv.URL + "/api/v1/status-pages/by-slug/No%20Page/unlock", `{"password":"x"}`, http.StatusNotFound},
		"not JSON":     {srv.URL + "/api/v1/status-pages/by-slug/baska/unlock", `{`, http.StatusBadRequest},
	} {
		if code := public(t, "POST", tc.url, tc.body, nil, nil); code != tc.want {
			t.Errorf("%s: status = %d, want %d", name, code, tc.want)
		}
	}

	// Clearing the password opens the page to all; unlocking a page without
	// one is pointless (asked of a page whose guesses are not locked).
	var cleared statusPageResponse
	if code := do(t, "PATCH", base+"/"+itoa(page.ID), `{"password":""}`, &cleared); code != http.StatusOK || cleared.HasPassword {
		t.Fatalf("clear the password: status = %d, page = %+v", code, cleared)
	}
	for name, url := range map[string]string{"view": view, "detail": detail, "badge": badge} {
		if code := public(t, "GET", url, "", nil, nil); code != http.StatusOK {
			t.Errorf("%s without a password: status = %d, want 200", name, code)
		}
	}
	do(t, "POST", base, `{"slug":"acik","title":"Açık","monitors":[{"monitor_id":`+itoa(web.ID)+`}]}`, nil)
	if code := public(t, "POST", srv.URL+"/api/v1/status-pages/by-slug/acik/unlock", `{"password":"x"}`, nil, nil); code != http.StatusBadRequest {
		t.Errorf("unlock a page without a password: status = %d, want 400", code)
	}
	// A password that would be refused for an account is refused here.
	for _, password := range []string{"kisa", "gizli", "password"} {
		if code := do(t, "PATCH", base+"/"+itoa(page.ID), `{"password":"`+password+`"}`, nil); code != http.StatusBadRequest {
			t.Errorf("password %q: status = %d, want 400", password, code)
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
	// A rule makes a monitor public as a listing does.
	do(t, "PATCH", srv.URL+"/api/v1/status-pages/1", `{"rules":[{}]}`, nil)
	if code, _, _ := get("2/status"); code != http.StatusOK {
		t.Errorf("badge of a monitor a rule matches: status = %d, want 200", code)
	}
	do(t, "PATCH", srv.URL+"/api/v1/status-pages/1", `{"rules":[],"published":false}`, nil)
	if code, _, _ := get("1/status"); code != http.StatusNotFound {
		t.Errorf("badge of an unpublished page: status = %d, want 404", code)
	}
}
