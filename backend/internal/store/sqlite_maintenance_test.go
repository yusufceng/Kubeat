package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMaintenanceScopes(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	inNS := func(name, ns string) Monitor {
		m := newMonitor(name)
		m.Namespace, m.Source = ns, "service/"+name
		created, err := s.CreateMonitor(ctx, m)
		if err != nil {
			t.Fatal(err)
		}
		return created
	}
	shopAPI, shopWeb, billing := inNS("api", "shop"), inNS("web", "shop"), inNS("pay", "billing")
	manual, err := s.CreateMonitor(ctx, newMonitor("manual"))
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	window := func(m Maintenance) Maintenance {
		t.Helper()
		m.StartsAt, m.EndsAt = now.Add(-time.Hour), now.Add(time.Hour)
		created, err := s.CreateMaintenance(ctx, m)
		if err != nil {
			t.Fatalf("create %q: %v", m.Name, err)
		}
		return created
	}
	covered := func(m Monitor, at time.Time) bool {
		t.Helper()
		ok, err := s.InMaintenance(ctx, m, at)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	all := []Monitor{shopAPI, shopWeb, billing, manual}
	expect := func(step string, want ...Monitor) {
		t.Helper()
		wanted := map[int64]bool{}
		for _, m := range want {
			wanted[m.ID] = true
		}
		for _, m := range all {
			if got := covered(m, now); got != wanted[m.ID] {
				t.Errorf("%s: %s covered = %v, want %v", step, m.Name, got, wanted[m.ID])
			}
		}
	}

	expect("no windows")

	one := window(Maintenance{Name: "one monitor", MonitorID: shopAPI.ID})
	expect("monitor scope", shopAPI)

	ns := window(Maintenance{Name: "shop deploy", Namespace: "shop"})
	expect("namespace scope", shopAPI, shopWeb)
	if err := s.DeleteMaintenance(ctx, one.ID); err != nil {
		t.Fatal(err)
	}
	expect("namespace scope alone", shopAPI, shopWeb)

	// The half-open interval: covered at the start, not at the end.
	if !covered(shopWeb, ns.StartsAt) || covered(shopWeb, ns.EndsAt) || covered(shopWeb, ns.StartsAt.Add(-time.Millisecond)) {
		t.Error("window boundaries are not [start, end)")
	}

	everything := window(Maintenance{Name: "cluster upgrade"})
	expect("global scope", all...)

	// Status reports the latest end among the windows covering a monitor.
	later, err := s.CreateMaintenance(ctx, Maintenance{Name: "longer", Namespace: "shop", StartsAt: now.Add(-time.Minute), EndsAt: now.Add(3 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	statuses, err := s.ListMonitorStatuses(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range statuses {
		want := everything.EndsAt
		if st.Namespace == "shop" {
			want = later.EndsAt
		}
		if !st.MaintenanceUntil.Equal(want) {
			t.Errorf("%s: MaintenanceUntil = %s, want %s", st.Name, st.MaintenanceUntil, want)
		}
	}

	list, err := s.ListMaintenance(ctx, 10)
	if err != nil || len(list) != 3 || list[0].ID != later.ID {
		t.Fatalf("list = %+v, %v; want 3 with the latest start first", list, err)
	}
	if limited, _ := s.ListMaintenance(ctx, 1); len(limited) != 1 {
		t.Errorf("limit 1 returned %d", len(limited))
	}

	for _, id := range []int64{ns.ID, everything.ID, later.ID} {
		if err := s.DeleteMaintenance(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	expect("after deleting every window")
	if st, _ := s.GetMonitorStatus(ctx, manual.ID, now); !st.MaintenanceUntil.IsZero() {
		t.Errorf("MaintenanceUntil after deletion = %s", st.MaintenanceUntil)
	}
	if err := s.DeleteMaintenance(ctx, ns.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete: err = %v, want ErrNotFound", err)
	}
}

func TestMaintenanceConstraints(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	m, _ := s.CreateMonitor(ctx, newMonitor("api"))
	now := time.Now()

	bad := map[string]Maintenance{
		"ends before it starts": {Name: "x", StartsAt: now, EndsAt: now.Add(-time.Minute)},
		"zero length":           {Name: "x", StartsAt: now, EndsAt: now},
		"two scopes":            {Name: "x", StartsAt: now, EndsAt: now.Add(time.Hour), Namespace: "shop", MonitorID: m.ID},
		"unknown monitor":       {Name: "x", StartsAt: now, EndsAt: now.Add(time.Hour), MonitorID: 999},
	}
	for name, w := range bad {
		if _, err := s.CreateMaintenance(ctx, w); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}

	// A window for one monitor goes away with the monitor, and its name is
	// reported while it exists.
	w, err := s.CreateMaintenance(ctx, Maintenance{Name: "one", StartsAt: now, EndsAt: now.Add(time.Hour), MonitorID: m.ID})
	if err != nil {
		t.Fatal(err)
	}
	if list, _ := s.ListMaintenance(ctx, 10); len(list) != 1 || list[0].MonitorName != "api" || list[0].MonitorID != m.ID || list[0].ID != w.ID {
		t.Errorf("list = %+v", list)
	}
	if err := s.DeleteMonitor(ctx, m.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.ListMaintenance(ctx, 10); len(list) != 0 {
		t.Errorf("window survived its monitor: %+v", list)
	}
}
