package checker

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"

	"github.com/yusufceng/Kubeat/backend/internal/explain"
	"github.com/yusufceng/Kubeat/backend/internal/netguard"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// The target scope of a user limited to some namespaces holds the checks of
// the monitors they set up, every connection a check makes, not only the
// target the API judged when they set it (ADR-0003 c, 0.51). A monitor's
// record of the scope (store.Monitor.Scope, or for one without a record its
// namespace's; store.Monitor.TargetScope) says which namespaces; the rule is
// netguard's, the one the API asks (netguard.ScopeRules). A check one of
// whose connections the scope refuses fails with the scope's refusal: down,
// and not the failure an inverted monitor expects.

// ScopeOptions configures how the checks are held to their monitors' scopes.
type ScopeOptions struct {
	// Rules are the operator's parts of the rule: the company ranges, the
	// resolver's bound and the proxy exception, as the API has them.
	Rules netguard.ScopeRules
	// Refused, if set, is told of every check the scope refused a
	// connection of, with the first host it refused. first is set for the
	// first refusal of the monitor's present settings since the checker
	// started, which is the one to write down: a monitor refused check
	// after check is one finding, and its row is written once, not every
	// check; a change of where it connects, a pause or a restart makes the
	// next refusal a first one again.
	Refused func(m store.Monitor, host string, first bool)
}

// ScopeActor is who the audit entries of the checks' refusals name: the
// checker, not a user.
const ScopeActor = "checker"

// scopeRefusalText is the error of a check the scope refused: the same for
// every refusal, naming neither the host nor the reason, so that the
// monitor's readers do not learn where a name leads (ADR-0003 d).
const scopeRefusalText = "refused by the target scope: a connection of this check leads outside the namespaces of whoever set up the monitor; a Service in one of them is reached as service.namespace.svc, anything else is set up by an administrator"

// errScopeRefusal is the error of such a check; errors.Is finds
// netguard.ErrScope in it.
var errScopeRefusal error = scopeRefusal{}

type scopeRefusal struct{}

func (scopeRefusal) Error() string { return scopeRefusalText }
func (scopeRefusal) Unwrap() error { return netguard.ErrScope }

// ScopeAuditSummary is the audit entry of a check the scope refused, in the
// language of the audit log's fixed texts ("tr" or English): the monitor and
// the host refused.
func ScopeAuditSummary(language, monitor, host string) string {
	if language == "tr" {
		return fmt.Sprintf("%s monitor'ü: kontrolünün %s bağlantısı reddedildi, monitor'ü kuranın namespace'lerinin dışında", monitor, host)
	}
	return fmt.Sprintf("monitor %s: its check was refused a connection to %s, outside the namespaces of whoever set it up", monitor, host)
}

// scopeKey carries the scope of the check under way in its context.
type scopeKey struct{}

// scopeOf returns the scope the check of ctx is held to; nil for none.
func scopeOf(ctx context.Context) *netguard.Scope {
	s, _ := ctx.Value(scopeKey{}).(*netguard.Scope)
	return s
}

// scope returns the scope a check of m is held to, or nil when its checks
// are held to none: a monitor whose record holds every namespace, or one
// whose check connects nowhere.
func (c *Checker) scope(m store.Monitor) *netguard.Scope {
	if !m.Outbound() {
		return nil
	}
	record := m.TargetScope()
	if record.All() {
		return nil
	}
	canRead := func(ns string) bool { return ns != "" && slices.Contains(record.Namespaces, ns) }
	own := ""
	if record.Discovered && canRead(m.Namespace) {
		own = m.Host()
	}
	named := []string{m.Host()}
	if m.Type == store.MonitorDNS {
		named = append(named, m.DNS.ResolverHost())
	}
	return c.opts.Scope.Rules.Connections(canRead, own, named...)
}

// drop forgets what the checker keeps of monitor id between its checks held
// to a scope: the transport, closing its idle connections, and the settings
// it was last refused under.
func (c *Checker) drop(id int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dropLocked(id)
}

func (c *Checker) dropLocked(id int64) {
	if held, ok := c.held[id]; ok {
		held.transport.CloseIdleConnections()
		delete(c.held, id)
	}
	delete(c.reported, id)
}

// Prune forgets what the checker keeps of the monitors not among enabled,
// those deleted or paused, as a check of one does of a monitor no longer
// held to a scope: the scheduler calls it every tick, so that what is kept
// is bounded by the monitors there are, a check that finished after its
// monitor went included.
func (c *Checker) Prune(enabled map[int64]bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id := range c.held {
		if !enabled[id] {
			c.dropLocked(id)
		}
	}
	for id := range c.reported {
		if !enabled[id] {
			delete(c.reported, id)
		}
	}
}

