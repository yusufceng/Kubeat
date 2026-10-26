package license

import (
	"crypto/ed25519"
	"encoding/base64"
)

// PublicKey verifies licenses. It is compiled in, not configured: a key
// that could be set would let anyone sign their own license. The private
// half is kept offline and never in this repository.
var PublicKey = mustKey("2WyL1aIq58bPSPV/jhrY4nZzlqH8LDGwMTsPWCrIFRg=")

func mustKey(s string) ed25519.PublicKey {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(b) != ed25519.PublicKeySize {
		panic("license: bad public key")
	}
	return ed25519.PublicKey(b)
}
