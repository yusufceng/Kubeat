package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/auth"
	"github.com/yusufceng/Kubeat/backend/internal/config"
	"github.com/yusufceng/Kubeat/backend/internal/scheduler"
	"github.com/yusufceng/Kubeat/backend/internal/secretbox"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

const testKey = "0123456789abcdef0123456789abcdef-main"

func openDB(t *testing.T, path string) *store.SQLite {
	t.Helper()
	db, err := store.OpenSQLite(context.Background(), path, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// secretMonitor is an HTTP monitor with a token and a request body.
func secretMonitor(name, token, body string) store.Monitor {
	return store.Monitor{
		Name: name, Type: store.MonitorHTTP, Target: "https://example.com/" + name, Interval: time.Minute, Timeout: 5 * time.Second,
		FailureThreshold: 1, Enabled: true,
		HTTP: store.HTTPOptions{Method: "POST", Auth: store.HTTPAuthBearer, AuthSecret: token, Body: body},
	}
}

// Without a key the server refuses to start, unless told to store the
// secrets unencrypted.
func TestProtectSecretsWithoutKey(t *testing.T) {
	ctx := context.Background()
	db := openDB(t, filepath.Join(t.TempDir(), "uptime.db"))
	err := protectSecrets(ctx, db, "", false, quiet())
	if err == nil || !strings.Contains(err.Error(), "UPTIME_ALLOW_PLAINTEXT_SECRETS") {
		t.Errorf("without a key: err = %v, want a refusal that names the way out", err)
	}
	var logs bytes.Buffer
	if err := protectSecrets(ctx, db, "", true, slog.New(slog.NewTextHandler(&logs, nil))); err != nil {
		t.Errorf("with plaintext allowed: %v", err)
	}
	if !strings.Contains(logs.String(), "level=WARN") {
		t.Errorf("storing secrets unencrypted is not warned about: %q", logs.String())
	}
	// Allowing plaintext does not let a database with encrypted secrets
	// start without its key.
	box, _ := secretbox.New(testKey)
	db.UseSecrets(box)
	if _, err := db.CreateMonitor(ctx, secretMonitor("sealed", "t0ken", "")); err != nil {
		t.Fatal(err)
	}
	if err := protectSecrets(ctx, db, "", true, quiet()); err == nil {
		t.Error("a database with encrypted secrets started without the key")
	}
}

// A database written without a key is encrypted when the key comes, and
// compacted once, so that no plain copy is left in its files.
func TestProtectSecretsEncryptsAndCompacts(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "uptime.db")
	db := openDB(t, path)
	if _, err := db.CreateMonitor(ctx, secretMonitor("api", "t0ken-in-the-clear", `{"password":"body-in-the-clear"}`)); err != nil {
		t.Fatal(err)
	}

	var logs bytes.Buffer
	if err := protectSecrets(ctx, db, testKey, false, slog.New(slog.NewTextHandler(&logs, nil))); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "compacting") {
		t.Errorf("the database was not compacted: %q", logs.String())
	}
	for _, file := range []string{path, path + "-wal"} {
		raw, _ := os.ReadFile(file)
		for _, secret := range []string{"t0ken-in-the-clear", "body-in-the-clear"} {
			if bytes.Contains(raw, []byte(secret)) {
				t.Errorf("%s still holds %q", filepath.Base(file), secret)
			}
		}
	}
	if counts, _ := db.CountSecrets(ctx); counts.Plain != 0 || counts.Sealed != 2 {
		t.Errorf("counts = %+v, want 2 sealed", counts)
	}

	// The next start has nothing to do.
	logs.Reset()
	if err := protectSecrets(ctx, db, testKey, false, slog.New(slog.NewTextHandler(&logs, nil))); err != nil || logs.Len() != 0 {
		t.Errorf("second start: %v, logged %q", err, logs.String())
	}
}

