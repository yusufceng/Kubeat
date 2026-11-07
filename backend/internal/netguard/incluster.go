package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
)

// clusterRanges are the address ranges in-cluster targets live in: the
// private ranges ClusterIPs, pod IPs and node IPs are taken from, the
// shared address space (100.64.0.0/10) some clouds and CNIs use for them,
// and the unique local IPv6 range. Loopback and link-local are among them
// too: both are the backend's own neighbourhood, and the target policy
// denies them anyway.
var clusterRanges = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("fe80::/10"),
}

// clusterSuffixes end the DNS names of Services and pods: a Service's
// "name.namespace.svc", its fully qualified form, and a pod's
// "ip.namespace.pod.cluster.local".
var clusterSuffixes = []string{".svc", ".svc.cluster.local", ".cluster.local"}

// InCluster reports whether host, a monitor target's host name or IP
// address without port or brackets, is inside the cluster: an address in
// a private range (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16,
// 100.64.0.0/10, fc00::/7, with loopback and link-local), a name ending in
// .svc, .svc.cluster.local or .cluster.local, or a name without a dot,
// which is a Service's short name in the pod's own namespace. Everything
// else is external: the public Internet, or the company's own network
// outside the cluster, which Kubeat's guardrails treat alike.
//
// The decision is made on the name as written, on purpose: a public DNS
// name that resolves to a private address counts as external. Resolving
// it here would make the classification depend on an answer that can
// change between two lookups, and let whoever controls a zone put a name
// in the cheaper class.
func InCluster(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if host == "" {
		return false
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return InClusterAddr(addr)
	}
	if !strings.Contains(host, ".") {
		return true
	}
	for _, suffix := range clusterSuffixes {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

// InClusterAddr reports whether addr is in one of the ranges in-cluster
// targets live in (the private ranges, the shared 100.64.0.0/10, loopback
// and link-local; see clusterRanges). It is InCluster's test for an
// address, exported so the dial-time scope check can apply it to an address
// a name resolved to. An IPv4 address written as IPv6 (::ffff:10.0.0.1) is
// taken as that IPv4 address, and a zone is dropped.
func InClusterAddr(addr netip.Addr) bool {
	plain := addr.Unmap().WithZone("")
	if !plain.IsValid() {
		return false
	}
	for _, prefix := range clusterRanges {
		if prefix.Contains(plain) {
			return true
		}
	}
	return false
}

// Resolver looks up the IP addresses of a host name. *net.Resolver, and so
// net.DefaultResolver, satisfies it; a test supplies its own.
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// ExternalRanges are the operator's own address ranges that the per-user
// scope check counts as outside the cluster although they are private
// (UPTIME_TARGET_SCOPE_EXTERNAL_CIDRS): the company's network, which the
// cluster reaches but which is not part of it. InClusterAddr counts every
// private address as inside the cluster, so without them a user limited to
// some namespaces cannot aim a monitor at a company host, by its address or
// by a name that resolves to it. The zero value holds no range, and every
// private address is inside the cluster, as before.
//
// This is the one place the rule is kept, for every scope check to ask, at
// create and edit (api.targetInScope) or wherever else a connection is held
// to the scope, so that two checks never classify one address apart: an
// address is inside the cluster for the scope when InClusterAddr says so and
// none of the ranges holds it (ExternalRanges.InClusterAddr). Only addresses
// are read this way — a host written as an address, and the addresses a name
// resolves to. A name that is cluster-internal by its spelling (a *.svc or
// pod name, a short name) stays so whatever it resolves to, and a name with
// one answer inside the cluster beside the company's is inside. Only the
// scope reads the ranges: the target policy and the load guardrails
// (guardrails.External, the scheduler's host limits) classify as InCluster
// does.
//
// The ranges must stay outside the cluster's pod, Service and node networks
// and its API VIP: an address of theirs listed here is one a scoped user
// reaches with no namespace to answer for. The router's VIP (and a
// LoadBalancer Service's VIP, for that Service alone) is the one cluster
// address an operator may list, and only on purpose: as a /32 when it sits in
// the node network, or within the company range that holds a load balancer
// outside the cluster in front of the router. Either way every Route behind
// that VIP then opens to scoped users. config.Load
// refuses ranges that hold the API server's address or one of the backend's
// own; the rest is the operator's to keep out (README, "Hedef politikası").
type ExternalRanges struct{ prefixes []netip.Prefix }

// mappedIPv4 is where IPv6 writes IPv4 addresses (::ffff:10.50.0.1).
// Contains reads such an address as IPv4, so a range written in it would
// hold nothing.
var mappedIPv4 = netip.MustParsePrefix("::ffff:0:0/96")

// ParseExternalRanges reads ExternalRanges from CIDRs such as
// "10.50.0.0/16", one range each; an entry that is not a CIDR is an error.
// So is one written in, or over, mappedIPv4 (::ffff:10.50.0.0/112): it
// would match no address, and the operator would not see why. The error
// gives the IPv4 spelling.
func ParseExternalRanges(cidrs []string) (ExternalRanges, error) {
	var e ExternalRanges
	for _, entry := range cidrs {
		entry = strings.TrimSpace(entry)
		prefix, err := netip.ParsePrefix(entry)
		if err != nil {
			return ExternalRanges{}, fmt.Errorf("scope external range %q: %w", entry, err)
		}
		prefix = prefix.Masked()
		if prefix.Overlaps(mappedIPv4) {
			if prefix.Bits() >= mappedIPv4.Bits() {
				return ExternalRanges{}, fmt.Errorf("scope external range %q is an IPv4 range written as IPv6, which no address matches; write it as %s",
					entry, netip.PrefixFrom(prefix.Addr().Unmap(), prefix.Bits()-mappedIPv4.Bits()))
			}
			return ExternalRanges{}, fmt.Errorf("scope external range %q holds %s, where IPv6 writes IPv4 addresses, which no address matches; write IPv4 ranges as IPv4, such as 10.50.0.0/16",
				entry, mappedIPv4)
		}
		e.prefixes = append(e.prefixes, prefix)
	}
	return e, nil
}

// Empty reports whether e holds no range.
func (e ExternalRanges) Empty() bool { return len(e.prefixes) == 0 }

// String lists the ranges, comma-separated, for the start's log.
func (e ExternalRanges) String() string {
	parts := make([]string, len(e.prefixes))
	for i, prefix := range e.prefixes {
		parts[i] = prefix.String()
	}
	return strings.Join(parts, ",")
}

// Contains reports whether addr is in one of the ranges. An IPv4 address
// written as IPv6 (::ffff:10.50.0.1) is taken as that IPv4 address, and a
// zone is dropped, as InClusterAddr reads them.
func (e ExternalRanges) Contains(addr netip.Addr) bool {
	_, ok := e.Holding(addr)
	return ok
}

// Holding returns the first range that holds addr, read as Contains reads
// it, for config.Load to name the range its start guard refuses.
func (e ExternalRanges) Holding(addr netip.Addr) (netip.Prefix, bool) {
	plain := addr.Unmap().WithZone("")
	if !plain.IsValid() {
		return netip.Prefix{}, false
	}
	for _, prefix := range e.prefixes {
		if prefix.Contains(plain) {
			return prefix, true
		}
	}
	return netip.Prefix{}, false
}

// InClusterAddr is the scope's reading of an address: inside the cluster
// when InClusterAddr says so and none of the ranges holds it.
func (e ExternalRanges) InClusterAddr(addr netip.Addr) bool {
	return InClusterAddr(addr) && !e.Contains(addr)
}

// InCluster is InCluster for the scope check: a host written as an address
// is classified by e.InClusterAddr, so one in the ranges is outside the
// cluster; a name is classified by its spelling, as InCluster does.
func (e ExternalRanges) InCluster(host string) bool {
	if addr, err := netip.ParseAddr(strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))); err == nil {
		return e.InClusterAddr(addr)
	}
	return InCluster(host)
}

