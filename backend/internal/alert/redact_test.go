package alert

import (
	"testing"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

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
