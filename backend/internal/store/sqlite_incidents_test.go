package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

// incidentFixture stores monitors in three places, shop, billing and none,
// and incidents of both kinds on them, and returns the incidents by name.
func incidentFixture(t *testing.T, s *SQLite, base time.Time) map[string]Incident {
	t.Helper()
	ctx := context.Background()
	monitors := map[string]Monitor{}
	for _, m := range []struct{ name, namespace string }{{"shop-api", "shop"}, {"shop_db", "shop"}, {"billing-api", "billing"}, {"loose", ""}} {
		mon := newMonitor(m.name)
		mon.Namespace = m.namespace
		created, err := s.CreateMonitor(ctx, mon)
		if err != nil {
			t.Fatal(err)
		}
		monitors[m.name] = created
	}
	incidents := map[string]Incident{}
	for _, i := range []struct {
		name, monitor string
		kind          IncidentKind
		start, end    time.Duration // from base; a zero end leaves it open
	}{
		{"shop-short", "shop-api", IncidentDown, 0, 5 * time.Minute},
		{"shop-long", "shop_db", IncidentDown, time.Hour, 3 * time.Hour},
		{"shop-cert", "shop-api", IncidentCert, 2 * time.Hour, 0},
		{"billing-open", "billing-api", IncidentDown, 4 * time.Hour, 0},
		{"loose-old", "loose", IncidentDown, -48 * time.Hour, -47 * time.Hour},
	} {
		opened, err := s.OpenIncident(ctx, Incident{MonitorID: monitors[i.monitor].ID, Kind: i.kind, StartedAt: base.Add(i.start), Cause: i.name})
		if err != nil {
			t.Fatal(err)
		}
		if i.end != 0 {
			if err := s.ResolveIncident(ctx, opened.ID, base.Add(i.end)); err != nil {
				t.Fatal(err)
			}
		}
		incidents[i.name] = opened
	}
	return incidents
}

func causes(list []Incident) []string {
	out := []string{}
	for _, i := range list {
		out = append(out, i.Cause)
	}
	return out
}

func TestFilterIncidents(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	base := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	incidentFixture(t, s, base)
	yes, no := true, false

	tests := []struct {
		name string
		f    IncidentFilter
		want []string
	}{
		{"everything, newest first", IncidentFilter{Desc: true}, []string{"billing-open", "shop-cert", "shop-long", "shop-short", "loose-old"}},
		{"oldest first", IncidentFilter{}, []string{"loose-old", "shop-short", "shop-long", "shop-cert", "billing-open"}},
		{"namespaces", IncidentFilter{Namespaces: []string{"shop", "billing"}, Desc: true}, []string{"billing-open", "shop-cert", "shop-long", "shop-short"}},
		{"monitors without a namespace", IncidentFilter{Namespaces: []string{""}}, []string{"loose-old"}},
		{"no namespace at all selects none", IncidentFilter{Namespaces: []string{}}, []string{}},
		{"kind", IncidentFilter{Kind: IncidentCert}, []string{"shop-cert"}},
		{"open", IncidentFilter{Open: &yes}, []string{"shop-cert", "billing-open"}},
		{"resolved", IncidentFilter{Open: &no, Kind: IncidentDown}, []string{"loose-old", "shop-short", "shop-long"}},
		{"one monitor", IncidentFilter{MonitorID: 1}, []string{"shop-short", "shop-cert"}},
		{"name, any case", IncidentFilter{MonitorName: "API"}, []string{"shop-short", "shop-cert", "billing-open"}},
		// "_" and "%" are what they are, not wildcards: "p_a" would match
		// shop-api, "%" everything.
		{"name with an underscore", IncidentFilter{MonitorName: "p_d"}, []string{"shop-long"}},
		{"an underscore is no wildcard", IncidentFilter{MonitorName: "p_a"}, []string{}},
		{"name with a percent sign", IncidentFilter{MonitorName: "%"}, []string{}},
		// Overlapping [1h30m, 2h30m): shop-long runs through it and
		// shop-cert begins in it; shop-short ended before.
		{"time", IncidentFilter{From: base.Add(90 * time.Minute), To: base.Add(150 * time.Minute)}, []string{"shop-long", "shop-cert"}},
		{"from only", IncidentFilter{From: base.Add(150 * time.Minute)}, []string{"shop-long", "shop-cert", "billing-open"}},
		// Open ones last until Now: billing-open (4h to 10h) is the longest.
		{"longest first", IncidentFilter{Kind: IncidentDown, Sort: SortByDuration, Desc: true, Now: base.Add(10 * time.Hour)},
			[]string{"billing-open", "shop-long", "loose-old", "shop-short"}},
		{"by monitor", IncidentFilter{Sort: SortByMonitor}, []string{"billing-open", "loose-old", "shop-cert", "shop-short", "shop-long"}},
		{"a page", IncidentFilter{Desc: true, Limit: 2, Offset: 1}, []string{"shop-cert", "shop-long"}},
		{"from an offset on", IncidentFilter{Desc: true, Offset: 3}, []string{"shop-short", "loose-old"}},
	}
	for _, tt := range tests {
		got, err := s.FilterIncidents(ctx, tt.f)
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if c := causes(got); !reflect.DeepEqual(c, tt.want) {
			t.Errorf("%s: got %v, want %v", tt.name, c, tt.want)
		}
		count := tt.f
		count.Limit, count.Offset = 0, 0
		all, _ := s.FilterIncidents(ctx, count)
		if n, err := s.CountIncidents(ctx, tt.f); err != nil || n != len(all) {
			t.Errorf("%s: count = %d, %v; want %d", tt.name, n, err, len(all))
		}
	}

	// The incidents carry their monitor's namespace.
	got, _ := s.FilterIncidents(ctx, IncidentFilter{Kind: IncidentCert})
	if len(got) != 1 || got[0].MonitorNamespace != "shop" || got[0].MonitorName != "shop-api" {
		t.Errorf("cert incident = %+v", got)
	}
}

