// Package backup keeps periodic copies of the database in a directory, so
// that a corrupted or wrongly edited database can be put back. The directory
// should be on a different volume than the database: a copy on the same
// volume is lost together with it.
package backup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/metrics"
)

const (
	prefix = "kubeat-"
	suffix = ".db"
	// stamp orders lexically by time, so file names sort chronologically.
	stamp = "20060102T150405Z"
)

// Store is the part of the persistence layer backups need.
type Store interface {
	// Backup copies the database to dest. A copy that ran out of space
	// reports an error that wraps syscall.ENOSPC.
	Backup(ctx context.Context, dest string) error
}

// Options configures Run.
type Options struct {
	Dir      string
	Interval time.Duration
	// Keep is how many backups to retain; older ones are deleted.
	Keep int
	// Verify checks a finished backup file; an error discards it.
	Verify  func(ctx context.Context, path string) error
	Metrics *metrics.Metrics
	Logger  *slog.Logger
}

// Run takes a backup whenever the newest one is older than Interval, until
// ctx is cancelled. Restarting the process therefore does not produce an
// extra backup each time.
func Run(ctx context.Context, st Store, opts Options) {
	if err := os.MkdirAll(opts.Dir, 0o750); err != nil {
		opts.Logger.Error("backup directory unusable, backups are off", "dir", opts.Dir, "error", err)
		opts.Metrics.Backup(false, time.Now())
		return
	}

	for {
		wait := opts.Interval
		if last, ok := newest(opts.Dir); ok {
			wait = time.Until(last.Add(opts.Interval))
		} else {
			wait = 0
		}
		if wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}

		now := time.Now().UTC()
		path, err := Take(ctx, st, opts, now)
		if ctx.Err() != nil {
			return
		}
		opts.Metrics.Backup(err == nil, now)
		if err != nil {
			opts.Logger.Error("backup failed", "error", err)
			// Do not spin on a persistent failure.
			select {
			case <-ctx.Done():
				return
			case <-time.After(min(opts.Interval, time.Minute)):
			}
			continue
		}
		opts.Logger.Info("backup written", "path", path)
	}
}

// Take writes one backup named after now, verifies it and leaves the newest
// opts.Keep copies in the directory. Room is made before the write, not
// after it: the copies beyond keep-1 go first, so the new one fits where
// the oldest stood and a full volume does not stop the backups for good.
// Should the write still run out of space, the oldest copy is removed and
// the write tried once more. With keep=1 the only copy is not given up in
// advance: the write is tried first, and only a full volume costs the copy
// before the retry, which leaves a moment without a backup. Both are
// warned about in the log, as the volume is too small for the database.
// It returns the backup's path.
func Take(ctx context.Context, st Store, opts Options, now time.Time) (string, error) {
	final := filepath.Join(opts.Dir, prefix+now.UTC().Format(stamp)+suffix)
	// Written under a temporary name so that a half-written file is never
	// mistaken for a backup.
	partial := final + ".partial"
	_ = os.Remove(partial)

	if opts.Keep >= 2 {
		if err := prune(opts.Dir, opts.Keep-1); err != nil {
			opts.Logger.Warn("removing old backups failed", "error", err)
		}
	}

	err := write(ctx, st, partial)
	if errors.Is(err, syscall.ENOSPC) && makeRoom(opts.Dir, opts.Logger) {
		err = write(ctx, st, partial)
	}
	if err != nil {
		return "", err
	}

	if opts.Verify != nil {
		if err := opts.Verify(ctx, partial); err != nil {
			_ = os.Remove(partial)
			return "", fmt.Errorf("backup did not verify: %w", err)
		}
	}
	if err := os.Rename(partial, final); err != nil {
		_ = os.Remove(partial)
		return "", err
	}
	if err := prune(opts.Dir, opts.Keep); err != nil {
		opts.Logger.Warn("removing old backups failed", "error", err)
	}
	return final, nil
}

// write has the store copy the database to partial. A failed copy leaves no
// file behind, so that what it wrote does not take up the room a retry
// needs.
func write(ctx context.Context, st Store, partial string) error {
	err := st.Backup(ctx, partial)
	if err != nil {
		_ = os.Remove(partial)
	}
	return err
}

// makeRoom deletes the oldest backup in dir so that a copy that ran out of
// space can be tried once more. It reports whether a copy was removed; the
// warning names it and says when it was the only one, as there is no
// backup then until a write succeeds.
func makeRoom(dir string, logger *slog.Logger) bool {
	names, err := list(dir)
	if err != nil || len(names) == 0 {
		return false
	}
	oldest := names[0]
	if err := os.Remove(filepath.Join(dir, oldest)); err != nil {
		logger.Warn("backup volume full and the oldest copy could not be removed", "error", err)
		return false
	}
	if len(names) == 1 {
		logger.Warn("backup volume full; removed the only copy and retried, there is no backup until a write succeeds", "removed", oldest)
	} else {
		logger.Warn("backup volume full; removed the oldest copy and retried", "removed", oldest, "left", len(names)-1)
	}
	return true
}

// list returns the backup file names in dir, oldest first.
func list(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		if e.Type().IsRegular() && strings.HasPrefix(name, prefix) && strings.HasSuffix(name, suffix) {
			if _, err := time.Parse(stamp, strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix)); err == nil {
				names = append(names, name)
			}
		}
	}
	sort.Strings(names)
	return names, nil
}

// newest returns when the latest backup in dir was taken.
func newest(dir string) (time.Time, bool) {
	names, err := list(dir)
	if err != nil || len(names) == 0 {
		return time.Time{}, false
	}
	last := names[len(names)-1]
	t, _ := time.Parse(stamp, strings.TrimSuffix(strings.TrimPrefix(last, prefix), suffix))
	return t, true
}

// prune deletes all but the keep newest backups. Only files this package
// named are touched.
func prune(dir string, keep int) error {
	names, err := list(dir)
	if err != nil {
		return err
	}
	for i := 0; i < len(names)-keep; i++ {
		if err := os.Remove(filepath.Join(dir, names[i])); err != nil {
			return err
		}
	}
	return nil
}
