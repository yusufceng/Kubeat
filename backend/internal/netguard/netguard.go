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
// The endpoints the operator sets, a proxy or a mail server, are trusted at
// their exact address instead (see Trust).
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
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/net/http/httpproxy"
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
	// The metadata services outside link-local, each in a range that is
	// otherwise open: private IPv6 or public.
	netip.MustParsePrefix("fd00:ec2::254/128"),  // AWS instance metadata over IPv6
	netip.MustParsePrefix("fd20:ce::254/128"),   // Google Cloud instance metadata over IPv6
	netip.MustParsePrefix("168.63.129.16/32"),   // Azure's host agent (WireServer), also the platform's DNS
	netip.MustParsePrefix("100.100.100.200/32"), // Alibaba Cloud instance metadata
	netip.MustParsePrefix("224.0.0.0/4"),        // multicast
	netip.MustParsePrefix("ff00::/8"),
	netip.MustParsePrefix("255.255.255.255/32"), // broadcast
}

// nat64 is the well-known NAT64 prefix (RFC 6052): a gateway connects to
// the IPv4 address in the last 32 bits on the caller's behalf. A network
// with a prefix of its own (64:ff9b:1::/48 or another) needs the mapped
// forms of the denied ranges among the extra denials: where it puts the
// IPv4 address is its own choice.
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
	// trusted are the addresses, host:port as connections ask for them,
	// that go through whatever they resolve to (see Trust).
	trusted []string
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

// Trust returns a copy of the policy that lets a connection asked for at
// one of the given addresses through, whatever it resolves to. The policy
// is for the addresses users set; these are the operator's own: the proxy
// a request is handed to, the mail server, a notification service's API.
// Each is a host:port, compared with the address a connection is asked
// for before it is resolved, the host without regard to case; "" is
// skipped. That host and port alone are trusted, so a sidecar on the
// pod's loopback needs no exception, which would open the rest of
// loopback to every monitor and channel.
func (p *Policy) Trust(addresses ...string) *Policy {
	if p == nil {
		return nil
	}
	trusted := slices.Clone(p.trusted)
	for _, address := range addresses {
		if a := normalAddress(address); a != "" && !slices.Contains(trusted, a) {
			trusted = append(trusted, a)
		}
	}
	copied := *p
	copied.trusted = trusted
	return &copied
}

// trusts reports whether a connection asked for at address goes through
// unchecked.
func (p *Policy) trusts(address string) bool {
	return p != nil && len(p.trusted) > 0 && slices.Contains(p.trusted, normalAddress(address))
}

// normalAddress is a host:port as Trust compares it: the host in lower
// case, an IPv6 one in brackets. It is "" for anything else.
func normalAddress(address string) string {
	host, port, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil || host == "" || port == "" {
		return ""
	}
	return net.JoinHostPort(strings.ToLower(host), port)
}

// defaultPorts are the ports net/http connects to for a scheme that names
// none: a request's, and a proxy's.
var defaultPorts = map[string]string{"http": "80", "https": "443", "socks5": "1080", "socks5h": "1080"}

// hostPort is the address a connection for u goes to: its host, and its
// port or the one its scheme implies. It is "" when there is neither.
func hostPort(u *url.URL) string {
	if u == nil || u.Hostname() == "" {
		return ""
	}
	port := u.Port()
	if port == "" {
		port = defaultPorts[strings.ToLower(u.Scheme)]
	}
	if port == "" {
		return ""
	}
	return net.JoinHostPort(strings.ToLower(u.Hostname()), port)
}

// HostPort is the address a connection to the URL rawURL goes to, for
// Trust: the host:port of a service's address from the settings, such as
// https://api.telegram.org (api.telegram.org:443). It is "" for anything
// that is not a URL with a host.
func HostPort(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	return hostPort(u)
}

// EnvironmentProxies returns the addresses of the proxies HTTP_PROXY and
// HTTPS_PROXY (or their lower-case forms) name, read as
// http.ProxyFromEnvironment reads them: the operator's choice, for Trust.
func EnvironmentProxies() []string {
	config := httpproxy.FromEnvironment()
	// Every proxy named, whichever requests NO_PROXY keeps away from it.
	config.NoProxy = ""
	proxyFor := config.ProxyFunc()
	var out []string
	for _, scheme := range []string{"http", "https"} {
		proxy, err := proxyFor(&url.URL{Scheme: scheme, Host: "kubeat.invalid"})
		if addr := hostPort(proxy); err == nil && addr != "" && !slices.Contains(out, addr) {
			out = append(out, addr)
		}
	}
	return out
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

// Dialer connects as a net.Dialer does, held to a policy.
type Dialer struct {
	policy *Policy
	// checked has the policy's Control; plain connects to the addresses
	// the policy trusts.
	checked, plain net.Dialer
}

// Dialer returns a dialer that connects as base does, with each connection
// checked by the policy (Control) unless it is asked for at an address the
// policy trusts. base's own Control functions are dropped from the checked
// connections. A nil policy connects as base does.
func (p *Policy) Dialer(base *net.Dialer) *Dialer {
	d := &Dialer{policy: p, checked: *base, plain: *base}
	if p != nil {
		d.checked.Control = nil
		d.checked.ControlContext = p.Control
	}
	return d
}

// DialContext connects to address on the named network, as
// net.Dialer.DialContext does. Whether the address is trusted is decided
// on it as given, before it is resolved.
func (d *Dialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if d.policy.trusts(address) {
		return d.plain.DialContext(ctx, network, address)
	}
	return d.checked.DialContext(ctx, network, address)
}

// CheckHost checks every address host resolves to. It is for connections
// that do not go to the host itself, where Control never sees its address:
// a request handed to a proxy, which then connects on its own. A name that
// does not resolve here is left to the proxy, which may know names this
// network does not.
//
// A host made of numbers is not a name, though: a proxy that reads it as
// inet_aton does takes 2852039166, 0xa9.0xfe.0xa9.0xfe and 169.254.43518
// for 169.254.169.254, and 127.1 for 127.0.0.1, while none of them
// resolves here. Such a host is checked as the address it spells, and
// refused when it spells none.
//
// Trailing dots only root a name, and a proxy drops every one of them, as
// Squid does: 169.254.169.254.. is 169.254.169.254 to it, and localhost..
// is localhost, though neither resolves here. Such a host is checked as
// the proxy reads it: a name both without the dots, as the hosts file has
// it, and rooted, as DNS has it.
func (p *Policy) CheckHost(ctx context.Context, host string) error {
	if p == nil {
		return nil
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return p.Check(addr)
	}
	spelled := strings.TrimRight(host, ".")
	if numeric(spelled) {
		addr, ok := parseNumeric(spelled)
		if !ok {
			return fmt.Errorf("%w: %q is written as an IPv4 address but is not one", ErrBlocked, host)
		}
		if err := p.Check(addr); err != nil {
			return fmt.Errorf("%s: %w", host, err)
		}
		return nil
	}
	names := []string{host}
	if spelled != host {
		names = []string{spelled, spelled + "."}
	}
	for _, name := range names {
		addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", name)
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			if err := p.Check(addr); err != nil {
				return err
			}
		}
	}
	return nil
}

