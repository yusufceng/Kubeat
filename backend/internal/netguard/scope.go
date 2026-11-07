package netguard

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"
)

// The per-user target scope (ADR-0003 c): a user limited to some namespaces
// may aim a monitor's check or a channel's delivery at a cluster-internal
// address only when the namespace its name carries is one of theirs. A
// deny-CIDR cannot separate tenants, because the API server and every team's
// Service share the ClusterIP range; the scope is the real cross-tenant
// defence. ScopeRules holds the rule, the one every scope check asks: the
// API when a target is set (ScopeRules.Refuses), and the checker for every
// connection a check makes (Scope).

// ErrScope is wrapped by the error about a connection the per-user target
// scope refuses, so that errors.Is tells it from a failure of the target and
// from the target policy's refusal (ErrBlocked). Its words say nothing about
// the host or why: a check's result is read by everyone who may see the
// monitor, and a reason per refusal would tell them where a name resolves.
var ErrScope = errors.New("outside the target scope")

// ScopeRules are the operator's parts of the scope check: the company ranges
// that count as outside the cluster (UPTIME_TARGET_SCOPE_EXTERNAL_CIDRS),
// the resolver and its bound (UPTIME_TARGET_RESOLVE_TIMEOUT), and the proxy
// exception (UPTIME_TARGET_SCOPE_TRUST_PROXY). The zero value holds no
// company range, resolves with net.DefaultResolver without a bound of its
// own and makes no exception.
type ScopeRules struct {
	External ExternalRanges
	// Resolver looks external-looking names up; nil is net.DefaultResolver,
	// the pod's own resolver with its search domains (ndots), which is what
	// a check would dial through.
	Resolver Resolver
	// ResolveTimeout bounds that lookup; zero leaves it to the context.
	ResolveTimeout time.Duration
	// TrustProxy lets an external-looking name that does not exist here
	// (NXDOMAIN) through when the request is handed to the proxy, which
	// resolves it outside the cluster.
	TrustProxy bool
	// Proxy decides whether a request goes to a proxy, for that exception;
	// nil is http.ProxyFromEnvironment, the checker's and the senders' own
	// choice.
	Proxy func(*http.Request) (*url.URL, error)
}

// ScopeHost is host as the scope check reads it, and as an audit entry of a
// refused target names it: in lower case, without surrounding space and
// without every trailing dot, as a proxy and the resolver read a name. It is
// "" for a host made of dots alone.
func ScopeHost(host string) string {
	return strings.ToLower(strings.TrimRight(strings.TrimSpace(host), "."))
}

// hostClass is how the scope reads a host by its spelling alone.
type hostClass int

const (
	// hostRefused: never reached by a limited scope, whatever it resolves
	// to: nothing but dots, a host that is not ASCII, a number not written
	// as an address, or a cluster-internal host whose name carries none of
	// the scope's namespaces (a bare ClusterIP or pod IP, a short name,
	// another team's Service, the API server).
	hostRefused hostClass = iota
	// hostInScope: a cluster-internal name that carries one of the scope's
	// namespaces. It reaches that namespace's Service or pod, whatever
	// address that is.
	hostInScope
	// hostExternal: external by its spelling, an address outside the
	// cluster among them; in scope only where it lands outside the cluster.
	hostExternal
)

// classify reads host for a scope that holds the namespaces canRead accepts,
// and returns it as ScopeHost reads it.
//
// The host is read as the proxy and the resolver read it: in lower case and
// without its trailing dots, which only root a name and which a proxy drops,
// as Squid does. A host made of numbers that is not an address as written
// (172.30.1, 0xac.0x1e.0.1 and 2887647233 all spell 172.30.0.1 to inet_aton;
// Numeric) is refused: a proxy and glibc read it as an address, the pure-Go
// resolver as a name that does not exist, and readers disagree on some forms
// (010 is 8 or 10), so the address it reaches cannot be told. A host that is
// not ASCII is refused too: net/http and NO_PROXY's matching take it through
// IDNA first, which drops a soft hyphen (U+00AD) or a zero-width space
// (U+200B) and turns the Unicode full stops (U+3002, U+FF0E, U+FF61) into
// dots, while the pure-Go resolver refuses it without asking. An
// international name is written in its xn-- form.
func (r ScopeRules) classify(host string, canRead func(string) bool) (string, hostClass) {
	name := ScopeHost(host)
	if name == "" {
		return name, hostRefused // nothing but dots: no host at all
	}
	for i := 0; i < len(host); i++ {
		if host[i] >= 0x80 {
			return name, hostRefused // not ASCII: the proxy reads it through IDNA
		}
	}
	if _, err := netip.ParseAddr(name); err != nil && numeric(name) {
		return name, hostRefused // a number not written as an address
	}
	if r.External.InCluster(name) {
		if ns, ok := Namespace(name); ok && canRead(ns) {
			return name, hostInScope
		}
		return name, hostRefused
	}
	return name, hostExternal
}

