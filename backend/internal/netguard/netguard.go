// Package netguard keeps the connections that users' settings aim, the
// checks of monitors and the deliveries of notification channels, away from
// addresses no monitor has any business with: the backend's own loopback,
// the link-local range with the cloud metadata services in it, multicast.
//
// Private ranges are deliberately not among them. Kubeat exists to check
// in-cluster services, and ClusterIPs, pod IPs and *.svc names resolve into
// 10.0.0.0/8, 172.16.0.0/12 (OpenShift's 172.30.0.0/16), 192.168.0.0/16 or
// fd00::/8; an operator who wants any of them closed adds it to the extra
// denied ranges.
//
// A Policy is applied as a connection is made, to the address actually
// connected to, so a name that resolves to a denied address, an answer that
// changes between two lookups and every redirect hop are held to it alike.
package netguard

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"syscall"
)

// defaultDeny are the ranges denied without any setting. Each holds
// something only the host itself, or the cloud it runs in, should talk to.
var defaultDeny = []netip.Prefix{
	netip.MustParsePrefix("127.0.0.0/8"), // loopback: the backend pod itself
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("0.0.0.0/8"), // "this host": 0.0.0.0 reaches loopback
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("169.254.0.0/16"), // link-local, with instance metadata at 169.254.169.254
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("fd00:ec2::254/128"),  // AWS instance metadata over IPv6
	netip.MustParsePrefix("100.100.100.200/32"), // Alibaba Cloud instance metadata
	netip.MustParsePrefix("224.0.0.0/4"),        // multicast
	netip.MustParsePrefix("ff00::/8"),
	netip.MustParsePrefix("255.255.255.255/32"), // broadcast
}

// nat64 is the well-known NAT64 prefix (RFC 6052): a gateway connects to
// the IPv4 address in the last 32 bits on the caller's behalf.
var nat64 = netip.MustParsePrefix("64:ff9b::/96")

// ErrBlocked is wrapped by the error about an address the policy refuses,
// so that errors.Is tells it from a failure of the target itself.
var ErrBlocked = errors.New("blocked by the target policy")

// Policy says where connections may go: anywhere but the denied ranges,
// unless an exception covers the address. A nil *Policy allows every
// address, which is what tests against local servers rely on.
type Policy struct {
	deny  []netip.Prefix // the built-in ranges and the extra ones
	extra []netip.Prefix // the extra ones alone, for the log
	allow []netip.Prefix
}

// New returns the policy that denies the built-in ranges and extraDeny,
// except for the ranges in allow: an exception wins over every denial, the
// built-in ones included. Each entry is a CIDR such as 10.0.0.0/8 or
// 127.0.0.1/32; an IPv4 range written in IPv6 form (::ffff:10.0.0.0/104)
// stands for the IPv4 range, since addresses are compared in that form.
func New(extraDeny, allow []string) (*Policy, error) {
	extra, err := parse(extraDeny)
	if err != nil {
		return nil, err
	}
	exceptions, err := parse(allow)
	if err != nil {
		return nil, err
	}
	return &Policy{deny: append(slices.Clone(defaultDeny), extra...), extra: extra, allow: exceptions}, nil
}

func parse(list []string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, entry := range list {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(entry)
		if err != nil {
			return nil, fmt.Errorf("%q is not a CIDR such as 10.0.0.0/8; a single address is written 10.0.0.1/32", entry)
		}
		if prefix.Addr().Is4In6() && prefix.Bits() >= 96 {
			prefix = netip.PrefixFrom(prefix.Addr().Unmap(), prefix.Bits()-96)
		}
		out = append(out, prefix.Masked())
	}
	return out, nil
}

// blockedError names the refused address and the denied range it is in.
type blockedError struct {
	addr   netip.Addr // as connected to
	form   netip.Addr // as found in the range: differs for a NAT64 address
	denied netip.Prefix
}

func (e *blockedError) Error() string {
	addr := e.addr.String()
	if e.form != e.addr {
		addr += " (" + e.form.String() + ")"
	}
	return fmt.Sprintf("%s is %s (%s); UPTIME_TARGET_ALLOW_CIDRS can allow it", addr, ErrBlocked, e.denied)
}

func (e *blockedError) Unwrap() error { return ErrBlocked }