// ResolvesInCluster is the package's ResolvesInCluster with the ranges read
// as outside the cluster: host resolves into the cluster when one of its
// addresses is inside it by e.InClusterAddr. A name that resolves only to
// company and public addresses does not; one with a cluster address among
// them does.
func (e ExternalRanges) ResolvesInCluster(ctx context.Context, r Resolver, host string) (bool, error) {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if host == "" {
		return false, nil
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return e.InClusterAddr(addr), nil
	}
	if r == nil {
		r = net.DefaultResolver
	}
	addrs, err := r.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return false, err
	}
	for _, addr := range addrs {
		if e.InClusterAddr(addr) {
			return true, nil
		}
	}
	return false, nil
}

// ResolvesInCluster reports whether host, a monitor target's host name,
// resolves to a cluster-internal address. It looks host up once through r
// (net.DefaultResolver when r is nil) and returns one of three outcomes:
//
//   - (true, nil)  host is, or resolves to, a cluster address: one of the
//     answers is in a cluster range (or host is already such an address).
//   - (false, nil) host is, or resolves only to, addresses outside the
//     cluster, or host is empty.
//   - (false, err) the lookup failed — a resolver outage, a timeout or an
//     NXDOMAIN — so whether host reaches the cluster could not be decided.
//     The caller must not read this as "external": the dial-time scope check
//     treats it as unsafe (ADR-0003 c, resolver-error fail-safe), except an
//     NXDOMAIN (NotFound) for a request the operator's proxy resolves.
//
// A host that is already an address is classified without a lookup and never
// returns an error.
//
// This is the resolution the dial-time scope check is built on, for the
// names InCluster classifies as external by their spelling. The lookup is
// the backend's own, with the pod's search domains (ndots), so a two-label
// short name such as "api.payments" that the resolver turns into
// "api.payments.svc.cluster.local" is seen for the cluster address it
// reaches, as is a public name that resolves to a cluster address
// ("172-30-0-1.sslip.io"). The lookup is made once and the decision rests on
// that one answer: a name whose answer changes between this lookup and the
// dial a check makes later is beyond this gate (TOCTOU), which is why
// ADR-0003 keeps the deny-CIDR and a mandatory multi-tenant proof.
//
// It reads every private address as inside the cluster; the scope check asks
// ExternalRanges.ResolvesInCluster, which leaves the operator's company
// ranges outside.
func ResolvesInCluster(ctx context.Context, r Resolver, host string) (bool, error) {
	return ExternalRanges{}.ResolvesInCluster(ctx, r, host)
}

