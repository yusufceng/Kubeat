package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strings"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/config"
	"github.com/yusufceng/Kubeat/backend/internal/netguard"
)

// resolveUsage is the help of the resolve command.
const resolveUsage = `usage: server resolve NAME...

Looks each NAME up from the backend's pod as the target scope does for a user
limited to some namespaces, and says what the scope makes of the answer. Run
it in the backend's container, so that the pod's own resolver and search
domains answer:

  kubectl -n kubeat exec deploy/kubeat-backend -- server resolve status.vendor.example

It reads UPTIME_TARGET_RESOLVE_TIMEOUT and UPTIME_TARGET_SCOPE_EXTERNAL_CIDRS,
without the ranges the backend ignores, which it names first; it ignores
UPTIME_TARGET_SCOPE_TRUST_PROXY, changes nothing and starts no server.`

// resolveCommand is the backend binary's resolve command (resolveUsage): it
// shows, before UPTIME_TARGET_SCOPE_TRUST_PROXY is turned on, which answer
// the backend's resolver gives an external name — NXDOMAIN, the one answer
// the setting lets through for a request handed to the proxy; a SERVFAIL or
// no answer in time, which it does not; or addresses, inside the cluster or
// outside — and so what a scoped user's target of that name comes to. The
// lookup is the scope check's (netguard.ScopeRules.Judge) under the API's
// bound, so the verdict is the API's. It answers 0 once every name was
// looked into, and 2 for a call without names or a configuration that
// cannot be read.
func resolveCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "-help" || args[0] == "--help" {
		fmt.Fprintln(stderr, resolveUsage)
		return 2
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(stderr, "invalid configuration:", err)
		return 2
	}
	// The ranges the backend's start sets aside are left out here too, so
	// that the verdict stays the API's; said, so that it does not surprise.
	for _, r := range cfg.TargetScopeExternalIgnored {
		fmt.Fprintf(stderr, "UPTIME_TARGET_SCOPE_EXTERNAL_CIDRS: %s; ignored, as the backend ignores it\n", r)
	}
	external, err := netguard.ParseExternalRanges(splitList(cfg.TargetScopeExternalCIDRs))
	if err != nil {
		fmt.Fprintln(stderr, "invalid configuration:", err)
		return 2
	}
	for _, name := range args {
		fmt.Fprintln(stdout, scopeLookup(context.Background(), net.DefaultResolver, external, cfg.TargetResolveTimeout, name))
	}
	return 0
}

// scopeLookup is the line resolveCommand prints for host: what r answers for
// it within timeout, and what the scope makes of that for a user limited to
// some namespaces, none of which host names.
func scopeLookup(ctx context.Context, r netguard.Resolver, external netguard.ExternalRanges, timeout time.Duration, host string) string {
	lookup := &recordedLookup{resolver: r}
	rules := netguard.ScopeRules{External: external, Resolver: lookup, ResolveTimeout: timeout}
	verdict := rules.Judge(ctx, func(string) bool { return false }, host, nil)
	name := netguard.ScopeHost(host)
	if name == "" {
		name = fmt.Sprintf("%q", host)
	}
	if !lookup.asked {
		switch {
		case verdict == netguard.NotRefused:
			return name + ": an address outside the cluster; not looked up: in scope for anyone"
		default:
			return name + ": refused by its spelling (a cluster address or name, a short name, a host not written as a name or an address); not looked up: a scoped user reaches a Service only as service.namespace.svc in a namespace of theirs (reason target)"
		}
	}
	var dnsErr *net.DNSError
	switch err := lookup.err; {
	case err == nil && verdict == netguard.RefusedOutside:
		return fmt.Sprintf("%s: resolves to %s, inside the cluster: refused for a scoped user (reason target)", name, addrList(lookup.addrs))
	case err == nil:
		return fmt.Sprintf("%s: resolves to %s, outside the cluster: in scope for anyone", name, addrList(lookup.addrs))
	case netguard.NotFound(err):
		return name + ": NXDOMAIN, no such name for this pod's resolver: refused for a scoped user (reason resolve_error); with UPTIME_TARGET_SCOPE_TRUST_PROXY=true an HTTP(S) request handed to the proxy is let through"
	case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &dnsErr) && dnsErr.IsTimeout):
		return fmt.Sprintf("%s: no answer within %s (UPTIME_TARGET_RESOLVE_TIMEOUT): refused for a scoped user (reason resolve_error); UPTIME_TARGET_SCOPE_TRUST_PROXY does not change that", name, timeout)
	default:
		return fmt.Sprintf("%s: did not resolve (%v): refused for a scoped user (reason resolve_error); UPTIME_TARGET_SCOPE_TRUST_PROXY does not change that", name, err)
	}
}

// recordedLookup is a resolver that keeps what it answered, so that the
// command can say which answer the scope's verdict rests on.
type recordedLookup struct {
	resolver netguard.Resolver
	asked    bool
	addrs    []netip.Addr
	err      error
}

func (l *recordedLookup) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	l.asked = true
	l.addrs, l.err = l.resolver.LookupNetIP(ctx, network, host)
	return l.addrs, l.err
}

// addrList is the addresses, comma-separated.
func addrList(addrs []netip.Addr) string {
	parts := make([]string, len(addrs))
	for i, addr := range addrs {
		parts[i] = addr.String()
	}
	return strings.Join(parts, ", ")
}