// Every byte of the key counts: the same text with a trailing newline is
// another key, and the refusal says that this may be why.
func TestProtectSecretsTakesTheKeyAsItIs(t *testing.T) {
	ctx := context.Background()
	db := openDB(t, filepath.Join(t.TempDir(), "uptime.db"))
	if err := protectSecrets(ctx, db, testKey, false, quiet()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateMonitor(ctx, secretMonitor("api", "t0ken", "")); err != nil {
		t.Fatal(err)
	}
	err := protectSecrets(ctx, db, testKey+"\n", false, quiet())
	if !errors.Is(err, secretbox.ErrWrongKey) || !strings.Contains(err.Error(), "trailing newline") {
		t.Errorf("the key with a newline: err = %v, want the wrong key, with the newline named as a cause", err)
	}
	if err := protectSecrets(ctx, db, testKey, false, quiet()); err != nil {
		t.Errorf("the key as it was: %v", err)
	}
}

// A compaction that does not finish, here because a reader holds on to the
// write-ahead log, is tried again at every start, with a warning, until it
// is done; the start after that has nothing to do.
func TestProtectSecretsCompactsUntilDone(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "uptime.db")
	db, err := store.OpenSQLite(ctx, path, 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.CreateMonitor(ctx, secretMonitor("api", "t0ken-in-the-clear", `{"password":"body-in-the-clear"}`)); err != nil {
		t.Fatal(err)
	}
	reader, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	tx, err := reader.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM monitors").Scan(&n); err != nil {
		t.Fatal(err)
	}

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	if err := protectSecrets(ctx, db, testKey, false, logger); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "could not be compacted") {
		t.Fatalf("the first start's compaction did not fail as the test needs: %q", logs.String())
	}
	tx.Rollback()

	logs.Reset()
	if err := protectSecrets(ctx, db, testKey, false, logger); err != nil {
		t.Fatal(err)
	}
	if got := logs.String(); !strings.Contains(got, "level=WARN msg=\"an earlier start did not finish compacting") ||
		!strings.Contains(got, "compacting the database") || strings.Contains(got, "could not be compacted") {
		t.Errorf("the next start: %q, want a warning and a compaction that succeeds", got)
	}
	for _, file := range []string{path, path + "-wal"} {
		raw, _ := os.ReadFile(file)
		for _, secret := range []string{"t0ken-in-the-clear", "body-in-the-clear"} {
			if bytes.Contains(raw, []byte(secret)) {
				t.Errorf("%s still holds %q", filepath.Base(file), secret)
			}
		}
	}

	logs.Reset()
	if err := protectSecrets(ctx, db, testKey, false, logger); err != nil || logs.Len() != 0 {
		t.Errorf("the start after: %v, logged %q", err, logs.String())
	}
}

// Starts that keep not finishing the compaction, as when a probe cuts each
// of them short because it takes longer than a start is given, do not keep
// the backend down: after maxCompactionAttempts of them, a start skips it
// and comes up, with an error that says what to do. The next start tries
// once more, and if that one does not finish either, the one after skips
// again: a restart costs at most one start cut short.
func TestProtectSecretsSkipsACompactionThatNeverFinishes(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "uptime.db")
	db, err := store.OpenSQLite(ctx, path, 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.CreateMonitor(ctx, secretMonitor("api", "t0ken-in-the-clear", `{"password":"body-in-the-clear"}`)); err != nil {
		t.Fatal(err)
	}
	// A reader holding on to the write-ahead log keeps every compaction
	// from finishing.
	reader, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	tx, err := reader.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM monitors").Scan(&n); err != nil {
		t.Fatal(err)
	}

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	for i := range maxCompactionAttempts {
		logs.Reset()
		if err := protectSecrets(ctx, db, testKey, false, logger); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(logs.String(), "could not be compacted") {
			t.Fatalf("start %d: the compaction did not fail as the test needs: %q", i+1, logs.String())
		}
		if owed, _, _ := db.CompactionPending(ctx); owed.Attempts != i+1 {
			t.Errorf("after start %d, %d attempts are counted, want %d", i+1, owed.Attempts, i+1)
		}
	}

	logs.Reset()
	if err := protectSecrets(ctx, db, testKey, false, logger); err != nil {
		t.Fatalf("the start after %d attempts: %v", maxCompactionAttempts, err)
	}
	if got := logs.String(); !strings.Contains(got, "level=ERROR") || !strings.Contains(got, "backend.startupProbe.failureThreshold") ||
		!strings.Contains(got, "VACUUM") || strings.Contains(got, "compacting now") || strings.Contains(got, "could not be compacted") {
		t.Errorf("the start after %d attempts logged %q, want it to skip the compaction with an error that names the remedies", maxCompactionAttempts, got)
	}
	if owed, pending, _ := db.CompactionPending(ctx); !pending || owed.Attempts != maxCompactionAttempts-1 {
		t.Errorf("after the skip: owed %v, %d attempts; want still owed, with one attempt left", pending, owed.Attempts)
	}

	// The next start tries once more; cut short again, the one after
	// skips again.
	logs.Reset()
	if err := protectSecrets(ctx, db, testKey, false, logger); err != nil {
		t.Fatal(err)
	}
	if got := logs.String(); !strings.Contains(got, "could not be compacted") {
		t.Errorf("the start after the skip logged %q, want one more attempt", got)
	}
	logs.Reset()
	if err := protectSecrets(ctx, db, testKey, false, logger); err != nil {
		t.Fatal(err)
	}
	if got := logs.String(); !strings.Contains(got, "level=ERROR") || strings.Contains(got, "could not be compacted") {
		t.Errorf("the start after a failed retry logged %q, want it to skip again", got)
	}

	// Given what it needs, the next start compacts.
	tx.Rollback()
	logs.Reset()
	if err := protectSecrets(ctx, db, testKey, false, logger); err != nil {
		t.Fatal(err)
	}
	if got := logs.String(); !strings.Contains(got, "compacting now") || strings.Contains(got, "could not be compacted") {
		t.Errorf("the start after the skip logged %q, want a compaction that succeeds", got)
	}
	if _, pending, _ := db.CompactionPending(ctx); pending {
		t.Error("the compaction is still owed after it was done")
	}
	for _, file := range []string{path, path + "-wal"} {
		raw, _ := os.ReadFile(file)
		for _, secret := range []string{"t0ken-in-the-clear", "body-in-the-clear"} {
			if bytes.Contains(raw, []byte(secret)) {
				t.Errorf("%s still holds %q", filepath.Base(file), secret)
			}
		}
	}
}

