package api

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/k8s"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// outageStory stores the story of one outage of a monitor discovered from
// shop's api service: checks every 30 s, ten of them failing from minute 2,
// the incident opened at the third failure and resolved at minute 7, what
// was sent about it, a maintenance window in its middle. It returns the
// incident and the time it all began.
func outageStory(t *testing.T, db *store.SQLite) (store.Incident, time.Time) {
	t.Helper()
	ctx := context.Background()
	m, err := db.CreateMonitor(ctx, store.Monitor{Name: "shop/service/api:80", Type: store.MonitorTCP, Target: "api.shop.svc:80", Namespace: "shop",
		Source: "service/api:80", Interval: 30 * time.Second, Timeout: 5 * time.Second, FailureThreshold: 3, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC().Add(-30 * time.Minute).Truncate(time.Second)
	var results []store.CheckResult
	for i := range 20 {
		r := store.CheckResult{MonitorID: m.ID, CheckedAt: base.Add(time.Duration(i) * 30 * time.Second), Up: true, Latency: 10 * time.Millisecond}
		if i >= 4 && i < 14 {
			r.Up, r.Error, r.StatusCode = false, "unexpected status 503", 503
		}
		results = append(results, r)
	}
	if err := db.InsertCheckResults(ctx, results); err != nil {
		t.Fatal(err)
	}
	incident, err := db.OpenIncident(ctx, store.Incident{MonitorID: m.ID, StartedAt: base.Add(2 * time.Minute), OpenedAt: base.Add(3 * time.Minute), Cause: "unexpected status 503"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ResolveIncident(ctx, incident.ID, base.Add(7*time.Minute)); err != nil {
		t.Fatal(err)
	}
	incident, _ = db.GetIncident(ctx, incident.ID)

	ops, _ := db.CreateChannel(ctx, store.Channel{Name: "ops", Type: store.ChannelWebhook, Target: "https://hooks.example.com/x", Enabled: true})
	chat, _ := db.CreateChannel(ctx, store.Channel{Name: "shop-chat", Type: store.ChannelSlack, Target: "https://hooks.slack.com/y", Namespaces: []string{"shop"}, Enabled: true})
	err = db.RecordDeliveries(ctx, []store.Delivery{
		{IncidentID: incident.ID, ChannelID: ops.ID, ChannelName: "ops", ChannelType: store.ChannelWebhook, Event: "down",
			EventAt: base.Add(3 * time.Minute), At: base.Add(3*time.Minute + time.Second), Delivered: true},
		{IncidentID: incident.ID, ChannelID: chat.ID, ChannelName: "shop-chat", ChannelType: store.ChannelSlack, Event: "down", GroupSize: 2,
			EventAt: base.Add(3 * time.Minute), At: base.Add(3*time.Minute + 31*time.Second), Error: "Slack answered 404: no_service"},
		{IncidentID: incident.ID, ChannelID: ops.ID, ChannelName: "ops", ChannelType: store.ChannelWebhook, Event: "reminder",
			EventAt: base.Add(5 * time.Minute), At: base.Add(5*time.Minute + time.Second), Delivered: true},
		// A pager that has been deleted since.
		{IncidentID: incident.ID, ChannelID: 99, ChannelName: "old-pager", ChannelType: store.ChannelPagerDuty, Event: "up",
			EventAt: base.Add(7 * time.Minute), At: base.Add(7*time.Minute + time.Second), Error: "PagerDuty answered 400: Invalid routing key"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateMaintenance(ctx, store.Maintenance{Name: "deploy", StartsAt: base.Add(4 * time.Minute), EndsAt: base.Add(4*time.Minute + 30*time.Second), MonitorID: m.ID}); err != nil {
		t.Fatal(err)
	}
	// One that covers others only.
	if _, err := db.CreateMaintenance(ctx, store.Maintenance{Name: "billing", StartsAt: base, EndsAt: base.Add(time.Hour), Namespace: "billing"}); err != nil {
		t.Fatal(err)
	}
	return incident, base
}

func newIncidentAPI(t *testing.T, timelineLimit int) (string, *store.SQLite) {
	t.Helper()
	workloads := &fakeWorkloads{workload: k8s.Workload{Service: "api", Pods: []k8s.Pod{{Name: "api-7d9f-a", Ready: true}}}}
	srv, db := newTestAPIWith(t, func(o *Options) {
		o.Workloads, o.EventLimit, o.MaxResults, o.TimelineLimit = workloads, 5, 100, timelineLimit
	})
	return srv.URL, db
}

// kinds lists the kinds of a timeline, in its order.
func kinds(items []timelineItem) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = item.Kind
	}
	return out
}

func itemsOf(items []timelineItem, kind string) []timelineItem {
	var out []timelineItem
	for _, item := range items {
		if item.Kind == kind {
			out = append(out, item)
		}
	}
	return out
}

func TestIncidentPage(t *testing.T) {
	base, db := newIncidentAPI(t, 100)
	incident, start := outageStory(t, db)
	url := base + "/api/v1/incidents/" + itoa(incident.ID)
	var note noteResponse
	if code := do(t, "POST", url+"/notes", `{"text":"rolled back"}`, &note); code != http.StatusCreated {
		t.Fatalf("note: status %d", code)
	}

	var got incidentPageResponse
	if code := do(t, "GET", url, "", &got); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if got.Incident.ID != incident.ID || got.Incident.DurationSeconds != 300 || got.Incident.OpenedAt == nil || got.Incident.MonitorNamespace != "shop" {
		t.Errorf("incident = %+v", got.Incident)
	}
	if m := got.Monitor; m.Kind != "service" || m.Source != "service/api:80" || m.Namespace != "shop" || m.Type != "tcp" || m.FailureThreshold != 3 || m.IntervalSeconds != 30 {
		t.Errorf("monitor = %+v", m)
	}
	// From the first failure to the first success: ten checks, all failed.
	if got.Checks == nil || got.Checks.Total != 10 || got.Checks.Failed != 10 || got.Checks.Partial {
		t.Errorf("checks = %+v", got.Checks)
	}
	if !got.CanNote || got.EventsReason != "" {
		t.Errorf("can_note = %v, events_reason = %q", got.CanNote, got.EventsReason)
	}

	items := got.Timeline
	for i := 1; i < len(items); i++ {
		if items[i].At.Before(items[i-1].At) {
			t.Fatalf("timeline out of order at %d: %v", i, kinds(items))
		}
	}
	// It begins with the first failure, and at the third the incident
	// opens; the maintenance window comes before the check of its moment.
	if want := []string{"check_failed", "check_failed", "check_failed", "opened", "notification", "check_failed", "notification", "maintenance", "check_failed"}; !slices.Equal(kinds(items)[:9], want) {
		t.Errorf("timeline begins %v, want %v", kinds(items)[:9], want)
	}
	if first := items[0]; !first.At.Equal(incident.StartedAt) || first.Check == nil || first.Check.Error == nil || *first.Check.Error != "unexpected status 503" {
		t.Errorf("first item = %+v", first)
	}
	if checks := itemsOf(items, "check_failed"); len(checks) != 10 || len(itemsOf(items, "checks_omitted")) != 0 {
		t.Errorf("%d failed checks", len(checks))
	}
	if opened := itemsOf(items, "opened"); len(opened) != 1 || !opened[0].At.Equal(start.Add(3*time.Minute)) || opened[0].Opened.FailureThreshold != 3 || opened[0].Opened.Cause != "unexpected status 503" {
		t.Errorf("opened = %+v", opened)
	}
	if resolved := itemsOf(items, "resolved"); len(resolved) != 1 || resolved[0].Resolved.DurationSeconds != 300 || !resolved[0].At.Equal(start.Add(7*time.Minute)) {
		t.Errorf("resolved = %+v", resolved)
	}
	if reminders := itemsOf(items, "reminder"); len(reminders) != 1 || reminders[0].Reminder.DownForSeconds != 180 {
		t.Errorf("reminders = %+v", reminders)
	}
	if windows := itemsOf(items, "maintenance"); len(windows) != 1 || windows[0].Maintenance.Name != "deploy" || windows[0].Maintenance.MonitorID == nil {
		t.Errorf("maintenance = %+v", windows)
	}
	if notes := itemsOf(items, "note"); len(notes) != 1 || notes[0].Note.Text != "rolled back" || notes[0].Note.Author != "api-token" || !notes[0].Note.CanEdit {
		t.Errorf("notes = %+v", notes)
	}
	if events := itemsOf(items, "cluster_event"); len(events) != 1 || events[0].Event.Object != "Pod/api-7d9f-a" {
		t.Errorf("events = %+v", events)
	}
	notified := itemsOf(items, "notification")
	if len(notified) != 4 {
		t.Fatalf("notifications = %+v", notified)
	}
	chat, pager := notified[1].Notification, notified[3].Notification
	if chat.ChannelName != "shop-chat" || !chat.Grouped || chat.GroupSize != 2 || chat.Delivered || chat.Error != "Slack answered 404: no_service" || chat.ChannelDeleted {
		t.Errorf("chat = %+v", chat)
	}
	if pager.ChannelName != "old-pager" || !pager.ChannelDeleted || pager.Event != "up" || pager.Error == "" {
		t.Errorf("pager = %+v", pager)
	}

	// An editor of shop sees the reason of a channel limited to shop, not
	// that of a channel they cannot manage; a viewer sees neither and
	// cannot note; a user of billing does not see the incident at all.
	addUser(t, base, "ed", store.RoleEditor, "shop")
	addUser(t, base, "vi", store.RoleViewer, "shop")
	addUser(t, base, "bill", store.RoleEditor, "billing")
	for _, who := range []struct {
		name               string
		chatError, canNote bool
	}{{"ed", true, true}, {"vi", false, false}} {
		token, _ := signIn(t, base, who.name, "parola-123")
		var page incidentPageResponse
		if code := doAs(t, token, "GET", url, "", &page); code != http.StatusOK {
			t.Fatalf("%s: status %d", who.name, code)
		}
		n := itemsOf(page.Timeline, "notification")
		if (n[1].Notification.Error != "") != who.chatError || n[3].Notification.Error != "" || page.CanNote != who.canNote {
			t.Errorf("%s: chat error %q, pager error %q, can_note %v", who.name, n[1].Notification.Error, n[3].Notification.Error, page.CanNote)
		}
		if notes := itemsOf(page.Timeline, "note"); len(notes) != 1 || notes[0].Note.CanEdit {
			t.Errorf("%s: may edit the token's note: %+v", who.name, notes)
		}
	}
	token, _ := signIn(t, base, "bill", "parola-123")
	if code := doAs(t, token, "GET", url, "", nil); code != http.StatusNotFound {
		t.Errorf("another namespace's incident: status %d, want 404", code)
	}
	if code := do(t, "GET", base+"/api/v1/incidents/"+itoa(incident.ID+100), "", nil); code != http.StatusNotFound {
		t.Errorf("unknown incident: status %d, want 404", code)
	}
}

// A long outage shows how it began and how it ended, and how many failed
// checks are left out between the two.
func TestIncidentPageCutsLongOutages(t *testing.T) {
	base, db := newIncidentAPI(t, 4)
	incident, start := outageStory(t, db)
	var got incidentPageResponse
	if code := do(t, "GET", base+"/api/v1/incidents/"+itoa(incident.ID), "", &got); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	checks := itemsOf(got.Timeline, "check_failed")
	omitted := itemsOf(got.Timeline, "checks_omitted")
	if len(checks) != 4 || len(omitted) != 1 {
		t.Fatalf("checks %d, omitted %+v", len(checks), omitted)
	}
	gap := omitted[0].Omitted
	if gap.Count != 6 || !gap.From.Equal(start.Add(150*time.Second)) || !gap.To.Equal(start.Add(6*time.Minute)) || !omitted[0].At.Equal(gap.From) {
		t.Errorf("omitted = %+v", gap)
	}
	if !checks[1].At.Equal(gap.From) || !checks[2].At.Equal(gap.To) {
		t.Errorf("the gap is not between the ends: %v, %v", checks[1].At, checks[2].At)
	}
	// Four notifications fit in four.
	if len(itemsOf(got.Timeline, "notification")) != 4 || len(itemsOf(got.Timeline, "notifications_omitted")) != 0 {
		t.Errorf("timeline = %v", kinds(got.Timeline))
	}
}

func TestIncidentNotesAPI(t *testing.T) {
	srv, db := newTestAPIWith(t, func(o *Options) { o.MaxResults = 100 })
	base := srv.URL
	ctx := context.Background()
	m, _ := db.CreateMonitor(ctx, store.Monitor{Name: "api", Type: store.MonitorTCP, Target: "api:80", Namespace: "shop", Interval: time.Minute, Timeout: time.Second, FailureThreshold: 1, Enabled: true})
	incident, _ := db.OpenIncident(ctx, store.Incident{MonitorID: m.ID, StartedAt: time.Now().Add(-time.Hour), Cause: "refused"})
	other, _ := db.OpenIncident(ctx, store.Incident{MonitorID: m.ID, Kind: store.IncidentCert, StartedAt: time.Now().Add(-time.Hour), Cause: "cert"})
	notes := base + "/api/v1/incidents/" + itoa(incident.ID) + "/notes"

	tokens := map[string]string{}
	for name, u := range map[string]struct {
		role store.Role
		ns   string
	}{"ed": {store.RoleEditor, "shop"}, "ed2": {store.RoleEditor, "shop"}, "vi": {store.RoleViewer, "shop"}, "bill": {store.RoleEditor, "billing"}} {
		addUser(t, base, name, u.role, u.ns)
		tokens[name], _ = signIn(t, base, name, "parola-123")
	}

	var written noteResponse
	if code := doAs(t, tokens["ed"], "POST", notes, "{\"text\":\"  known: DB migration\\r\\nrolling back  \",\"show_on_status_page\":true}", &written); code != http.StatusCreated {
		t.Fatalf("create: status %d", code)
	}
	if written.Author != "ed" || written.Text != "known: DB migration\nrolling back" || !written.ShowOnStatusPage || !written.CanEdit || written.IncidentID != incident.ID {
		t.Errorf("written = %+v", written)
	}
	var problem errorBody
	for name, tt := range map[string]struct {
		token, body string
		want        int
	}{
		"a viewer":            {tokens["vi"], `{"text":"x"}`, http.StatusForbidden},
		"another namespace":   {tokens["bill"], `{"text":"x"}`, http.StatusNotFound},
		"no text":             {tokens["ed"], `{"show_on_status_page":true}`, http.StatusBadRequest},
		"blank text":          {tokens["ed"], `{"text":"   "}`, http.StatusBadRequest},
		"too long":            {tokens["ed"], `{"text":"` + strings.Repeat("x", maxNoteLength+1) + `"}`, http.StatusBadRequest},
		"a control character": {tokens["ed"], `{"text":"bell\u0007"}`, http.StatusBadRequest},
		"an unknown field":    {tokens["ed"], `{"text":"x","pinned":true}`, http.StatusBadRequest},
	} {
		if code := doAsError(t, tt.token, "POST", notes, tt.body, &problem); code != tt.want {
			t.Errorf("%s: status %d (%s), want %d", name, code, problem.Error, tt.want)
		}
	}
	noteURL := notes + "/" + itoa(written.ID)

	// Only the author, or an administrator, changes a note; the fields
	// left out stay.
	if code := doAsError(t, tokens["ed2"], "PATCH", noteURL, `{"text":"mine now"}`, &problem); code != http.StatusForbidden {
		t.Errorf("another editor's change: status %d", code)
	}
	var updated noteResponse
	if code := doAs(t, tokens["ed"], "PATCH", noteURL, `{"show_on_status_page":false}`, &updated); code != http.StatusOK ||
		updated.Text != written.Text || updated.ShowOnStatusPage || updated.UpdatedAt.Before(updated.CreatedAt) {
		t.Errorf("author's change: status %d, %+v", code, updated)
	}
	if code := do(t, "PATCH", noteURL, `{"text":"rolled back at 14:05"}`, &updated); code != http.StatusOK || updated.Text != "rolled back at 14:05" || updated.Author != "ed" {
		t.Errorf("administrator's change: status %d, %+v", code, updated)
	}
	// The note belongs to its incident.
	if code := do(t, "PATCH", base+"/api/v1/incidents/"+itoa(other.ID)+"/notes/"+itoa(written.ID), `{"text":"x"}`, nil); code != http.StatusNotFound {
		t.Errorf("through another incident: status %d", code)
	}

	var list []noteResponse
	if code := doAs(t, tokens["vi"], "GET", notes, "", &list); code != http.StatusOK || len(list) != 1 || list[0].CanEdit || list[0].Text != "rolled back at 14:05" {
		t.Errorf("viewer's list: status %d, %+v", code, list)
	}
	if code := doAs(t, tokens["bill"], "GET", notes, "", nil); code != http.StatusNotFound {
		t.Errorf("another namespace's list: status %d", code)
	}

	if code := doAsError(t, tokens["ed2"], "DELETE", noteURL, "", &problem); code != http.StatusForbidden {
		t.Errorf("another editor's delete: status %d", code)
	}
	if code := doAs(t, tokens["ed"], "DELETE", noteURL, "", nil); code != http.StatusNoContent {
		t.Errorf("author's delete: status %d", code)
	}
	if code := doAs(t, tokens["ed"], "DELETE", noteURL, "", nil); code != http.StatusNotFound {
		t.Errorf("deleting twice: status %d", code)
	}

	var audit []auditResponse
	do(t, "GET", base+"/api/v1/audit?limit=50", "", &audit)
	var actions []string
	for _, e := range audit {
		if e.ObjectType == "incident_note" && e.ObjectID == written.ID {
			actions = append(actions, e.Username+" "+e.Action)
		}
	}
	slices.Reverse(actions) // the log is newest first
	if want := []string{"ed create", "ed update", "api-token update", "ed delete"}; !slices.Equal(actions, want) {
		t.Errorf("audit = %v, want %v", actions, want)
	}
}

// incidentsFixture makes monitors in shop, billing and none, with incidents
// of both kinds, open and resolved; the shop monitor's name is a formula.
func incidentsFixture(t *testing.T, db *store.SQLite) time.Time {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	ids := map[string]int64{}
	for _, m := range []struct{ name, ns string }{{"=HYPERLINK(\"http://x\")", "shop"}, {"billing-api", "billing"}, {"loose", ""}} {
		created, err := db.CreateMonitor(ctx, store.Monitor{Name: m.name, Type: store.MonitorTCP, Target: "x:1", Namespace: m.ns, Interval: time.Minute, Timeout: time.Second, FailureThreshold: 1, Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		ids[m.ns] = created.ID
	}
	for _, i := range []struct {
		ns         string
		kind       store.IncidentKind
		start, end time.Duration // before now; no end leaves it open
		cause      string
	}{
		{"shop", store.IncidentDown, 5 * time.Hour, 4 * time.Hour, "-1 pods ready"},
		{"shop", store.IncidentCert, 3 * time.Hour, 0, "certificate expires"},
		{"billing", store.IncidentDown, 2 * time.Hour, 0, "refused"},
		{"", store.IncidentDown, 30 * time.Hour, 29*time.Hour + 50*time.Minute, "timeout"},
	} {
		opened, err := db.OpenIncident(ctx, store.Incident{MonitorID: ids[i.ns], Kind: i.kind, StartedAt: now.Add(-i.start), Cause: i.cause})
		if err != nil {
			t.Fatal(err)
		}
		if i.end != 0 {
			if err := db.ResolveIncident(ctx, opened.ID, now.Add(-i.end)); err != nil {
				t.Fatal(err)
			}
		}
	}
	return now
}

func TestIncidentListFilters(t *testing.T) {
	srv, db := newTestAPIWith(t, func(o *Options) { o.MaxResults = 100 })
	base := srv.URL + "/api/v1/incidents"
	now := incidentsFixture(t, db)

	list := func(token, query string) ([]string, string) {
		t.Helper()
		req, _ := http.NewRequest("GET", base+query, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("%s: status %d", query, res.StatusCode)
		}
		var got []incidentResponse
		if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		out := []string{}
		for _, i := range got {
			out = append(out, i.Cause)
		}
		return out, res.Header.Get("X-Total-Count")
	}
	from := url.QueryEscape(now.Add(-150 * time.Minute).Format(time.RFC3339))
	to := url.QueryEscape(now.Add(-90 * time.Minute).Format(time.RFC3339))
	for _, tt := range []struct {
		query, total string
		want         []string
	}{
		{"", "4", []string{"refused", "certificate expires", "-1 pods ready", "timeout"}},
		{"?namespace=shop&namespace=billing", "3", []string{"refused", "certificate expires", "-1 pods ready"}},
		{"?namespace=", "1", []string{"timeout"}},
		{"?kind=cert", "1", []string{"certificate expires"}},
		{"?state=open", "2", []string{"refused", "certificate expires"}},
		{"?state=resolved&kind=down", "2", []string{"-1 pods ready", "timeout"}},
		{"?q=BILLING", "1", []string{"refused"}},
		{"?from=" + from + "&to=" + to, "2", []string{"refused", "certificate expires"}},
		{"?sort=duration", "4", []string{"certificate expires", "refused", "-1 pods ready", "timeout"}},
		{"?sort=duration&order=asc", "4", []string{"timeout", "-1 pods ready", "refused", "certificate expires"}},
		{"?sort=monitor", "4", []string{"certificate expires", "-1 pods ready", "refused", "timeout"}},
		{"?limit=2&offset=1", "4", []string{"certificate expires", "-1 pods ready"}},
		{"?monitor_id=2", "1", []string{"refused"}},
	} {
		got, total := list(testToken, tt.query)
		if !slices.Equal(got, tt.want) || total != tt.total {
			t.Errorf("%q = %v (total %s), want %v (total %s)", tt.query, got, total, tt.want, tt.total)
		}
	}
	for _, q := range []string{"?kind=slow", "?state=closed", "?sort=name", "?order=up", "?offset=-1", "?from=yesterday",
		"?from=" + to + "&to=" + from, "?q=" + strings.Repeat("x", maxSearchLength+1), "?monitor_id=x"} {
		if code := do(t, "GET", base+q, "", nil); code != http.StatusBadRequest {
			t.Errorf("%q: status %d, want 400", q, code)
		}
	}

	// A viewer of shop sees shop's incidents only, whatever they ask for.
	addUser(t, srv.URL, "vi", store.RoleViewer, "shop")
	token, _ := signIn(t, srv.URL, "vi", "parola-123")
	if got, total := list(token, ""); !slices.Equal(got, []string{"certificate expires", "-1 pods ready"}) || total != "2" {
		t.Errorf("viewer = %v (total %s)", got, total)
	}
	if got, total := list(token, "?namespace=billing&namespace="); len(got) != 0 || total != "0" {
		t.Errorf("viewer asking for others = %v (total %s)", got, total)
	}

	// The export has every matching incident, in cells a spreadsheet does
	// not run.
	req, _ := http.NewRequest("GET", base+"?format=csv&namespace=shop&sort=started_at&order=asc", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	rows, err := csv.NewReader(res.Body).ReadAll()
	if err != nil || res.StatusCode != http.StatusOK || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/csv") ||
		!strings.Contains(res.Header.Get("Content-Disposition"), "kubeat-incidents-") {
		t.Fatalf("csv: status %d, %q, %v", res.StatusCode, res.Header.Get("Content-Type"), err)
	}
	if len(rows) != 3 || strings.Join(rows[0], ",") != "id,monitor_id,monitor,namespace,kind,state,started_at,opened_at,resolved_at,duration_seconds,cause" {
		t.Fatalf("csv = %q", rows)
	}
	first := rows[1]
	if first[2] != `'=HYPERLINK("http://x")` || first[5] != "resolved" || first[9] != "3600" || first[10] != "'-1 pods ready" {
		t.Errorf("first row = %q", first)
	}
	if rows[2][5] != "open" || rows[2][8] != "" {
		t.Errorf("open row = %q", rows[2])
	}
}

func TestStatsEndpoint(t *testing.T) {
	srv, db := newTestAPI(t)
	ctx := context.Background()
	now := incidentsFixture(t, db)
	// Six checks in the last hour, one of them failed.
	var results []store.CheckResult
	for i, id := range []int64{1, 1, 1, 1, 2, 2} {
		results = append(results, store.CheckResult{MonitorID: id, CheckedAt: now.Add(-time.Duration(i+1) * time.Minute), Up: i != 0, Latency: time.Millisecond})
	}
	if err := db.InsertCheckResults(ctx, results); err != nil {
		t.Fatal(err)
	}
	hour := now.Truncate(time.Hour).Add(-time.Hour)
	if err := db.RollupCheckResults(ctx, hour, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	var got statsResponse
	if code := do(t, "GET", srv.URL+"/api/v1/stats?days=1", "", &got); code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	// The day holds the shop outage of an hour and billing's, two hours
	// on; loose's ended a day and more ago.
	if got.Days != 1 || got.Monitors != 3 || got.Checks != 6 || got.UpChecks != 5 || got.Uptime == nil || *got.Uptime < 83 || *got.Uptime > 84 {
		t.Errorf("stats = %+v", got)
	}
	if got.Outages != 2 || got.DowntimeSeconds < 3*3600-5 || got.DowntimeSeconds > 3*3600+5 {
		t.Errorf("outages %d, downtime %d", got.Outages, got.DowntimeSeconds)
	}
	if got.MTTRSeconds == nil || *got.MTTRSeconds != (got.DowntimeSeconds+1)/2 && *got.MTTRSeconds != got.DowntimeSeconds/2 {
		t.Errorf("mttr = %v, downtime %d", got.MTTRSeconds, got.DowntimeSeconds)
	}
	if up := max(got.MonitoredSeconds-got.DowntimeSeconds, 0); got.MTBFSeconds == nil || *got.MTBFSeconds < up/2-1 || *got.MTBFSeconds > up/2+1 {
		t.Errorf("mtbf = %v, monitored %d, downtime %d", got.MTBFSeconds, got.MonitoredSeconds, got.DowntimeSeconds)
	}
	if got.OpenIncidents != 2 || got.OpenOutages != 1 || got.OpenCertWarnings != 1 {
		t.Errorf("open = %d (%d outages, %d certificates)", got.OpenIncidents, got.OpenOutages, got.OpenCertWarnings)
	}

	// A viewer of shop counts shop alone; a namespace without outages has
	// no MTTR or MTBF.
	addUser(t, srv.URL, "vi", store.RoleViewer, "shop")
	token, _ := signIn(t, srv.URL, "vi", "parola-123")
	if code := doAs(t, token, "GET", srv.URL+"/api/v1/stats?days=1", "", &got); code != http.StatusOK || got.Monitors != 1 || got.Outages != 1 ||
		got.OpenIncidents != 1 || got.OpenOutages != 0 || got.Checks != 4 {
		t.Errorf("viewer's stats = %+v", got)
	}
	got = statsResponse{}
	if code := do(t, "GET", srv.URL+"/api/v1/stats?days=1&namespace=", "", &got); code != http.StatusOK || got.Monitors != 1 || got.Outages != 0 ||
		got.MTTRSeconds != nil || got.MTBFSeconds != nil || got.Uptime != nil {
		t.Errorf("stats without outages = %+v", got)
	}
	for _, q := range []string{"?days=0", "?days=366", "?days=x"} {
		if code := do(t, "GET", srv.URL+"/api/v1/stats"+q, "", nil); code != http.StatusBadRequest {
			t.Errorf("%s: status %d", q, code)
		}
	}
}

func TestReportReliabilityAndSafeCSV(t *testing.T) {
	srv, db := newTestAPI(t)
	incidentsFixture(t, db)

	var got reportResponse
	if code := do(t, "GET", srv.URL+"/api/v1/reports?days=1&namespace=shop", "", &got); code != http.StatusOK || len(got.Namespaces) != 1 {
		t.Fatalf("status %d, %+v", code, got)
	}
	shop := got.Namespaces[0]
	m := shop.Monitors[0]
	// One outage of an hour; no checks, so no time up between failures.
	if m.MTTRSeconds == nil || *m.MTTRSeconds != 3600 || m.MTBFSeconds == nil || *m.MTBFSeconds != 0 || m.MonitoredSeconds != 0 {
		t.Errorf("monitor = %+v", m)
	}
	if shop.MTTRSeconds == nil || *shop.MTTRSeconds != 3600 {
		t.Errorf("namespace = %+v", shop)
	}
	do(t, "GET", srv.URL+"/api/v1/reports?days=1&namespace=billing", "", &got)
	if b := got.Namespaces[0]; b.Outages != 1 || b.MTTRSeconds == nil || *b.MTTRSeconds < 7195 {
		t.Errorf("billing's open outage = %+v", b)
	}
	do(t, "GET", srv.URL+"/api/v1/reports?days=1&namespace=", "", &got)
	if loose := got.Namespaces[0]; loose.Outages != 0 || loose.MTTRSeconds != nil || loose.MTBFSeconds != nil {
		t.Errorf("no outages = %+v", loose)
	}

	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/reports?days=1&namespace=shop&format=csv", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	rows, err := csv.NewReader(res.Body).ReadAll()
	if err != nil || len(rows) != 2 {
		t.Fatalf("csv = %q, %v", rows, err)
	}
	header, row := rows[0], rows[1]
	if !slices.Equal(header[len(header)-3:], []string{"monitored_seconds", "mttr_seconds", "mtbf_seconds"}) {
		t.Errorf("header = %q", header)
	}
	if row[1] != `'=HYPERLINK("http://x")` || row[len(row)-2] != "3600" || row[len(row)-1] != "0" {
		t.Errorf("row = %q", row)
	}
}

func TestSpreadsheetSafe(t *testing.T) {
	for in, want := range map[string]string{
		"=1+2": "'=1+2", "+1": "'+1", "-1": "'-1", "@SUM(A1)": "'@SUM(A1)", "\t=1": "'\t=1", "\r=1": "'\r=1",
		"api": "api", "": "", "1=1": "1=1", "'quoted": "'quoted",
	} {
		if got := spreadsheetSafe(in); got != want {
			t.Errorf("spreadsheetSafe(%q) = %q, want %q", in, got, want)
		}
	}
}