func TestOpenedAtAndGetIncident(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	m, _ := s.CreateMonitor(ctx, newMonitor("api"))
	base := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)

	opened, err := s.OpenIncident(ctx, Incident{MonitorID: m.ID, StartedAt: base, OpenedAt: base.Add(time.Minute + 123456*time.Nanosecond), Cause: "refused"})
	if err != nil {
		t.Fatal(err)
	}
	if !opened.OpenedAt.Equal(base.Add(time.Minute)) {
		t.Errorf("OpenedAt as returned = %v, want it to the millisecond", opened.OpenedAt)
	}
	got, err := s.GetIncident(ctx, opened.ID)
	if err != nil || got.ID != opened.ID || !got.OpenedAt.Equal(opened.OpenedAt) || !got.StartedAt.Equal(base) || got.MonitorName != "api" || !got.Open() {
		t.Errorf("GetIncident = %+v, %v; want %+v", got, err, opened)
	}
	if _, err := s.GetIncident(ctx, opened.ID+1); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown incident: err = %v, want ErrNotFound", err)
	}
	// Incidents from before the moment was recorded have none.
	old, _ := s.OpenIncident(ctx, Incident{MonitorID: m.ID, Kind: IncidentCert, StartedAt: base, Cause: "cert"})
	if got, _ := s.GetIncident(ctx, old.ID); !got.OpenedAt.IsZero() {
		t.Errorf("OpenedAt of an incident opened without one = %v", got.OpenedAt)
	}
}