// spelledOut reports whether a host m's check names before it connects is
// refused by its spelling. A target with no host to read is left to the
// check, which fails on it as it would without a scope.
func spelledOut(s *netguard.Scope, m store.Monitor) bool {
	if host := m.Host(); host != "" && s.Host(host) != nil {
		return true
	}
	if host := m.DNS.ResolverHost(); m.Type == store.MonitorDNS && host != "" {
		return s.Host(host) != nil
	}
	return false
}

// refusedByScope is the outcome of a check of m the scope refused a
// connection of, host the first one: its error says only that, and its
// diagnostics only what it means, without the address or the phases of the
// connection refused. The connections the check made before stay its hops.
func (c *Checker) refusedByScope(m store.Monitor, out outcome, host string) outcome {
	key := scopeSettings(m)
	c.mu.Lock()
	first := c.reported[m.ID] != key
	c.reported[m.ID] = key
	c.mu.Unlock()
	if c.opts.Scope.Refused != nil {
		c.opts.Scope.Refused(m, host, first)
	}
	return outcome{
		err:  errScopeRefusal,
		diag: &store.Diagnostics{Explanation: &store.Explanation{Code: explain.BlockedByScope}},
		hops: out.hops,
	}
}

// scopeSettings are the settings of m that decide where its check connects
// and what holds it, as one key: a change of any of them is a new finding.
func scopeSettings(m store.Monitor) string {
	return fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%v", m.Type, m.Target, m.Destination(), m.DNS.Resolver, m.TargetScope())
}

// heldTransport is the transport of a monitor held to a scope, under the
// settings it was made for.
type heldTransport struct {
	key       string
	transport *http.Transport
}

// heldTransport returns the transport the HTTP checks of m, held to a scope,
// send their requests with: m's own, made from base and kept between its
// checks under the same settings, so that a check never gets a connection
// made for another monitor, which may be held to another scope or to none.
// It keeps no connection between the checks either: each closes the ones it
// opened when it ends (checkHTTPAt), so that a check of m opens its own as
// every check does. A connection it makes, and every request it hands to a
// proxy, is held to the scope of the check under way (scopeOf).
func (c *Checker) heldTransport(m store.Monitor, base *http.Transport, skipVerify bool) *http.Transport {
	key := fmt.Sprintf("%t\x00%s", skipVerify, scopeSettings(m))
	c.mu.Lock()
	defer c.mu.Unlock()
	held, ok := c.held[m.ID]
	if ok && held.key == key {
		return held.transport
	}
	if ok {
		held.transport.CloseIdleConnections()
	}
	t := base.Clone()
	next := t.Proxy
	t.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return c.dialer.In(scopeOf(ctx), "").DialContext(ctx, network, address)
	}
	t.Proxy = func(req *http.Request) (*url.URL, error) { return scopeOf(req.Context()).Proxy(next)(req) }
	c.held[m.ID] = heldTransport{key: key, transport: t}
	return t
}

// eachPodHost is the Service a check of each pod reaches the pods of, as the
// scope reads it: "name.namespace.svc", from the monitor's source.
func eachPodHost(m store.Monitor) string {
	name := strings.TrimPrefix(m.Source, "service/")
	if i := strings.LastIndexByte(name, ':'); i >= 0 {
		name = name[:i]
	}
	return name + "." + m.Namespace + ".svc"
}

// answersInScope reports, as ErrScope or nil, whether the records a DNS
// check of m found may be read by its scope: the addresses of a name that is
// external by its spelling must be outside the cluster, and the names it
// found must not be another namespace's, as the target itself must not be.
func answersInScope(s *netguard.Scope, m store.Monitor, records []string) error {
	if s == nil {
		return nil
	}
	for _, record := range records {
		var err error
		switch m.DNS.RecordType {
		case "A", "AAAA":
			if addr, parseErr := netip.ParseAddr(record); parseErr == nil {
				err = s.Addr(m.Target, addr)
			}
		case "CNAME", "NS", "PTR":
			err = s.Host(record)
		case "MX":
			_, host, _ := strings.Cut(record, " ")
			err = s.Host(host)
		case "SRV":
			if host, _, splitErr := net.SplitHostPort(record); splitErr == nil {
				err = s.Host(host)
			}
		}
		if err != nil {
			return err
		}
	}
	return nil
}
