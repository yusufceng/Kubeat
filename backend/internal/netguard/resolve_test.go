package netguard

import (
	"context"
	"net"
	"net/netip"
	"strings"
	"testing"
)

// countingResolver answers from a fixed table and counts its lookups, so a
// test can assert that a classification resolves once (pinned).
type countingResolver struct {
	table map[string][]netip.Addr
	calls int
}

func (r *countingResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	r.calls++
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if addrs, ok := r.table[host]; ok {
		return addrs, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
}

func addrsOf(t *testing.T, ips ...string) []netip.Addr {
	t.Helper()
	out := make([]netip.Addr, len(ips))
	for i, ip := range ips {
		out[i] = netip.MustParseAddr(ip)
	}
	return out
}

func TestInClusterAddr(t *testing.T) {
	for ip, want := range map[string]bool{
		"10.0.0.1":        true,
		"172.30.0.1":      true, // OpenShift's ClusterIP range
		"192.168.1.1":     true,
		"100.64.0.1":      true,
		"127.0.0.1":       true,
		"::1":             true,
		"fd00::1":         true,
		"::ffff:10.0.0.1": true, // IPv4 written as IPv6
		"8.8.8.8":         false,
		"172.32.0.1":      false,
		"2001:db8::1":     false,
	} {
		if got := InClusterAddr(netip.MustParseAddr(ip)); got != want {
			t.Errorf("InClusterAddr(%q) = %v, want %v", ip, got, want)
		}
	}
	if InClusterAddr(netip.Addr{}) {
		t.Error("InClusterAddr(invalid) = true, want false")
	}
}

// ResolvesInCluster resolves the names InCluster calls external by spelling
// and reports whether they reach a cluster address: the dial-time half of the
// scope check (ADR-0003 c). The lookup carries the pod's search domains, so a
// two-label short name the resolver turns into a Service, and a public name
// that points at a cluster address, are both seen for where they land.
func TestResolvesInCluster(t *testing.T) {
	table := map[string][]netip.Addr{
		// A two-label short name the pod's search domains (ndots) turn into a
		// Service, and the API server's two-label name: both land in-cluster.
		"api.payments":       addrsOf(t, "172.30.44.9"),
		"kubernetes.default": addrsOf(t, "172.30.0.1"),
		// A public name that resolves to a cluster address.
		"172-30-0-1.sslip.io": addrsOf(t, "172.30.0.1"),
		// A genuinely external name.
		"status.example.com": addrsOf(t, "93.184.216.34"),
		// An external name that resolves to several addresses, one internal:
		// one cluster answer is enough to refuse.
		"rebind.example.com": addrsOf(t, "93.184.216.34", "10.1.2.3"),
	}
	for host, want := range map[string]bool{
		"api.payments":                 true,
		"kubernetes.default":           true,
		"172-30-0-1.sslip.io":          true,
		"status.example.com":           false,
		"rebind.example.com":           true,
		"does-not-resolve.example.com": false, // lookup fails -> not in cluster
		"":                             false,
		// An address needs no lookup.
		"172.30.0.1": true,
		"8.8.8.8":    false,
	} {
		r := &countingResolver{table: table}
		if got := ResolvesInCluster(context.Background(), r, host); got != want {
			t.Errorf("ResolvesInCluster(%q) = %v, want %v", host, got, want)
		}
		// Pinned (TOCTOU): a name is resolved exactly once per decision, an
		// address not at all.
		wantCalls := 0
		if host != "" {
			if _, err := netip.ParseAddr(strings.TrimSuffix(host, ".")); err != nil {
				wantCalls = 1
			}
		}
		if r.calls != wantCalls {
			t.Errorf("ResolvesInCluster(%q) made %d lookups, want %d", host, r.calls, wantCalls)
		}
	}
}

func TestResolvesInClusterNilResolver(t *testing.T) {
	// A nil resolver falls back to net.DefaultResolver; an address is
	// classified without touching it.
	if !ResolvesInCluster(context.Background(), nil, "10.0.0.1") {
		t.Error("ResolvesInCluster(nil, 10.0.0.1) = false, want true")
	}
}
