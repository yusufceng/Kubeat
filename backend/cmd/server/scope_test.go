package main

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/api"
	"github.com/yusufceng/Kubeat/backend/internal/config"
	"github.com/yusufceng/Kubeat/backend/internal/metrics"
	"github.com/yusufceng/Kubeat/backend/internal/netguard"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// Where nobody may edit monitors in only some namespaces, the monitors set
// up before the target scope was recorded keep reaching every namespace;
// where someone may — an account, a directory group, or the default role a
// directory or an identity provider gives — they are held to their
// namespaces, and the log says how many. An annotation monitor is held to
// its object's namespace in either install.
func TestRecordTargetScopes(t *testing.T) {
	ctx := context.Background()
	setUp := func(t *testing.T) *store.SQLite {
		db := openDB(t, filepath.Join(t.TempDir(), "kubeat.db"))
		for _, m := range []store.Monitor{
			{Name: "old", Type: store.MonitorHTTP, Target: "https://api.payments.svc/", Namespace: "shop", Enabled: true},
			{Name: "new", Type: store.MonitorHTTP, Target: "https://api.shop.svc/", Namespace: "shop", Enabled: true,
				Scope: &store.TargetScope{Namespaces: []string{"shop"}}},
			{Name: "imported", Type: store.MonitorHTTP, Target: "https://orders.apps.example/", Namespace: "shop", Source: "route/orders", Enabled: true},
			{Name: "annotated", Type: store.MonitorHTTP, Target: "https://web.example.com/", Namespace: "shop", Source: "ingress/web", Managed: true, Enabled: true},
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
	object := store.ObjectScope("shop")

	t.Run("nobody limited", func(t *testing.T) {
		db := setUp(t)
		var logs bytes.Buffer
		if err := recordTargetScopes(ctx, db, config.Config{}, slog.New(slog.NewTextHandler(&logs, nil))); err != nil {
			t.Fatal(err)
		}
		want := map[string]*store.TargetScope{
			"old": all, "new": shop, "annotated": object,
			"imported": {Namespaces: []string{store.AllNamespaces}, Discovered: true},
		}
		if got := scopes(t, db); !reflect.DeepEqual(got, want) {
			t.Errorf("scopes = %+v", got)
		}
		if !strings.Contains(logs.String(), "level=INFO") || !strings.Contains(logs.String(), "monitors=2") ||
			!strings.Contains(logs.String(), "annotation monitors") || strings.Contains(logs.String(), "level=WARN") {
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
			if got := scopes(t, db); got["old"] != nil || got["imported"] != nil || !reflect.DeepEqual(got["new"], shop) || !reflect.DeepEqual(got["annotated"], object) {
				t.Errorf("scopes = %+v, want old and imported without a record, annotated its object's", got)
			}
			if !strings.Contains(logs.String(), "level=WARN") || !strings.Contains(logs.String(), "monitors=2") {
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
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	refused := scopeRefused(db, meter, "tr", api.NewAuditCap(0, checkRefusalsDropped, logger), time.Now, logger)
	m := store.Monitor{ID: 42, Name: "orders", Namespace: "shop", Scope: &store.TargetScope{Namespaces: []string{"shop"}}}
	refused(m, "api.payments.svc", netguard.RefusedOutside, true)
	refused(m, "api.payments.svc", netguard.RefusedOutside, false)
	refused(m, "api.payments.svc", netguard.RefusedOutside, false)

	got, err := meter.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if n := got["kubeat_target_scope_denied_total{reason=connection}{source=user}"]; n != 3 {
		t.Errorf("reason=connection source=user = %v, want 3", n)
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
	if strings.Count(logs.String(), "level=WARN") != 1 ||
		!strings.Contains(logs.String(), "monitor_id=42 monitor=orders namespace=shop source=user host=api.payments.svc reason=connection") {
		t.Errorf("log: %s", logs.String())
	}
}

// A check's WARN line names the monitor's namespace and whose record holds
// it, so that a central log tells an annotation monitor, or one set up
// before the scope was recorded, from a user's.
func TestScopeRefusedWarnNamesNamespaceAndSource(t *testing.T) {
	db := openDB(t, filepath.Join(t.TempDir(), "kubeat.db"))
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	refused := scopeRefused(db, metrics.New("test"), "en", api.NewAuditCap(0, checkRefusalsDropped, logger), time.Now, logger)
	refused(store.Monitor{ID: 2, Name: "web", Namespace: "shop", Source: "ingress/web", Managed: true, Scope: store.ObjectScope("shop")},
		"login.example.test", netguard.RefusedUnresolved, true)
	refused(store.Monitor{ID: 3, Name: "old", Namespace: "billing"}, "api.payments.svc", netguard.RefusedOutside, true)
	for _, want := range []string{
		"monitor_id=2 monitor=web namespace=shop source=annotation host=login.example.test reason=resolve_error",
		"monitor_id=3 monitor=old namespace=billing source=unrecorded host=api.payments.svc reason=connection",
	} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("log lacks %q: %s", want, logs.String())
		}
	}
}

// Of a minute's first refusals of checks, no more than
// UPTIME_LOGIN_AUDIT_PER_MINUTE leave a row and a WARN line, by the clock
// given; every refused check is counted all the same. The next minute
// writes again and first says how many rows the minute before left out.
func TestScopeRefusedRowsCappedPerMinute(t *testing.T) {
	ctx := context.Background()
	db := openDB(t, filepath.Join(t.TempDir(), "kubeat.db"))
	meter := metrics.New("test")
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	at := time.Date(2026, 11, 7, 12, 0, 10, 0, time.UTC)
	refused := scopeRefused(db, meter, "en", api.NewAuditCap(2, checkRefusalsDropped, logger), func() time.Time { return at }, logger)
	annotated := func(id int64) store.Monitor {
		return store.Monitor{ID: id, Name: "web-" + strconv.FormatInt(id, 10), Namespace: "shop", Source: "ingress/web", Managed: true, Scope: store.ObjectScope("shop")}
	}
	// A fault of the cluster's DNS after a start: every annotation monitor's
	// first refusal at once.
	for id := int64(1); id <= 5; id++ {
		refused(annotated(id), "login.example.test", netguard.RefusedUnresolved, true)
	}
	rows := func() int {
		t.Helper()
		entries, err := db.ListAudit(ctx, 100)
		if err != nil {
			t.Fatal(err)
		}
		return len(entries)
	}
	got, err := meter.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if n := got["kubeat_target_scope_denied_total{reason=resolve_error}{source=annotation}"]; n != 5 {
		t.Errorf("reason=resolve_error source=annotation = %v, want 5", n)
	}
	if n := rows(); n != 2 {
		t.Errorf("deny rows = %d, want 2, the minute's cap", n)
	}
	if n := strings.Count(logs.String(), "a check was refused"); n != 2 {
		t.Errorf("WARN lines = %d, want 2\n%s", n, logs.String())
	}

	// The next minute: its first row is written, after the warning about
	// the three the minute before left out.
	at = at.Add(time.Minute)
	refused(annotated(6), "login.example.test", netguard.RefusedUnresolved, true)
	if n := rows(); n != 3 {
		t.Errorf("deny rows the next minute = %d, want 3", n)
	}
	log := logs.String()
	if strings.Count(log, checkRefusalsDropped) != 1 || !strings.Contains(log, "dropped=3") || !strings.Contains(log, "limit=2") ||
		strings.Index(log, checkRefusalsDropped) > strings.Index(log, "monitor=web-6") {
		t.Errorf("log: %s", log)
	}
}

// A check's refusal of a name that did not resolve is counted as
// resolve_error, apart from one that leads outside the scope, and its first
// is logged and written down as such, in both languages. Each is counted by
// whose record holds the monitor: a user's, the annotation sync's (in every
// install) or none.
func TestScopeRefusedTellsReasonAndSource(t *testing.T) {
	ctx := context.Background()
	recorded := store.Monitor{ID: 1, Name: "orders", Namespace: "shop", Scope: &store.TargetScope{Namespaces: []string{"shop"}}}
	annotated := store.Monitor{ID: 2, Name: "web", Namespace: "shop", Source: "ingress/web", Managed: true, Scope: store.ObjectScope("shop")}
	unrecorded := store.Monitor{ID: 3, Name: "old", Namespace: "shop"}

	for _, language := range []string{"en", "tr"} {
		t.Run(language, func(t *testing.T) {
			db := openDB(t, filepath.Join(t.TempDir(), "kubeat.db"))
			meter := metrics.New("test")
			var logs bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logs, nil))
			refused := scopeRefused(db, meter, language, api.NewAuditCap(0, checkRefusalsDropped, logger), time.Now, logger)
			for _, m := range []store.Monitor{recorded, annotated, unrecorded} {
				refused(m, "login.example.test", netguard.RefusedUnresolved, true)
				refused(m, "login.example.test", netguard.RefusedUnresolved, false)
				refused(m, "api.payments.svc", netguard.RefusedOutside, true)
			}

			got, err := meter.Gather()
			if err != nil {
				t.Fatal(err)
			}
			for _, source := range []string{metrics.ScopeSourceUser, metrics.ScopeSourceAnnotation, metrics.ScopeSourceUnrecorded} {
				if n := got["kubeat_target_scope_denied_total{reason=resolve_error}{source="+source+"}"]; n != 2 {
					t.Errorf("reason=resolve_error source=%s = %v, want 2", source, n)
				}
				if n := got["kubeat_target_scope_denied_total{reason=connection}{source="+source+"}"]; n != 1 {
					t.Errorf("reason=connection source=%s = %v, want 1", source, n)
				}
			}
			for _, reason := range []string{metrics.ScopeDeniedTarget, metrics.ScopeDeniedNamespace} {
				if n := got["kubeat_target_scope_denied_total{reason="+reason+"}{source=user}"]; n != 0 {
					t.Errorf("reason=%s = %v, want 0: a check counts neither", reason, n)
				}
			}

			entries, err := db.ListAudit(ctx, 20)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 6 {
				t.Fatalf("audit entries = %d, want two a monitor, one for each reason", len(entries))
			}
			unresolved, outside := "a name that did not resolve", "outside the namespaces of whoever set it up"
			if language == "tr" {
				unresolved, outside = "ad çözülemediği için", "monitor'ü kuranın namespace'lerinin dışında"
			}
			for _, e := range entries {
				var want string
				switch {
				case strings.Contains(e.Summary, "login.example.test"):
					want = unresolved
				case strings.Contains(e.Summary, "api.payments.svc"):
					want = outside
				}
				if want == "" || !strings.Contains(e.Summary, want) || e.Username != "checker" || e.Action != "deny" {
					t.Errorf("audit entry = %+v, want one saying %q", e, want)
				}
			}
			log := logs.String()
			if strings.Count(log, "level=WARN") != 6 || strings.Count(log, "reason=resolve_error") != 3 ||
				strings.Count(log, "reason=connection") != 3 || strings.Count(log, "did not resolve") != 3 {
				t.Errorf("log: %s", log)
			}
		})
	}
}
