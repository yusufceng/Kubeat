package api

import (
	"context"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"strings"

	"github.com/yusufceng/Kubeat/backend/internal/auth"
	"github.com/yusufceng/Kubeat/backend/internal/metrics"
	"github.com/yusufceng/Kubeat/backend/internal/netguard"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// Who may aim a monitor's check or a channel's delivery at a cluster-internal
// address (ADR-0003 c). A deny-CIDR cannot separate tenants, because the API
// server and every team's Service share the ClusterIP range; the scope is
// the real cross-tenant defence.

// codeTargetScope is the machine-readable code a scope refusal carries in
// the error body, so the frontend can map it to a translated message without
// matching on the English wording.
const codeTargetScope = "target_scope"

// scopeRefusal is why a target is outside the caller's scope. It carries no
// code of its own: every scope refusal uses codeTargetScope, written by the
// handler beside the body's message.
type scopeRefusal struct{ msg string }

func (e *scopeRefusal) Error() string { return e.msg }

// scopeRefused is the single body message every scope refusal carries,
// whatever the reason — a cluster-internal address whose namespace is not in
// the caller's scope, an address or short name that carries no namespace at
// all, an external-looking name that resolves into the cluster, or a name
// the resolver could not answer for (an outage or a timeout; see
// targetInScope). The reasons are deliberately not told apart in the body: a
// message per reason would tell a scoped caller whether an arbitrary name
// resolves into the cluster, or which namespace it lands in — an
// internal-address oracle. The machine-readable codeTargetScope still rides
// along, and the frontend maps it to its own help text, so collapsing the
// wording changes nothing a client reads by code.
const scopeRefused = "the target is outside your namespaces: a monitor's check or a channel's delivery may reach only a Service in a namespace you can read, named service.namespace.svc, or an address outside the cluster; ask an administrator for anything else"

// targetInScope reports why host — a monitor's target host or a channel
// webhook's host — is outside the caller's namespaces, or nil when the caller
// may aim a check or a delivery at it. A non-nil error is a *scopeRefusal.
// request is the URL of the HTTP request the check or the delivery sends to
// host (monitorRequestURL, channelRequestURL), or nil for one that connects
// to host directly; it matters only for the proxy exception below.
//
// A caller who sees every namespace (an administrator, the service token, an
// editor or a viewer of all) reaches any target, as before, so a
// single-tenant install is unchanged.
//
// For a caller limited to some namespaces the check is made at the target's
// reachability, not only its spelling (dial-time; ADR-0003 c, "Kapsamın kalan
// boşluğu" closed in 0.48):
//
//   - A host that is cluster-internal by its name (ExternalRanges.InCluster:
//     an address in a cluster range and not in the company's ranges below, a
//     *.svc or pod name, a short name) is let through only when the namespace
//     its name carries is one of theirs. The API server, another team's
//     Service and a bare ClusterIP — which carries no namespace — are
//     refused.
//   - The company's own ranges, which the operator lists in
//     Options.ScopeExternal (UPTIME_TARGET_SCOPE_EXTERNAL_CIDRS, empty by
//     default), are outside the cluster for this check although they are
//     private: an address in them, written bare (10.50.1.1) or one an
//     external-looking name resolves to, is let through as a public one is.
//     Only addresses are read so: a *.svc or pod name and a short name stay
//     cluster-internal by their spelling whatever they resolve to, and a
//     name with one answer inside the cluster beside the company's is
//     refused. netguard.ExternalRanges keeps the rule, the one every scope
//     check asks, and config.Load refuses ranges that hold the API server's
//     address or the backend's own.
//   - A host that is external by its name is let through only when it provably
//     resolves to addresses outside the cluster. A name that resolves into the
//     cluster is refused — a two-label short name the pod's search domains turn
//     into a Service ("api.payments", "kubernetes.default"), or a public name
//     pointing at a cluster address ("172-30-0-1.sslip.io") — because its name
//     carries no namespace the resolver honoured, so it cannot be attributed to
//     the caller. A name the resolver cannot answer for — a CoreDNS outage, a
//     SERVFAIL, a timeout, or an NXDOMAIN — is refused too: it cannot be
//     proven external, and accepting it would leave the hole this closes,
//     where a scoped caller registers an external-looking name during a
//     resolver outage and the checker dials it into the cluster once the
//     resolver recovers (ADR-0003 c, resolver-error fail-safe). The resolution
//     is bounded by Options.ResolveTimeout (a few seconds) so a slow resolver
//     does not hold a create or an edit open to the server's write timeout.
//   - The one exception is an NXDOMAIN for a request handed to the proxy,
//     behind Options.ScopeTrustProxy (UPTIME_TARGET_SCOPE_TRUST_PROXY, off by
//     default). Where only the operator's proxy, outside the cluster, resolves
//     external names, a local NXDOMAIN says nothing about where the request
//     goes: the proxy resolves the name and connects, the backend never dials
//     it. The request goes to the proxy when netguard.Proxied says so, the
//     choice the checker's and the senders' transports make (HTTP_PROXY,
//     HTTPS_PROXY, NO_PROXY). The name is still resolved, so one that
//     resolves into the cluster is still refused, and so are a SERVFAIL and a
//     timeout. A request NO_PROXY keeps away from the proxy, and a type that
//     connects directly (TCP, DNS, gRPC, ping, the databases, a check of each
//     pod), gets no exception.
//
// The host is first read as the proxy and the resolver read it (scopeHost):
// in lower case and without its trailing dots, which only root a name and
// which a proxy drops, as Squid does. A host made of numbers that is not an
// address as written (172.30.1, 0xac.0x1e.0.1 and 2887647233 all spell
// 172.30.0.1 to inet_aton; netguard.Numeric) is refused: a proxy and glibc
// read it as an address, the pure-Go resolver as a name that does not exist,
// and readers disagree on some forms (010 is 8 or 10), so the address it
// reaches cannot be told. Read as written instead, "api.payments.svc.." or
// "172.30.0.1.." would be an external name the resolver refuses without
// asking (an NXDOMAIN, which the proxy exception accepts) and NO_PROXY's
// patterns do not match, and "172.30.1" an external name that does not
// resolve, while the proxy connects to the Service or the ClusterIP they
// spell. The proxy exception also wants the host written without trailing
// dots: NO_PROXY reads such a host as written, so the request would go to
// the proxy for a name NO_PROXY means to keep away from it. A host that is
// not ASCII is refused too, before it is classified: net/http and NO_PROXY's
// matching take it through IDNA first, which drops a soft hyphen (U+00AD)
// or a zero-width space (U+200B) and turns the Unicode full stops (U+3002,
// U+FF0E, U+FF61) into dots, so "kubernetes.default­" reaches the API
// server, while the pure-Go resolver refuses it without asking (an
// NXDOMAIN). HTTP, WebSocket, ping and DNS monitors and the other webhook
// types are held to ASCII already (asciiHost, validHost); an ntfy channel's
// URL and a host:port target are not. An international name is written in
// its xn-- form.
//
// An external target that resolves externally (the public Internet, the
// company ranges of Options.ScopeExternal, an OpenShift Route's public host)
// is left to the target policy, unchanged. Options.Resolver is the backend's own
// (net.DefaultResolver when nil); only a scoped caller's external-looking
// name is resolved, so single-tenant installs and administrators never pay a
// lookup. A refusal carries the single generic scopeRefused message, so the
// body does not become an internal-address oracle.
func (h *handler) targetInScope(ctx context.Context, p auth.Principal, host string, request *url.URL) error {
	if p.SeesAll() || host == "" {
		return nil
	}
	name := scopeHost(host)
	if name == "" {
		return &scopeRefusal{scopeRefused} // nothing but dots: no host at all
	}
	for i := 0; i < len(host); i++ {
		if host[i] >= 0x80 {
			return &scopeRefusal{scopeRefused} // not ASCII: the proxy reads it through IDNA
		}
	}
	if _, err := netip.ParseAddr(name); err != nil && netguard.Numeric(name) {
		return &scopeRefusal{scopeRefused} // a number not written as an address
	}
	if h.opts.ScopeExternal.InCluster(name) {
		if ns, ok := netguard.Namespace(name); !ok || !p.CanRead(ns) {
			return &scopeRefusal{scopeRefused}
		}
		return nil
	}
	// host is external by its spelling. Bound the resolution so a slow or
	// stuck resolver cannot hold the request open to HTTPWriteTimeout.
	if h.opts.ResolveTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, h.opts.ResolveTimeout)
		defer cancel()
	}
	inCluster, err := h.opts.ScopeExternal.ResolvesInCluster(ctx, h.opts.Resolver, name)
	switch {
	case inCluster:
		return &scopeRefusal{scopeRefused}
	case err == nil:
		return nil
	case h.opts.ScopeTrustProxy && netguard.NotFound(err) && netguard.Proxied(h.opts.Proxy, request) &&
		!strings.HasSuffix(request.Hostname(), "."):
		return nil
	}
	return &scopeRefusal{scopeRefused}
}

