package backup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/metrics"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

// errFull is a copy that ran out of space, wrapped the way the store wraps
// the driver's "database or disk is full".
var errFull = fmt.Errorf("backup: %w: database or disk is full", syscall.ENOSPC)

// fakeStore writes a small file as its "backup". With room set, the volume
// is pretended to hold that many copies: a write that finds room copies
// there already runs out of space. A failed write leaves a partial file
// behind, as an interrupted copy would.
type fakeStore struct {
	calls atomic.Int32
	err   error // every write fails with it
	room  int   // copies the volume holds; 0 means no limit

	mu   sync.Mutex
	seen []int // copies in the directory at each write
}

func (f *fakeStore) Backup(_ context.Context, dest string) error {
	f.calls.Add(1)
	copies, _ := list(filepath.Dir(dest))
	f.mu.Lock()
	f.seen = append(f.seen, len(copies))
	f.mu.Unlock()

	err := f.err
	if err == nil && f.room > 0 && len(copies) >= f.room {
		err = errFull
	}
	if err != nil {
		os.WriteFile(dest, []byte("half"), 0o600)
		return err
	}
	return os.WriteFile(dest, []byte("database copy"), 0o600)
}

// copiesSeen is how many copies were in the directory at each write.
func (f *fakeStore) copiesSeen() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return fmt.Sprint(f.seen)
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

// waitFor polls cond until it holds or five seconds are up.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
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

	// A backup that does not verify is discarded and reported. Room was
	// made for it first, so the directory is down to keep-1 copies; the
	// attempt leaves no partial file.
	opts.Verify = func(context.Context, string) error { return errors.New("not a database") }
	if _, err := Take(context.Background(), st, opts, base.AddDate(0, 0, 9)); err == nil {
		t.Error("an unverifiable backup was kept")
	}
	want = "kubeat-20261015T030000Z.db kubeat-manual-copy.db notes.txt"
	if after := strings.Join(names(t, dir), " "); after != want {
		t.Errorf("after a failed verification: %s\nwant %s", after, want)
	}
	// So is one the store failed to write; the directory holds no more
	// copies than before.
	opts.Verify = nil
	st.err = errors.New("disk read error")
	if _, err := Take(context.Background(), st, opts, base.AddDate(0, 0, 10)); err == nil {
		t.Error("a failed backup reported success")
	}
	if after := strings.Join(names(t, dir), " "); after != want {
		t.Errorf("a failed write changed the directory: %s\nwant %s", after, want)
	}
}

// The oldest copies go before the new one is written, so a volume sized
// for keep copies never fills up: with keep=3 the store finds two copies
// when it writes the fourth.
func TestTakeMakesRoomBeforeWriting(t *testing.T) {
	dir := t.TempDir()
	st := &fakeStore{}
	opts := Options{Dir: dir, Keep: 3, Logger: discard}
	base := time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC)
	for day := 0; day < 4; day++ {
		if _, err := Take(context.Background(), st, opts, base.AddDate(0, 0, day)); err != nil {
			t.Fatalf("take %d: %v", day, err)
		}
	}
	if saw := st.copiesSeen(); saw != "[0 1 2 2]" {
		t.Errorf("copies present at each write = %s, want [0 1 2 2]", saw)
	}
	got := strings.Join(names(t, dir), " ")
	want := "kubeat-20261013T030000Z.db kubeat-20261014T030000Z.db kubeat-20261015T030000Z.db"
	if got != want {
		t.Errorf("directory = %s\nwant        %s", got, want)
	}

	// A failed write leaves no more copies than there were, and no partial.
	st.err = errors.New("disk read error")
	if _, err := Take(context.Background(), st, opts, base.AddDate(0, 0, 4)); err == nil {
		t.Error("a failed backup reported success")
	}
	want = "kubeat-20261014T030000Z.db kubeat-20261015T030000Z.db"
	if got := strings.Join(names(t, dir), " "); got != want {
		t.Errorf("after a failed write: %s\nwant %s", got, want)
	}
}

