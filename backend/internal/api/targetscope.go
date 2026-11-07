package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"

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

// targetInScope reports why host — a monitor's target host, a channel
// webhook's host, or another endpoint a monitor's check connects to — is
// outside the caller's namespaces, or nil when the caller may aim a check or
// a delivery at it. A non-nil error is a *scopeRefusal. request is the URL of
// the HTTP request the check or the delivery sends to host
// (monitorRequestURL, channelRequestURL), or nil for one that connects to
// host directly; it matters only for the proxy exception.
//
// A caller who sees every namespace (an administrator, the service token, an
// editor or a viewer of all) reaches any target, as before, so a
// single-tenant install is unchanged, and an empty host (a push monitor, a
// channel without a URL) has nothing to hold.
//
// For a caller limited to some namespaces the rule is netguard's
// (ScopeRules.Refuses), the one the checker holds every connection of their
// monitors' checks to (ADR-0003 c): a cluster-internal name is let through
// only in one of their namespaces, a bare cluster address or a short name
// never, an external-looking name only where it provably resolves outside
// the cluster (Options.ScopeExternal counting as outside), with the proxy
// exception behind Options.ScopeTrustProxy. Options.Resolver is the
// backend's own (net.DefaultResolver when nil); only a scoped caller's
// external-looking name is resolved, so single-tenant installs and
// administrators never pay a lookup. A refusal carries the single generic
// scopeRefused message, so the body does not become an internal-address
// oracle.
func (h *handler) targetInScope(ctx context.Context, p auth.Principal, host string, request *url.URL) error {
	if p.SeesAll() || host == "" {
		return nil
	}
	if h.scopeRules().Refuses(ctx, p.CanRead, host, request) {
		return &scopeRefusal{scopeRefused}
	}
	return nil
}

// scopeRules are the operator's parts of the scope check, from the options.
func (h *handler) scopeRules() netguard.ScopeRules {
	return netguard.ScopeRules{
		External: h.opts.ScopeExternal, Resolver: h.opts.Resolver, ResolveTimeout: h.opts.ResolveTimeout,
		TrustProxy: h.opts.ScopeTrustProxy, Proxy: h.opts.Proxy,
	}
}

// scopeHost is host as the scope check reads it, and as the audit entry of a
// refused target names it (netguard.ScopeHost): in lower case, without
// surrounding space and without every trailing dot. It is "" for a host made
// of dots alone.
func scopeHost(host string) string { return netguard.ScopeHost(host) }

// endpointsInScope reports why an endpoint a monitor's check connects to,
// other than its target host, is outside the caller's namespaces, and which
// one, as targetInScope does for the target: nil when every one is in
// scope. The record of the scope the checks are then held to is
// targetScopeRecord's.
func (h *handler) endpointsInScope(ctx context.Context, p auth.Principal, m store.Monitor) (string, error) {
	if m.Type == store.MonitorDNS {
		if host := m.DNS.ResolverHost(); host != "" {
			if err := h.targetInScope(ctx, p, host, nil); err != nil {
				return host, err
			}
		}
	}
	return "", nil
}

// targetScopeRecord is the record of the scope a monitor's checks are held
// to once p sets where they connect (ADR-0003 c, 0.51): p's namespaces as
// they are now, or every namespace for one who sees all. It is a snapshot:
// a later change to p's namespaces does not reach it (S73).
func targetScopeRecord(p auth.Principal) *store.TargetScope {
	if p.SeesAll() {
		return &store.TargetScope{Namespaces: []string{store.AllNamespaces}}
	}
	namespaces := slices.Clone(p.Namespaces)
	slices.Sort(namespaces)
	return &store.TargetScope{Namespaces: slices.Compact(namespaces)}
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
