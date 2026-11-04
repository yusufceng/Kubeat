package alert

import (
	"testing"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

func TestMaskAddrsHidesResolvedAddresses(t *testing.T) {
	cases := map[string]string{
		"dial tcp 172.30.126.132:8080: connect: connection refused": "dial tcp [address hidden]: connect: connection refused",
		"lookup test-mail on 10.0.0.10:53: no such host":            "lookup test-mail on [address hidden]: no such host",
		"dial tcp [fd00::1]:443: i/o timeout":                       "dial tcp [address hidden]: i/o timeout",
		// No address: the class is kept untouched.
		"connection refused": "connection refused",
	}
	for in, want := range cases {
		if got := MaskAddrs(in); got != want {
			t.Errorf("MaskAddrs(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMaskAddrsLeavesHostnames(t *testing.T) {
	// A host name is not an address and must survive, so the user still sees
	// what the channel tried to reach.
	in := "Post \"https://hooks.example.com/x\": dial tcp: lookup hooks.example.com: no such host"
	if got := MaskAddrs(in); got != in {
		t.Errorf("MaskAddrs changed a hostname-only message: %q", got)
	}
}

func TestDeliveryErrorHidesAddressAndSecret(t *testing.T) {
	ch := store.Channel{Target: "https://hooks.slack.com/services/T/B/zzz", Secret: "s3cr3t"}
	raw := "Post \"https://hooks.slack.com/services/T/B/zzz\": dial tcp 172.30.1.2:443: connect: connection refused; token s3cr3t"
	got := MaskAddrs(Redact(raw, ch))
	for _, leak := range []string{"172.30.1.2", "s3cr3t", "/services/T/B/zzz"} {
		if contains(got, leak) {
			t.Errorf("delivery error still shows %q: %q", leak, got)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
