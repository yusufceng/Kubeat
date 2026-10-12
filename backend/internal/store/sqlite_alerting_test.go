package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestIncidents(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	api, _ := s.CreateMonitor(ctx, newMonitor("api"))
	web, _ := s.CreateMonitor(ctx, newMonitor("web"))
	base := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)

	if _, err := s.GetOpenIncident(ctx, api.ID, IncidentDown); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no incident yet: err = %v, want ErrNotFound", err)
	}

	opened, err := s.OpenIncident(ctx, Incident{MonitorID: api.ID, StartedAt: base, Cause: "timeout"})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if opened.ID == 0 || !opened.Open() {
		t.Errorf("opened = %+v", opened)
	}
	if _, err := s.OpenIncident(ctx, Incident{MonitorID: api.ID, StartedAt: base, Cause: "again"}); !errors.Is(err, ErrConflict) {
		t.Errorf("second open incident: err = %v, want ErrConflict", err)
	}

	got, err := s.GetOpenIncident(ctx, api.ID, IncidentDown)
	if err != nil {
		t.Fatalf("get open: %v", err)
	}
	want := Incident{ID: opened.ID, MonitorID: api.ID, Kind: IncidentDown, MonitorName: "api", StartedAt: base, Cause: "timeout"}
	if got != want {
		t.Errorf("open incident = %+v, want %+v", got, want)
	}

	st, err := s.GetMonitorStatus(ctx, api.ID, base)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.Incident == nil || *st.Incident != want {
		t.Errorf("status incident = %+v, want %+v", st.Incident, want)
	}

	if err := s.ResolveIncident(ctx, opened.ID, base.Add(5*time.Minute)); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if err := s.ResolveIncident(ctx, opened.ID, base.Add(9*time.Minute)); !errors.Is(err, ErrNotFound) {
		t.Errorf("resolving twice: err = %v, want ErrNotFound", err)
	}
	if st, _ := s.GetMonitorStatus(ctx, api.ID, base); st.Incident != nil {
		t.Errorf("resolved incident still shown as open: %+v", st.Incident)
	}

	// After resolution the monitor can have a new open incident.
	second, err := s.OpenIncident(ctx, Incident{MonitorID: api.ID, StartedAt: base.Add(time.Hour), Cause: "503"})
	if err != nil {
		t.Fatalf("open after resolve: %v", err)
	}
	if _, err := s.OpenIncident(ctx, Incident{MonitorID: web.ID, StartedAt: base.Add(2 * time.Hour), Cause: "refused"}); err != nil {
		t.Fatalf("open for other monitor: %v", err)
	}

	all, err := s.ListIncidents(ctx, 0, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 3 || all[0].MonitorName != "web" || all[1].ID != second.ID || all[2].ID != opened.ID {
		t.Errorf("list all not newest first: %+v", all)
	}
	if !all[2].ResolvedAt.Equal(base.Add(5*time.Minute)) || all[2].Open() {
		t.Errorf("resolved incident = %+v", all[2])
	}
	if onlyAPI, _ := s.ListIncidents(ctx, api.ID, 10); len(onlyAPI) != 2 {
		t.Errorf("list for one monitor: len = %d, want 2", len(onlyAPI))
	}
	if limited, _ := s.ListIncidents(ctx, 0, 1); len(limited) != 1 {
		t.Errorf("limit 1: len = %d", len(limited))
	}

	if err := s.DeleteMonitor(ctx, api.ID); err != nil {
		t.Fatalf("delete monitor: %v", err)
	}
	if left, _ := s.ListIncidents(ctx, 0, 10); len(left) != 1 {
		t.Errorf("incidents after monitor delete: len = %d, want 1", len(left))
	}
}

