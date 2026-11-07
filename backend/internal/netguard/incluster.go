package netguard

import (
	"context"
	"errors"
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
func ResolvesInCluster(ctx context.Context, r Resolver, host string) (bool, error) {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if host == "" {
		return false, nil
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return InClusterAddr(addr), nil
	}
	if r == nil {
		r = net.DefaultResolver
	}
	addrs, err := r.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return false, err
	}
	for _, addr := range addrs {
		if InClusterAddr(addr) {
			return true, nil
		}
	}
	return false, nil
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