// When the write still runs out of space, the oldest copy goes and the
// write is tried once more; the log says so. The retry is one: a store that
// keeps running out of space costs one copy per attempt, and never the
// last one.
func TestTakeRetriesOnceWhenTheVolumeIsFull(t *testing.T) {
	dir := t.TempDir()
	var log bytes.Buffer
	st := &fakeStore{room: 2}
	opts := Options{Dir: dir, Keep: 3, Logger: slog.New(slog.NewTextHandler(&log, nil))}
	base := time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC)
	for day := 0; day < 2; day++ {
		if _, err := Take(context.Background(), st, opts, base.AddDate(0, 0, day)); err != nil {
			t.Fatalf("take %d: %v", day, err)
		}
	}
	if log.Len() != 0 {
		t.Errorf("writes that fit were logged: %s", log.String())
	}

	// The third copy does not fit beside the two the volume holds.
	if _, err := Take(context.Background(), st, opts, base.AddDate(0, 0, 2)); err != nil {
		t.Fatalf("take on a full volume: %v", err)
	}
	if saw := st.copiesSeen(); saw != "[0 1 2 1]" {
		t.Errorf("copies present at each write = %s, want [0 1 2 1]", saw)
	}
	want := "kubeat-20261013T030000Z.db kubeat-20261014T030000Z.db"
	if got := strings.Join(names(t, dir), " "); got != want {
		t.Errorf("directory = %s\nwant        %s", got, want)
	}
	if !strings.Contains(log.String(), "level=WARN") ||
		!strings.Contains(log.String(), "backup volume full; removed the oldest copy and retried") ||
		!strings.Contains(log.String(), "removed=kubeat-20261012T030000Z.db") {
		t.Errorf("the log does not say why a copy went: %s", log.String())
	}

	// From now on nothing fits: each attempt costs one copy and fails, down
	// to the last copy, which stays: with keep >= 2 it is never given up,
	// so the attempts after it get no retry. No partial file stays behind.
	st.err = errFull
	log.Reset()
	for day := 3; day < 6; day++ {
		if _, err := Take(context.Background(), st, opts, base.AddDate(0, 0, day)); !errors.Is(err, syscall.ENOSPC) {
			t.Errorf("take %d: error = %v, want ENOSPC", day, err)
		}
	}
	if saw := st.copiesSeen(); saw != "[0 1 2 1 2 1 1 1]" {
		t.Errorf("copies present at each write = %s, want [0 1 2 1 2 1 1 1]", saw)
	}
	if got := strings.Join(names(t, dir), " "); got != "kubeat-20261014T030000Z.db" {
		t.Errorf("directory = %s, want the last copy kept", got)
	}
	if !strings.Contains(log.String(), "backup volume full; keeping the only copy, no retry") ||
		!strings.Contains(log.String(), "kept=kubeat-20261014T030000Z.db") {
		t.Errorf("the log does not say that the last copy was kept: %s", log.String())
	}
	if strings.Contains(log.String(), "removed the only copy") {
		t.Errorf("the last copy was given up with keep=3: %s", log.String())
	}
}

// With keep=1 the only copy is not given up before the new one is written:
// it goes only when the write ran out of space, and the warning says that
// there is no backup until the retry succeeds.
func TestTakeWithKeepOneRemovesTheCopyOnlyWhenFull(t *testing.T) {
	dir := t.TempDir()
	var log bytes.Buffer
	st := &fakeStore{room: 2}
	opts := Options{Dir: dir, Keep: 1, Logger: slog.New(slog.NewTextHandler(&log, nil))}
	base := time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC)
	for day := 0; day < 2; day++ {
		if _, err := Take(context.Background(), st, opts, base.AddDate(0, 0, day)); err != nil {
			t.Fatalf("take %d: %v", day, err)
		}
	}
	// Room for two: the second copy was written beside the first, which
	// went only afterwards.
	if saw := st.copiesSeen(); saw != "[0 1]" {
		t.Errorf("copies present at each write = %s, want [0 1]", saw)
	}
	if got := strings.Join(names(t, dir), " "); got != "kubeat-20261013T030000Z.db" {
		t.Errorf("directory = %s", got)
	}
	if log.Len() != 0 {
		t.Errorf("writes that fit were logged: %s", log.String())
	}

	// Room for one: the write is still tried first, then the copy goes.
	st.room = 1
	if _, err := Take(context.Background(), st, opts, base.AddDate(0, 0, 2)); err != nil {
		t.Fatalf("take on a full volume: %v", err)
	}
	if saw := st.copiesSeen(); saw != "[0 1 1 0]" {
		t.Errorf("copies present at each write = %s, want [0 1 1 0]", saw)
	}
	if got := strings.Join(names(t, dir), " "); got != "kubeat-20261014T030000Z.db" {
		t.Errorf("directory = %s", got)
	}
	if !strings.Contains(log.String(), "level=WARN") || !strings.Contains(log.String(), "removed the only copy") {
		t.Errorf("the moment without a backup was not warned about: %s", log.String())
	}

	// Nothing fits any more: with keep=1 the only copy still goes, as
	// accepted, and the directory ends up empty.
	st.err = errFull
	log.Reset()
	if _, err := Take(context.Background(), st, opts, base.AddDate(0, 0, 3)); !errors.Is(err, syscall.ENOSPC) {
		t.Errorf("take on a volume that stays full: error = %v, want ENOSPC", err)
	}
	if saw := st.copiesSeen(); saw != "[0 1 1 0 1 0]" {
		t.Errorf("copies present at each write = %s, want [0 1 1 0 1 0]", saw)
	}
	if got := names(t, dir); len(got) != 0 {
		t.Errorf("directory = %v, want empty", got)
	}
	if !strings.Contains(log.String(), "removed the only copy") {
		t.Errorf("the log does not say that no backup is left: %s", log.String())
	}
}

