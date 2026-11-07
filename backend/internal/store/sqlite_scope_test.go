package store

import (
	"context"
	"encoding/json"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// The record of a monitor's target scope is kept with its settings, as it
// was given, and read back alike.
func TestTargetScopeIsKept(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	m := newMonitor("held")
	m.Namespace = "shop"
	m.Scope = &TargetScope{Namespaces: []string{"billing", "shop"}}
	created, err := s.CreateMonitor(ctx, m)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetMonitor(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Scope, m.Scope) {
		t.Errorf("Scope = %+v, want %+v", got.Scope, m.Scope)
	}
	// An update keeps the record it is given, and so drops none.
	got.Name = "held-2"
	updated, err := s.UpdateMonitor(ctx, got)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(updated.Scope, m.Scope) {
		t.Errorf("after an update Scope = %+v", updated.Scope)
	}
	// One without a record is stored without one.
	plain, err := s.CreateMonitor(ctx, newMonitor("plain"))
	if err != nil {
		t.Fatal(err)
	}
	if plain.Scope != nil || strings.Contains(rawSettings(t, s, plain.ID), "scope") {
		t.Errorf("a monitor without a record: %+v, %s", plain.Scope, rawSettings(t, s, plain.ID))
	}
}

// settings050 is the stored form of a monitor's options as 0.50.0 reads and
// writes it (v0.50.0, backend/internal/store/sqlite.go): the options of its
// type, and nothing of a target scope.
type settings050 struct {
	HTTP      *HTTPOptions      `json:"http,omitempty"`
	TCP       *TCPOptions       `json:"tcp,omitempty"`
	DNS       *DNSOptions       `json:"dns,omitempty"`
	GRPC      *GRPCOptions      `json:"grpc,omitempty"`
	WebSocket *WebSocketOptions `json:"websocket,omitempty"`
	Database  *DatabaseOptions  `json:"database,omitempty"`
	Workload  *WorkloadOptions  `json:"workload,omitempty"`
}

func rawSettings(t *testing.T, s *SQLite, id int64) string {
	t.Helper()
	var text string
	if err := s.read.QueryRowContext(context.Background(), "SELECT settings FROM monitors WHERE id = ?", id).Scan(&text); err != nil {
		t.Fatal(err)
	}
	return text
}

// After a rollback to 0.50, which reads the settings as before, saving a
// monitor writes its options alone: the record is gone, and back on this
// version the monitor is read as one without a record, held to its
// namespace.
func TestTargetScopeDroppedBy050(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	m := newMonitor("held")
	m.Namespace = "shop"
	m.HTTP = HTTPOptions{Method: "GET", ExpectedStatus: "200", AuthSecret: "s3cret", Auth: HTTPAuthBearer}
	m.Scope = &TargetScope{Namespaces: []string{"billing", "shop"}}
	created, err := s.CreateMonitor(ctx, m)
	if err != nil {
		t.Fatal(err)
	}
	stored := rawSettings(t, s, created.ID)
	if !strings.Contains(stored, `"scope":{"namespaces":["billing","shop"]}`) {
		t.Fatalf("stored settings: %s", stored)
	}

	// 0.50 reads the settings without an error, and writes back what it
	// read of them, as its UpdateMonitor does with every save.
	var options Monitor
	old := settings050{HTTP: &options.HTTP, TCP: &options.TCP, DNS: &options.DNS, GRPC: &options.GRPC,
		WebSocket: &options.WebSocket, Database: &options.Database, Workload: &options.Workload}
	if err := json.Unmarshal([]byte(stored), &old); err != nil {
		t.Fatalf("0.50 cannot read the settings: %v", err)
	}
	rewritten, err := json.Marshal(settings050{HTTP: &options.HTTP})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rewritten), "scope") {
		t.Fatalf("0.50 kept the record: %s", rewritten)
	}
	if _, err := s.write.ExecContext(ctx, "UPDATE monitors SET settings = ? WHERE id = ?", string(rewritten), created.ID); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetMonitor(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Scope != nil {
		t.Errorf("Scope = %+v after 0.50 saved the monitor, want none", got.Scope)
	}
	if got.HTTP.AuthSecret != "s3cret" {
		t.Errorf("the secret did not survive: %q", got.HTTP.AuthSecret)
	}
	want := TargetScope{Namespaces: []string{"shop"}}
	if scope := got.TargetScope(); !reflect.DeepEqual(scope, want) {
		t.Errorf("TargetScope() = %+v, want the namespace's %+v", scope, want)
	}
}