// numeric reports whether host is written the way inet_aton reads an IPv4
// address: one to four parts separated by dots, each decimal, octal (with
// a leading 0) or hexadecimal (with 0x). A part that is not a valid number
// of its kind, such as 08 or 0x alone, still counts: parseNumeric refuses
// it, where implementations of inet_aton disagree.
func numeric(host string) bool {
	parts := strings.Split(host, ".")
	if len(parts) > 4 {
		return false
	}
	for _, part := range parts {
		digits, hex := part, false
		if len(part) >= 2 && part[0] == '0' && (part[1] == 'x' || part[1] == 'X') {
			digits, hex = part[2:], true
		} else if part == "" {
			return false
		}
		for _, c := range digits {
			if !(c >= '0' && c <= '9' || hex && (c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F')) {
				return false
			}
		}
	}
	return true
}

// parseNumeric reads a numeric host as inet_aton does: every part but the
// last is one byte, and the last fills the bytes left, so 127.1 is
// 127.0.0.1 and a single part is the whole address.
func parseNumeric(host string) (netip.Addr, bool) {
	parts := strings.Split(host, ".")
	var ip uint32
	for i, part := range parts {
		base := 10
		switch {
		case len(part) >= 2 && part[0] == '0' && (part[1] == 'x' || part[1] == 'X'):
			base, part = 16, part[2:]
		case len(part) > 1 && part[0] == '0':
			base, part = 8, part[1:]
		}
		value, err := strconv.ParseUint(part, base, 32)
		if err != nil {
			return netip.Addr{}, false
		}
		// The bits from this part's on: all of them for the last part.
		bits := 32 - 8*i
		switch {
		case i < len(parts)-1 && value <= 0xff:
			ip |= uint32(value) << (bits - 8)
		case i == len(parts)-1 && (bits == 32 || value < 1<<bits):
			ip |= uint32(value)
		default:
			return netip.Addr{}, false
		}
	}
	return netip.AddrFrom4([4]byte{byte(ip >> 24), byte(ip >> 16), byte(ip >> 8), byte(ip)}), true
}

// Proxy wraps the Proxy function of an http.Transport. A request sent
// through a proxy reaches its target from the proxy, so the connection the
// dialer checks is the one to the proxy; the target is checked here, before
// the request is handed over, unless the policy trusts it. A nil policy
// returns next as it is.
func (p *Policy) Proxy(next func(*http.Request) (*url.URL, error)) func(*http.Request) (*url.URL, error) {
	if p == nil || next == nil {
		return next
	}
	return func(req *http.Request) (*url.URL, error) {
		proxy, err := next(req)
		if err != nil || proxy == nil || p.trusts(hostPort(req.URL)) {
			return proxy, err
		}
		if err := p.CheckHost(req.Context(), req.URL.Hostname()); err != nil {
			return nil, err
		}
		// A plain http:// request goes to the proxy in absolute form, and
		// net/http writes the Host header there, not the URL's host: that
		// is the address the proxy fetches. Over https the proxy is asked
		// to CONNECT to the URL's host, and the header stays in the tunnel.
		if req.URL.Scheme == "http" && req.Host != "" {
			host := req.Host
			if h, _, err := net.SplitHostPort(host); err == nil {
				host = h
			}
			host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
			if !strings.EqualFold(host, req.URL.Hostname()) {
				if err := p.CheckHost(req.Context(), host); err != nil {
					return nil, err
				}
			}
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
// the built-in ones, the exceptions and the trusted addresses, as they
// are in effect.
func (p *Policy) LogValue() slog.Value {
	if p == nil {
		return slog.StringValue("off")
	}
	return slog.GroupValue(slog.Any("extra_deny", texts(p.extra)), slog.Any("allow", texts(p.allow)),
		slog.Any("trusted", append([]string{}, p.trusted...)))
}

func texts(list []netip.Prefix) []string {
	out := make([]string, 0, len(list))
	for _, prefix := range list {
		out = append(out, prefix.String())
	}
	return out
}
