package secretbox

import (
	"errors"
	"strings"
	"testing"
)

const key = "0123456789abcdef0123456789abcdef-test"

func TestSealAndOpen(t *testing.T) {
	box, err := New(key)
	if err != nil {
		t.Fatal(err)
	}
	for _, plain := range []string{"s3cret", "Bearer ey.şifre.🙂", strings.Repeat("x", 5000), "enc:looks-sealed-but-is-not"} {
		sealed, err := box.Seal(plain)
		if err != nil {
			t.Fatal(err)
		}
		if !Sealed(sealed) || strings.Contains(sealed, plain) {
			t.Errorf("Seal(%q) = %q", plain, sealed)
		}
		if got, err := box.Open(sealed); err != nil || got != plain {
			t.Errorf("Open(Seal(%q)) = %q, %v", plain, got, err)
		}
	}

	// The same value seals differently each time, so equal secrets do not show.
	a, _ := box.Seal("same")
	b, _ := box.Seal("same")
	if a == b {
		t.Error("sealing is deterministic")
	}
	// Nothing stays nothing, and a value from before the key is read as it is.
	if got, _ := box.Seal(""); got != "" {
		t.Errorf("Seal(\"\") = %q", got)
	}
	if got, err := box.Open("written before the key"); err != nil || got != "written before the key" {
		t.Errorf("Open(plain) = %q, %v", got, err)
	}
}

func TestWrongKeyAndDamage(t *testing.T) {
	box, _ := New(key)
	other, _ := New(strings.Repeat("z", MinKeyLength))
	sealed, _ := box.Seal("s3cret")

	if _, err := other.Open(sealed); err == nil || !strings.Contains(err.Error(), "key") {
		t.Errorf("another key: err = %v", err)
	}
	// A changed ciphertext is refused, not decrypted to something else.
	damaged := sealed[:len(sealed)-2] + "AA"
	if _, err := box.Open(damaged); err == nil {
		t.Error("a damaged value was opened")
	}
	if _, err := box.Open(prefix + "!!!"); err == nil {
		t.Error("a value that is not base64 was opened")
	}
	if _, err := New("too short"); err == nil {
		t.Error("a short key was accepted")
	}
}

func TestWithoutAKey(t *testing.T) {
	var box *Box
	if got, err := box.Seal("s3cret"); err != nil || got != "s3cret" {
		t.Errorf("Seal without a key = %q, %v", got, err)
	}
	if got, err := box.Open("plain"); err != nil || got != "plain" {
		t.Errorf("Open(plain) without a key = %q, %v", got, err)
	}
	real, _ := New(key)
	sealed, _ := real.Seal("s3cret")
	if _, err := box.Open(sealed); !errors.Is(err, ErrNoKey) {
		t.Errorf("Open(sealed) without a key: err = %v, want ErrNoKey", err)
	}
}