// A database that had the key, with a value only now counted among the
// secrets (a request body), is encrypted without being compacted.
func TestProtectSecretsNewSecretField(t *testing.T) {
	ctx := context.Background()
	db := openDB(t, filepath.Join(t.TempDir(), "uptime.db"))
	box, _ := secretbox.New(testKey)
	db.UseSecrets(box)
	db.CreateMonitor(ctx, secretMonitor("sealed", "t0ken", ""))
	// What an older version wrote: the body as it is.
	db.UseSecrets(nil)
	db.CreateMonitor(ctx, secretMonitor("older", "", `{"query":"{ me }"}`))

	var logs bytes.Buffer
	if err := protectSecrets(ctx, db, testKey, false, slog.New(slog.NewTextHandler(&logs, nil))); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logs.String(), "compacting") {
		t.Error("a database that had the key was compacted")
	}
	if counts, _ := db.CountSecrets(ctx); counts.Plain != 0 || counts.Sealed != 2 {
		t.Errorf("counts = %+v, want 2 sealed", counts)
	}
	if m, err := db.GetMonitor(ctx, 2); err != nil || m.HTTP.Body != `{"query":"{ me }"}` {
		t.Errorf("the older monitor's body = %q, %v", m.HTTP.Body, err)
	}
}

// UPTIME_ADMIN_PASSWORD is held to the password rules when, and only when,
// it is used: to create the first account or to reset admin's password.
func TestBootstrapAdmin(t *testing.T) {
	ctx := context.Background()
	db := openDB(t, filepath.Join(t.TempDir(), "uptime.db"))
	cfg := config.Config{PasswordMinLength: 8}

	for _, weak := range []string{"admin", "ADMINISTRATOR", "password", "kisa"} {
		cfg.AdminPassword = weak
		if err := bootstrapAdmin(ctx, db, cfg, quiet()); err == nil || !strings.HasPrefix(err.Error(), "UPTIME_ADMIN_PASSWORD: ") {
			t.Errorf("first account with %q: err = %v", weak, err)
		}
	}
	if n, _ := db.CountUsers(ctx); n != 0 {
		t.Fatalf("a weak password created %d accounts", n)
	}

	cfg.AdminPassword = "uzun-ve-rastgele-1"
	if err := bootstrapAdmin(ctx, db, cfg, quiet()); err != nil {
		t.Fatal(err)
	}
	admin, err := db.GetUserByUsername(ctx, "admin")
	if err != nil || admin.Role != store.RoleAdmin || !auth.CheckPassword(admin.PasswordHash, "uzun-ve-rastgele-1") {
		t.Fatalf("admin = %+v, %v", admin, err)
	}

	// With an account there, the value is not used and not judged.
	cfg.AdminPassword = "admin"
	if err := bootstrapAdmin(ctx, db, cfg, quiet()); err != nil {
		t.Errorf("an unused weak password stopped the start: %v", err)
	}
	// A reset uses it: a weak one stops the start and changes nothing.
	cfg.AdminPasswordReset = true
	if err := bootstrapAdmin(ctx, db, cfg, quiet()); err == nil || !strings.HasPrefix(err.Error(), "UPTIME_ADMIN_PASSWORD: ") {
		t.Errorf("reset to a weak password: err = %v", err)
	}
	if admin, _ := db.GetUserByUsername(ctx, "admin"); !auth.CheckPassword(admin.PasswordHash, "uzun-ve-rastgele-1") {
		t.Error("a refused reset changed the password")
	}
	cfg.AdminPassword = "baska-uzun-parola-2"
	if err := bootstrapAdmin(ctx, db, cfg, quiet()); err != nil {
		t.Fatal(err)
	}
	if admin, _ := db.GetUserByUsername(ctx, "admin"); !auth.CheckPassword(admin.PasswordHash, "baska-uzun-parola-2") {
		t.Error("the reset did not set the password")
	}
}