// A partial file left behind by a copy that was cut short, say by a restart
// during a large copy, is removed before the next write, so that it does
// not take up a copy's worth of room for good. Files this package did not
// name stay.
func TestTakeRemovesAPartialLeftBehind(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "kubeat-20261011T030000Z.db.partial"), []byte("half"), 0o600)
	os.WriteFile(filepath.Join(dir, "kubeat-manual-copy.db.partial"), []byte("keep me"), 0o600)
	opts := Options{Dir: dir, Keep: 3, Logger: discard}
	if _, err := Take(context.Background(), &fakeStore{}, opts, time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("take: %v", err)
	}
	got := strings.Join(names(t, dir), " ")
	want := "kubeat-20261012T030000Z.db kubeat-manual-copy.db.partial"
	if got != want {
		t.Errorf("directory = %s\nwant        %s", got, want)
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

	// Nothing there yet: a backup is taken at once.
	first := &fakeStore{}
	stop := run(first, time.Hour)
	waitFor(t, "the first backup", func() bool { return first.calls.Load() == 1 })
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
	failing := &fakeStore{err: errors.New("disk read error")}
	os.RemoveAll(dir)
	stop = run(failing, 20*time.Millisecond)
	// Wait for the count itself: an attempt is counted when it ends, and
	// one that is cut short by stopping is not counted at all.
	waitFor(t, "two counted failures", func() bool {
		got, _ := m.Gather()
		return got["kubeat_backups_total{result=failed}"] >= 2
	})
	stop()
	if n := failing.calls.Load(); n > 20 {
		t.Errorf("%d attempts in a moment: the loop is spinning", n)
	}
}

// A volume that stays full is tried again after a whole interval, not after
// the usual pause: it does not empty within a minute, and every attempt on
// it may cost a copy. Any other failure is retried after the pause.
func TestRunWaitsAnIntervalAfterAFullVolume(t *testing.T) {
	failurePause = time.Millisecond
	t.Cleanup(func() { failurePause = time.Minute })
	run := func(st *fakeStore, interval time.Duration) (stop func()) {
		dir := t.TempDir()
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			Run(ctx, st, Options{Dir: dir, Interval: interval, Keep: 3, Metrics: metrics.New("test"), Logger: discard})
		}()
		return func() {
			cancel()
			<-done
		}
	}

	// A broken store is tried again after the pause, well within the
	// interval.
	broken := &fakeStore{err: errors.New("disk read error")}
	stop := run(broken, 5*time.Second)
	waitFor(t, "three attempts on a broken store", func() bool { return broken.calls.Load() >= 3 })
	stop()

	// A full volume is tried again only once the interval has passed.
	full := &fakeStore{err: errFull}
	stop = run(full, 400*time.Millisecond)
	waitFor(t, "the first attempt on a full volume", func() bool { return full.calls.Load() == 1 })
	time.Sleep(100 * time.Millisecond)
	if n := full.calls.Load(); n != 1 {
		t.Errorf("%d attempts within the interval, want 1", n)
	}
	waitFor(t, "the attempt after the interval", func() bool { return full.calls.Load() == 2 })
	stop()
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
