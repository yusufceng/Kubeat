package api

import (
	"context"
	"log/slog"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

// publicAddressRefresh is how often PublicAddresses resolves the public
// URL's host again, and publicAddressLookup how long one resolution may
// take. Neither is a setting (PO, sprint 10): the addresses of a load
// balancer change seldom, and a missed change only quiets the signal for a
// few minutes.
const (
	publicAddressRefresh = 5 * time.Minute
	publicAddressLookup  = 10 * time.Second
)

// A HostResolver resolves a host name: the canonical name it ends at after
// any CNAME, and its addresses. *net.Resolver is one.
type HostResolver interface {
	LookupCNAME(ctx context.Context, host string) (string, error)
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// PublicAddresses are the addresses Kubeat's own public URL
// (UPTIME_PUBLIC_URL) resolves to: those of the load balancer or proxy in
// front of the UI. A throttled request keyed on one of them was keyed on
// that load balancer, not on its client, which means the trusted hops are
// one too few (behind a GCE Ingress or a GKE Gateway, 1 where 2 are
// needed): clientIP counts it as hops_too_low and warns.
//
// The host is resolved when Run starts and every publicAddressRefresh after
// it, never on a request's path. When it cannot be resolved, or resolves
// through a CNAME (a CDN, a cloud load balancer's own name), the set is
// empty and the signal quiet: a false alarm would teach the operator to
// ignore it. Loopback, link-local and unspecified addresses are left out, so
// that a public URL of localhost, as behind a port-forward, matches no
// source. A nil *PublicAddresses holds none. It is safe for concurrent use.
type PublicAddresses struct {
	// host is the public URL's host as an absolute name, so that the pod's
	// search domains do not apply to it; literal is the host when it is an
	// address, and then nothing is resolved.
	host     string
	literal  netip.Addr
	resolver HostResolver
	logger   *slog.Logger
	// after is time.After, a test's clock in a test.
	after func(time.Duration) <-chan time.Time

	mu    sync.RWMutex
	addrs map[netip.Addr]bool
	// said is what the last resolution logged, so that only a change of it
	// is logged again.
	said string
}

// NewPublicAddresses returns the PublicAddresses of publicURL's host, looked
// up with resolver and logged to logger, or nil when publicURL has no host
// (it is unset): there is then no address to tell a load balancer by.
func NewPublicAddresses(publicURL string, resolver HostResolver, logger *slog.Logger) *PublicAddresses {
	u, err := url.Parse(publicURL)
	if publicURL == "" || err != nil || u.Hostname() == "" {
		return nil
	}
	p := &PublicAddresses{resolver: resolver, logger: logger, after: time.After}
	host := u.Hostname()
	if addr, err := netip.ParseAddr(host); err == nil {
		p.literal = addr
		host = addr.String()
	} else {
		host = strings.TrimSuffix(host, ".") + "."
	}
	p.host = host
	return p
}

// Run resolves the host now and then every publicAddressRefresh until ctx
// ends. An address written in the URL itself is taken once.
func (p *PublicAddresses) Run(ctx context.Context) {
	if p == nil {
		return
	}
	for {
		p.Refresh(ctx)
		if p.literal.IsValid() {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-p.after(publicAddressRefresh):
		}
	}
}

// Refresh resolves the host once and keeps what it found in place of what
// was kept before; a failed resolution keeps none.
func (p *PublicAddresses) Refresh(ctx context.Context) {
	if p == nil {
		return
	}
	addrs, why := p.resolve(ctx)
	said := why
	if len(addrs) > 0 {
		said = "on " + joinAddrs(addrs)
	}
	p.mu.Lock()
	p.addrs = addrs
	changed := said != p.said
	p.said = said
	p.mu.Unlock()
	if !changed || p.logger == nil {
		return
	}
	host := strings.TrimSuffix(p.host, ".")
	if len(addrs) > 0 {
		p.logger.Info("throttle: a source at one of the public URL's addresses will be taken for hops too low",
			"host", host, "addresses", joinAddrs(addrs))
		return
	}
	p.logger.Info("throttle: no check of the hops against the public URL's addresses", "host", host, "reason", why)
}

// resolve returns the usable addresses of the host, or none and why.
func (p *PublicAddresses) resolve(ctx context.Context) (map[netip.Addr]bool, string) {
	if p.literal.IsValid() {
		addrs := usableAddrs([]netip.Addr{p.literal})
		if len(addrs) == 0 {
			return nil, "the address is loopback, link-local or unspecified"
		}
		return addrs, ""
	}
	ctx, cancel := context.WithTimeout(ctx, publicAddressLookup)
	defer cancel()
	canonical, err := p.resolver.LookupCNAME(ctx, p.host)
	if err != nil {
		return nil, "the host does not resolve: " + err.Error()
	}
	if !strings.EqualFold(strings.TrimSuffix(canonical, "."), strings.TrimSuffix(p.host, ".")) {
		return nil, "the host is a CNAME, as for a CDN or a cloud load balancer's name"
	}
	found, err := p.resolver.LookupNetIP(ctx, "ip", p.host)
	if err != nil {
		return nil, "the host does not resolve: " + err.Error()
	}
	addrs := usableAddrs(found)
	if len(addrs) == 0 {
		return nil, "the host resolves to no address but loopback, link-local or unspecified ones"
	}
	return addrs, ""
}

// Contains says whether source, an address as clientIP keys on it, is one
// of the host's. It does no lookup.
func (p *PublicAddresses) Contains(source string) bool {
	if p == nil {
		return false
	}
	addr, err := netip.ParseAddr(source)
	if err != nil {
		return false
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.addrs[addr.Unmap().WithZone("")]
}

// usableAddrs are the addresses of found a client's could be: neither
// loopback, link-local, multicast nor unspecified.
func usableAddrs(found []netip.Addr) map[netip.Addr]bool {
	addrs := make(map[netip.Addr]bool)
	for _, addr := range found {
		addr = addr.Unmap().WithZone("")
		if !addr.IsValid() || addr.IsLoopback() || addr.IsUnspecified() || addr.IsLinkLocalUnicast() || addr.IsMulticast() {
			continue
		}
		addrs[addr] = true
	}
	return addrs
}

// joinAddrs lists addrs in order, for the log.
func joinAddrs(addrs map[netip.Addr]bool) string {
	list := make([]netip.Addr, 0, len(addrs))
	for addr := range addrs {
		list = append(list, addr)
	}
	slices.SortFunc(list, netip.Addr.Compare)
	parts := make([]string, len(list))
	for i, addr := range list {
		parts[i] = addr.String()
	}
	return strings.Join(parts, ", ")
}
