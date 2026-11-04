package checker

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/yusufceng/Kubeat/backend/internal/netguard"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// A failed check's stored error must not disclose the resolved in-cluster
// address the network layer dialed: whoever may read the monitor reads its
// results, so an error like "dial tcp 172.30.x.x:8080: connection refused"
// would leak a service or pod IP the viewer cannot reach otherwise.
func TestCheckMasksResolvedAddressInError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close() // nothing listens: the dial is refused, naming the address

	c := New(Options{BodyLimit: 1 << 20}) // no guard: loopback is allowed here

	for _, m := range []store.Monitor{
		{Type: store.MonitorTCP, Target: addr},
		httpMonitor("http://" + addr + "/"),
	} {
		got := c.Check(context.Background(), m)
		if got.Up || got.Error == "" {
			t.Fatalf("%s: want a failed check with an error: %+v", m.Type, got)
		}
		if strings.Contains(got.Error, "127.0.0.1") {
			t.Errorf("%s: error still shows the resolved address: %q", m.Type, got.Error)
		}
		if !strings.Contains(got.Error, "[address hidden]") {
			t.Errorf("%s: error does not mark a hidden address: %q", m.Type, got.Error)
		}
		// The error class survives, so the failure is still readable.
		if !strings.Contains(got.Error, "refused") {
			t.Errorf("%s: error lost its class: %q", m.Type, got.Error)
		}
	}
}

// maskResolved hides a resolved address in an ordinary network error, but
// keeps the two kinds of error that are about an address on purpose.
func TestMaskResolved(t *testing.T) {
	dial := "dial tcp 10.0.0.1:8080: connect: connection refused"
	if got := maskResolved(store.MonitorHTTP, fmt.Errorf("%s", dial)); strings.Contains(got, "10.0.0.1") {
		t.Errorf("a dial error still shows the address: %q", got)
	}

	// The target policy's own refusal names the blocked address and range so
	// an operator can allow it; it is kept.
	blocked := fmt.Errorf("169.254.169.254 is %w (169.254.0.0/16); UPTIME_TARGET_ALLOW_CIDRS can allow it", netguard.ErrBlocked)
	if got := maskResolved(store.MonitorTCP, blocked); !strings.Contains(got, "169.254.169.254") {
		t.Errorf("a policy refusal lost the address it refused: %q", got)
	}

	// A DNS check is about addresses by its nature: the records it compared
	// and found are kept, as they are on a successful check.
	dns := fmt.Errorf("no A record contains %q; found 10.0.0.1, 10.0.0.2", "10.0.0.9")
	if got := maskResolved(store.MonitorDNS, dns); !strings.Contains(got, "10.0.0.9") || !strings.Contains(got, "10.0.0.1") {
		t.Errorf("a DNS record check lost its addresses: %q", got)
	}
}
