package license

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Sign makes a license with key, as the offline issuer does.
func sign(t *testing.T, key ed25519.PrivateKey, c Claims) []byte {
	t.Helper()
	payload, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	signed := prefix + base64.RawURLEncoding.EncodeToString(payload)
	return []byte(signed + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, []byte(signed))) + "\n")
}

func testKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func TestParse(t *testing.T) {
	pub, priv := testKey(t)
	want := Claims{ID: "KL-1", Customer: "Acme", Features: []Feature{SSO, Audit},
		IssuedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), ExpiresAt: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)}
	data := sign(t, priv, want)
	got, err := Parse(data, pub)
	if err != nil || got.Customer != "Acme" || len(got.Features) != 2 || !got.ExpiresAt.Equal(want.ExpiresAt) {
		t.Fatalf("Parse = %+v, %v", got, err)
	}

	otherPub, _ := testKey(t)
	if _, err := Parse(data, otherPub); err == nil {
		t.Error("a license signed with another key was accepted")
	}
	// Changing a single claim breaks the signature.
	parts := strings.Split(strings.TrimSpace(string(data)), ".")
	forged, _ := json.Marshal(Claims{ID: "KL-1", Customer: "Acme", Features: Features, ExpiresAt: want.ExpiresAt.AddDate(10, 0, 0)})
	parts[1] = base64.RawURLEncoding.EncodeToString(forged)
	if _, err := Parse([]byte(strings.Join(parts, ".")), pub); err == nil {
		t.Error("a changed license was accepted")
	}
	for _, junk := range []string{"", "hello", prefix, prefix + "abc", prefix + "abc.def"} {
		if _, err := Parse([]byte(junk), pub); err == nil {
			t.Errorf("Parse(%q) succeeded", junk)
		}
	}
	if _, err := Parse(sign(t, priv, Claims{Customer: "x", Features: Features}), pub); err == nil {
		t.Error("a license without an end date was accepted")
	}
}

func TestManagerStates(t *testing.T) {
	pub, priv := testKey(t)
	path := filepath.Join(t.TempDir(), "license")
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	write := func(c Claims) {
		t.Helper()
		if err := os.WriteFile(path, sign(t, priv, c), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if m := NewManager("", pub); m.Status().State != Community || m.Allows(SSO) {
		t.Errorf("no license: %+v", m.Status())
	}
	var none *Manager
	if none.Allows(SSO) {
		t.Error("a nil manager allows a feature")
	}

	missing := NewManager(path, pub)
	if s := missing.Status(); s.State != Invalid || s.Problem == "" || missing.Allows(SSO) {
		t.Errorf("missing file: %+v", s)
	}
	if missing.Reload() {
		t.Error("an unchanged missing file was read again")
	}

	write(Claims{Customer: "Acme", Features: []Feature{SSO}, ExpiresAt: now.AddDate(1, 0, 0)})
	m := NewManager(path, pub)
	m.now = func() time.Time { return now }
	if s := m.Status(); s.State != Active || s.Customer != "Acme" || !m.Allows(SSO) || m.Allows(Audit) {
		t.Errorf("valid: %+v", s)
	}
	m.now = func() time.Time { return now.AddDate(1, 0, 0).Add(-WarnBefore) }
	if s := m.Status(); s.State != Expiring || !m.Allows(SSO) {
		t.Errorf("ending: %+v", s)
	}
	m.now = func() time.Time { return now.AddDate(1, 0, 0) }
	if s := m.Status(); s.State != Expired || m.Allows(SSO) {
		t.Errorf("ended: %+v", s)
	}

	// A renewed file is read again; an unchanged one is not.
	m.now = func() time.Time { return now }
	if m.Reload() {
		t.Error("an unchanged file was read again")
	}
	write(Claims{Customer: "Acme", Features: []Feature{SSO, Audit}, ExpiresAt: now.AddDate(2, 0, 0)})
	future := time.Now().Add(time.Minute)
	os.Chtimes(path, future, future)
	if !m.Reload() || !m.Allows(Audit) {
		t.Errorf("renewal not read: %+v", m.Status())
	}
	// A file replaced by junk turns everything off.
	os.WriteFile(path, []byte("junk junk junk junk junk junk"), 0o600)
	later := future.Add(time.Minute)
	os.Chtimes(path, later, later)
	if !m.Reload() || m.Allows(SSO) || m.Status().State != Invalid {
		t.Errorf("junk: %+v", m.Status())
	}
}

func TestPublicKey(t *testing.T) {
	if len(PublicKey) != ed25519.PublicKeySize {
		t.Fatal("the compiled-in key is not an Ed25519 public key")
	}
}