func TestChannels(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)

	created, err := s.CreateChannel(ctx, Channel{Name: "ops", Type: ChannelWebhook, Target: "https://hooks.example.com/abc", Enabled: true})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.CreateChannel(ctx, Channel{Name: "ops", Type: ChannelWebhook, Target: "https://x", Enabled: true}); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate name: err = %v, want ErrConflict", err)
	}
	if _, err := s.CreateChannel(ctx, Channel{Name: "pager", Type: "pagerduty", Target: "x"}); err == nil {
		t.Error("unknown channel type was accepted")
	}
	for _, typ := range []ChannelType{ChannelSlack, ChannelTeams, ChannelEmail} {
		c, err := s.CreateChannel(ctx, Channel{Name: string(typ), Type: typ, Target: "t", Enabled: true})
		if err != nil {
			t.Errorf("%s channel: %v", typ, err)
			continue
		}
		if err := s.DeleteChannel(ctx, c.ID); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.GetChannel(ctx, created.ID)
	if err != nil || got != created {
		t.Errorf("get = %+v, %v; want %+v", got, err, created)
	}
	list, err := s.ListChannels(ctx)
	if err != nil || len(list) != 1 || list[0] != created {
		t.Errorf("list = %+v, %v", list, err)
	}

	if err := s.DeleteChannel(ctx, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := s.DeleteChannel(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete: err = %v, want ErrNotFound", err)
	}
	if _, err := s.GetChannel(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("get after delete: err = %v, want ErrNotFound", err)
	}
}

// A database created before alerting existed must upgrade in place and keep
// its monitors, which get the default threshold.
func TestUpgradeFromVersion1(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "old.db")

	old, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	v1, err := migrationFiles.ReadFile("migrations/0001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		string(v1),
		"PRAGMA user_version = 1",
		`INSERT INTO monitors (name, type, target, interval_seconds, timeout_seconds, enabled, created_at)
		 VALUES ('legacy', 'http', 'https://example.com', 30, 5, 1, 0)`,
	} {
		if _, err := old.Exec(stmt); err != nil {
			t.Fatalf("prepare v1 database: %v", err)
		}
	}
	old.Close()

	s, err := OpenSQLite(ctx, path, 5*time.Second)
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	defer s.Close()

	list, err := s.ListMonitors(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].Name != "legacy" || list[0].FailureThreshold != 3 {
		t.Errorf("monitors after upgrade = %+v", list)
	}
	if _, err := s.OpenIncident(ctx, Incident{MonitorID: list[0].ID, StartedAt: time.Now(), Cause: "x"}); err != nil {
		t.Errorf("incidents table unusable after upgrade: %v", err)
	}
}

