package store

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"sync"
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
	if got[0] != batch[2] || got[1] != batch[1] {
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
	if got.Last == nil || *got.Last != wantLast {
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

	bad := newMonitor("bad-method")
	bad.HTTP.Method = "DELETE"
	if _, err := s.CreateMonitor(ctx, bad); err == nil {
		t.Error("method DELETE was accepted")
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
		if got[a.ID][i] != single[i] {
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
