package api

import (
	"context"
	"net/url"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/auth"
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
//
// A caller who sees every namespace (an administrator, the service token, an
// editor or a viewer of all) reaches any target, as before, so a
// single-tenant install is unchanged.
//
// For a caller limited to some namespaces the check is made at the target's
// reachability, not only its spelling (dial-time; ADR-0003 c, "Kapsamın kalan
// boşluğu" closed in 0.48):
//
//   - A host that is cluster-internal by its name (netguard.InCluster: an
//     address in a cluster range, a *.svc or pod name, a short name) is let
//     through only when the namespace its name carries is one of theirs. The
//     API server, another team's Service and a bare ClusterIP — which carries
//     no namespace — are refused.
//   - A host that is external by its name is let through only when it provably
//     resolves to addresses outside the cluster. A name that resolves into the
//     cluster is refused — a two-label short name the pod's search domains turn
//     into a Service ("api.payments", "kubernetes.default"), or a public name
//     pointing at a cluster address ("172-30-0-1.sslip.io") — because its name
//     carries no namespace the resolver honoured, so it cannot be attributed to
//     the caller. A name the resolver cannot answer for — a CoreDNS outage, a
//     timeout, or an NXDOMAIN — is refused too: it cannot be proven external,
//     and accepting it would leave the hole this closes, where a scoped caller
//     registers an external-looking name during a resolver outage and the
//     checker dials it into the cluster once the resolver recovers (ADR-0003 c,
//     resolver-error fail-safe). The resolution is bounded by resolveTimeout
//     (a few seconds) so a slow resolver does not hold a create or an edit open
//     to the server's write timeout.
//
// An external target that resolves externally (the public Internet, the
// company network outside the cluster, an OpenShift Route's public host) is
// left to the target policy, unchanged. resolver is the backend's own
// (net.DefaultResolver when nil); only a scoped caller's external-looking
// name is resolved, so single-tenant installs and administrators never pay a
// lookup. A refusal carries the single generic scopeRefused message, so the
// body does not become an internal-address oracle.
func targetInScope(ctx context.Context, resolver netguard.Resolver, resolveTimeout time.Duration, p auth.Principal, host string) error {
	if p.SeesAll() || host == "" {
		return nil
	}
	if netguard.InCluster(host) {
		if ns, ok := netguard.Namespace(host); !ok || !p.CanRead(ns) {
			return &scopeRefusal{scopeRefused}
		}
		return nil
	}
	// host is external by its spelling. Bound the resolution so a slow or
	// stuck resolver cannot hold the request open to HTTPWriteTimeout.
	if resolveTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, resolveTimeout)
		defer cancel()
	}
	if inCluster, err := netguard.ResolvesInCluster(ctx, resolver, host); err != nil || inCluster {
		return &scopeRefusal{scopeRefused}
	}
	return nil
}

// channelTargetHost is the host a channel's delivery connects to, when a user
// chooses it and it can be a cluster-internal address: the host of a webhook
// or an ntfy URL. It is "" for the types whose target is not such a URL — a
// Telegram chat, a PagerDuty key, e-mail addresses — since those reach the
// operator's own endpoints, which the scope does not govern.
func channelTargetHost(typ store.ChannelType, target string) string {
	switch typ {
	case store.ChannelWebhook, store.ChannelSlack, store.ChannelTeams, store.ChannelDiscord,
		store.ChannelMattermost, store.ChannelGoogleChat, store.ChannelGotify, store.ChannelNtfy:
		if u, err := url.Parse(target); err == nil {
			return u.Hostname()
		}
	}
	return ""
}