func TestDeliveryLog(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	api, _ := s.CreateMonitor(ctx, newMonitor("api"))
	web, _ := s.CreateMonitor(ctx, newMonitor("web"))
	base := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	incident, _ := s.OpenIncident(ctx, Incident{MonitorID: api.ID, StartedAt: base, Cause: "refused"})
	other, _ := s.OpenIncident(ctx, Incident{MonitorID: web.ID, StartedAt: base, Cause: "refused"})

	var log []Delivery
	for i := range 7 {
		log = append(log, Delivery{
			IncidentID: incident.ID, MonitorID: 999, // the monitor comes from the incident
			ChannelID: 5, ChannelName: "ops", ChannelType: ChannelSlack, Event: "down", EventAt: base, GroupSize: 2,
			At: base.Add(time.Duration(i) * time.Minute), Delivered: i%2 == 0,
		})
	}
	log[1].Error = "Slack answered 404: no_service"
	// An attempt about an incident that is gone is left out, not an error.
	log = append(log, Delivery{IncidentID: 12345, ChannelID: 5, ChannelName: "ops", ChannelType: ChannelSlack, Event: "down", At: base})
	if err := s.RecordDeliveries(ctx, log); err != nil {
		t.Fatal(err)
	}
	later := base.Add(10 * time.Minute)
	if err := s.RecordDeliveries(ctx, []Delivery{{IncidentID: other.ID, ChannelID: 1, ChannelName: "hook", ChannelType: ChannelWebhook, Event: "down", At: later}}); err != nil {
		t.Fatal(err)
	}

	all, total, err := s.DeliveriesEnds(ctx, incident.ID, 10)
	if err != nil || total != 7 || len(all) != 7 {
		t.Fatalf("all entries = %d of %d, %v", len(all), total, err)
	}
	first := all[1]
	if first.MonitorID != api.ID || first.ChannelName != "ops" || first.ChannelType != ChannelSlack || first.Event != "down" ||
		!first.EventAt.Equal(base) || first.GroupSize != 2 || first.Delivered || first.Error != "Slack answered 404: no_service" || !first.At.Equal(base.Add(time.Minute)) {
		t.Errorf("entry = %+v", first)
	}
	// Of more than fit, the oldest and the newest: 0, 1, 2 and 5, 6.
	ends, total, err := s.DeliveriesEnds(ctx, incident.ID, 5)
	var minutes []int
	for _, d := range ends {
		minutes = append(minutes, int(d.At.Sub(base)/time.Minute))
	}
	if err != nil || total != 7 || !reflect.DeepEqual(minutes, []int{0, 1, 2, 5, 6}) {
		t.Errorf("ends = %v of %d, %v", minutes, total, err)
	}

	// Retention deletes by age; deleting the monitor deletes the rest.
	if n, err := s.DeleteDeliveriesBefore(ctx, base.Add(3*time.Minute)); err != nil || n != 3 {
		t.Errorf("deleted %d, %v; want the 3 older entries", n, err)
	}
	if err := s.DeleteMonitor(ctx, api.ID); err != nil {
		t.Fatal(err)
	}
	if _, total, _ := s.DeliveriesEnds(ctx, incident.ID, 10); total != 0 {
		t.Errorf("%d entries outlived their incident", total)
	}
	if _, total, _ := s.DeliveriesEnds(ctx, other.ID, 10); total != 1 {
		t.Errorf("another incident's log = %d entries, want 1", total)
	}
}