// Refuses reports whether a caller who holds only the namespaces canRead
// accepts is refused host: a monitor's target host, a channel webhook's, or
// another endpoint a monitor's check connects to. request is the URL of the
// HTTP request sent to host, or nil for a connection made to host directly;
// it matters only for the proxy exception. The caller decides who is limited
// at all: one who sees every namespace is never asked.
//
// The check is made at the target's reachability, not only its spelling
// (dial-time; ADR-0003 c):
//
//   - A host that is cluster-internal by its name (ExternalRanges.InCluster:
//     an address in a cluster range and not in the company's ranges, a *.svc
//     or pod name, a short name) is let through only when the namespace its
//     name carries is one of theirs.
//   - The company's ranges (External) are outside the cluster for this check
//     although they are private: an address in them, written bare or one an
//     external-looking name resolves to, is let through as a public one is.
//     Only addresses are read so: a *.svc or pod name and a short name stay
//     cluster-internal by their spelling whatever they resolve to, and a name
//     with one answer inside the cluster beside the company's is refused.
//   - A host that is external by its name is let through only when it
//     provably resolves to addresses outside the cluster. A name that
//     resolves into the cluster is refused — a two-label short name the
//     pod's search domains turn into a Service ("api.payments"), or a public
//     name pointing at a cluster address ("172-30-0-1.sslip.io") — and so is
//     a name the resolver cannot answer for (an outage, a SERVFAIL, a
//     timeout, an NXDOMAIN): it cannot be proven external.
//   - The one exception is an NXDOMAIN for a request handed to the proxy,
//     behind TrustProxy: a local NXDOMAIN says nothing about where the proxy
//     goes. The request goes to the proxy when Proxied says so (HTTP_PROXY,
//     HTTPS_PROXY, NO_PROXY), and the host must be written without trailing
//     dots, since NO_PROXY reads a host as written.
func (r ScopeRules) Refuses(ctx context.Context, canRead func(string) bool, host string, request *url.URL) bool {
	name, class := r.classify(host, canRead)
	switch class {
	case hostRefused:
		return true
	case hostInScope:
		return false
	}
	return r.resolvesRefused(ctx, name, request)
}

// resolvesRefused is Refuses for a name that is external by its spelling.
func (r ScopeRules) resolvesRefused(ctx context.Context, name string, request *url.URL) bool {
	// Bound the resolution so a slow or stuck resolver cannot hold the
	// caller open.
	if r.ResolveTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.ResolveTimeout)
		defer cancel()
	}
	inCluster, err := r.External.ResolvesInCluster(ctx, r.Resolver, name)
	switch {
	case inCluster:
		return true
	case err == nil:
		return false
	case r.TrustProxy && NotFound(err) && Proxied(r.Proxy, request) && !strings.HasSuffix(request.Hostname(), "."):
		return false
	}
	return true
}

// Scope is the target scope one check of a monitor is held to, for every
// connection the check makes, not only for the target it was given when it
// was set: the target scope is applied where the check connects (ADR-0003 c,
// 0.51). It is made for one check (ScopeRules.Connections) and remembers
// whether it refused anything, so that the check fails with the scope's
// refusal whatever the library in between made of it. It is safe for
// concurrent use.
//
// A connection is judged by the same reading as a target set through the
// API (ScopeRules.Refuses): a host refused by its spelling is refused before
// anything is sent; a cluster-internal name in one of the scope's namespaces
// reaches what it names; an external-looking one may connect only to an
// address outside the cluster, which the dialer checks on the address it
// connects to (Dialer.In), so the answer the connection uses is the one
// judged. A request handed to a proxy never connects to its host here: its
// hosts are held to the full rule, resolution and the proxy exception
// included, before it is handed over (Scope.Proxy).
type Scope struct {
	rules   ScopeRules
	canRead func(string) bool
	// own is the monitor's own host when its target was taken from a
	// cluster object of its namespace (discovery), which is in scope by
	// that namespace as an import judges it; "" otherwise.
	own string

	mu      sync.Mutex
	refused bool
	host    string // the first host refused
}

// Connections returns the scope of one check, holding the namespaces canRead
// accepts. own is the monitor's own host when it is in scope by the
// monitor's namespace whatever it resolves to (a target taken from a cluster
// object), or "".
func (r ScopeRules) Connections(canRead func(string) bool, own string) *Scope {
	if own != "" {
		own = ScopeHost(own)
	}
	return &Scope{rules: r, canRead: canRead, own: own}
}

// class reads host for this scope.
func (s *Scope) class(host string) (string, hostClass) {
	if s.own != "" && ScopeHost(host) == s.own {
		return s.own, hostInScope
	}
	return s.rules.classify(host, s.canRead)
}