// NotFound reports whether err, a lookup error from ResolvesInCluster, says
// that the name does not exist (NXDOMAIN, *net.DNSError with IsNotFound), as
// opposed to a resolver that could not answer: a SERVFAIL, a refusal, a
// timeout or a cancelled lookup, for which it is false. A local NXDOMAIN
// tells where a name lands only for a connection the backend makes itself:
// a request handed to a proxy outside the cluster is resolved there, so the
// scope check may let it through behind UPTIME_TARGET_SCOPE_TRUST_PROXY
// (api.targetInScope, ADR-0003 c). Every other failure stays unsafe.
func NotFound(err error) bool {
	var dnsErr *net.DNSError
	return errors.As(err, &dnsErr) && dnsErr.IsNotFound && !dnsErr.IsTimeout
}

// Namespace returns the Kubernetes namespace a cluster-internal target names,
// and true, or "" and false when it cannot be told from the host alone.
//
// A Service or a pod carries its namespace in its DNS name, in the label
// before "svc" or "pod": "api.shop.svc", "api.shop.svc.cluster.local",
// "10-0-0-1.shop.pod.cluster.local" and the headless form
// "pod-0.api.shop.svc.cluster.local" all name the namespace "shop". The scan
// runs from the end, so a Service named "svc" or a namespace named "svc" is
// read right. A bare short name without a dot ("api") is a Service in the
// backend pod's own namespace, Kubeat's, not a tenant's; an IP address (a
// ClusterIP or a pod IP) names no namespace on its own. For those, and for a
// cluster-internal host whose namespace is not written, false is returned:
// the caller cannot attribute the target to a tenant.
//
// Only a host InCluster reports true for has a namespace to look for; an
// external name is not asked about here.
func Namespace(host string) (string, bool) {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if host == "" {
		return "", false
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return "", false // an address names no namespace
	}
	labels := strings.Split(host, ".")
	for i := len(labels) - 1; i >= 1; i-- {
		if labels[i] == "svc" || labels[i] == "pod" {
			if ns := labels[i-1]; ns != "" {
				return ns, true
			}
			return "", false
		}
	}
	return "", false
}