// Settings with which SSO cannot work turn SSO off, with the reason logged
// as an error and kept for the administrators; they never stop the server.
func TestSetUpOIDC(t *testing.T) {
	if oidc, problem := setUpOIDC(config.Config{}, quiet()); oidc != nil || problem != "" {
		t.Errorf("no issuer: %v %q, want neither", oidc, problem)
	}
	good := config.Config{OIDCIssuer: "https://sso.example.com/realms/corp", OIDCClientID: "kubeat",
		OIDCRedirectURL: "https://kubeat.example/auth/callback", OIDCTimeout: time.Second}
	if oidc, problem := setUpOIDC(good, quiet()); oidc == nil || problem != "" {
		t.Errorf("usable settings: %v %q, want a client", oidc, problem)
	}

	found := good
	found.OIDCProblem = errors.New(`UPTIME_OIDC_REDIRECT_URL: "http://kubeat.example/auth/callback" must use https`)
	unreadableCA := good
	unreadableCA.OIDCCAFile = filepath.Join(t.TempDir(), "missing.crt")
	for name, cfg := range map[string]config.Config{"a problem Load found": found, "a CA file that cannot be read": unreadableCA} {
		var logs bytes.Buffer
		oidc, problem := setUpOIDC(cfg, slog.New(slog.NewTextHandler(&logs, nil)))
		if oidc != nil || problem == "" {
			t.Errorf("%s: %v %q, want SSO off with the reason", name, oidc, problem)
		}
		if !strings.Contains(logs.String(), "level=ERROR") || !strings.Contains(logs.String(), "SSO is off") {
			t.Errorf("%s: not logged as an error: %q", name, logs.String())
		}
	}
}

// More enabled monitors than the delivery queue holds is said once, with
// both numbers: at start if there are already, else the first time a tick
// finds them past it. Ticks at or under the size say nothing.
func TestQueueWarning(t *testing.T) {
	warnings := func(counts ...int) []string {
		var logs bytes.Buffer
		check := queueWarning(1000, slog.New(slog.NewTextHandler(&logs, nil)))
		for _, n := range counts {
			check(n)
		}
		return strings.Split(strings.TrimSpace(logs.String()), "\n")
	}
	for _, tt := range []struct {
		name    string
		counts  []int
		enabled string // in the one warning; empty for none
	}{
		{"never past the queue", []int{3, 999, 1000, 1000}, ""},
		{"past it at start", []int{1200, 1200, 1300, 900, 1250}, "enabled_monitors=1200"},
		{"past it later", []int{998, 1000, 1001, 1002, 1000, 1003}, "enabled_monitors=1001"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			lines := warnings(tt.counts...)
			if tt.enabled == "" {
				if lines[0] != "" {
					t.Errorf("logged %q, want nothing", lines)
				}
				return
			}
			if len(lines) != 1 || !strings.Contains(lines[0], "level=WARN") || !strings.Contains(lines[0], tt.enabled) ||
				!strings.Contains(lines[0], "notify_queue_size=1000") || !strings.Contains(lines[0], "UPTIME_NOTIFY_QUEUE_SIZE") {
				t.Errorf("logged %q, want one WARN with %s and notify_queue_size=1000", lines, tt.enabled)
			}
		})
	}
}

// selfHealthFailing is how the log starts the line saying Kubeat stopped
// doing its job, up to the reason.
const selfHealthFailing = `level=ERROR msg="self-health failing: checks are not running, or their results are not stored" reason="`

