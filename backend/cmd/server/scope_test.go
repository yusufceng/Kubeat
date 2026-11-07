package main

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/config"
	"github.com/yusufceng/Kubeat/backend/internal/metrics"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// Where nobody may edit monitors in only some namespaces, the monitors set
// up before the target scope was recorded keep reaching every namespace;
// where someone may — an account, a directory group, or the default role a
// directory or an identity provider gives — they are held to their
// namespaces, and the log says how many.
func TestRecordTargetScopes(t *testing.T) {
	ctx := context.Background()
	setUp := func(t *testing.T) *store.SQLite {
		db := openDB(t, filepath.Join(t.TempDir(), "kubeat.db"))
		for _, m := range []store.Monitor{
			{Name: "old", Type: store.MonitorHTTP, Target: "https://api.payments.svc/", Namespace: "shop", Enabled: true},
			{Name: "new", Type: store.MonitorHTTP, Target: "https://api.shop.svc/", Namespace: "shop", Enabled: true,
				Scope: &store.TargetScope{Namespaces: []string{"shop"}}},
		} {
			m.Interval, m.Timeout, m.FailureThreshold = time.Minute, 5*time.Second, 1
			if _, err := db.CreateMonitor(ctx, m); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := db.CreateUser(ctx, store.User{Username: "ops", Role: store.RoleEditor, Namespaces: []string{store.AllNamespaces}, Source: store.UserLocal, Enabled: true}); err != nil {
			t.Fatal(err)
		}
		return db
	}
	scopes := func(t *testing.T, db *store.SQLite) map[string]*store.TargetScope {
		monitors, err := db.ListMonitors(ctx)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]*store.TargetScope{}
		for _, m := range monitors {
			out[m.Name] = m.Scope
		}
		return out
	}
	all := &store.TargetScope{Namespaces: []string{store.AllNamespaces}}
	shop := &store.TargetScope{Namespaces: []string{"shop"}}

	t.Run("nobody limited", func(t *testing.T) {
		db := setUp(t)
		var logs bytes.Buffer
		if err := recordTargetScopes(ctx, db, config.Config{}, slog.New(slog.NewTextHandler(&logs, nil))); err != nil {
			t.Fatal(err)
		}
		if got := scopes(t, db); !reflect.DeepEqual(got, map[string]*store.TargetScope{"old": all, "new": shop}) {
			t.Errorf("scopes = %+v", got)
		}
		if !strings.Contains(logs.String(), "level=INFO") || !strings.Contains(logs.String(), "monitors=1") {
			t.Errorf("log: %s", logs.String())
		}
	})
	for name, limit := range map[string]func(t *testing.T, db *store.SQLite) config.Config{
		"a limited account": func(t *testing.T, db *store.SQLite) config.Config {
			if _, err := db.CreateUser(ctx, store.User{Username: "dev", Role: store.RoleEditor, Namespaces: []string{"shop"}, Source: store.UserLocal}); err != nil {
				t.Fatal(err)
			}
			return config.Config{}
		},
		"a limited directory group": func(t *testing.T, db *store.SQLite) config.Config {
			if _, err := db.CreateLDAPGroupRole(ctx, store.LDAPGroupRole{GroupDN: "cn=shop", Role: store.RoleEditor, Namespaces: []string{"shop"}}); err != nil {
				t.Fatal(err)
			}
			return config.Config{}
		},
		"the directory's default role": func(*testing.T, *store.SQLite) config.Config {
			return config.Config{LDAPDefaultRole: "editor", LDAPDefaultNamespaces: "shop, billing"}
		},
		"the identity provider's default role": func(*testing.T, *store.SQLite) config.Config {
			return config.Config{OIDCDefaultRole: "editor", OIDCDefaultNamespaces: "shop"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			db := setUp(t)
			cfg := limit(t, db)
			var logs bytes.Buffer
			if err := recordTargetScopes(ctx, db, cfg, slog.New(slog.NewTextHandler(&logs, nil))); err != nil {
				t.Fatal(err)
			}
			if got := scopes(t, db); got["old"] != nil || !reflect.DeepEqual(got["new"], shop) {
				t.Errorf("scopes = %+v, want old without a record", got)
			}
			if !strings.Contains(logs.String(), "level=WARN") || !strings.Contains(logs.String(), "monitors=1") {
				t.Errorf("log: %s", logs.String())
			}
		})
	}
	// A default role of every namespace limits nobody.
	db := setUp(t)
	if err := recordTargetScopes(ctx, db, config.Config{LDAPDefaultRole: "editor", LDAPDefaultNamespaces: "*", OIDCDefaultRole: "viewer", OIDCDefaultNamespaces: "shop"}, quiet()); err != nil {
		t.Fatal(err)
	}
	if got := scopes(t, db); !reflect.DeepEqual(got["old"], all) {
		t.Errorf("with defaults that limit nobody: %+v", got)
	}
}

// Every check the scope refused counts; the first under a monitor's settings
// is logged and written to the audit log, once.
func TestScopeRefusedIsCountedAndWrittenOnce(t *testing.T) {
	ctx := context.Background()
	db := openDB(t, filepath.Join(t.TempDir(), "kubeat.db"))
	meter := metrics.New("test")
	var logs bytes.Buffer
	refused := scopeRefused(db, meter, "tr", slog.New(slog.NewTextHandler(&logs, nil)))
	m := store.Monitor{ID: 42, Name: "orders"}
	refused(m, "api.payments.svc", true)
	refused(m, "api.payments.svc", false)
	refused(m, "api.payments.svc", false)

	got, err := meter.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if n := got["kubeat_target_scope_denied_total{reason=connection}"]; n != 3 {
		t.Errorf("reason=connection = %v, want 3", n)
	}
	entries, err := db.ListAudit(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("audit entries = %+v, want one", entries)
	}
	e := entries[0]
	if e.Action != "deny" || e.ObjectType != "target-scope" || e.ObjectID != 42 || e.Username != "checker" ||
		!strings.Contains(e.Summary, "orders") || !strings.Contains(e.Summary, "api.payments.svc") || !strings.Contains(e.Summary, "reddedildi") {
		t.Errorf("audit entry = %+v", e)
	}
	if strings.Count(logs.String(), "level=WARN") != 1 || !strings.Contains(logs.String(), "monitor_id=42") {
		t.Errorf("log: %s", logs.String())
	}
}
