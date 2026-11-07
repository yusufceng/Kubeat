package netguard

import (
	"context"
	"fmt"
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
// that points at a cluster address, are both seen for where they land. A
// lookup that fails is reported as an error, not as "external", so the scope
// check can refuse an unresolvable name on the safe side.
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
	for host, want := range map[string]struct{ inCluster, wantErr bool }{
		"api.payments":        {inCluster: true},
		"kubernetes.default":  {inCluster: true},
		"172-30-0-1.sslip.io": {inCluster: true},
		"status.example.com":  {inCluster: false},
		"rebind.example.com":  {inCluster: true},
		// A lookup that fails is an error now, not a quiet "external": the
		// name could not be resolved at all.
		"does-not-resolve.example.com": {inCluster: false, wantErr: true},
		"":                             {inCluster: false},
		// An address needs no lookup and never errors.
		"172.30.0.1": {inCluster: true},
		"8.8.8.8":    {inCluster: false},
	} {
		r := &countingResolver{table: table}
		got, err := ResolvesInCluster(context.Background(), r, host)
		if got != want.inCluster {
			t.Errorf("ResolvesInCluster(%q) = %v, want %v", host, got, want.inCluster)
		}
		if (err != nil) != want.wantErr {
			t.Errorf("ResolvesInCluster(%q) err = %v, wantErr %v", host, err, want.wantErr)
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

// NotFound tells a name that does not exist (NXDOMAIN) from a resolver that
// could not answer: only the first may be let through behind a proxy (S81);
// a SERVFAIL, a refusal, a timeout and a cancelled lookup stay unsafe.
func TestNotFound(t *testing.T) {
	nxdomain := &net.DNSError{Err: "no such host", Name: "status.vendor.example", IsNotFound: true}
	for name, tc := range map[string]struct {
		err  error
		want bool
	}{
		"nxdomain":         {nxdomain, true},
		"wrapped nxdomain": {fmt.Errorf("lookup: %w", nxdomain), true},
		"servfail":         {&net.DNSError{Err: "server misbehaving", Name: "x", IsTemporary: true}, false},
		"refused":          {&net.DNSError{Err: "server misbehaving", Name: "x"}, false},
		"dns timeout":      {&net.DNSError{Err: "i/o timeout", Name: "x", IsTimeout: true}, false},
		"deadline":         {context.DeadlineExceeded, false},
		"cancelled":        {context.Canceled, false},
		"nil":              {nil, false},
	} {
		if got := NotFound(tc.err); got != tc.want {
			t.Errorf("NotFound(%s) = %v, want %v", name, got, tc.want)
		}
	}
	// What ResolvesInCluster returns for a name the resolver does not know.
	_, err := ResolvesInCluster(context.Background(), &countingResolver{}, "status.vendor.example")
	if !NotFound(err) {
		t.Errorf("ResolvesInCluster's NXDOMAIN: NotFound(%v) = false, want true", err)
	}
}

func TestResolvesInClusterNilResolver(t *testing.T) {
	// A nil resolver falls back to net.DefaultResolver; an address is
	// classified without touching it.
	if in, err := ResolvesInCluster(context.Background(), nil, "10.0.0.1"); !in || err != nil {
		t.Errorf("ResolvesInCluster(nil, 10.0.0.1) = (%v, %v), want (true, nil)", in, err)
	}
}

// ExternalRanges are the operator's company ranges, which the scope check
// counts as outside the cluster although they are private
// (UPTIME_TARGET_SCOPE_EXTERNAL_CIDRS, S80). Only addresses are read with
// them: one written bare and the ones a name resolves to. Every other private
// address stays inside the cluster, a name cluster-internal by its spelling
// stays so, and one cluster answer among company ones is enough to be inside.
// The zero value is the 0.50 reading.
func TestExternalRanges(t *testing.T) {
	ranges, err := ParseExternalRanges([]string{"10.50.0.0/16", " fd50::/16 ", "192.168.7.1/24"})
	if err != nil {
		t.Fatalf("ParseExternalRanges: %v", err)
	}
	if got, want := ranges.String(), "10.50.0.0/16,fd50::/16,192.168.7.0/24"; got != want {
		t.Errorf("String() = %q, want %q (masked)", got, want)
	}
	if ranges.Empty() || !(ExternalRanges{}).Empty() {
		t.Errorf("Empty() = %v for ranges, %v for the zero value", ranges.Empty(), (ExternalRanges{}).Empty())
	}
	for _, bad := range [][]string{{"10.50.0.1"}, {"10.50.0.0/33"}, {"corp"}, {""}} {
		if _, err := ParseExternalRanges(bad); err == nil {
			t.Errorf("ParseExternalRanges(%q) took it", bad)
		}
	}
	// An IPv4 range written as IPv6, or one over where IPv6 writes IPv4,
	// would match no address, since Contains reads ::ffff:10.50.0.1 as IPv4:
	// it is refused, with the IPv4 spelling where there is one.
	for entry, want := range map[string]string{
		"::ffff:10.50.0.0/112": "write it as 10.50.0.0/16",
		"::ffff:10.50.0.1/128": "write it as 10.50.0.1/32",
		"::ffff:0:0/96":        "write it as 0.0.0.0/0",
		"::/0":                 "write IPv4 ranges as IPv4",
		"::/80":                "write IPv4 ranges as IPv4",
	} {
		if _, err := ParseExternalRanges([]string{"10.50.0.0/16", entry}); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ParseExternalRanges(%q): err = %v, want one saying %q", entry, err, want)
		}
	}

	for ip, want := range map[string]bool{
		// Company addresses are outside the cluster, however they are written.
		"10.50.0.10":       false,
		"10.50.255.255":    false,
		"::ffff:10.50.1.1": false, // IPv4 written as IPv6
		"fd50::1":          false,
		"fd50::1%eth0":     false, // a zone is dropped
		"192.168.7.200":    false,
		// Every other private address is inside, as before.
		"10.49.255.255": true,
		"10.51.0.0":     true,
		"10.96.0.1":     true, // a Kubernetes API server's ClusterIP
		"10.128.2.15":   true, // an OpenShift pod IP
		"172.30.0.1":    true, // OpenShift's API server ClusterIP
		"192.168.8.1":   true,
		"fd00::1":       true,
		"127.0.0.1":     true,
		// A public address is outside either way.
		"203.0.113.10": false,
	} {
		addr := netip.MustParseAddr(ip)
		if got := ranges.InClusterAddr(addr); got != want {
			t.Errorf("InClusterAddr(%s) = %v, want %v", ip, got, want)
		}
		if got := ranges.InCluster(ip); got != want {
			t.Errorf("InCluster(%s) = %v, want %v", ip, got, want)
		}
		if (ExternalRanges{}).InClusterAddr(addr) != InClusterAddr(addr) {
			t.Errorf("the zero value reads %s otherwise than InClusterAddr", ip)
		}
	}
	// Names are classified by their spelling, as InCluster does.
	for host, want := range map[string]bool{
		"api":                               true,
		"api.payments.svc":                  true,
		"api.payments.svc.cluster.local":    true,
		"kubernetes.default.svc":            true,
		"10-50-0-10.shop.pod.cluster.local": true,
		"intranet.corp.example":             false,
		"api.payments":                      false, // resolved, below
	} {
		if got := ranges.InCluster(host); got != want {
			t.Errorf("InCluster(%q) = %v, want %v", host, got, want)
		}
	}

	table := map[string][]netip.Addr{
		"intranet.corp.example": addrsOf(t, "10.50.0.10"),
		"dual.corp.example":     addrsOf(t, "10.50.0.11", "fd50::11"),
		"public.corp.example":   addrsOf(t, "10.50.0.12", "203.0.113.12"),
		"split.corp.example":    addrsOf(t, "10.50.0.13", "172.30.44.9"), // one answer in the cluster
		"other.corp.example":    addrsOf(t, "10.51.0.1"),                 // private, not listed
		"api.payments":          addrsOf(t, "172.30.44.9"),               // ndots: another team's Service
		"kubernetes.default":    addrsOf(t, "172.30.0.1"),                // ndots: the API server
		"172-30-0-1.sslip.io":   addrsOf(t, "172.30.0.1"),
		"10-96-0-1.sslip.io":    addrsOf(t, "10.96.0.1"),
	}
	for host, want := range map[string]bool{
		"intranet.corp.example": false,
		"dual.corp.example":     false,
		"public.corp.example":   false,
		"split.corp.example":    true,
		"other.corp.example":    true,
		"api.payments":          true,
		"kubernetes.default":    true,
		"172-30-0-1.sslip.io":   true,
		"10-96-0-1.sslip.io":    true,
		"10.50.1.1":             false, // an address: no lookup
		"10.96.0.1":             true,
	} {
		got, err := ranges.ResolvesInCluster(context.Background(), &countingResolver{table: table}, host)
		if err != nil || got != want {
			t.Errorf("ResolvesInCluster(%q) = (%v, %v), want (%v, nil)", host, got, err, want)
		}
	}
	// Without the ranges a company answer is inside the cluster, as in 0.50.
	for _, host := range []string{"intranet.corp.example", "dual.corp.example", "10.50.1.1"} {
		if got, err := ResolvesInCluster(context.Background(), &countingResolver{table: table}, host); err != nil || !got {
			t.Errorf("ResolvesInCluster(%q) without ranges = (%v, %v), want (true, nil)", host, got, err)
		}
	}
	// A failed lookup is still an error, not "external".
	if got, err := ranges.ResolvesInCluster(context.Background(), &countingResolver{}, "nowhere.corp.example"); got || !NotFound(err) {
		t.Errorf("ResolvesInCluster(NXDOMAIN) = (%v, %v), want (false, NXDOMAIN)", got, err)
	}
}
