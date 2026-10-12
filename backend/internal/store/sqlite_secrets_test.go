package store

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/secretbox"
)

const testKey = "0123456789abcdef0123456789abcdef-store"

// everything returns all the text the two tables with secrets hold.
func everything(t *testing.T, s *SQLite) string {
	t.Helper()
	var all strings.Builder
	for _, query := range []string{"SELECT settings FROM monitors", "SELECT target || ' ' || secret FROM notification_channels"} {
		rows, err := s.read.QueryContext(context.Background(), query)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var text string
			if err := rows.Scan(&text); err != nil {
				t.Fatal(err)
			}
			all.WriteString(text + "\n")
		}
		rows.Close()
	}
	return all.String()
}

func secretMonitors() []Monitor {
	api := newMonitor("api")
	api.HTTP = HTTPOptions{Auth: HTTPAuthBearer, AuthSecret: "t0ken-in-the-clear", Headers: map[string]string{"X-Api-Key": "header-in-the-clear"}}
	db := newMonitor("db")
	db.Type, db.Target = MonitorPostgres, "db:5432"
	db.Database = DatabaseOptions{Username: "probe", Password: "password-in-the-clear", Database: "shop"}
	return []Monitor{api, db, newMonitor("plain")}
}

var secretValues = []string{"t0ken-in-the-clear", "header-in-the-clear", "password-in-the-clear", "hooks.example.com/s3cret-path", "bot-token-in-the-clear"}

func TestSecretsAreEncryptedAtRest(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	box, err := secretbox.New(testKey)
	if err != nil {
		t.Fatal(err)
	}
	s.UseSecrets(box)

	var created []Monitor
	for _, m := range secretMonitors() {
		c, err := s.CreateMonitor(ctx, m)
		if err != nil {
			t.Fatal(err)
		}
		// What the caller gets back, and what it passed in, stay readable.
		if !reflect.DeepEqual(c.HTTP, m.HTTP) && m.Type == MonitorHTTP && m.HTTP.AuthSecret != "" {
			if c.HTTP.AuthSecret != m.HTTP.AuthSecret || c.HTTP.Headers["X-Api-Key"] != "header-in-the-clear" {
				t.Errorf("created %s = %+v", m.Name, c.HTTP)
			}
		}
		created = append(created, c)
	}
	hook, _ := s.CreateChannel(ctx, Channel{Name: "hook", Type: ChannelWebhook, Target: "https://hooks.example.com/s3cret-path", Enabled: true})
	bot, _ := s.CreateChannel(ctx, Channel{Name: "bot", Type: ChannelTelegram, Target: "-100", Secret: "bot-token-in-the-clear", Enabled: true})

	// Nothing in the file reads as a secret; names of headers and users do.
	raw := everything(t, s)
	for _, secret := range secretValues {
		if strings.Contains(raw, secret) {
			t.Errorf("the database contains %q in the clear", secret)
		}
	}
	if !strings.Contains(raw, "X-Api-Key") || !strings.Contains(raw, `"username":"probe"`) {
		t.Errorf("more than the secrets was encrypted:\n%s", raw)
	}

	// Every way of reading gives the secrets back.
	api, _ := s.GetMonitor(ctx, created[0].ID)
	if api.HTTP.AuthSecret != "t0ken-in-the-clear" || api.HTTP.Headers["X-Api-Key"] != "header-in-the-clear" {
		t.Errorf("GetMonitor = %+v", api.HTTP)
	}
	if list, _ := s.ListMonitors(ctx); list[1].Database.Password != "password-in-the-clear" {
		t.Errorf("ListMonitors = %+v", list[1].Database)
	}
	if st, _ := s.GetMonitorStatus(ctx, created[1].ID, time.Now()); st.Database.Password != "password-in-the-clear" {
		t.Errorf("status = %+v", st.Database)
	}
	if got, _ := s.GetChannel(ctx, hook.ID); got.Target != "https://hooks.example.com/s3cret-path" {
		t.Errorf("channel target = %q", got.Target)
	}
	if list, _ := s.ListChannels(ctx); list[1].Secret != "bot-token-in-the-clear" || list[1].Target != "-100" {
		t.Errorf("ListChannels = %+v", list)
	}
	// An update stores the new value encrypted too.
	api.HTTP.AuthSecret = "rotated-in-the-clear"
	if updated, err := s.UpdateMonitor(ctx, api); err != nil || updated.HTTP.AuthSecret != "rotated-in-the-clear" {
		t.Errorf("update = %+v, %v", updated.HTTP, err)
	}
	bot.Secret = "rotated-bot-in-the-clear"
	if updated, err := s.UpdateChannel(ctx, bot); err != nil || updated.Secret != "rotated-bot-in-the-clear" {
		t.Errorf("channel update = %+v, %v", updated, err)
	}
	if raw := everything(t, s); strings.Contains(raw, "rotated") {
		t.Error("an updated secret is stored in the clear")
	}

	counts, err := s.CountSecrets(ctx)
	if err != nil || counts.Plain != 0 || counts.Sealed != 6 {
		t.Errorf("counts = %+v, %v; want 6 sealed (token, header, password, two targets, one channel secret)", counts, err)
	}

	// Without the key, or with another, the secrets cannot be read, and the
	// failure says so instead of handing out ciphertext.
	s.UseSecrets(nil)
	if _, err := s.GetMonitor(ctx, created[0].ID); !errors.Is(err, secretbox.ErrNoKey) {
		t.Errorf("without a key: err = %v, want ErrNoKey", err)
	}
	if _, err := s.ListChannels(ctx); !errors.Is(err, secretbox.ErrNoKey) {
		t.Errorf("channels without a key: err = %v, want ErrNoKey", err)
	}
	if plain, err := s.GetMonitor(ctx, created[2].ID); err != nil || plain.Name != "plain" {
		t.Errorf("a monitor without secrets needs no key: %+v, %v", plain, err)
	}
	other, _ := secretbox.New(strings.Repeat("k", secretbox.MinKeyLength))
	s.UseSecrets(other)
	if _, err := s.GetMonitor(ctx, created[0].ID); err == nil || !strings.Contains(err.Error(), "key") {
		t.Errorf("with another key: err = %v", err)
	}
}

