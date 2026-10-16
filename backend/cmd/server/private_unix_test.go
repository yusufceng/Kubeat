//go:build unix

package main

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// The database, its journal and its backups are made readable by the
// server's own user only.
func TestPrivateFiles(t *testing.T) {
	previous := syscall.Umask(0o022)
	t.Cleanup(func() { syscall.Umask(previous) })
	privateFiles()

	dir := t.TempDir()
	path := filepath.Join(dir, "uptime.db")
	db := openDB(t, path)
	if _, err := db.CreateMonitor(context.Background(), secretMonitor("api", "t0ken", "")); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(dir, "backup.db")
	if err := db.Backup(context.Background(), backup); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{path, path + "-wal", path + "-shm", backup} {
		info, err := os.Stat(file)
		if err != nil {
			t.Fatal(err)
		}
		if mode := info.Mode().Perm(); mode != 0o600 {
			t.Errorf("%s has mode %o, want 600", filepath.Base(file), mode)
		}
	}
}