// scopeHost is host as the scope check reads it, and as the audit entry of a
// refused target names it: in lower case, without surrounding space and
// without every trailing dot, as a proxy and the resolver read a name. It is
// "" for a host made of dots alone.
func scopeHost(host string) string {
	return strings.ToLower(strings.TrimRight(strings.TrimSpace(host), "."))
}

// monitorRequestURL is the URL of the HTTP request a monitor's check sends,
// the one a proxy may be handed: an HTTP monitor's target (keyword and JSON
// checks are HTTP monitors), and a WebSocket monitor's with ws and wss read
// as http and https, as the checker sends the handshake. It is nil for a type
// that connects to its target directly (TCP, DNS, gRPC, ping, the
// databases), for a check of each pod, which goes to the pods' addresses,
// and for a target that is not such a URL.
func monitorRequestURL(m store.Monitor) *url.URL {
	if m.EachPod || (m.Type != store.MonitorHTTP && m.Type != store.MonitorWebSocket) {
		return nil
	}
	u, err := url.Parse(m.Target)
	if err != nil {
		return nil
	}
	switch {
	case m.Type == store.MonitorWebSocket && u.Scheme == "ws":
		u.Scheme = "http"
	case m.Type == store.MonitorWebSocket && u.Scheme == "wss":
		u.Scheme = "https"
	case m.Type == store.MonitorWebSocket, u.Scheme != "http" && u.Scheme != "https":
		return nil
	}
	return u
}

