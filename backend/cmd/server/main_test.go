package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/auth"
	"github.com/yusufceng/Kubeat/backend/internal/config"
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
