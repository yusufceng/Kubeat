package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestStatusPages(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	api, _ := s.CreateMonitor(ctx, newMonitor("api"))
	web, _ := s.CreateMonitor(ctx, newMonitor("web"))
	internal, _ := s.CreateMonitor(ctx, newMonitor("internal"))

	created, err := s.CreateStatusPage(ctx, StatusPage{
		Slug: "shop", Title: "Mağaza", Description: "Müşteriye dönük servisler", Published: true, DetailPages: true,
		Sections: []StatusPageSection{{Name: "Vitrin"}, {Name: "Arka plan"}},
		Monitors: []StatusPageMonitor{{MonitorID: web.ID, DisplayName: "Web sitesi", Section: "Vitrin"}, {MonitorID: api.ID}},
		Rules:    []StatusPageRule{{Namespace: "shop", Tag: "public", Section: "Arka plan"}, {Tag: "customer"}},
		NoIndex:  true, HidePaused: true, Language: "tr", PasswordHash: "$2a$12$hash",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(created.Sections) != 2 || len(created.Monitors) != 2 || len(created.Rules) != 2 || !created.DetailPages ||
		created.Monitors[0].Section != "Vitrin" || created.Monitors[1].Section != "" || created.Rules[0].Section != "Arka plan" || created.Rules[1].Section != "" {
		t.Fatalf("created = %+v", created)
	}
	for name, read := range map[string]func() (StatusPage, error){
		"by id":   func() (StatusPage, error) { return s.GetStatusPage(ctx, created.ID) },
		"by slug": func() (StatusPage, error) { return s.GetStatusPageBySlug(ctx, "shop") },
	} {
		if got, err := read(); err != nil || !reflect.DeepEqual(got, created) {
			t.Errorf("%s = %+v, %v; want %+v", name, got, err, created)
		}
	}
	if _, err := s.CreateStatusPage(ctx, StatusPage{Slug: "shop", Title: "x"}); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate slug: err = %v, want ErrConflict", err)
	}
	for name, page := range map[string]StatusPage{
		"unknown monitor": {Slug: "ghost", Title: "x", Monitors: []StatusPageMonitor{{MonitorID: 999}}},
		"unknown section": {Slug: "ghost", Title: "x", Monitors: []StatusPageMonitor{{MonitorID: api.ID, Section: "nope"}}},
		"rule section":    {Slug: "ghost", Title: "x", Rules: []StatusPageRule{{Tag: "x", Section: "nope"}}},
		"section twice":   {Slug: "ghost", Title: "x", Sections: []StatusPageSection{{Name: "a"}, {Name: "a"}}},
		"nameless":        {Slug: "ghost", Title: "x", Sections: []StatusPageSection{{Name: ""}}},
	} {
		if _, err := s.CreateStatusPage(ctx, page); err == nil {
			t.Errorf("%s: the page was created", name)
		}
	}
	if _, err := s.GetStatusPageBySlug(ctx, "ghost"); !errors.Is(err, ErrNotFound) {
		t.Errorf("the failed pages left something behind: err = %v", err)
	}

	draft, _ := s.CreateStatusPage(ctx, StatusPage{Slug: "draft", Title: "Ara", Monitors: []StatusPageMonitor{{MonitorID: internal.ID}}})
	if pages, err := s.ListStatusPages(ctx); err != nil || len(pages) != 2 || pages[0].Slug != "draft" || pages[1].Slug != "shop" {
		t.Errorf("list = %+v, %v (want by title)", pages, err)
	}

	// An update replaces the sections, monitors and rules, and their order.
	created.Title, created.Announcement, created.Published = "Mağaza durumu", "Planlı bakım: cumartesi 02:00", false
	created.Sections = []StatusPageSection{{Name: "Hepsi"}}
	created.Monitors = []StatusPageMonitor{{MonitorID: api.ID, DisplayName: "API", Section: "Hepsi"}}
	created.Rules = nil
	created.PasswordHash, created.Language = "", ""
	updated, err := s.UpdateStatusPage(ctx, created)
	if err != nil || !reflect.DeepEqual(updated, created) {
		t.Errorf("updated = %+v, %v; want %+v", updated, err, created)
	}
	draft.Slug = "shop"
	if _, err := s.UpdateStatusPage(ctx, draft); !errors.Is(err, ErrConflict) {
		t.Errorf("slug taken: err = %v, want ErrConflict", err)
	}
	if _, err := s.UpdateStatusPage(ctx, StatusPage{ID: 999, Slug: "nobody", Title: "x"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown page: err = %v, want ErrNotFound", err)
	}

	// A deleted monitor leaves its pages; a deleted page leaves its monitors.
	if err := s.DeleteMonitor(ctx, api.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetStatusPage(ctx, created.ID); len(got.Monitors) != 0 || len(got.Sections) != 1 {
		t.Errorf("after the monitor was deleted: monitors = %+v, sections = %+v", got.Monitors, got.Sections)
	}
	if err := s.DeleteStatusPage(ctx, draft.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteStatusPage(ctx, draft.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete: err = %v, want ErrNotFound", err)
	}
	if _, err := s.GetMonitor(ctx, internal.ID); err != nil {
		t.Errorf("the monitor went with its page: %v", err)
	}
}

func TestStatusPageShows(t *testing.T) {
	shop := Monitor{ID: 1, Namespace: "shop", Tags: []string{"public", "web"}}
	billing := Monitor{ID: 2, Namespace: "billing", Tags: []string{"public"}}
	plain := Monitor{ID: 3, Namespace: "shop"}
	for name, tc := range map[string]struct {
		rule StatusPageRule
		want []bool // for shop, billing, plain
	}{
		"any":           {StatusPageRule{}, []bool{true, true, true}},
		"namespace":     {StatusPageRule{Namespace: "shop"}, []bool{true, false, true}},
		"tag":           {StatusPageRule{Tag: "public"}, []bool{true, true, false}},
		"namespace+tag": {StatusPageRule{Namespace: "shop", Tag: "public"}, []bool{true, false, false}},
		"other tag":     {StatusPageRule{Tag: "internal"}, []bool{false, false, false}},
	} {
		for i, m := range []Monitor{shop, billing, plain} {
			if got := tc.rule.Matches(m); got != tc.want[i] {
				t.Errorf("%s: rule %+v matches monitor %d = %v, want %v", name, tc.rule, m.ID, got, tc.want[i])
			}
		}
	}
	page := StatusPage{Monitors: []StatusPageMonitor{{MonitorID: 3}}, Rules: []StatusPageRule{{Namespace: "billing"}}}
	for _, tc := range []struct {
		m    Monitor
		want bool
	}{{shop, false}, {billing, true}, {plain, true}} {
		if got := page.Shows(tc.m); got != tc.want {
			t.Errorf("page shows monitor %d = %v, want %v", tc.m.ID, got, tc.want)
		}
	}
}

func TestDailyUptimeAndWindows(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	shown, _ := s.CreateMonitor(ctx, newMonitor("shown"))
	hidden, _ := s.CreateMonitor(ctx, newMonitor("hidden"))

	day := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	var results []CheckResult
	add := func(id int64, at time.Time, up bool) {
		results = append(results, CheckResult{MonitorID: id, CheckedAt: at, Up: up, Latency: time.Millisecond})
	}
	// Day one: 3 of 4 up, across two hours. Day two: nothing. Day three: 1 of 1.
	add(shown.ID, day.Add(1*time.Hour), true)
	add(shown.ID, day.Add(1*time.Hour+time.Minute), false)
	add(shown.ID, day.Add(23*time.Hour), true)
	add(shown.ID, day.Add(23*time.Hour+59*time.Minute), true)
	add(shown.ID, day.Add(48*time.Hour), true)
	add(hidden.ID, day.Add(2*time.Hour), false)
	if err := s.InsertCheckResults(ctx, results); err != nil {
		t.Fatal(err)
	}
	if err := s.RollupCheckResults(ctx, day, day.Add(72*time.Hour)); err != nil {
		t.Fatal(err)
	}

	got, err := s.DailyUptime(ctx, []int64{shown.ID}, day, day.Add(72*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	want := map[int64][]SeriesPoint{shown.ID: {
		{Start: day, Checks: 4, UpChecks: 3},
		{Start: day.Add(48 * time.Hour), Checks: 1, UpChecks: 1},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("daily uptime = %+v\nwant          %+v", got, want)
	}
	// The window is honoured, and so is the list of monitors.
	if got, _ := s.DailyUptime(ctx, []int64{shown.ID}, day.Add(24*time.Hour), day.Add(48*time.Hour)); len(got) != 0 {
		t.Errorf("a day without checks = %+v", got)
	}
	if got, _ := s.DailyUptime(ctx, nil, day, day.Add(72*time.Hour)); len(got) != 0 {
		t.Errorf("no monitors = %+v", got)
	}

	// Windows ending in the third day's second hour: the last 2 hours hold
	// the one check of day three, 26 hours hold that and the two of day
	// one's last hour (25 hours ago, on the hour), and 72 hours hold
	// everything.
	now := day.Add(48*time.Hour + 90*time.Minute)
	counts, err := s.UptimeWindows(ctx, []int64{shown.ID, hidden.ID}, now, []time.Duration{2 * time.Hour, 26 * time.Hour, 72 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	wantCounts := map[int64][]UptimeCount{
		shown.ID:  {{Checks: 1, UpChecks: 1}, {Checks: 3, UpChecks: 3}, {Checks: 5, UpChecks: 4}},
		hidden.ID: {{}, {}, {Checks: 1}},
	}
	if !reflect.DeepEqual(counts, wantCounts) {
		t.Errorf("uptime windows = %+v\nwant           %+v", counts, wantCounts)
	}
}

func TestStatusPageUpdatesAndNotes(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	web, _ := s.CreateMonitor(ctx, newMonitor("web"))
	other, _ := s.CreateMonitor(ctx, newMonitor("other"))
	page, _ := s.CreateStatusPage(ctx, StatusPage{Slug: "p", Title: "p", Published: true, Monitors: []StatusPageMonitor{{MonitorID: web.ID}}})
	incident, _ := s.OpenIncident(ctx, Incident{MonitorID: web.ID, StartedAt: time.Now(), Cause: "x"})

	first, err := s.CreateStatusPageUpdate(ctx, StatusPageUpdate{PageID: page.ID, Kind: UpdateInvestigating, Title: "Yavaşlık", Body: "Bakıyoruz", CreatedBy: "ayse", IncidentID: incident.ID})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == 0 || first.IncidentID != incident.ID || first.CreatedAt.IsZero() || !first.UpdatedAt.Equal(first.CreatedAt) || first.CreatedBy != "ayse" {
		t.Errorf("created = %+v", first)
	}
	if _, err := s.CreateStatusPageUpdate(ctx, StatusPageUpdate{PageID: 999, Kind: UpdateNotice, Title: "x"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("update of an unknown page: err = %v, want ErrNotFound", err)
	}
	second, _ := s.CreateStatusPageUpdate(ctx, StatusPageUpdate{PageID: page.ID, Kind: UpdateNotice, Title: "Duyuru"})
	if got, err := s.GetStatusPageUpdate(ctx, first.ID); err != nil || !reflect.DeepEqual(got, first) {
		t.Errorf("get = %+v, %v; want %+v", got, err, first)
	}
	list, err := s.ListStatusPageUpdates(ctx, page.ID, 10)
	if err != nil || len(list) != 2 || list[0].ID != second.ID || list[1].ID != first.ID {
		t.Errorf("list = %+v, %v (want newest first)", list, err)
	}
	if list, _ := s.ListStatusPageUpdates(ctx, page.ID, 1); len(list) != 1 {
		t.Errorf("limited list = %+v", list)
	}

	first.Kind, first.Title, first.IncidentID = UpdateResolved, "Çözüldü", 0
	updated, err := s.UpdateStatusPageUpdate(ctx, first)
	if err != nil || updated.Kind != UpdateResolved || updated.Title != "Çözüldü" || updated.IncidentID != 0 || updated.UpdatedAt.Before(first.UpdatedAt) {
		t.Errorf("updated = %+v, %v", updated, err)
	}
	if _, err := s.UpdateStatusPageUpdate(ctx, StatusPageUpdate{ID: 999, Kind: UpdateNotice}); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown update: err = %v, want ErrNotFound", err)
	}
	// The incident going clears the reference; the page going takes the
	// updates with it.
	second.IncidentID = incident.ID
	s.UpdateStatusPageUpdate(ctx, second)
	if err := s.DeleteMonitor(ctx, web.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetStatusPageUpdate(ctx, second.ID); got.IncidentID != 0 {
		t.Errorf("update after its incident went = %+v", got)
	}
	if err := s.DeleteStatusPageUpdate(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteStatusPageUpdate(ctx, second.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete: err = %v, want ErrNotFound", err)
	}
	if err := s.DeleteStatusPage(ctx, page.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetStatusPageUpdate(ctx, first.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("update after its page went: err = %v, want ErrNotFound", err)
	}

	// Notes flagged for status pages, of the given monitors only.
	shown, _ := s.OpenIncident(ctx, Incident{MonitorID: other.ID, StartedAt: time.Now(), Cause: "x"})
	s.CreateIncidentNote(ctx, IncidentNote{IncidentID: shown.ID, Author: "ayse", Text: "görünmez"})
	visible, _ := s.CreateIncidentNote(ctx, IncidentNote{IncidentID: shown.ID, Author: "ayse", Text: "görünür", ShowOnStatusPage: true})
	notes, err := s.ListStatusPageNotes(ctx, []int64{other.ID}, 10)
	if err != nil || len(notes) != 1 || notes[0].ID != visible.ID || notes[0].MonitorID != other.ID || notes[0].Text != "görünür" {
		t.Errorf("notes = %+v, %v", notes, err)
	}
	if notes, _ := s.ListStatusPageNotes(ctx, []int64{999}, 10); len(notes) != 0 {
		t.Errorf("notes of another monitor = %+v", notes)
	}
	if notes, _ := s.ListStatusPageNotes(ctx, nil, 10); len(notes) != 0 {
		t.Errorf("notes of no monitor = %+v", notes)
	}
}
