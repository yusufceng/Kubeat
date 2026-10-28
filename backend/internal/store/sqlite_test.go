package store

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync"
	"syscall"
	"testing"
	"time"
)

func openTest(t *testing.T) *SQLite {
	t.Helper()
	s, err := OpenSQLite(context.Background(), filepath.Join(t.TempDir(), "test.db"), 5*time.Second)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func newMonitor(name string) Monitor {
	return Monitor{
		Name:     name,
		Type:     MonitorHTTP,
		Target:   "https://example.com/healthz",
		Interval: 30 * time.Second,
		Timeout:  5 * time.Second,
		Enabled:  true,

		FailureThreshold: 3,
	}
}

func TestMonitorCRUD(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)

	created, err := s.CreateMonitor(ctx, newMonitor("api"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID == 0 || created.CreatedAt.IsZero() {
		t.Fatalf("ID and CreatedAt not set: %+v", created)
	}

	got, err := s.GetMonitor(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !reflect.DeepEqual(got, created) {
		t.Errorf("get = %+v, want %+v", got, created)
	}

	if _, err := s.CreateMonitor(ctx, newMonitor("api")); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate name: err = %v, want ErrConflict", err)
	}

	if _, err := s.CreateMonitor(ctx, newMonitor("web")); err != nil {
		t.Fatalf("create second: %v", err)
	}
	list, err := s.ListMonitors(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 || list[0].Name != "api" || list[1].Name != "web" {
		t.Errorf("list = %+v", list)
	}

	if err := s.DeleteMonitor(ctx, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.GetMonitor(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("get after delete: err = %v, want ErrNotFound", err)
	}
	if err := s.DeleteMonitor(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete: err = %v, want ErrNotFound", err)
	}
}

func TestInvalidMonitorRejected(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)

	m := newMonitor("bad-type")
	m.Type = "icmp"
	if _, err := s.CreateMonitor(ctx, m); err == nil {
		t.Error("unknown type was accepted")
	}

	m = newMonitor("bad-interval")
	m.Interval = 0
	if _, err := s.CreateMonitor(ctx, m); err == nil {
		t.Error("zero interval was accepted")
	}
}

func TestCheckResults(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)

	m, err := s.CreateMonitor(ctx, newMonitor("api"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	base := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	batch := []CheckResult{
		{MonitorID: m.ID, CheckedAt: base, Up: true, Latency: 42 * time.Millisecond, StatusCode: 200},
		{MonitorID: m.ID, CheckedAt: base.Add(30 * time.Second), Up: false, Latency: 5 * time.Second, Error: "timeout"},
		{MonitorID: m.ID, CheckedAt: base.Add(60 * time.Second), Up: true, Latency: 40 * time.Millisecond, StatusCode: 200},
	}
	if err := s.InsertCheckResults(ctx, batch); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := s.InsertCheckResults(ctx, nil); err != nil {
		t.Fatalf("insert empty batch: %v", err)
	}

	got, err := s.ListCheckResults(ctx, m.ID, 2)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if !reflect.DeepEqual(got[0], batch[2]) || !reflect.DeepEqual(got[1], batch[1]) {
		t.Errorf("list = %+v, want newest first %+v, %+v", got, batch[2], batch[1])
	}

	n, err := s.DeleteCheckResultsBefore(ctx, base.Add(30*time.Second))
	if err != nil {
		t.Fatalf("retention delete: %v", err)
	}
	if n != 1 {
		t.Errorf("deleted = %d, want 1", n)
	}

	if err := s.DeleteMonitor(ctx, m.ID); err != nil {
		t.Fatalf("delete monitor: %v", err)
	}
	got, err = s.ListCheckResults(ctx, m.ID, 10)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("results survived monitor delete: %+v", got)
	}
}

func TestCheckResultForUnknownMonitorRejected(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)

	err := s.InsertCheckResults(ctx, []CheckResult{{MonitorID: 999, CheckedAt: time.Now(), Up: true}})
	if err == nil {
		t.Error("result for unknown monitor was accepted")
	}
}

func TestReopenKeepsDataAndSkipsAppliedMigrations(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "test.db")

	s, err := OpenSQLite(ctx, path, 5*time.Second)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := s.CreateMonitor(ctx, newMonitor("api")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	s, err = OpenSQLite(ctx, path, 5*time.Second)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()
	list, err := s.ListMonitors(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("len = %d, want 1", len(list))
	}
}

func TestConcurrentWritersAndReaders(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)

	m, err := s.CreateMonitor(ctx, newMonitor("api"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	const writers, batches, batchSize = 8, 20, 25
	var wg sync.WaitGroup
	errs := make(chan error, writers*2)
	for w := 0; w < writers; w++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for b := 0; b < batches; b++ {
				batch := make([]CheckResult, batchSize)
				for i := range batch {
					batch[i] = CheckResult{MonitorID: m.ID, CheckedAt: time.Now(), Up: true}
				}
				if err := s.InsertCheckResults(ctx, batch); err != nil {
					errs <- err
					return
				}
			}
		}()
		go func() {
			defer wg.Done()
			for b := 0; b < batches; b++ {
				if _, err := s.ListCheckResults(ctx, m.ID, 10); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent access: %v", err)
	}

	got, err := s.ListCheckResults(ctx, m.ID, writers*batches*batchSize+1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if want := writers * batches * batchSize; len(got) != want {
		t.Errorf("rows = %d, want %d", len(got), want)
	}
}

func TestMonitorStatuses(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)

	api, err := s.CreateMonitor(ctx, newMonitor("api"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	idle, err := s.CreateMonitor(ctx, newMonitor("never-checked"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	base := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	err = s.InsertCheckResults(ctx, []CheckResult{
		// Before the window: must not count towards the statistics.
		{MonitorID: api.ID, CheckedAt: base.Add(-time.Hour), Up: false, Latency: 900 * time.Millisecond, Error: "old"},
		{MonitorID: api.ID, CheckedAt: base, Up: true, Latency: 100 * time.Millisecond, StatusCode: 200},
		{MonitorID: api.ID, CheckedAt: base.Add(time.Minute), Up: true, Latency: 200 * time.Millisecond, StatusCode: 200},
		{MonitorID: api.ID, CheckedAt: base.Add(2 * time.Minute), Up: true, Latency: 300 * time.Millisecond, StatusCode: 200},
		{MonitorID: api.ID, CheckedAt: base.Add(3 * time.Minute), Up: false, Latency: 400 * time.Millisecond, StatusCode: 503, Error: "unexpected status 503"},
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Statistics are read from the hourly summaries.
	if err := s.RollupCheckResults(ctx, base.Add(-time.Hour), base.Add(time.Hour)); err != nil {
		t.Fatalf("rollup: %v", err)
	}

	statuses, err := s.ListMonitorStatuses(ctx, base)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(statuses) != 2 {
		t.Fatalf("len = %d, want 2", len(statuses))
	}

	got := statuses[0]
	if !reflect.DeepEqual(got.Monitor, api) {
		t.Errorf("monitor = %+v, want %+v", got.Monitor, api)
	}
	// The average covers the three successful checks only: 100, 200, 300 ms.
	if got.Checks != 4 || got.UpChecks != 3 || got.AvgLatency != 200*time.Millisecond {
		t.Errorf("Checks=%d UpChecks=%d AvgLatency=%s, want 4, 3, 200ms", got.Checks, got.UpChecks, got.AvgLatency)
	}
	wantLast := CheckResult{MonitorID: api.ID, CheckedAt: base.Add(3 * time.Minute), Up: false,
		Latency: 400 * time.Millisecond, StatusCode: 503, Error: "unexpected status 503"}
	if got.Last == nil || !reflect.DeepEqual(*got.Last, wantLast) {
		t.Errorf("Last = %+v, want %+v", got.Last, wantLast)
	}

	if st := statuses[1]; st.ID != idle.ID || st.Last != nil || st.Checks != 0 || st.AvgLatency != 0 {
		t.Errorf("never-checked monitor: %+v", st)
	}

	one, err := s.GetMonitorStatus(ctx, api.ID, base)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if one.ID != api.ID || one.Checks != 4 || one.Last == nil {
		t.Errorf("GetMonitorStatus = %+v", one)
	}
	if _, err := s.GetMonitorStatus(ctx, 999, base); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown id: err = %v, want ErrNotFound", err)
	}

	// The window starts on the hour: a cutoff inside the hour still counts
	// the whole hour, never part of it.
	if mid, _ := s.GetMonitorStatus(ctx, api.ID, base.Add(30*time.Minute)); mid.Checks != 4 {
		t.Errorf("cutoff inside the hour: Checks = %d, want the hour's 4", mid.Checks)
	}
	// Results newer than the last summary run do not count yet, but are
	// already the monitor's latest result.
	newest := CheckResult{MonitorID: api.ID, CheckedAt: base.Add(5 * time.Minute), Up: true, Latency: time.Second}
	if err := s.InsertCheckResults(ctx, []CheckResult{newest}); err != nil {
		t.Fatal(err)
	}
	if stale, _ := s.GetMonitorStatus(ctx, api.ID, base); stale.Checks != 4 || !stale.Last.CheckedAt.Equal(newest.CheckedAt) {
		t.Errorf("before the next summary run: Checks=%d Last=%s; want 4 and the new result as latest", stale.Checks, stale.Last.CheckedAt)
	}

	// With the window after the last result the latest result is still shown.
	statuses, err = s.ListMonitorStatuses(ctx, base.Add(time.Hour))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if statuses[0].Checks != 0 || statuses[0].Last == nil {
		t.Errorf("empty window: %+v", statuses[0])
	}
}

func TestMonitorSource(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)

	discovered := newMonitor("shop/service/api:80")
	discovered.Namespace, discovered.Source = "shop", "service/api:80"
	created, err := s.CreateMonitor(ctx, discovered)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := s.GetMonitor(ctx, created.ID)
	if err != nil || got.Namespace != "shop" || got.Source != "service/api:80" {
		t.Errorf("GetMonitor = %+v, %v", got, err)
	}
	if st, _ := s.GetMonitorStatus(ctx, created.ID, time.Now()); st.Namespace != "shop" || st.Source != "service/api:80" {
		t.Errorf("status = %+v", st.Monitor)
	}

	// The same object cannot be monitored twice, even under another name.
	again := newMonitor("another-name")
	again.Namespace, again.Source = "shop", "service/api:80"
	if _, err := s.CreateMonitor(ctx, again); !errors.Is(err, ErrConflict) {
		t.Errorf("same source twice: err = %v, want ErrConflict", err)
	}
	// The same source name in a different namespace is a different object.
	other := newMonitor("billing/service/api:80")
	other.Namespace, other.Source = "billing", "service/api:80"
	if _, err := s.CreateMonitor(ctx, other); err != nil {
		t.Errorf("same source in another namespace: %v", err)
	}
	// Hand-made monitors have no source and never collide on it.
	for _, name := range []string{"manual-1", "manual-2"} {
		if _, err := s.CreateMonitor(ctx, newMonitor(name)); err != nil {
			t.Errorf("manual monitor %s: %v", name, err)
		}
	}
}

func TestUpdateMonitor(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)

	discovered := newMonitor("shop/service/api:80")
	discovered.Namespace, discovered.Source = "shop", "service/api:80"
	created, err := s.CreateMonitor(ctx, discovered)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateMonitor(ctx, newMonitor("other")); err != nil {
		t.Fatal(err)
	}

	change := created
	change.Name = "renamed"
	change.Target = "https://example.com/ready"
	change.Interval = time.Minute
	change.Timeout = 10 * time.Second
	change.FailureThreshold = 5
	change.Enabled = false
	// Attempts to change the immutable fields are ignored.
	change.Type = MonitorTCP
	change.Namespace, change.Source = "elsewhere", "route/x"

	updated, err := s.UpdateMonitor(ctx, change)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	want := change
	want.Type, want.Namespace, want.Source = created.Type, created.Namespace, created.Source
	if !reflect.DeepEqual(updated, want) {
		t.Errorf("updated = %+v, want %+v", updated, want)
	}
	if got, _ := s.GetMonitor(ctx, created.ID); !reflect.DeepEqual(got, want) {
		t.Errorf("stored = %+v, want %+v", got, want)
	}

	change.Name = "other"
	if _, err := s.UpdateMonitor(ctx, change); !errors.Is(err, ErrConflict) {
		t.Errorf("rename onto an existing name: err = %v, want ErrConflict", err)
	}
	change.Name, change.Interval = "renamed", 0
	if _, err := s.UpdateMonitor(ctx, change); err == nil {
		t.Error("zero interval was accepted")
	}
	change.ID, change.Interval = 999, time.Minute
	if _, err := s.UpdateMonitor(ctx, change); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown id: err = %v, want ErrNotFound", err)
	}
}

func TestHTTPOptions(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)

	// Left empty, the options take the defaults.
	plain, err := s.CreateMonitor(ctx, newMonitor("plain"))
	if err != nil {
		t.Fatal(err)
	}
	wantDefaults := HTTPOptions{Method: "GET", ExpectedStatus: "200-399"}
	if !reflect.DeepEqual(plain.HTTP, wantDefaults) {
		t.Errorf("returned defaults = %+v, want %+v", plain.HTTP, wantDefaults)
	}
	if got, _ := s.GetMonitor(ctx, plain.ID); !reflect.DeepEqual(got.HTTP, wantDefaults) {
		t.Errorf("stored defaults = %+v, want %+v", got.HTTP, wantDefaults)
	}

	custom := newMonitor("custom")
	custom.HTTP = HTTPOptions{
		Method:         "HEAD",
		ExpectedStatus: "200,401",
		Keyword:        `"status":"ok"`,
		Headers:        map[string]string{"Authorization": "Bearer s3cret", "X-Tenant": "çağrı"},
		SkipTLSVerify:  true,
	}
	created, err := s.CreateMonitor(ctx, custom)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetMonitor(ctx, created.ID)
	if err != nil || !reflect.DeepEqual(got.HTTP, custom.HTTP) {
		t.Errorf("GetMonitor HTTP = %+v, %v; want %+v", got.HTTP, err, custom.HTTP)
	}
	if st, _ := s.GetMonitorStatus(ctx, created.ID, time.Now()); !reflect.DeepEqual(st.HTTP, custom.HTTP) {
		t.Errorf("status HTTP = %+v, want %+v", st.HTTP, custom.HTTP)
	}

	got.HTTP = HTTPOptions{Method: "GET", ExpectedStatus: "204", Headers: map[string]string{}}
	updated, err := s.UpdateMonitor(ctx, got)
	if err != nil {
		t.Fatal(err)
	}
	if want := (HTTPOptions{Method: "GET", ExpectedStatus: "204"}); !reflect.DeepEqual(updated.HTTP, want) {
		t.Errorf("after update = %+v, want %+v (headers and keyword cleared)", updated.HTTP, want)
	}
}

// Each type keeps its own options and nothing of the others.
func TestOptionsByType(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)

	everything := func(name string, typ MonitorType) Monitor {
		m := newMonitor(name)
		m.Type = typ
		m.HTTP = HTTPOptions{Method: "POST", ExpectedStatus: "201", Body: `{"a":1}`, ContentType: "application/json",
			Auth: HTTPAuthBasic, AuthUser: "u", AuthSecret: "p", MaxRedirects: 3, JSONPath: "status", JSONOperator: "==", JSONExpected: "ok",
			InvertKeyword: true, Keyword: "error"}
		m.TCP = TCPOptions{TLS: true, SkipTLSVerify: true}
		m.DNS = DNSOptions{RecordType: "MX", Resolver: "10.0.0.10:53", Expected: "mail"}
		m.GRPC = GRPCOptions{Service: "shop.Cart", TLS: true}
		m.WebSocket = WebSocketOptions{Subprotocol: "graphql-ws"}
		m.Database = DatabaseOptions{Username: "probe", Password: "s3cret", Database: "shop", Query: "SELECT count(*) FROM orders", TLS: true}
		m.Workload = WorkloadOptions{MinReady: 2}
		return m
	}

	for _, typ := range MonitorTypes {
		input := everything("m-"+string(typ), typ)
		created, err := s.CreateMonitor(ctx, input)
		if err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
		want := Monitor{}
		switch typ {
		case MonitorHTTP:
			want.HTTP = input.HTTP
		case MonitorTCP:
			want.TCP = input.TCP
		case MonitorDNS:
			want.DNS = input.DNS
		case MonitorGRPC:
			want.GRPC = input.GRPC
		case MonitorWebSocket:
			want.WebSocket = input.WebSocket
		case MonitorPostgres, MonitorMySQL:
			want.Database = input.Database
		case MonitorRedis:
			// Redis has no database name or query to keep.
			want.Database = DatabaseOptions{Username: "probe", Password: "s3cret", TLS: true}
		case MonitorWorkload:
			want.Workload = input.Workload
		}
		stored, err := s.GetMonitor(ctx, created.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, got := range []Monitor{created, stored} {
			if !reflect.DeepEqual(got.HTTP, want.HTTP) || got.TCP != want.TCP || got.DNS != want.DNS ||
				got.GRPC != want.GRPC || got.WebSocket != want.WebSocket || got.Database != want.Database || got.Workload != want.Workload {
				t.Errorf("%s: options = %+v %+v %+v %+v %+v %+v", typ, got.HTTP, got.TCP, got.DNS, got.GRPC, got.WebSocket, got.Database)
			}
		}
	}

	// A DNS monitor without a record type asks for A records.
	plain := newMonitor("plain-dns")
	plain.Type = MonitorDNS
	if created, _ := s.CreateMonitor(ctx, plain); created.DNS.RecordType != "A" {
		t.Errorf("default record type = %q", created.DNS.RecordType)
	}
}

func TestMonitorExtras(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)

	m := newMonitor("nightly-job")
	m.Type, m.Target = MonitorPush, ""
	m.Description, m.RetryInterval, m.Inverted, m.PushToken = "gece yedeği", 20*time.Second, true, "tok-1"
	created, err := s.CreateMonitor(ctx, m)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetMonitor(ctx, created.ID)
	if err != nil || got.Description != "gece yedeği" || got.RetryInterval != 20*time.Second || !got.Inverted || got.PushToken != "tok-1" {
		t.Errorf("stored = %+v, %v", got, err)
	}

	// Tokens are unique, and a monitor is found by its token.
	dup := newMonitor("other-job")
	dup.Type, dup.PushToken = MonitorPush, "tok-1"
	if _, err := s.CreateMonitor(ctx, dup); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate token: err = %v, want ErrConflict", err)
	}
	if byToken, err := s.GetMonitorByPushToken(ctx, "tok-1"); err != nil || byToken.ID != created.ID {
		t.Errorf("by token = %+v, %v", byToken, err)
	}
	for _, token := range []string{"", "nope"} {
		if _, err := s.GetMonitorByPushToken(ctx, token); !errors.Is(err, ErrNotFound) {
			t.Errorf("token %q: err = %v, want ErrNotFound", token, err)
		}
	}

	// An update changes the extras but never the token.
	got.Description, got.RetryInterval, got.Inverted, got.PushToken = "", 0, false, "tok-2"
	updated, err := s.UpdateMonitor(ctx, got)
	if err != nil || updated.Description != "" || updated.RetryInterval != 0 || updated.Inverted || updated.PushToken != "tok-1" {
		t.Errorf("updated = %+v, %v", updated, err)
	}

	// SetPushToken does, and the old token finds nothing after.
	if err := s.SetPushToken(ctx, created.ID, "tok-2"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetMonitorByPushToken(ctx, "tok-1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("the old token: err = %v", err)
	}
	if byToken, err := s.GetMonitorByPushToken(ctx, "tok-2"); err != nil || byToken.ID != created.ID {
		t.Errorf("the new token: %+v, %v", byToken, err)
	}
	third := newMonitor("job-3")
	third.Type, third.PushToken = MonitorPush, "tok-3"
	other, err := s.CreateMonitor(ctx, third)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetPushToken(ctx, other.ID, "tok-2"); !errors.Is(err, ErrConflict) {
		t.Errorf("another monitor's token: err = %v, want ErrConflict", err)
	}
	web, err := s.CreateMonitor(ctx, newMonitor("web"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{web.ID, 999} {
		if err := s.SetPushToken(ctx, id, "tok-4"); !errors.Is(err, ErrNotFound) {
			t.Errorf("monitor %d, no push monitor: err = %v, want ErrNotFound", id, err)
		}
	}

	// The latest report replaces the one before it.
	if _, err := s.LastPush(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("no report yet: err = %v, want ErrNotFound", err)
	}
	at := time.Now().UTC().Truncate(time.Millisecond)
	for i, report := range []PushReport{
		{MonitorID: created.ID, ReceivedAt: at.Add(-time.Minute), Up: true, Message: "ok", Latency: 120 * time.Millisecond},
		{MonitorID: created.ID, ReceivedAt: at, Up: false, Message: "disk dolu"},
	} {
		if err := s.RecordPush(ctx, report); err != nil {
			t.Fatal(err)
		}
		if last, err := s.LastPush(ctx, created.ID); err != nil || last != report {
			t.Errorf("report %d: last = %+v, %v; want %+v", i, last, err, report)
		}
	}
	if err := s.RecordPush(ctx, PushReport{MonitorID: 999, ReceivedAt: at, Up: true}); err == nil {
		t.Error("a report for an unknown monitor was accepted")
	}

	// A result carries what the check found.
	result := CheckResult{MonitorID: created.ID, CheckedAt: at, Up: true, Detail: "10.0.0.1, 10.0.0.2"}
	if err := s.InsertCheckResults(ctx, []CheckResult{result}); err != nil {
		t.Fatal(err)
	}
	if results, _ := s.ListCheckResults(ctx, created.ID, 1); len(results) != 1 || results[0].Detail != result.Detail {
		t.Errorf("results = %+v", results)
	}
	if st, _ := s.GetMonitorStatus(ctx, created.ID, at); st.Last == nil || st.Last.Detail != result.Detail {
		t.Errorf("status last = %+v", st.Last)
	}
}

// A database written before the monitors table was rebuilt keeps its
// monitors, their options and everything that refers to them.
func TestUpgradeFromColumnOptions(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "old.db")

	old, err := openPool(path, time.Second, true)
	if err != nil {
		t.Fatal(err)
	}
	names, _ := fs.Glob(migrationFiles, "migrations/*.sql")
	sort.Strings(names)
	const before = 9 // the last schema version with one column per option
	for _, name := range names[:before] {
		body, _ := migrationFiles.ReadFile(name)
		if _, err := old.ExecContext(ctx, string(body)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	for _, stmt := range []string{
		fmt.Sprintf("PRAGMA user_version = %d", before),
		`INSERT INTO monitors (id, name, type, target, interval_seconds, timeout_seconds, failure_threshold, enabled, created_at, namespace, source,
		                       http_method, http_expected_status, http_keyword, http_headers, tls_skip_verify)
		 VALUES (1, 'shop/api', 'http', 'https://api.shop.svc/healthz', 30, 5, 2, 1, 1000, 'shop', 'service/api:443',
		         'HEAD', '200,401', '', '{"Authorization":"Bearer s3cret"}', 1),
		        (2, 'plain', 'http', 'http://plain/', 60, 5, 3, 0, 2000, '', '', 'GET', '200-399', 'ok', '{}', 0),
		        (3, 'db', 'tcp', 'db:5432', 60, 5, 3, 1, 3000, '', '', 'GET', '200-399', '', '{}', 0)`,
		`INSERT INTO check_results (monitor_id, checked_at, up, latency_ms) VALUES (1, 5000, 1, 12), (3, 6000, 0, 40)`,
		`INSERT INTO incidents (monitor_id, started_at, cause) VALUES (3, 6000, 'refused')`,
		`INSERT INTO maintenance_windows (name, starts_at, ends_at, monitor_id, created_at) VALUES ('w', 1, 2, 1, 1)`,
	} {
		if _, err := old.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	old.Close()

	s, err := OpenSQLite(ctx, path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	monitors, err := s.ListMonitors(ctx)
	if err != nil || len(monitors) != 3 {
		t.Fatalf("monitors = %+v, %v", monitors, err)
	}
	api, plain, db := monitors[0], monitors[1], monitors[2]
	wantAPI := HTTPOptions{Method: "HEAD", ExpectedStatus: "200,401", Headers: map[string]string{"Authorization": "Bearer s3cret"},
		SkipTLSVerify: true, MaxRedirects: 10}
	if !reflect.DeepEqual(api.HTTP, wantAPI) || api.Namespace != "shop" || api.Source != "service/api:443" ||
		api.FailureThreshold != 2 || api.Interval != 30*time.Second || !api.Enabled {
		t.Errorf("api = %+v", api)
	}
	if want := (HTTPOptions{Method: "GET", ExpectedStatus: "200-399", Keyword: "ok", MaxRedirects: 10}); !reflect.DeepEqual(plain.HTTP, want) || plain.Enabled {
		t.Errorf("plain = %+v", plain)
	}
	if db.Type != MonitorTCP || db.TCP != (TCPOptions{}) || !reflect.DeepEqual(db.HTTP, HTTPOptions{}) {
		t.Errorf("db = %+v", db)
	}

	// Nothing that refers to a monitor was lost, and the references work.
	if results, _ := s.ListCheckResults(ctx, 1, 10); len(results) != 1 {
		t.Errorf("results of monitor 1 = %+v", results)
	}
	if incident, err := s.GetOpenIncident(ctx, 3, IncidentDown); err != nil || incident.Cause != "refused" {
		t.Errorf("incident = %+v, %v", incident, err)
	}
	if windows, _ := s.ListMaintenance(ctx, 10); len(windows) != 1 || windows[0].MonitorID != 1 {
		t.Errorf("maintenance = %+v", windows)
	}
	if err := s.DeleteMonitor(ctx, 3); err != nil {
		t.Fatal(err)
	}
	if results, _ := s.ListCheckResults(ctx, 3, 10); len(results) != 0 {
		t.Errorf("results outlived their monitor: %+v", results)
	}
	// The rules of the old table still hold in the new one.
	if _, err := s.CreateMonitor(ctx, Monitor{Name: "shop/api", Type: MonitorTCP, Target: "x:1", Interval: time.Minute, Timeout: time.Second, FailureThreshold: 1}); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate name: err = %v, want ErrConflict", err)
	}
	if err := s.InsertCheckResults(ctx, []CheckResult{{MonitorID: 999, CheckedAt: time.Now()}}); err == nil {
		t.Error("a result for an unknown monitor was accepted: foreign keys are off")
	}
}

func TestTagsAndGroups(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)

	m := newMonitor("api")
	m.Namespace, m.Tags = "Ödeme sistemleri", []string{"prod", "team-pay", "prod"}
	created, err := s.CreateMonitor(ctx, m)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"prod", "team-pay"} // sorted, once each
	if !reflect.DeepEqual(created.Tags, want) {
		t.Errorf("created tags = %v, want %v", created.Tags, want)
	}
	got, _ := s.GetMonitor(ctx, created.ID)
	if !reflect.DeepEqual(got.Tags, want) || got.Namespace != "Ödeme sistemleri" {
		t.Errorf("stored = %+v", got)
	}
	if st, _ := s.GetMonitorStatus(ctx, created.ID, time.Now()); !reflect.DeepEqual(st.Tags, want) {
		t.Errorf("status tags = %v", st.Tags)
	}

	// A monitor entered by hand can move to another group and lose its tags.
	got.Namespace, got.Tags = "Altyapı", nil
	updated, err := s.UpdateMonitor(ctx, got)
	if err != nil || updated.Namespace != "Altyapı" || updated.Tags != nil {
		t.Errorf("updated = %+v, %v", updated, err)
	}
	if plain, _ := s.CreateMonitor(ctx, newMonitor("plain")); plain.Tags != nil {
		t.Errorf("a monitor without tags has %v", plain.Tags)
	}
}

func TestRecentCheckResults(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	a, _ := s.CreateMonitor(ctx, newMonitor("a"))
	b, _ := s.CreateMonitor(ctx, newMonitor("b"))
	idle, _ := s.CreateMonitor(ctx, newMonitor("idle"))
	base := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)

	var batch []CheckResult
	for i := 0; i < 5; i++ {
		batch = append(batch,
			CheckResult{MonitorID: a.ID, CheckedAt: base.Add(time.Duration(i) * time.Minute), Up: true, Latency: time.Duration(i) * time.Millisecond},
			CheckResult{MonitorID: b.ID, CheckedAt: base.Add(time.Duration(i) * time.Minute), Up: false, Error: "x"})
	}
	if err := s.InsertCheckResults(ctx, batch); err != nil {
		t.Fatal(err)
	}

	got, err := s.RecentCheckResults(ctx, []int64{a.ID, idle.ID, 999}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || len(got[a.ID]) != 3 || len(got[idle.ID]) != 0 || len(got[999]) != 0 {
		t.Fatalf("results per monitor = %d/%d/%d, want 3/0/0", len(got[a.ID]), len(got[idle.ID]), len(got[999]))
	}
	if _, asked := got[b.ID]; asked {
		t.Error("results returned for a monitor that was not asked for")
	}
	// Newest first, and the same rows ListCheckResults gives.
	single, _ := s.ListCheckResults(ctx, a.ID, 3)
	for i := range single {
		if !reflect.DeepEqual(got[a.ID][i], single[i]) {
			t.Errorf("result %d = %+v, want %+v", i, got[a.ID][i], single[i])
		}
	}
	if !got[a.ID][0].CheckedAt.Equal(base.Add(4 * time.Minute)) {
		t.Errorf("first result is at %s, want the newest", got[a.ID][0].CheckedAt)
	}
	if empty, err := s.RecentCheckResults(ctx, nil, 3); err != nil || len(empty) != 0 {
		t.Errorf("no monitors: %v, %v", empty, err)
	}
}

func TestBackup(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	m, _ := s.CreateMonitor(ctx, newMonitor("api"))
	base := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	if err := s.InsertCheckResults(ctx, []CheckResult{{MonitorID: m.ID, CheckedAt: base, Up: true, Latency: time.Millisecond}}); err != nil {
		t.Fatal(err)
	}

	// A destination with characters that need quoting in SQL.
	dest := filepath.Join(t.TempDir(), "it's a backup.db")
	if err := s.Backup(ctx, dest); err != nil {
		t.Fatalf("backup: %v", err)
	}

	// Changes after the backup are not in it.
	if _, err := s.CreateMonitor(ctx, newMonitor("later")); err != nil {
		t.Fatal(err)
	}

	if v, err := SchemaVersion(ctx, dest); err != nil || v != CurrentSchemaVersion() {
		t.Errorf("SchemaVersion of the backup = %d, %v; want %d", v, err, CurrentSchemaVersion())
	}
	restored, err := OpenSQLite(ctx, dest, 5*time.Second)
	if err != nil {
		t.Fatalf("open the backup: %v", err)
	}
	defer restored.Close()
	monitors, _ := restored.ListMonitors(ctx)
	if len(monitors) != 1 || monitors[0].Name != "api" {
		t.Errorf("monitors in the backup = %+v, want only api", monitors)
	}
	if results, _ := restored.ListCheckResults(ctx, m.ID, 10); len(results) != 1 || !results[0].CheckedAt.Equal(base) {
		t.Errorf("results in the backup = %+v", results)
	}

	// An existing file is never overwritten.
	if err := s.Backup(ctx, dest); err == nil {
		t.Error("backup overwrote an existing file")
	}
	if _, err := SchemaVersion(ctx, filepath.Join(t.TempDir(), "missing.db")); err == nil {
		t.Error("SchemaVersion of a missing file succeeded")
	}
	junk := filepath.Join(t.TempDir(), "junk.db")
	os.WriteFile(junk, []byte("this is not a database, just some text that is long enough"), 0o600)
	if _, err := SchemaVersion(ctx, junk); err == nil {
		t.Error("SchemaVersion of a non-database file succeeded")
	}
}

// A copy that ran out of space reports an error that wraps syscall.ENOSPC,
// which the backup package looks for before it makes room and tries again.
// SQLite reports a write refused for lack of space as "database or disk is
// full"; a page limit raises the same result code without filling a disk.
func TestBackupErrorNamesAFullVolume(t *testing.T) {
	ctx := context.Background()
	db := openBare(t)
	// The limit is per connection, so the write must use the same one.
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, "PRAGMA max_page_count = 1"); err != nil {
		t.Fatal(err)
	}
	_, full := db.ExecContext(ctx, "CREATE TABLE t (id INTEGER)")
	if full == nil {
		t.Fatal("a database limited to one page grew")
	}
	if err := backupError(full); !errors.Is(err, syscall.ENOSPC) {
		t.Errorf("backupError(%v) = %v, want it to wrap ENOSPC", full, err)
	}
	// Any other failure is passed on as it is, without the errno.
	other := backupError(errors.New("file exists"))
	if errors.Is(other, syscall.ENOSPC) || other.Error() != "backup: file exists" {
		t.Errorf("backupError(file exists) = %v", other)
	}
}

func TestRetentionDeletesInChunks(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	m, _ := s.CreateMonitor(ctx, newMonitor("api"))
	base := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)

	// More than two chunks of old results, plus a few to keep.
	const old = 2*deleteChunk + 123
	batch := make([]CheckResult, 0, old+3)
	for i := 0; i < old; i++ {
		batch = append(batch, CheckResult{MonitorID: m.ID, CheckedAt: base.Add(-time.Duration(i+1) * time.Second), Up: true})
	}
	for i := 0; i < 3; i++ {
		batch = append(batch, CheckResult{MonitorID: m.ID, CheckedAt: base.Add(time.Duration(i) * time.Second), Up: true})
	}
	if err := s.InsertCheckResults(ctx, batch); err != nil {
		t.Fatal(err)
	}

	n, err := s.DeleteCheckResultsBefore(ctx, base)
	if err != nil || n != old {
		t.Fatalf("deleted %d, %v; want %d", n, err, old)
	}
	left, _ := s.ListCheckResults(ctx, m.ID, 100)
	if len(left) != 3 || left[2].CheckedAt.Before(base) {
		t.Errorf("%d results left, want the 3 at or after the cutoff", len(left))
	}
	if n, _ := s.DeleteCheckResultsBefore(ctx, base); n != 0 {
		t.Errorf("second run deleted %d", n)
	}
}
