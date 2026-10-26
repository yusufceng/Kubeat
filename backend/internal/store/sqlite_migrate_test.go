package store

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func recordedMigrations(t *testing.T, db *sql.DB) map[string]int64 {
	t.Helper()
	rows, err := db.Query("SELECT name, applied_at FROM schema_migrations")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]int64{}
	for rows.Next() {
		var name string
		var at int64
		if err := rows.Scan(&name, &at); err != nil {
			t.Fatal(err)
		}
		got[name] = at
	}
	return got
}

func userVersion(t *testing.T, db *sql.DB) int {
	t.Helper()
	var v int
	if err := db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

// A new database records every embedded migration by name.
func TestMigrationsRecordedByName(t *testing.T) {
	s := openTest(t)
	got := recordedMigrations(t, s.write)
	names, _ := fs.Glob(migrationFiles, "migrations/*.sql")
	if len(got) != len(names) {
		t.Fatalf("recorded %d migrations, want %d", len(got), len(names))
	}
	for _, name := range names {
		if got[path.Base(name)] == 0 {
			t.Errorf("%s not recorded with a time: %v", name, got)
		}
	}
	if v := userVersion(t, s.write); v != CurrentSchemaVersion() {
		t.Errorf("user_version = %d, want %d", v, CurrentSchemaVersion())
	}
}

// A database that only has the count in user_version, as every release
// before name tracking wrote it, gets its first that many files recorded
// and the rest applied.
func TestMigrationsSeededFromUserVersion(t *testing.T) {
	ctx := context.Background()
	p := filepath.Join(t.TempDir(), "old.db")
	old, err := openPool(p, time.Second, true)
	if err != nil {
		t.Fatal(err)
	}
	names, _ := fs.Glob(migrationFiles, "migrations/*.sql")
	sort.Strings(names)
	const before = 20
	for _, name := range names[:before] {
		body, _ := migrationFiles.ReadFile(name)
		if _, err := old.ExecContext(ctx, string(body)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if _, err := old.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", before)); err != nil {
		t.Fatal(err)
	}
	old.Close()

	s, err := OpenSQLite(ctx, p, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got := recordedMigrations(t, s.write)
	for i, name := range names {
		at, ok := got[path.Base(name)]
		if seeded := i < before; !ok || seeded != (at == 0) {
			t.Errorf("%s: recorded %v at %d; seeded %v", name, ok, at, seeded)
		}
	}
	if v := userVersion(t, s.write); v != len(names) {
		t.Errorf("user_version = %d, want %d", v, len(names))
	}
}

func migrationSet(names ...string) fstest.MapFS {
	files := fstest.MapFS{}
	for _, name := range names {
		table := strings.TrimSuffix(name, ".sql")[5:]
		files["migrations/"+name] = &fstest.MapFile{Data: []byte("CREATE TABLE " + table + " (id INTEGER);")}
	}
	return files
}

func openBare(t *testing.T) *sql.DB {
	t.Helper()
	db, err := openPool(filepath.Join(t.TempDir(), "bare.db"), time.Second, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// A migration that sorts below one already applied, as when two branches
// each add one and merge in either order, still runs.
func TestMigrationAddedBelowTheNewestRuns(t *testing.T) {
	ctx := context.Background()
	db := openBare(t)
	if err := migrateFrom(ctx, db, migrationSet("0001_a.sql", "0003_c.sql")); err != nil {
		t.Fatal(err)
	}
	if err := migrateFrom(ctx, db, migrationSet("0001_a.sql", "0002_b.sql", "0003_c.sql")); err != nil {
		t.Fatal(err)
	}
	got := recordedMigrations(t, db)
	names := make([]string, 0, len(got))
	for name := range got {
		names = append(names, name)
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"0001_a.sql", "0002_b.sql", "0003_c.sql"}) {
		t.Errorf("recorded %v", names)
	}
	if _, err := db.Exec("INSERT INTO b (id) VALUES (1)"); err != nil {
		t.Errorf("0002 did not run: %v", err)
	}
	if v := userVersion(t, db); v != 3 {
		t.Errorf("user_version = %d, want 3", v)
	}
	// Running again applies nothing twice.
	if err := migrateFrom(ctx, db, migrationSet("0001_a.sql", "0002_b.sql", "0003_c.sql")); err != nil {
		t.Errorf("rerun: %v", err)
	}
}

// A binary refuses a database that has a migration it does not know, even
// when it knows as many migrations as the database has.
func TestUnknownMigrationRefused(t *testing.T) {
	ctx := context.Background()
	db := openBare(t)
	if err := migrateFrom(ctx, db, migrationSet("0001_a.sql", "0002_b.sql")); err != nil {
		t.Fatal(err)
	}
	err := migrateFrom(ctx, db, migrationSet("0001_a.sql", "0002_x.sql"))
	if err == nil || !strings.Contains(err.Error(), "0002_b.sql") {
		t.Errorf("err = %v, want a refusal naming 0002_b.sql", err)
	}
}

// A count-only database newer than the binary is refused before anything is
// recorded, as it was before name tracking.
func TestNewerUserVersionRefused(t *testing.T) {
	ctx := context.Background()
	db := openBare(t)
	if _, err := db.Exec("PRAGMA user_version = 5"); err != nil {
		t.Fatal(err)
	}
	if err := migrateFrom(ctx, db, migrationSet("0001_a.sql")); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Errorf("err = %v, want a refusal", err)
	}
	if n := len(recordedMigrations(t, db)); n != 0 {
		t.Errorf("recorded %d migrations on a refused database", n)
	}
}

// A failed migration leaves neither its record nor its changes.
func TestFailedMigrationNotRecorded(t *testing.T) {
	ctx := context.Background()
	db := openBare(t)
	files := migrationSet("0001_a.sql")
	files["migrations/0002_bad.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE bad (id INTEGER); SELECT * FROM missing;")}
	if err := migrateFrom(ctx, db, files); err == nil {
		t.Fatal("bad migration succeeded")
	}
	got := recordedMigrations(t, db)
	if _, ok := got["0002_bad.sql"]; ok || len(got) != 1 {
		t.Errorf("recorded %v", got)
	}
	if v := userVersion(t, db); v != 1 {
		t.Errorf("user_version = %d, want 1", v)
	}
	if _, err := db.Exec("SELECT * FROM bad"); err == nil {
		t.Error("the failed migration's table exists")
	}
}