func TestCertificateIncidentsAndExpiry(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	api, _ := s.CreateMonitor(ctx, newMonitor("api"))
	base := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	expiry := base.Add(10 * 24 * time.Hour)

	err := s.InsertCheckResults(ctx, []CheckResult{
		{MonitorID: api.ID, CheckedAt: base, Up: true, StatusCode: 200},
		{MonitorID: api.ID, CheckedAt: base.Add(time.Minute), Up: true, StatusCode: 200, CertExpiresAt: expiry},
	})
	if err != nil {
		t.Fatal(err)
	}
	results, _ := s.ListCheckResults(ctx, api.ID, 10)
	if len(results) != 2 || !results[0].CertExpiresAt.Equal(expiry) || !results[1].CertExpiresAt.IsZero() {
		t.Errorf("cert expiry round trip: %+v", results)
	}

	// One open incident of each kind can coexist; a second of a kind cannot.
	if _, err := s.OpenIncident(ctx, Incident{MonitorID: api.ID, StartedAt: base, Cause: "timeout"}); err != nil {
		t.Fatal(err)
	}
	cert, err := s.OpenIncident(ctx, Incident{MonitorID: api.ID, Kind: IncidentCert, StartedAt: base, Cause: "expires soon"})
	if err != nil {
		t.Fatalf("cert incident beside an outage: %v", err)
	}
	if _, err := s.OpenIncident(ctx, Incident{MonitorID: api.ID, Kind: IncidentCert, StartedAt: base, Cause: "again"}); !errors.Is(err, ErrConflict) {
		t.Errorf("second cert incident: err = %v, want ErrConflict", err)
	}

	got, err := s.GetOpenIncident(ctx, api.ID, IncidentCert)
	if err != nil || got.ID != cert.ID || got.Kind != IncidentCert {
		t.Errorf("open cert incident = %+v, %v", got, err)
	}
	if down, _ := s.GetOpenIncident(ctx, api.ID, IncidentDown); down.Kind != IncidentDown || down.ID == cert.ID {
		t.Errorf("open outage = %+v", down)
	}

	st, err := s.GetMonitorStatus(ctx, api.ID, base)
	if err != nil {
		t.Fatal(err)
	}
	if !st.CertWarning || st.Incident == nil || st.Incident.Kind != IncidentDown || !st.Last.CertExpiresAt.Equal(expiry) {
		t.Errorf("status = warning:%v incident:%+v last:%+v", st.CertWarning, st.Incident, st.Last)
	}

	if err := s.ResolveIncident(ctx, cert.ID, base.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if st, _ := s.GetMonitorStatus(ctx, api.ID, base); st.CertWarning || st.Incident == nil {
		t.Errorf("after resolving the cert incident: warning:%v incident:%+v", st.CertWarning, st.Incident)
	}
	if all, _ := s.ListIncidents(ctx, 0, 10); len(all) != 2 {
		t.Errorf("incidents = %+v", all)
	}
}

// Channels created before other channel types existed keep their URL.
func TestChannelsSurviveTheTypeMigration(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "old.db")

	old, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	names, _ := migrationFiles.ReadDir("migrations")
	for _, entry := range names[:6] { // everything before 0007
		body, err := migrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := old.Exec(string(body)); err != nil {
			t.Fatalf("%s: %v", entry.Name(), err)
		}
	}
	for _, stmt := range []string{
		"PRAGMA user_version = 6",
		"INSERT INTO notification_channels (id, name, type, url, enabled, created_at) VALUES (7, 'ops', 'webhook', 'https://hooks.example.com/abc', 0, 1000)",
	} {
		if _, err := old.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	old.Close()

	s, err := OpenSQLite(ctx, path, 5*time.Second)
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	defer s.Close()

	got, err := s.GetChannel(ctx, 7)
	want := Channel{ID: 7, Name: "ops", Type: ChannelWebhook, Target: "https://hooks.example.com/abc", Enabled: false, CreatedAt: time.UnixMilli(1000).UTC()}
	if err != nil || got != want {
		t.Errorf("channel after upgrade = %+v, %v; want %+v", got, err, want)
	}
	if _, err := s.CreateChannel(ctx, Channel{Name: "mail", Type: ChannelEmail, Target: "a@example.com", Enabled: true}); err != nil {
		t.Errorf("new channel type after upgrade: %v", err)
	}
}

func TestIncidentLastNotified(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	api, _ := s.CreateMonitor(ctx, newMonitor("api"))
	base := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)

	opened, err := s.OpenIncident(ctx, Incident{MonitorID: api.ID, StartedAt: base, Cause: "x", LastNotifiedAt: base.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetOpenIncident(ctx, api.ID, IncidentDown); !got.LastNotifiedAt.Equal(base.Add(time.Minute)) {
		t.Errorf("LastNotifiedAt after open = %s", got.LastNotifiedAt)
	}
	if err := s.MarkIncidentNotified(ctx, opened.ID, base.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetOpenIncident(ctx, api.ID, IncidentDown); !got.LastNotifiedAt.Equal(base.Add(time.Hour)) {
		t.Errorf("LastNotifiedAt after mark = %s", got.LastNotifiedAt)
	}
	if err := s.MarkIncidentNotified(ctx, 999, base); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown incident: err = %v, want ErrNotFound", err)
	}
}
