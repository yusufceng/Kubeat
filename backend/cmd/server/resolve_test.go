package main

import (
	"bytes"
	"context"
	"net"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/netguard"
)

// tableResolver answers a name from its table, with addresses or with an
// error; an unknown name does not exist (NXDOMAIN). A name it is asked for
// is noted, so that a test can tell the names looked up from those judged
// by their spelling.
type tableResolver struct {
	answers map[string]any
	asked   []string
}

func (r *tableResolver) LookupNetIP(ctx context.Context, _, host string) ([]netip.Addr, error) {
	r.asked = append(r.asked, host)
	switch answer := r.answers[host].(type) {
	case []netip.Addr:
		return answer, nil
	case error:
		return nil, answer
	case func(context.Context) error:
		return nil, answer(ctx)
	}
	return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
}

// The resolve command says, for each name, which answer the backend's
// resolver gives and what the scope makes of it for a scoped user, without
// UPTIME_TARGET_SCOPE_TRUST_PROXY: an NXDOMAIN is the one answer the setting
// would let through, a SERVFAIL and no answer in time are not. Names refused
// by their spelling — a bare cluster address, a short name, another
// namespace's .svc and full name, the API server's names — are not looked
// up, as the scope does not look them up; a two-label name the search
// domains (ndots) turn into a Service, and a public name pointing into the
// cluster, are refused where they resolve. A company range counts as
// outside the cluster.
func TestScopeLookup(t *testing.T) {
	addrs := func(ips ...string) []netip.Addr {
		out := make([]netip.Addr, len(ips))
		for i, ip := range ips {
			out[i] = netip.MustParseAddr(ip)
		}
		return out
	}
	resolver := &tableResolver{answers: map[string]any{
		"status.vendor.example": addrs("203.0.113.7"),
		"intranet.corp.example": addrs("10.50.0.10"),
		"api.payments":          addrs("172.30.44.9"),
		"kubernetes.default":    addrs("172.30.0.1"),
		"172-30-0-1.sslip.io":   addrs("172.30.0.1"),
		"servfail.example.com":  &net.DNSError{Err: "server misbehaving", Name: "servfail.example.com", IsTemporary: true},
		"slow.example.com":      &net.DNSError{Err: "i/o timeout", Name: "slow.example.com", IsTimeout: true},
		"stuck.example.com": func(ctx context.Context) error {
			<-ctx.Done() // the API's bound ends it
			return ctx.Err()
		},
	}}
	external, err := netguard.ParseExternalRanges([]string{"10.50.0.0/16"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ host, want string }{
		{"status.vendor.example", "status.vendor.example: resolves to 203.0.113.7, outside the cluster: in scope for anyone"},
		{"Status.Vendor.Example.", "status.vendor.example: resolves to 203.0.113.7, outside the cluster"},
		{"intranet.corp.example", "intranet.corp.example: resolves to 10.50.0.10, outside the cluster"},
		{"api.payments", "api.payments: resolves to 172.30.44.9, inside the cluster: refused for a scoped user (reason target)"},
		{"kubernetes.default", "kubernetes.default: resolves to 172.30.0.1, inside the cluster: refused"},
		{"172-30-0-1.sslip.io", "172-30-0-1.sslip.io: resolves to 172.30.0.1, inside the cluster: refused"},
		{"nowhere.example.com", "nowhere.example.com: NXDOMAIN, no such name for this pod's resolver: refused for a scoped user (reason resolve_error); with UPTIME_TARGET_SCOPE_TRUST_PROXY=true"},
		{"servfail.example.com", "servfail.example.com: did not resolve (lookup servfail.example.com: server misbehaving): refused for a scoped user (reason resolve_error); UPTIME_TARGET_SCOPE_TRUST_PROXY does not change that"},
		{"slow.example.com", "slow.example.com: no answer within 20ms (UPTIME_TARGET_RESOLVE_TIMEOUT): refused for a scoped user (reason resolve_error); UPTIME_TARGET_SCOPE_TRUST_PROXY does not change that"},
		{"stuck.example.com", "stuck.example.com: no answer within 20ms (UPTIME_TARGET_RESOLVE_TIMEOUT)"},
		{"203.0.113.9", "203.0.113.9: an address outside the cluster; not looked up"},
		{"10.50.1.1", "10.50.1.1: an address outside the cluster; not looked up"},
		{"172.30.0.1", "172.30.0.1: refused by its spelling"},
		{"api", "api: refused by its spelling"},
		{"api.payments.svc", "api.payments.svc: refused by its spelling"},
		{"api.payments.svc.cluster.local", "api.payments.svc.cluster.local: refused by its spelling"},
		{"kubernetes.default.svc", "kubernetes.default.svc: refused by its spelling"},
		{"172.30.1", "172.30.1: refused by its spelling"},
	} {
		resolver.asked = nil
		got := scopeLookup(context.Background(), resolver, external, 20*time.Millisecond, tc.host)
		if !strings.HasPrefix(got, tc.want) {
			t.Errorf("%s:\n got %q\nwant %q…", tc.host, got, tc.want)
		}
		if spelled := strings.Contains(tc.want, "not looked up") || strings.Contains(tc.want, "by its spelling"); spelled && len(resolver.asked) != 0 {
			t.Errorf("%s was looked up: %v", tc.host, resolver.asked)
		}
	}
}

// Without a name the command says how it is used and fails; it starts no
// server either way.
func TestResolveCommandUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := resolveCommand(nil, &stdout, &stderr); code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "usage: server resolve NAME") {
		t.Errorf("no name: %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
	stderr.Reset()
	if code := resolveCommand([]string{"--help"}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "kubectl -n kubeat exec deploy/kubeat-backend -- server resolve") {
		t.Errorf("--help: %d, stderr %q", code, stderr.String())
	}
}

// The command reads the scope's settings as the backend does: a company
// range it cannot read fails it before any lookup.
func TestResolveCommandReadsSettings(t *testing.T) {
	// The settings of the environment the tests run in are not the test's.
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(name, "UPTIME_") {
			t.Setenv(name, "")
		}
	}
	t.Setenv("UPTIME_TARGET_SCOPE_EXTERNAL_CIDRS", "not-a-range")
	var stdout, stderr bytes.Buffer
	if code := resolveCommand([]string{"203.0.113.7"}, &stdout, &stderr); code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "invalid configuration") {
		t.Errorf("bad range: %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
	t.Setenv("UPTIME_TARGET_SCOPE_EXTERNAL_CIDRS", "")
	stderr.Reset()
	if code := resolveCommand([]string{"203.0.113.7", "172.30.0.1"}, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
		t.Fatalf("addresses: %d, stderr %q", code, stderr.String())
	}
	if lines := strings.Split(strings.TrimSpace(stdout.String()), "\n"); len(lines) != 2 ||
		!strings.HasPrefix(lines[0], "203.0.113.7: an address outside the cluster") || !strings.HasPrefix(lines[1], "172.30.0.1: refused by its spelling") {
		t.Errorf("stdout = %q", stdout.String())
	}
}