func TestIncidentNotes(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	m, _ := s.CreateMonitor(ctx, newMonitor("api"))
	incident, _ := s.OpenIncident(ctx, Incident{MonitorID: m.ID, StartedAt: time.Now().Add(-time.Hour), Cause: "refused"})
	user, err := s.CreateUser(ctx, User{Username: "yusuf", Role: RoleEditor, Namespaces: []string{"*"}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}

	written, err := s.CreateIncidentNote(ctx, IncidentNote{IncidentID: incident.ID, AuthorID: user.ID, Author: "yusuf", Text: "rolled back", ShowOnStatusPage: true})
	if err != nil {
		t.Fatal(err)
	}
	if written.ID == 0 || written.AuthorID != user.ID || written.CreatedAt.IsZero() || !written.UpdatedAt.Equal(written.CreatedAt) || !written.ShowOnStatusPage {
		t.Errorf("written = %+v", written)
	}
	// The API token has no account to refer to.
	byToken, err := s.CreateIncidentNote(ctx, IncidentNote{IncidentID: incident.ID, Author: "api-token", Text: "second"})
	if err != nil || byToken.AuthorID != 0 {
		t.Errorf("note by the token = %+v, %v", byToken, err)
	}
	if _, err := s.CreateIncidentNote(ctx, IncidentNote{IncidentID: incident.ID + 1, Author: "x", Text: "x"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("note on a missing incident: err = %v, want ErrNotFound", err)
	}

	time.Sleep(2 * time.Millisecond) // so that the update gets a later time
	written.Text, written.ShowOnStatusPage = "rolled back at 14:05", false
	updated, err := s.UpdateIncidentNote(ctx, written)
	if err != nil || updated.Text != "rolled back at 14:05" || updated.ShowOnStatusPage || !updated.UpdatedAt.After(updated.CreatedAt) || !updated.CreatedAt.Equal(written.CreatedAt) {
		t.Errorf("updated = %+v, %v", updated, err)
	}
	if notes, err := s.ListIncidentNotes(ctx, incident.ID, 10); err != nil || len(notes) != 2 || notes[0].ID != written.ID || notes[1].ID != byToken.ID {
		t.Errorf("notes = %+v, %v", notes, err)
	}
	if notes, _ := s.ListIncidentNotes(ctx, incident.ID, 1); len(notes) != 1 {
		t.Errorf("limit ignored: %d notes", len(notes))
	}

	// A deleted account leaves its notes behind, without an author to
	// change them.
	if err := s.DeleteUser(ctx, user.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := s.GetIncidentNote(ctx, written.ID); err != nil || got.AuthorID != 0 || got.Author != "yusuf" {
		t.Errorf("note of a deleted account = %+v, %v", got, err)
	}

	if err := s.DeleteIncidentNote(ctx, byToken.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteIncidentNote(ctx, byToken.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleting twice: err = %v", err)
	}
	if _, err := s.UpdateIncidentNote(ctx, byToken); !errors.Is(err, ErrNotFound) {
		t.Errorf("updating a deleted note: err = %v", err)
	}
	// Notes go with their incident.
	if err := s.DeleteMonitor(ctx, m.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetIncidentNote(ctx, written.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("note of a deleted monitor: err = %v", err)
	}
}

func TestFailedChecksEnds(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	m, _ := s.CreateMonitor(ctx, newMonitor("api"))
	base := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	var results []CheckResult
	for i := range 20 { // a check a minute; every third one succeeds
		results = append(results, CheckResult{MonitorID: m.ID, CheckedAt: base.Add(time.Duration(i) * time.Minute), Up: i%3 == 0, Error: "e"})
	}
	if err := s.InsertCheckResults(ctx, results); err != nil {
		t.Fatal(err)
	}
	minutes := func(list []CheckResult) []int {
		out := []int{}
		for _, r := range list {
			out = append(out, int(r.CheckedAt.Sub(base)/time.Minute))
		}
		return out
	}
	// The window [1m, 10m) holds six failures, at 1 2 4 5 7 8; the one at
	// 10 is past its end.
	window := func(keep int) ([]int, int) {
		got, total, err := s.FailedChecksEnds(ctx, m.ID, base.Add(time.Minute), base.Add(10*time.Minute), keep)
		if err != nil {
			t.Fatal(err)
		}
		return minutes(got), total
	}
	if got, total := window(10); total != 6 || !reflect.DeepEqual(got, []int{1, 2, 4, 5, 7, 8}) {
		t.Errorf("all = %v of %d", got, total)
	}
	if got, total := window(3); total != 6 || !reflect.DeepEqual(got, []int{1, 2, 8}) {
		t.Errorf("ends of 3 = %v of %d", got, total)
	}
	if got, total := window(4); total != 6 || !reflect.DeepEqual(got, []int{1, 2, 7, 8}) {
		t.Errorf("ends of 4 = %v of %d", got, total)
	}
	got, total, err := s.FailedChecksEnds(ctx, m.ID, base.Add(time.Hour), base.Add(2*time.Hour), 4)
	if err != nil || total != 0 || len(got) != 0 {
		t.Errorf("an empty window = %v of %d, %v", got, total, err)
	}
}

func TestEnds(t *testing.T) {
	for _, tt := range []struct{ total, keep, oldest, newest int }{
		{0, 10, 0, 0}, {3, 10, 3, 0}, {10, 10, 10, 0}, {11, 10, 5, 5}, {100, 5, 3, 2}, {100, 2, 1, 1},
	} {
		if oldest, newest := Ends(tt.total, tt.keep); oldest != tt.oldest || newest != tt.newest {
			t.Errorf("Ends(%d, %d) = %d, %d; want %d, %d", tt.total, tt.keep, oldest, newest, tt.oldest, tt.newest)
		}
	}
}

func TestMaintenanceOccurrences(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	m, _ := s.CreateMonitor(ctx, newMonitor("api"))
	day := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	if _, err := s.CreateMaintenance(ctx, Maintenance{Name: "nightly", StartsAt: day.Add(2 * time.Hour), EndsAt: day.Add(3 * time.Hour), Repeat: RepeatDaily}); err != nil {
		t.Fatal(err)
	}
	once, _ := s.CreateMaintenance(ctx, Maintenance{Name: "once", StartsAt: day.Add(20 * time.Hour), EndsAt: day.Add(22 * time.Hour), MonitorID: m.ID})
	if _, err := s.CreateMaintenance(ctx, Maintenance{Name: "later", StartsAt: day.Add(96 * time.Hour), EndsAt: day.Add(97 * time.Hour)}); err != nil {
		t.Fatal(err)
	}

	// An outage from 02:30 on the 12th to 02:15 on the 14th.
	from, to := day.Add(150*time.Minute), day.Add(50*time.Hour+15*time.Minute)
	windows, err := s.ListMaintenanceBetween(ctx, from, to)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, w := range windows {
		names = append(names, w.Name)
	}
	if !reflect.DeepEqual(names, []string{"nightly", "once"}) {
		t.Fatalf("windows = %v", names)
	}
	// The nightly one: going on at the start, the next night, and the one
	// that begins before the end.
	spans := windows[0].Occurrences(from, to, 10)
	var starts []time.Time
	for _, o := range spans {
		starts = append(starts, o.Start)
		if o.End.Sub(o.Start) != time.Hour {
			t.Errorf("occurrence %v lasts %s", o.Start, o.End.Sub(o.Start))
		}
	}
	if want := []time.Time{day.Add(2 * time.Hour), day.Add(26 * time.Hour), day.Add(50 * time.Hour)}; !reflect.DeepEqual(starts, want) {
		t.Errorf("nightly starts = %v, want %v", starts, want)
	}
	if spans := windows[0].Occurrences(from, to, 2); len(spans) != 2 {
		t.Errorf("limit ignored: %d occurrences", len(spans))
	}
	if spans := windows[1].Occurrences(from, to, 10); len(spans) != 1 || !spans[0].Start.Equal(once.StartsAt) || !spans[0].End.Equal(once.EndsAt) {
		t.Errorf("once = %+v", spans)
	}
	// A window that ended before the span has none in it.
	if spans := once.Occurrences(day.Add(23*time.Hour), to, 10); len(spans) != 0 {
		t.Errorf("an earlier window = %+v", spans)
	}
}

// A monitor counts as monitored for the hours it has checks in, but not
// before it was created nor after now.
func TestMonitoredTime(t *testing.T) {
	hour := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)
	from, to := hour.Add(-24*time.Hour), hour.Add(time.Hour)
	long := hour.Add(-48 * time.Hour)
	for _, tt := range []struct {
		name         string
		hours        int
		first, last  time.Time
		created, now time.Time
		want         time.Duration
	}{
		{"whole hours", 3, hour.Add(-5 * time.Hour), hour.Add(-time.Hour), long, to.Add(time.Hour), 3 * time.Hour},
		{"created within the first hour", 3, hour.Add(-5 * time.Hour), hour.Add(-time.Hour), hour.Add(-5*time.Hour + 20*time.Minute), to.Add(time.Hour), 3*time.Hour - 20*time.Minute},
		{"the current hour", 2, hour.Add(-time.Hour), hour, long, hour.Add(15 * time.Minute), time.Hour + 15*time.Minute},
		{"without checks", 0, time.Time{}, time.Time{}, long, to, 0},
	} {
		if got := monitored(tt.hours, tt.first, tt.last, from, to, tt.created, tt.now); got != tt.want {
			t.Errorf("%s: monitored = %s, want %s", tt.name, got, tt.want)
		}
	}
}
