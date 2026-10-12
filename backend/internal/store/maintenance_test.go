package store

import (
	"context"
	"testing"
	"time"
)

func mustLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("time zone data is not available: %v", err)
	}
	return loc
}

func TestMaintenanceOccurrence(t *testing.T) {
	istanbul := mustLocation(t, "Europe/Istanbul")
	berlin := mustLocation(t, "Europe/Berlin")
	at := func(loc *time.Location, s string) time.Time {
		parsed, err := time.ParseInLocation("2006-01-02 15:04", s, loc)
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	window := func(repeat Repeat, loc *time.Location, start string, length time.Duration) Maintenance {
		s := at(loc, start)
		return Maintenance{StartsAt: s.UTC(), EndsAt: s.Add(length).UTC(), Repeat: repeat, Timezone: loc.String()}
	}

	tests := []struct {
		name   string
		w      Maintenance
		loc    *time.Location
		at     string
		active bool
		start  string // of the occurrence that covers at, or of the next one
	}{
		// Once: only itself, and over when it ends.
		{"once, before", window(RepeatNone, istanbul, "2026-10-12 02:00", 2*time.Hour), istanbul, "2026-10-11 12:00", false, "2026-10-12 02:00"},
		{"once, during", window(RepeatNone, istanbul, "2026-10-12 02:00", 2*time.Hour), istanbul, "2026-10-12 03:59", true, "2026-10-12 02:00"},
		{"once, at its end", window(RepeatNone, istanbul, "2026-10-12 02:00", 2*time.Hour), istanbul, "2026-10-12 04:00", false, ""},

		{"daily, first occurrence", window(RepeatDaily, istanbul, "2026-10-12 02:00", 2*time.Hour), istanbul, "2026-10-12 02:00", true, "2026-10-12 02:00"},
		{"daily, a week later", window(RepeatDaily, istanbul, "2026-10-12 02:00", 2*time.Hour), istanbul, "2026-10-19 03:30", true, "2026-10-19 02:00"},
		{"daily, between two", window(RepeatDaily, istanbul, "2026-10-12 02:00", 2*time.Hour), istanbul, "2026-10-19 04:00", false, "2026-10-20 02:00"},
		{"daily, before the first", window(RepeatDaily, istanbul, "2026-10-12 02:00", 2*time.Hour), istanbul, "2026-10-11 03:00", false, "2026-10-12 02:00"},
		// Across midnight: at 00:30 it is yesterday's occurrence that is on.
		{"daily, past midnight", window(RepeatDaily, istanbul, "2026-10-12 23:00", 2*time.Hour), istanbul, "2026-10-15 00:30", true, "2026-10-14 23:00"},
		{"daily, past midnight, before it", window(RepeatDaily, istanbul, "2026-10-12 23:00", 2*time.Hour), istanbul, "2026-10-15 22:00", false, "2026-10-15 23:00"},

		// 2026-10-12 is a Monday.
		{"weekly, same weekday", window(RepeatWeekly, istanbul, "2026-10-12 02:00", time.Hour), istanbul, "2026-11-02 02:30", true, "2026-11-02 02:00"},
		{"weekly, another weekday", window(RepeatWeekly, istanbul, "2026-10-12 02:00", time.Hour), istanbul, "2026-10-14 02:30", false, "2026-10-19 02:00"},
		{"weekly, days long", window(RepeatWeekly, istanbul, "2026-10-12 22:00", 30*time.Hour), istanbul, "2026-10-21 03:00", true, "2026-10-19 22:00"},

		{"monthly, same day", window(RepeatMonthly, istanbul, "2026-10-12 02:00", time.Hour), istanbul, "2027-01-12 02:10", true, "2027-01-12 02:00"},
		{"monthly, another day", window(RepeatMonthly, istanbul, "2026-10-12 02:00", time.Hour), istanbul, "2026-10-13 02:10", false, "2026-11-12 02:00"},
		// A month without a 31st is skipped, not moved to the 1st.
		{"monthly on the 31st, in November", window(RepeatMonthly, istanbul, "2026-10-31 02:00", time.Hour), istanbul, "2026-11-30 02:10", false, "2026-12-31 02:00"},
		{"monthly on the 31st, on 1 December", window(RepeatMonthly, istanbul, "2026-10-31 02:00", time.Hour), istanbul, "2026-12-01 02:10", false, "2026-12-31 02:00"},
		{"monthly on the 30th, over February", window(RepeatMonthly, istanbul, "2027-01-30 02:00", time.Hour), istanbul, "2027-02-10 00:00", false, "2027-03-30 02:00"},

		// Berlin leaves summer time on 2026-10-25: the window stays at 02:30
		// on the clock, which is an hour later in UTC.
		{"daily across a clock change", window(RepeatDaily, berlin, "2026-10-20 02:30", time.Hour), berlin, "2026-10-28 03:00", true, "2026-10-28 02:30"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := at(tt.loc, tt.at)
			start, end, ok := tt.w.Occurrence(now)
			if tt.start == "" {
				if ok {
					t.Fatalf("occurrence = %s .. %s, want none", start, end)
				}
				return
			}
			want := at(tt.loc, tt.start)
			if !ok || !start.Equal(want) || end.Sub(start) != tt.w.EndsAt.Sub(tt.w.StartsAt) {
				t.Fatalf("occurrence = %s .. %s (ok=%v), want one starting %s", start.In(tt.loc), end.In(tt.loc), ok, want)
			}
			until, active := tt.w.ActiveUntil(now)
			if active != tt.active || (active && !until.Equal(end)) {
				t.Errorf("active = %v until %s, want %v", active, until, tt.active)
			}
		})
	}

	// The clock change, checked in UTC: before it 02:30 is 00:30 UTC, after it 01:30.
	w := window(RepeatDaily, berlin, "2026-10-20 02:30", time.Hour)
	if start, _, _ := w.Occurrence(time.Date(2026, 10, 28, 1, 45, 0, 0, time.UTC)); start.UTC().Hour() != 1 || start.UTC().Minute() != 30 {
		t.Errorf("after the clock change the window starts at %s UTC, want 01:30", start.UTC().Format("15:04"))
	}
	// An unknown time zone falls back to UTC instead of failing.
	w.Timezone = "Mars/Olympus"
	if _, _, ok := w.Occurrence(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)); !ok {
		t.Error("a window with an unknown time zone has no occurrences")
	}
}

