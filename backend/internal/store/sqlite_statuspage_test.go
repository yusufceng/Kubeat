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
		Slug: "shop", Title: "Mağaza", Description: "Müşteriye dönük servisler", Published: true,
		Monitors: []StatusPageMonitor{{MonitorID: web.ID, DisplayName: "Web sitesi"}, {MonitorID: api.ID}},
	})
	if err != nil {
		t.Fatal(err)
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
	if _, err := s.CreateStatusPage(ctx, StatusPage{Slug: "ghost", Title: "x", Monitors: []StatusPageMonitor{{MonitorID: 999}}}); err == nil {
		t.Error("a page with an unknown monitor was created")
	}
	if _, err := s.GetStatusPageBySlug(ctx, "ghost"); !errors.Is(err, ErrNotFound) {
		t.Errorf("the failed page left something behind: err = %v", err)
	}

	draft, _ := s.CreateStatusPage(ctx, StatusPage{Slug: "draft", Title: "Ara", Monitors: []StatusPageMonitor{{MonitorID: internal.ID}}})
	if pages, err := s.ListStatusPages(ctx); err != nil || len(pages) != 2 || pages[0].Slug != "draft" || pages[1].Slug != "shop" {
		t.Errorf("list = %+v, %v (want by title)", pages, err)
	}
	// Only what a published page shows is public.
	for id, want := range map[int64]bool{api.ID: true, web.ID: true, internal.ID: false, 999: false} {
		if got, err := s.MonitorIsPublic(ctx, id); err != nil || got != want {
			t.Errorf("monitor %d public = %v, %v; want %v", id, got, err, want)
		}
	}

	// An update replaces the monitors and their order.
	created.Title, created.Announcement, created.Published = "Mağaza durumu", "Planlı bakım: cumartesi 02:00", false
	created.Monitors = []StatusPageMonitor{{MonitorID: api.ID, DisplayName: "API"}}
	updated, err := s.UpdateStatusPage(ctx, created)
	if err != nil || !reflect.DeepEqual(updated, created) {
		t.Errorf("updated = %+v, %v; want %+v", updated, err, created)
	}
	if public, _ := s.MonitorIsPublic(ctx, api.ID); public {
		t.Error("a monitor of an unpublished page is public")
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
	if got, _ := s.GetStatusPage(ctx, created.ID); len(got.Monitors) != 0 {
		t.Errorf("monitors after the monitor was deleted = %+v", got.Monitors)
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

func TestStatusPageDailyUptime(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	shown, _ := s.CreateMonitor(ctx, newMonitor("shown"))
	hidden, _ := s.CreateMonitor(ctx, newMonitor("hidden"))
	page, _ := s.CreateStatusPage(ctx, StatusPage{Slug: "p", Title: "p", Published: true, Monitors: []StatusPageMonitor{{MonitorID: shown.ID}}})

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

	got, err := s.StatusPageDailyUptime(ctx, page.ID, day, day.Add(72*time.Hour))
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
	// The window is honoured.
	if got, _ := s.StatusPageDailyUptime(ctx, page.ID, day.Add(24*time.Hour), day.Add(48*time.Hour)); len(got) != 0 {
		t.Errorf("a day without checks = %+v", got)
	}
}
