package store

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func openTest(t *testing.T) *SQLite {
	t.Helper()
	s, err := OpenSQLite(context.Background(), filepath.Join(t.TempDir(), "test.db"))
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
	if got != created {
		t.Errorf("get = %+v, want %+v", got, created)
	}

	if _, err := s.CreateMonitor(ctx, newMonitor("api")); err == nil {
		t.Error("duplicate name was accepted")
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

	s, err := OpenSQLite(ctx, path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := s.CreateMonitor(ctx, newMonitor("api")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	s, err = OpenSQLite(ctx, path)
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