// Check reports whether a connection may go to addr. The address is taken
// in its plain form: an IPv4 address written as IPv6 (::ffff:10.0.0.1) is
// that IPv4 address. One under the NAT64 prefix 64:ff9b::/96 is checked as
// the IPv4 address it carries too, since that is where a gateway connects.
func (p *Policy) Check(addr netip.Addr) error {
	if p == nil {
		return nil
	}
	// A zone would keep a link-local address out of every prefix.
	plain := addr.Unmap().WithZone("")
	if !plain.IsValid() {
		return fmt.Errorf("%w: there is no address to check", ErrBlocked)
	}
	forms := []netip.Addr{plain}
	if nat64.Contains(plain) {
		b := plain.As16()
		forms = append(forms, netip.AddrFrom4([4]byte(b[12:])))
	}
	for _, form := range forms {
		if denied, ok := containing(p.deny, form); ok && !contains(p.allow, form) {
			return &blockedError{addr: plain, form: form, denied: denied}
		}
	}
	return nil
}

func containing(list []netip.Prefix, addr netip.Addr) (netip.Prefix, bool) {
	for _, prefix := range list {
		if prefix.Contains(addr) {
			return prefix, true
		}
	}
	return netip.Prefix{}, false
}

func contains(list []netip.Prefix, addr netip.Addr) bool {
	_, ok := containing(list, addr)
	return ok
}

// Control is a net.Dialer's ControlContext. It runs for every connection
// the dialer attempts, with the resolved "ip:port" about to be connected
// to, and refuses a denied address before anything is sent to it.
func (p *Policy) Control(_ context.Context, _, address string, _ syscall.RawConn) error {
	if p == nil {
		return nil
	}
	addrPort, err := netip.ParseAddrPort(address)
	if err != nil {
		return fmt.Errorf("%w: %q is not an address that can be checked", ErrBlocked, address)
	}
	return p.Check(addrPort.Addr())
}

// Dialer returns a copy of base whose connections the policy checks; the
// copy's ControlContext is the policy's, and base's own Control functions
// are dropped. A nil policy returns a plain copy.
func (p *Policy) Dialer(base *net.Dialer) *net.Dialer {
	d := *base
	if p != nil {
		d.Control = nil
		d.ControlContext = p.Control
	}
	return &d
}

// CheckHost checks every address host resolves to. It is for connections
// that do not go to the host itself, where Control never sees its address:
// a request handed to a proxy, which then connects on its own. A name that
// does not resolve here is left to the proxy, which may know names this
// network does not.
func (p *Policy) CheckHost(ctx context.Context, host string) error {
	if p == nil {
		return nil
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return p.Check(addr)
	}
	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil
	}
	for _, addr := range addrs {
		if err := p.Check(addr); err != nil {
			return err
		}
	}
	return nil
}

// Proxy wraps the Proxy function of an http.Transport. A request sent
// through a proxy reaches its target from the proxy, so the connection the
// dialer checks is the one to the proxy; the target is checked here, before
// the request is handed over. A nil policy returns next as it is.
func (p *Policy) Proxy(next func(*http.Request) (*url.URL, error)) func(*http.Request) (*url.URL, error) {
	if p == nil || next == nil {
		return next
	}
	return func(req *http.Request) (*url.URL, error) {
		proxy, err := next(req)
		if err != nil || proxy == nil {
			return proxy, err
		}
		if err := p.CheckHost(req.Context(), req.URL.Hostname()); err != nil {
			return nil, err
		}
		return proxy, nil
	}
}

// Transport returns a copy of http.DefaultTransport held to the policy: it
// connects through Dialer, and a request it sends through a proxy from the
// environment has its target checked first.
func (p *Policy) Transport() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DialContext = p.Dialer(&net.Dialer{}).DialContext
	t.Proxy = p.Proxy(t.Proxy)
	return t
}

// LogValue is how the policy shows in a log: the ranges denied on top of
// the built-in ones, and the exceptions, as they are in effect.
func (p *Policy) LogValue() slog.Value {
	if p == nil {
		return slog.StringValue("off")
	}
	return slog.GroupValue(slog.Any("extra_deny", texts(p.extra)), slog.Any("allow", texts(p.allow)))
}

func texts(list []netip.Prefix) []string {
	out := make([]string, 0, len(list))
	for _, prefix := range list {
		out = append(out, prefix.String())
	}
	return out
}
