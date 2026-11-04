package netguard

import (
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
		plain := addr.Unmap().WithZone("")
		for _, prefix := range clusterRanges {
			if prefix.Contains(plain) {
				return true
			}
		}
		return false
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
