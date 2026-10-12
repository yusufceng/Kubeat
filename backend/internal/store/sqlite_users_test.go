package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestUsers(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)

	ayse, err := s.CreateUser(ctx, User{Username: "Ayse", DisplayName: "Ayşe Yılmaz", Email: "ayse@example.com",
		PasswordHash: "$2a$hash", Role: RoleEditor, Namespaces: []string{"shop", "billing"}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if ayse.ID == 0 || ayse.Source != UserLocal || ayse.CreatedAt.IsZero() {
		t.Errorf("created = %+v", ayse)
	}
	// Names are unique whatever their case, and found whatever their case.
	if _, err := s.CreateUser(ctx, User{Username: "AYSE", Role: RoleViewer}); !errors.Is(err, ErrConflict) {
		t.Errorf("same name in another case: err = %v, want ErrConflict", err)
	}
	if got, err := s.GetUserByUsername(ctx, " ayse "); err != nil || !reflect.DeepEqual(got, ayse) {
		t.Errorf("by username = %+v, %v", got, err)
	}
	if got, _ := s.GetUser(ctx, ayse.ID); got.PasswordHash != "$2a$hash" || len(got.Namespaces) != 2 {
		t.Errorf("get = %+v", got)
	}
	for _, bad := range []User{{Username: "", Role: RoleViewer}, {Username: "x", Role: "owner"}} {
		if _, err := s.CreateUser(ctx, bad); err == nil {
			t.Errorf("user %+v was created", bad)
		}
	}
	// An admin has no namespace list: they see everything.
	admin, _ := s.CreateUser(ctx, User{Username: "admin", Role: RoleAdmin, Namespaces: []string{"shop"}, Enabled: true})
	if admin.Namespaces != nil {
		t.Errorf("admin namespaces = %v", admin.Namespaces)
	}
	if n, _ := s.CountUsers(ctx); n != 2 {
		t.Errorf("count = %d", n)
	}
	if list, _ := s.ListUsers(ctx); len(list) != 2 || list[0].Username != "admin" || list[1].Username != "Ayse" {
		t.Errorf("list = %+v", list)
	}

	// An update changes everything but the password and the creation time.
	ayse.DisplayName, ayse.Role, ayse.Namespaces, ayse.Enabled, ayse.PasswordHash, ayse.LastLoginAt = "A. Yılmaz", RoleViewer, []string{AllNamespaces}, false, "ignored", time.Now().UTC().Truncate(time.Millisecond)
	updated, err := s.UpdateUser(ctx, ayse)
	if err != nil || updated.DisplayName != "A. Yılmaz" || updated.Role != RoleViewer || updated.Namespaces[0] != AllNamespaces || updated.Enabled ||
		updated.PasswordHash != "$2a$hash" || !updated.LastLoginAt.Equal(ayse.LastLoginAt) || !updated.CreatedAt.Equal(ayse.CreatedAt) {
		t.Errorf("updated = %+v, %v", updated, err)
	}
	if err := s.SetPassword(ctx, ayse.ID, "$2a$new"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetUser(ctx, ayse.ID); got.PasswordHash != "$2a$new" {
		t.Errorf("hash after SetPassword = %q", got.PasswordHash)
	}
	if err := s.SetPassword(ctx, 999, "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetPassword of an unknown user: err = %v", err)
	}
	admin.Username = "ayse"
	if _, err := s.UpdateUser(ctx, admin); !errors.Is(err, ErrConflict) {
		t.Errorf("rename to a taken name: err = %v, want ErrConflict", err)
	}

	// Deleting a user takes their sessions along.
	now := time.Now().UTC().Truncate(time.Millisecond)
	s.CreateSession(ctx, Session{TokenHash: "h1", UserID: ayse.ID, CreatedAt: now, ExpiresAt: now.Add(time.Hour), LastSeenAt: now})
	if err := s.DeleteUser(ctx, ayse.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSession(ctx, "h1", now); !errors.Is(err, ErrNotFound) {
		t.Errorf("session outlived its user: err = %v", err)
	}
	if err := s.DeleteUser(ctx, ayse.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete: err = %v", err)
	}
}

func TestSessions(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	u, _ := s.CreateUser(ctx, User{Username: "u", Role: RoleViewer, Enabled: true})
	now := time.Now().UTC().Truncate(time.Millisecond)

	live := Session{TokenHash: "live", UserID: u.ID, CreatedAt: now, ExpiresAt: now.Add(time.Hour), LastSeenAt: now}
	old := Session{TokenHash: "old", UserID: u.ID, CreatedAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-time.Hour), LastSeenAt: now.Add(-time.Hour)}
	for _, sess := range []Session{live, old} {
		if err := s.CreateSession(ctx, sess); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := s.GetSession(ctx, "live", now); err != nil || got != live {
		t.Errorf("live session = %+v, %v", got, err)
	}
	// An expired session is as good as gone, and so is one asked for later.
	if _, err := s.GetSession(ctx, "old", now); !errors.Is(err, ErrNotFound) {
		t.Errorf("expired session: err = %v", err)
	}
	if _, err := s.GetSession(ctx, "live", now.Add(2*time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Errorf("session after its expiry: err = %v", err)
	}
	if err := s.TouchSession(ctx, "live", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetSession(ctx, "live", now); !got.LastSeenAt.Equal(now.Add(time.Minute)) {
		t.Errorf("last seen = %s", got.LastSeenAt)
	}
	if n, err := s.DeleteExpiredSessions(ctx, now); err != nil || n != 1 {
		t.Errorf("expired deleted = %d, %v", n, err)
	}
	if err := s.DeleteSession(ctx, "live"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSession(ctx, "live", now); !errors.Is(err, ErrNotFound) {
		t.Error("a deleted session is still there")
	}
	s.CreateSession(ctx, live)
	s.CreateSession(ctx, Session{TokenHash: "other", UserID: u.ID, CreatedAt: now, ExpiresAt: now.Add(time.Hour), LastSeenAt: now})
	if err := s.DeleteUserSessions(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSession(ctx, "other", now); !errors.Is(err, ErrNotFound) {
		t.Error("a user's sessions survived DeleteUserSessions")
	}
	if err := s.CreateSession(ctx, Session{TokenHash: "orphan", UserID: 999, CreatedAt: now, ExpiresAt: now.Add(time.Hour), LastSeenAt: now}); err == nil {
		t.Error("a session for an unknown user was created")
	}
}

func TestLDAPGroupRolesAndAudit(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)

	g, err := s.CreateLDAPGroupRole(ctx, LDAPGroupRole{GroupDN: "cn=shop-team,ou=groups,dc=example,dc=com", Role: RoleEditor, Namespaces: []string{"shop"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateLDAPGroupRole(ctx, LDAPGroupRole{GroupDN: "CN=Shop-Team,OU=groups,DC=example,DC=com", Role: RoleViewer}); !errors.Is(err, ErrConflict) {
		t.Errorf("same DN in another case: err = %v, want ErrConflict", err)
	}
	admins, _ := s.CreateLDAPGroupRole(ctx, LDAPGroupRole{GroupDN: "cn=platform-admins,ou=groups,dc=example,dc=com", Role: RoleAdmin, Namespaces: []string{"x"}})
	if admins.Namespaces != nil {
		t.Errorf("admin group namespaces = %v", admins.Namespaces)
	}
	if list, _ := s.ListLDAPGroupRoles(ctx); len(list) != 2 || list[0].ID != admins.ID || !reflect.DeepEqual(list[1], g) {
		t.Errorf("list = %+v", list)
	}
	g.Role, g.Namespaces = RoleViewer, []string{"shop", "billing"}
	if updated, err := s.UpdateLDAPGroupRole(ctx, g); err != nil || !reflect.DeepEqual(updated, g) {
		t.Errorf("updated = %+v, %v", updated, err)
	}
	if err := s.DeleteLDAPGroupRole(ctx, g.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteLDAPGroupRole(ctx, g.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete: err = %v", err)
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	entries := []AuditEntry{
		{At: now.Add(-48 * time.Hour), Username: "api-token", Action: "create", ObjectType: "monitor", ObjectID: 1, Summary: "api"},
		{At: now.Add(-time.Minute), UserID: 7, Username: "ayse", Action: "login"},
		{At: now, UserID: 7, Username: "ayse", Action: "delete", ObjectType: "monitor", ObjectID: 1, Summary: "api"},
	}
	for _, e := range entries {
		if err := s.AppendAudit(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.ListAudit(ctx, 2)
	if err != nil || len(got) != 2 || got[0].Action != "delete" || got[0].UserID != 7 || got[1].Action != "login" {
		t.Errorf("audit = %+v, %v", got, err)
	}
	if n, _ := s.DeleteAuditBefore(ctx, now.Add(-24*time.Hour)); n != 1 {
		t.Errorf("deleted %d old entries, want 1", n)
	}
}

// Incidents and maintenance windows carry the namespace of their monitor,
// so that what a user may see can be decided without another lookup.
func TestMonitorNamespaceOnIncidentsAndMaintenance(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	m := newMonitor("api")
	m.Namespace = "shop"
	created, _ := s.CreateMonitor(ctx, m)
	s.OpenIncident(ctx, Incident{MonitorID: created.ID, StartedAt: time.Now(), Cause: "x"})
	s.CreateMaintenance(ctx, Maintenance{Name: "w", StartsAt: time.Now(), EndsAt: time.Now().Add(time.Hour), MonitorID: created.ID})

	if incidents, _ := s.ListIncidents(ctx, 0, 10); len(incidents) != 1 || incidents[0].MonitorNamespace != "shop" {
		t.Errorf("incidents = %+v", incidents)
	}
	if windows, _ := s.ListMaintenance(ctx, 10); len(windows) != 1 || windows[0].MonitorNamespace != "shop" || windows[0].MonitorName != "api" {
		t.Errorf("maintenance = %+v", windows)
	}
}