// Whether Kubeat is doing its job is logged when the answer changes, and
// only then: an error with the reason when it stops, a line when it does
// its job again, nothing for an answer that stays. A hang, which logs
// nothing else, is said once, not at every look.
func TestWatchHealth(t *testing.T) {
	hung := errors.New("the writer has been busy with one batch of check results for 1m5s")
	stalled := errors.New("the scheduler has not completed a tick for 1m10s")
	failing := func(problem error) string { return selfHealthFailing + problem.Error() + `"` }
	const restored = `level=INFO msg="self-health restored"`
	for _, tt := range []struct {
		name    string
		answers []error // one a look, in order
		want    []string
	}{
		{"healthy throughout", []error{nil, nil, nil}, nil},
		{"a write that hangs, then ends", []error{nil, hung, hung, hung, nil, nil}, []string{failing(hung), restored}},
		{"failing from the first look, and again later", []error{stalled, stalled, nil, nil, hung, hung, nil},
			[]string{failing(stalled), restored, failing(hung), restored}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			// healthy gives the answers one look at a time, then keeps
			// giving the last one; only the watch calls it.
			looked := make(chan struct{})
			n := 0
			healthy := func() error {
				answer := tt.answers[min(n, len(tt.answers)-1)]
				if n++; n == len(tt.answers) {
					close(looked)
				}
				return answer
			}
			done := make(chan struct{})
			go func() {
				defer close(done)
				watchHealth(ctx, healthy, time.Millisecond, slog.New(slog.NewTextHandler(&logs, nil)))
			}()
			select {
			case <-looked:
			case <-time.After(5 * time.Second):
				t.Fatal("the watch did not look often enough")
			}
			cancel()
			<-done

			var got []string
			if out := strings.TrimSpace(logs.String()); out != "" {
				got = strings.Split(out, "\n")
			}
			if len(got) != len(tt.want) {
				t.Fatalf("logged %d lines, want %d:\n%s", len(got), len(tt.want), logs.String())
			}
			for i, want := range tt.want {
				if !strings.Contains(got[i], want) {
					t.Errorf("line %d = %q, want it to hold %q", i+1, got[i], want)
				}
			}
		})
	}
}

// hungStore holds the scheduler until release is closed, as a hung volume
// does: in its reads of the monitors from the second tick on (list), or in
// its writes of results (write).
type hungStore struct {
	list, write bool
	release     chan struct{}
	reads       atomic.Int32
}

func (h *hungStore) ListMonitors(context.Context) ([]store.Monitor, error) {
	if h.list && h.reads.Add(1) > 1 {
		<-h.release
	}
	return []store.Monitor{{ID: 1, Type: store.MonitorHTTP, Interval: 5 * time.Millisecond, Timeout: time.Second, Enabled: true}}, nil
}

func (h *hungStore) InsertCheckResults(context.Context, []store.CheckResult) error {
	if h.write {
		<-h.release
	}
	return nil
}

// upChecker finds every monitor up, at once.
type upChecker struct{}

func (upChecker) Check(_ context.Context, m store.Monitor) store.CheckResult {
	return store.CheckResult{MonitorID: m.ID, Up: true}
}

// syncBuffer is a log one goroutine writes while a test reads it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// Without a heartbeat, a hang reaches the log from the scheduler's own
// judgement, with its reason, and so does its end: a read of the monitors
// or a write of results that does not return logs nothing else.
func TestWatchHealthLogsAHangWithoutAHeartbeat(t *testing.T) {
	const window = 400 * time.Millisecond
	for _, tt := range []struct {
		name   string
		st     *hungStore
		reason string
	}{
		{"a read of the monitors hangs", &hungStore{list: true, release: make(chan struct{})}, "the scheduler has not completed a tick for "},
		{"a write of results hangs", &hungStore{write: true, release: make(chan struct{})}, "the writer has been busy with one batch of check results for "},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sched := scheduler.New(tt.st, upChecker{}, scheduler.Options{
				Tick: 5 * time.Millisecond, Workers: 1, BatchSize: 1, FlushInterval: 5 * time.Millisecond, Logger: quiet(),
			})
			logs := &syncBuffer{}
			ctx, cancel := context.WithCancel(context.Background())
			var running sync.WaitGroup
			running.Add(2)
			go func() {
				defer running.Done()
				sched.Run(ctx)
			}()
			go func() {
				defer running.Done()
				watchHealth(ctx, func() error { return sched.Healthy(window) }, window/4, slog.New(slog.NewTextHandler(logs, nil)))
			}()
			release := sync.OnceFunc(func() { close(tt.st.release) })
			defer func() {
				release()
				cancel()
				running.Wait()
			}()

			failing := selfHealthFailing + tt.reason
			waitFor(t, "the hang to be logged", func() bool { return strings.Contains(logs.String(), failing) })
			release()
			waitFor(t, "its end to be logged", func() bool {
				out := logs.String()
				return strings.Contains(out[strings.Index(out, failing):], `level=INFO msg="self-health restored"`)
			})
		})
	}
}

// TestMain makes password hashes cheap: these tests sign in and set up
// accounts hundreds of times, and at the real cost they time out under the
// race detector on a small CI runner.
func TestMain(m *testing.M) {
	auth.FastHashes()
	os.Exit(m.Run())
}