func TestMaintenanceCovers(t *testing.T) {
	shop, other := Monitor{ID: 1, Namespace: "shop"}, Monitor{ID: 2, Namespace: "billing"}
	for name, tc := range map[string]struct {
		w          Maintenance
		shop, rest bool
	}{
		"everything": {Maintenance{}, true, true},
		"namespace":  {Maintenance{Namespace: "shop"}, true, false},
		"monitor":    {Maintenance{MonitorID: 2}, false, true},
	} {
		if tc.w.Covers(shop) != tc.shop || tc.w.Covers(other) != tc.rest {
			t.Errorf("%s: covers shop = %v, other = %v", name, tc.w.Covers(shop), tc.w.Covers(other))
		}
	}
}

// A repeating window that began long ago covers monitors now, and the
// status of a monitor says until when.
func TestRecurringMaintenanceInTheStore(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	shop := newMonitor("shop")
	shop.Namespace = "shop"
	inScope, _ := s.CreateMonitor(ctx, shop)
	outside, _ := s.CreateMonitor(ctx, newMonitor("outside"))

	now := time.Now().UTC()
	// Daily, started 30 days ago, half an hour ago on the clock, two hours long.
	first := now.Add(-30*24*time.Hour - 30*time.Minute)
	created, err := s.CreateMaintenance(ctx, Maintenance{
		Name: "gece yedeği", StartsAt: first, EndsAt: first.Add(2 * time.Hour), Repeat: RepeatDaily, Timezone: "UTC", Namespace: "shop",
	})
	if err != nil {
		t.Fatal(err)
	}
	// And one that is not due now: it runs half a day from now.
	later := now.Add(-30*24*time.Hour + 12*time.Hour)
	if _, err := s.CreateMaintenance(ctx, Maintenance{Name: "öğlen", StartsAt: later, EndsAt: later.Add(time.Hour), Repeat: RepeatDaily}); err != nil {
		t.Fatal(err)
	}

	for m, want := range map[*Monitor]bool{&inScope: true, &outside: false} {
		if got, err := s.InMaintenance(ctx, *m, now); err != nil || got != want {
			t.Errorf("%s in maintenance = %v, %v; want %v", m.Name, got, err, want)
		}
	}
	st, err := s.GetMonitorStatus(ctx, inScope.ID, now)
	wantUntil := now.Add(90 * time.Minute)
	if err != nil || st.MaintenanceUntil.Sub(wantUntil).Abs() > time.Second {
		t.Errorf("maintenance until = %s, %v; want about %s", st.MaintenanceUntil, err, wantUntil)
	}
	if st, _ := s.GetMonitorStatus(ctx, outside.ID, now); !st.MaintenanceUntil.IsZero() {
		t.Errorf("a monitor outside the scope is in maintenance until %s", st.MaintenanceUntil)
	}

	// Repeating windows are listed first, and with what they were given.
	windows, err := s.ListMaintenance(ctx, 10)
	if err != nil || len(windows) != 2 || windows[1].ID != created.ID || windows[1].Repeat != RepeatDaily || windows[1].Timezone != "UTC" {
		t.Errorf("list = %+v, %v", windows, err)
	}
}
