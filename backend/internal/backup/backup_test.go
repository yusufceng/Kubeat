package backup

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/metrics"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

// fakeStore writes a small file as its "backup".
type fakeStore struct {
	calls atomic.Int32
	err   error
}

func (f *fakeStore) Backup(_ context.Context, dest string) error {
	f.calls.Add(1)
	if f.err != nil {
		// Leave a partial file behind, as an interrupted copy would.
		os.WriteFile(dest, []byte("half"), 0o600)
		return f.err
	}
	return os.WriteFile(dest, []byte("database copy"), 0o600)
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func TestTakeNamesVerifiesAndPrunes(t *testing.T) {
	dir := t.TempDir()
	st := &fakeStore{}
	opts := Options{Dir: dir, Keep: 2, Logger: discard}
	base := time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC)
	// Files that are not ours must survive pruning.
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("keep me"), 0o600)
	os.WriteFile(filepath.Join(dir, "kubeat-manual-copy.db"), []byte("keep me too"), 0o600)

	for day := 0; day < 4; day++ {
		path, err := Take(context.Background(), st, opts, base.AddDate(0, 0, day))
		if err != nil {
			t.Fatalf("take %d: %v", day, err)
		}
		if got, _ := os.ReadFile(path); string(got) != "database copy" {
			t.Errorf("backup %s contains %q", path, got)
		}
	}
	got := strings.Join(names(t, dir), " ")
	want := "kubeat-20261014T030000Z.db kubeat-20261015T030000Z.db kubeat-manual-copy.db notes.txt"
	if got != want {
		t.Errorf("directory = %s\nwant        %s", got, want)
	}

	// A backup that does not verify is discarded and reported.
	opts.Verify = func(context.Context, string) error { return errors.New("not a database") }
	if _, err := Take(context.Background(), st, opts, base.AddDate(0, 0, 9)); err == nil {
		t.Error("an unverifiable backup was kept")
	}
	// So is one the store failed to write; neither leaves a partial file.
	opts.Verify = nil
	st.err = errors.New("disk full")
	if _, err := Take(context.Background(), st, opts, base.AddDate(0, 0, 10)); err == nil {
		t.Error("a failed backup reported success")
	}
	if after := strings.Join(names(t, dir), " "); after != want {
		t.Errorf("failed backups changed the directory: %s", after)
	}
}

func TestRunWaitsForTheIntervalSinceTheNewestBackup(t *testing.T) {
	dir := t.TempDir()
	m := metrics.New("test")
	run := func(st *fakeStore, interval time.Duration) (stop func()) {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			Run(ctx, st, Options{Dir: dir, Interval: interval, Keep: 100, Metrics: m, Logger: discard})
		}()
		return func() {
			cancel()
			<-done
		}
	}
	waitFor := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}

	// Nothing there yet: a backup is taken at once.
	first := &fakeStore{}
	stop := run(first, time.Hour)
	waitFor("the first backup", func() bool { return first.calls.Load() == 1 })
	time.Sleep(30 * time.Millisecond)
	stop()
	if n := first.calls.Load(); n != 1 {
		t.Errorf("%d backups within one interval, want 1", n)
	}

	// A restart inside the interval takes none.
	restarted := &fakeStore{}
	stop = run(restarted, time.Hour)
	time.Sleep(50 * time.Millisecond)
	stop()
	if n := restarted.calls.Load(); n != 0 {
		t.Errorf("restart took %d extra backups", n)
	}

	got, err := m.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if got["kubeat_backups_total{result=ok}"] != 1 || got["kubeat_backup_last_success_timestamp_seconds"] == 0 {
		t.Errorf("metrics = ok:%v last:%v", got["kubeat_backups_total{result=ok}"], got["kubeat_backup_last_success_timestamp_seconds"])
	}

	// A failing store is retried without spinning, and counted.
	failing := &fakeStore{err: errors.New("disk full")}
	os.RemoveAll(dir)
	stop = run(failing, 20*time.Millisecond)
	waitFor("retries", func() bool { return failing.calls.Load() >= 2 })
	stop()
	if n := failing.calls.Load(); n > 20 {
		t.Errorf("%d attempts in a moment: the loop is spinning", n)
	}
	if got, _ := m.Gather(); got["kubeat_backups_total{result=failed}"] < 2 {
		t.Errorf("failed backups counted = %v", got["kubeat_backups_total{result=failed}"])
	}
}

// End to end with the real store: the backup opens and holds the data.
func TestBackupOfARealDatabase(t *testing.T) {
	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, filepath.Join(t.TempDir(), "live.db"), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.CreateMonitor(ctx, store.Monitor{Name: "api", Type: store.MonitorTCP, Target: "x:1",
		Interval: 30 * time.Second, Timeout: 5 * time.Second, FailureThreshold: 3, Enabled: true}); err != nil {
		t.Fatal(err)
	}

	verify := func(ctx context.Context, path string) error {
		v, err := store.SchemaVersion(ctx, path)
		if err == nil && v != store.CurrentSchemaVersion() {
			err = errors.New("unexpected schema version")
		}
		return err
	}
	path, err := Take(ctx, db, Options{Dir: t.TempDir(), Keep: 3, Verify: verify, Logger: discard}, time.Now())
	if err != nil {
		t.Fatalf("take: %v", err)
	}
	restored, err := store.OpenSQLite(ctx, path, 5*time.Second)
	if err != nil {
		t.Fatalf("open backup: %v", err)
	}
	defer restored.Close()
	if monitors, _ := restored.ListMonitors(ctx); len(monitors) != 1 || monitors[0].Name != "api" {
		t.Errorf("monitors in the backup = %+v", monitors)
	}
}
