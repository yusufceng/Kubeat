// Package secretbox encrypts the secrets the database holds: tokens,
// passwords and webhook addresses. Someone who gets a copy of the database
// file, or of a backup of it, should not get those with it.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// prefix marks a stored value as encrypted, and with which scheme. A value
// without it is plain text: one written before a key was configured.
const prefix = "enc:v1:"

// MinKeyLength is the shortest key accepted. The key should be random; this
// only keeps out the obviously weak.
const MinKeyLength = 32

// ErrNoKey means a value is encrypted and no key was configured to read it.
var ErrNoKey = errors.New("secretbox: the value is encrypted, but no key is configured (UPTIME_SECRET_KEY)")

// ErrWrongKey means a value was encrypted with another key.
var ErrWrongKey = errors.New("secretbox: the value cannot be decrypted: the key is not the one it was encrypted with")

// Box encrypts and decrypts with one key. A nil *Box is valid: it leaves
// values as they are and can read only plain ones.
type Box struct {
	aead cipher.AEAD
}

// New returns a Box for the given key, a random string of at least
// MinKeyLength characters. The AES-256 key is derived from it.
func New(key string) (*Box, error) {
	if len(key) < MinKeyLength {
		return nil, fmt.Errorf("secretbox: the key must be at least %d characters", MinKeyLength)
	}
	sum := sha256.Sum256([]byte(key))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Sealed reports whether a stored value is encrypted.
func Sealed(stored string) bool { return strings.HasPrefix(stored, prefix) }

// Seal returns the form of a value to store. Empty stays empty, so that "no
// secret" needs no key to be told apart.
func (b *Box) Seal(plain string) (string, error) {
	if b == nil || plain == "" {
		return plain, nil
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := b.aead.Seal(nonce, nonce, []byte(plain), nil)
	return prefix + base64.RawStdEncoding.EncodeToString(sealed), nil
}

// Open returns the value a stored form stands for. A plain value is
// returned as it is.
func (b *Box) Open(stored string) (string, error) {
	if !Sealed(stored) {
		return stored, nil
	}
	if b == nil {
		return "", ErrNoKey
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(stored, prefix))
	if err != nil || len(raw) < b.aead.NonceSize() {
		return "", errors.New("secretbox: the stored value is damaged")
	}
	nonce, ciphertext := raw[:b.aead.NonceSize()], raw[b.aead.NonceSize():]
	plain, err := b.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", ErrWrongKey
	}
	return string(plain), nil
}