// A database written without a key is encrypted once a key is configured,
// and nothing changes for those who read it.
func TestEncryptingAnExistingDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "plain.db")
	s, err := OpenSQLite(ctx, path, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, m := range secretMonitors() {
		if _, err := s.CreateMonitor(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	s.CreateChannel(ctx, Channel{Name: "hook", Type: ChannelWebhook, Target: "https://hooks.example.com/s3cret-path", Enabled: true})
	s.CreateChannel(ctx, Channel{Name: "bot", Type: ChannelTelegram, Target: "-100", Secret: "bot-token-in-the-clear", Enabled: true})

	before, _ := s.ListMonitors(ctx)
	if counts, _ := s.CountSecrets(ctx); counts.Sealed != 0 || counts.Plain != 6 {
		t.Fatalf("counts before = %+v", counts)
	}
	if !strings.Contains(everything(t, s), "password-in-the-clear") {
		t.Fatal("without a key the secrets should be stored as they are")
	}

	box, _ := secretbox.New(testKey)
	s.UseSecrets(box)
	// With the key configured the plain values are still readable.
	if m, err := s.GetMonitor(ctx, before[0].ID); err != nil || m.HTTP.AuthSecret != "t0ken-in-the-clear" {
		t.Fatalf("plain value with a key: %+v, %v", m.HTTP, err)
	}
	if err := s.EncryptSecrets(ctx); err != nil {
		t.Fatal(err)
	}
	if counts, _ := s.CountSecrets(ctx); counts.Plain != 0 || counts.Sealed != 6 {
		t.Errorf("counts after = %+v", counts)
	}
	raw := everything(t, s)
	for _, secret := range secretValues {
		if strings.Contains(raw, secret) {
			t.Errorf("after encrypting, the database still contains %q", secret)
		}
	}
	after, _ := s.ListMonitors(ctx)
	if !reflect.DeepEqual(before, after) {
		t.Errorf("monitors changed:\nbefore %+v\nafter  %+v", before, after)
	}
	if channels, _ := s.ListChannels(ctx); channels[0].Target != "https://hooks.example.com/s3cret-path" || channels[1].Secret != "bot-token-in-the-clear" {
		t.Errorf("channels after = %+v", channels)
	}
}