// A monitor without a record is held to what its namespace says of who may
// have set it up: nothing without one, its namespace otherwise, its own host
// by the namespace alone when the target came from a cluster object.
func TestTargetScopeOfAMonitorWithoutARecord(t *testing.T) {
	all := TargetScope{Namespaces: []string{AllNamespaces}}
	for _, c := range []struct {
		m    Monitor
		want TargetScope
	}{
		{Monitor{}, all},
		{Monitor{Namespace: "shop"}, TargetScope{Namespaces: []string{"shop"}}},
		{Monitor{Namespace: "shop", Source: "route/web"}, TargetScope{Namespaces: []string{"shop"}, Discovered: true}},
		{Monitor{Namespace: "shop", Scope: &all}, all},
		{Monitor{Scope: &TargetScope{Namespaces: []string{"billing"}}}, TargetScope{Namespaces: []string{"billing"}}},
	} {
		if got := c.m.TargetScope(); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%+v: TargetScope() = %+v, want %+v", c.m, got, c.want)
		}
	}
	if !all.All() || (TargetScope{Namespaces: []string{"shop"}}).All() {
		t.Error("All() is wrong")
	}
}

// The monitors without a record are counted, and given one, without
// touching those that have one or the rest of their settings.
func TestRecordScopes(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	held := newMonitor("held")
	held.Scope = &TargetScope{Namespaces: []string{"shop"}}
	if _, err := s.CreateMonitor(ctx, held); err != nil {
		t.Fatal(err)
	}
	secret := newMonitor("secret")
	secret.HTTP = HTTPOptions{Method: "GET", ExpectedStatus: "200", Auth: HTTPAuthBearer, AuthSecret: "s3cret"}
	unrecorded, err := s.CreateMonitor(ctx, secret)
	if err != nil {
		t.Fatal(err)
	}
	// One whose record is null, as a hand-made row might hold.
	nullRecord, err := s.CreateMonitor(ctx, newMonitor("null"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.write.ExecContext(ctx, `UPDATE monitors SET settings = json_set(settings, '$.scope', json('null')) WHERE id = ?`, nullRecord.ID); err != nil {
		t.Fatal(err)
	}

	if n, err := s.MonitorsWithoutScope(ctx); err != nil || n != 2 {
		t.Fatalf("MonitorsWithoutScope = %d, %v; want 2", n, err)
	}
	all := TargetScope{Namespaces: []string{AllNamespaces}}
	if n, err := s.RecordScopes(ctx, all); err != nil || n != 2 {
		t.Fatalf("RecordScopes = %d, %v; want 2", n, err)
	}
	if n, err := s.MonitorsWithoutScope(ctx); err != nil || n != 0 {
		t.Errorf("MonitorsWithoutScope after = %d, %v", n, err)
	}
	monitors, err := s.ListMonitors(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range monitors {
		want := &all
		if m.Name == "held" {
			want = held.Scope
		}
		if !reflect.DeepEqual(m.Scope, want) {
			t.Errorf("%s: Scope = %+v, want %+v", m.Name, m.Scope, want)
		}
		if m.ID == unrecorded.ID && m.HTTP.AuthSecret != "s3cret" {
			t.Errorf("the sealed secret did not survive: %q", m.HTTP.AuthSecret)
		}
	}
	if n, err := s.RecordScopes(ctx, all); err != nil || n != 0 {
		t.Errorf("RecordScopes again = %d, %v; want 0", n, err)
	}
}

// An annotation monitor without a record is given its object's, in every
// install; a monitor imported by hand is given the record asked for, its
// target marked as taken from the object, as a monitor without a record
// reads it; one entered by hand the record as it is.
func TestRecordObjectScopes(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	annotated := newMonitor("annotated")
	annotated.Namespace, annotated.Source, annotated.Managed = "shop", "ingress/web", true
	imported := newMonitor("imported")
	imported.Namespace, imported.Source = "shop", "route/orders"
	entered := newMonitor("entered")
	entered.Namespace = "shop"
	for _, m := range []Monitor{annotated, imported, entered} {
		if _, err := s.CreateMonitor(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := s.RecordObjectScopes(ctx); err != nil || n != 1 {
		t.Fatalf("RecordObjectScopes = %d, %v; want 1", n, err)
	}
	all := TargetScope{Namespaces: []string{AllNamespaces}}
	if n, err := s.RecordScopes(ctx, all); err != nil || n != 2 {
		t.Fatalf("RecordScopes = %d, %v; want 2", n, err)
	}
	monitors, err := s.ListMonitors(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]*TargetScope{
		"annotated": ObjectScope("shop"),
		"imported":  {Namespaces: []string{AllNamespaces}, Discovered: true},
		"entered":   &all,
	}
	for _, m := range monitors {
		if !reflect.DeepEqual(m.Scope, want[m.Name]) {
			t.Errorf("%s: Scope = %+v, want %+v", m.Name, m.Scope, want[m.Name])
		}
		if m.Name == "annotated" && !HeldToObject(m) {
			t.Errorf("%s is not held to its object", m.Name)
		}
	}
	if n, err := s.RecordObjectScopes(ctx); err != nil || n != 0 {
		t.Errorf("RecordObjectScopes again = %d, %v; want 0", n, err)
	}
}

// SameCheck tells a change of how a monitor is checked from a save that
// leaves it as it was, defaults filled in, the rest of the monitor and its
// record left out.
func TestSameCheck(t *testing.T) {
	base := newMonitor("a")
	base.HTTP = HTTPOptions{Method: "GET", ExpectedStatus: "200-399", MaxRedirects: 10, Headers: map[string]string{"X-A": "1"}}
	same := base
	same.Name, same.Tags, same.Interval, same.Enabled = "b", []string{"x"}, time.Hour, false
	same.Scope = &TargetScope{Namespaces: []string{"shop"}}
	same.HTTP.Method, same.HTTP.ExpectedStatus = "", "" // the defaults, as stored
	if !SameCheck(base, same) {
		t.Error("a rename, tags, interval, pause and record count as a change of the check")
	}
	for name, change := range map[string]func(*Monitor){
		"target":        func(m *Monitor) { m.Target += "x" },
		"each pod":      func(m *Monitor) { m.EachPod = true },
		"max redirects": func(m *Monitor) { m.HTTP.MaxRedirects = 0 },
		"keyword":       func(m *Monitor) { m.HTTP.Keyword = "ok" },
		"Host header":   func(m *Monitor) { m.HTTP.Headers = map[string]string{"X-A": "1", "Host": "other.example"} },
		"secret":        func(m *Monitor) { m.HTTP.AuthSecret = "s" },
		"type":          func(m *Monitor) { m.Type = MonitorWebSocket },
	} {
		changed := base
		changed.HTTP.Headers = maps.Clone(base.HTTP.Headers)
		change(&changed)
		if SameCheck(base, changed) {
			t.Errorf("a change of the %s counts as none", name)
		}
	}
	dns := Monitor{Type: MonitorDNS, Target: "example.com", DNS: DNSOptions{RecordType: "A"}}
	other := dns
	other.DNS.Resolver = "10.96.0.10"
	if SameCheck(dns, other) {
		t.Error("a change of the DNS resolver counts as none")
	}
}

// SameConnections tells a change of the hosts a check connects to — the
// target, the server or site an HTTP request names, a DNS check's resolver —
// from a change of the rest of the check, which names none.
func TestSameConnections(t *testing.T) {
	base := newMonitor("a")
	base.HTTP = HTTPOptions{Method: "GET", MaxRedirects: 10, Headers: map[string]string{"Host": "Site.Example"}}
	for name, change := range map[string]func(*Monitor){
		"name, tags and record": func(m *Monitor) {
			m.Name, m.Tags, m.Scope = "b", []string{"x"}, &TargetScope{Namespaces: []string{"shop"}}
		},
		"method, body and content type": func(m *Monitor) { m.HTTP.Method, m.HTTP.Body, m.HTTP.ContentType = "POST", "a\r\nb", "text/plain" },
		"keyword and JSON value":        func(m *Monitor) { m.HTTP.Keyword, m.HTTP.JSONPath, m.HTTP.JSONExpected = "ok", "status", " ok " },
		"other headers":                 func(m *Monitor) { m.HTTP.Headers = map[string]string{"Host": "Site.Example", "X-A": "1"} },
		"secrets":                       func(m *Monitor) { m.HTTP.Auth, m.HTTP.AuthSecret = HTTPAuthBearer, "s" },
		"max redirects":                 func(m *Monitor) { m.HTTP.MaxRedirects = 0 },
		"each pod":                      func(m *Monitor) { m.EachPod = true },
		"TLS verification":              func(m *Monitor) { m.HTTP.SkipTLSVerify = true },
		"the case of the site":          func(m *Monitor) { m.HTTP.Headers = map[string]string{"host": "site.example"} },
	} {
		changed := base
		changed.HTTP.Headers = maps.Clone(base.HTTP.Headers)
		change(&changed)
		if !SameConnections(base, changed) {
			t.Errorf("a change of the %s counts as one of where the check connects", name)
		}
	}
	for name, change := range map[string]func(*Monitor){
		"target":      func(m *Monitor) { m.Target += "x" },
		"Host header": func(m *Monitor) { m.HTTP.Headers = map[string]string{"Host": "other.example"} },
		"no Host":     func(m *Monitor) { m.HTTP.Headers = nil },
		"type":        func(m *Monitor) { m.Type = MonitorWebSocket },
	} {
		changed := base
		changed.HTTP.Headers = maps.Clone(base.HTTP.Headers)
		change(&changed)
		if SameConnections(base, changed) {
			t.Errorf("a change of the %s counts as none", name)
		}
	}
	dns := Monitor{Type: MonitorDNS, Target: "example.com", DNS: DNSOptions{RecordType: "A", Resolver: "8.8.8.8"}}
	asked := dns
	asked.DNS.RecordType, asked.DNS.Expected = "AAAA", "2001:db8::1"
	if !SameConnections(dns, asked) {
		t.Error("a change of what a DNS check asks counts as one of where it connects")
	}
	other := dns
	other.DNS.Resolver = "10.96.0.10"
	if SameConnections(dns, other) {
		t.Error("a change of the DNS resolver counts as none")
	}
}

// Within holds a scope to the namespaces of another: a narrower one narrows
// it, a wider one leaves it, two that overlap leave what both hold.
func TestTargetScopeWithin(t *testing.T) {
	all := []string{AllNamespaces}
	for _, c := range []struct {
		scope, namespaces, want []string
	}{
		{[]string{"shop"}, all, []string{"shop"}},
		{[]string{"billing", "shop"}, []string{"billing", "payments", "shop"}, []string{"billing", "shop"}},
		{all, []string{"shop", "billing", "shop"}, []string{"billing", "shop"}},
		{[]string{"billing", "shop"}, []string{"shop"}, []string{"shop"}},
		{[]string{"billing", "shop"}, []string{"payments", "shop"}, []string{"shop"}},
		{[]string{"billing"}, []string{"shop"}, []string{}},
		{all, all, all},
	} {
		got := TargetScope{Namespaces: c.scope, Discovered: true}.Within(c.namespaces)
		if !slices.Equal(got.Namespaces, c.want) || got.Namespaces == nil || !got.Discovered {
			t.Errorf("%v within %v = %+v, want %v", c.scope, c.namespaces, got, c.want)
		}
	}
}

// Only an editor of some namespaces, as an account or through a directory
// group, is a limited editor: an administrator, an editor of every
// namespace and a viewer are not.
func TestLimitedEditors(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	limited := func() bool {
		t.Helper()
		got, err := s.LimitedEditors(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if limited() {
		t.Error("an empty install has a limited editor")
	}
	for _, u := range []User{
		{Username: "admin", Role: RoleAdmin, Source: UserLocal, Enabled: true},
		{Username: "all", Role: RoleEditor, Namespaces: []string{AllNamespaces}, Source: UserLocal, Enabled: true},
		{Username: "viewer", Role: RoleViewer, Namespaces: []string{"shop"}, Source: UserLocal, Enabled: true},
	} {
		if _, err := s.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.CreateLDAPGroupRole(ctx, LDAPGroupRole{GroupDN: "cn=ops", Role: RoleEditor, Namespaces: []string{AllNamespaces}}); err != nil {
		t.Fatal(err)
	}
	if limited() {
		t.Error("no limited editor, yet one is found")
	}
	if _, err := s.CreateLDAPGroupRole(ctx, LDAPGroupRole{GroupDN: "cn=shop", Role: RoleEditor, Namespaces: []string{"shop"}}); err != nil {
		t.Fatal(err)
	}
	if !limited() {
		t.Error("a directory group of a limited editor is not found")
	}

	s2 := openTest(t)
	if _, err := s2.CreateUser(ctx, User{Username: "dev", Role: RoleEditor, Namespaces: []string{"shop"}, Source: UserLocal}); err != nil {
		t.Fatal(err)
	}
	if got, err := s2.LimitedEditors(ctx); err != nil || !got {
		t.Errorf("a disabled limited editor: %v, %v", got, err)
	}
	if LimitedEditor(RoleEditor, []string{"shop", AllNamespaces}) || !LimitedEditor(RoleEditor, nil) || LimitedEditor(RoleViewer, []string{"shop"}) {
		t.Error("LimitedEditor is wrong")
	}
}