// channelRequestURL is the URL a channel's delivery is posted to, when a
// user chooses it: a webhook-type channel's (webhook, Slack, Teams, Discord,
// Mattermost, Google Chat, Gotify, ntfy), sent over the senders' transport
// and so through the proxy where HTTP_PROXY, HTTPS_PROXY and NO_PROXY say.
// It is nil for the types whose target is not such a URL — a Telegram chat,
// a PagerDuty key, e-mail addresses — since those reach the operator's own
// endpoints, which the scope does not govern.
func channelRequestURL(typ store.ChannelType, target string) *url.URL {
	switch typ {
	case store.ChannelWebhook, store.ChannelSlack, store.ChannelTeams, store.ChannelDiscord,
		store.ChannelMattermost, store.ChannelGoogleChat, store.ChannelGotify, store.ChannelNtfy:
		if u, err := url.Parse(target); err == nil {
			return u
		}
	}
	return nil
}

// channelTargetHost is the host a channel's delivery connects to, when a user
// chooses it and it can be a cluster-internal address: the host of a webhook
// or an ntfy URL (channelRequestURL), or "" for the other types.
func channelTargetHost(typ store.ChannelType, target string) string {
	if u := channelRequestURL(typ, target); u != nil {
		return u.Hostname()
	}
	return ""
}

// Audit summaries for a refused cross-tenant write (S68), localised like the
// other fixed audit phrases (auditTR). %s is the target host or the namespace
// the caller aimed at, outside their scope; the action and object
// ("deny"/"target-scope") carry the rest.
const (
	scopeDeniedTargetText    = "target %s is outside your namespaces"
	scopeDeniedNamespaceText = "namespace %s is outside the namespaces you may edit"
)

// auditScopeDenied records a cross-tenant write a scoped caller was refused —
// a monitor or a channel aimed at a target host outside their namespaces
// (metrics.ScopeDeniedTarget), or created or edited in a namespace they may
// not change (metrics.ScopeDeniedNamespace). It writes an audit entry, beside
// login_failed, so a SOC sees the attempt over the 90-day window — before, a
// refusal left only a 403 and no trace — and counts it in
// kubeat_target_scope_denied_total for alerting. detail names the target or
// the namespace for the log; a target is written as the scope check read it
// (scopeHost), without its trailing dots, so one host is one spelling in the
// log however it was written. The client still gets the very same 403 with the
// same generic body: the response is written by the caller and left untouched,
// so neither the log nor the metric tells a client anything new.
func (h *handler) auditScopeDenied(r *http.Request, reason, detail string) {
	phrase := scopeDeniedTargetText
	if reason == metrics.ScopeDeniedNamespace {
		phrase = scopeDeniedNamespaceText
	} else if host := scopeHost(detail); host != "" {
		detail = host
	}
	h.audit(r, "deny", "target-scope", 0, fmt.Sprintf(h.auditText(phrase), detail))
	h.opts.Meter.TargetScopeDenied(reason)
}