// refuse notes that a connection to host was refused and returns the error.
func (s *Scope) refuse(host string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.refused {
		s.refused, s.host = true, ScopeHost(host)
	}
	return ErrScope
}

// Refused reports whether the scope refused any connection, and the first
// host it refused, for the operator's log and the audit entry: a nil Scope
// refused nothing.
func (s *Scope) Refused() (string, bool) {
	if s == nil {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.host, s.refused
}

// Host reports whether a check may name host at all, as ErrScope or nil: it
// is refused by its spelling. A host that may be reached still has its
// connections held to the scope (Dialer.In, Proxy, Addr). A nil Scope allows
// every host.
func (s *Scope) Host(host string) error {
	if s == nil {
		return nil
	}
	if _, class := s.class(host); class == hostRefused {
		return s.refuse(host)
	}
	return nil
}

// Addr reports whether a check of host may reach addr, an address host
// resolved to, as ErrScope or nil: for a check that reaches an address
// without a dialer of the checker's, or that reads addresses for its
// answer. A nil Scope allows every address.
func (s *Scope) Addr(host string, addr netip.Addr) error {
	if s == nil {
		return nil
	}
	_, class := s.class(host)
	switch {
	case class == hostRefused:
		return s.refuse(host)
	case class == hostExternal && s.rules.External.InClusterAddr(addr):
		return s.refuse(host)
	}
	return nil
}

// Proxy wraps the Proxy function of an http.Transport. A request handed to a
// proxy reaches its hosts from the proxy, so the dialer sees only the
// proxy's address: the hosts the proxy is asked for (proxiedHosts) are held
// to the full rule before the request is handed over, the resolution and
// the proxy exception included. A request that goes to its host directly is
// left to the dialer. A nil Scope returns next as it is.
func (s *Scope) Proxy(next func(*http.Request) (*url.URL, error)) func(*http.Request) (*url.URL, error) {
	if s == nil || next == nil {
		return next
	}
	return func(req *http.Request) (*url.URL, error) {
		proxy, err := next(req)
		if err != nil || proxy == nil {
			return proxy, err
		}
		for _, host := range proxiedHosts(req) {
			name, class := s.class(host)
			if class == hostRefused || class == hostExternal && s.rules.resolvesRefused(req.Context(), name, req.URL) {
				return nil, s.refuse(host)
			}
		}
		return proxy, nil
	}
}

// proxiedHosts are the hosts a proxy connects to for req: the URL's, and for
// a plain http:// request, which goes to the proxy in absolute form with the
// Host header net/http writes there, that header's host where it differs.
// Over https the proxy is asked to CONNECT to the URL's host, and the header
// stays in the tunnel.
func proxiedHosts(req *http.Request) []string {
	hosts := []string{req.URL.Hostname()}
	if req.URL.Scheme == "http" && req.Host != "" {
		host := req.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
		if !strings.EqualFold(host, req.URL.Hostname()) {
			hosts = append(hosts, host)
		}
	}
	return hosts
}

// In returns a dialer that connects as d does, with each connection also held
// to the scope s: a host refused by its spelling is not dialed, and one that
// is external by its spelling may connect only to an address outside the
// cluster, checked on the address connected to. as is the host every
// connection is for, where the address dialed does not say: one an engine
// resolved itself, or a pod behind a Service; "" reads the host of each
// address dialed. An address the policy trusts, the proxy's, is reached as
// d reaches it. A nil s returns d.
func (d *Dialer) In(s *Scope, as string) *Dialer {
	if s == nil {
		return d
	}
	scoped := *d
	scoped.scope, scoped.as = s, as
	return &scoped
}

// dialScoped connects to address for host, held to d's scope.
func (d *Dialer) dialScoped(ctx context.Context, network, address string) (net.Conn, error) {
	host := d.as
	if host == "" {
		host = address
		if h, _, err := net.SplitHostPort(address); err == nil {
			host = h
		}
	}
	switch _, class := d.scope.class(host); class {
	case hostRefused:
		return nil, d.scope.refuse(host)
	case hostInScope:
		return d.checked.DialContext(ctx, network, address)
	}
	// External by its spelling: wherever it resolves, the address connected
	// to must be outside the cluster.
	external := d.checked
	control, controlContext := external.Control, external.ControlContext
	external.Control = nil
	external.ControlContext = func(ctx context.Context, network, address string, c syscall.RawConn) error {
		var err error
		switch {
		case controlContext != nil: // the policy's (Policy.Control)
			err = controlContext(ctx, network, address, c)
		case control != nil:
			err = control(network, address, c)
		}
		if err != nil {
			return err
		}
		addrPort, err := netip.ParseAddrPort(address)
		if err != nil {
			return d.scope.refuse(host)
		}
		return d.scope.Addr(host, addrPort.Addr())
	}
	return external.DialContext(ctx, network, address)
}
