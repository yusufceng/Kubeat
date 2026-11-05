package api

import (
	"context"
	"fmt"
	"net/url"

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
//   - A host that is external by its name but resolves into the cluster now is
//     refused outright: a two-label short name the pod's search domains turn
//     into a Service ("api.payments", "kubernetes.default"), or a public name
//     pointing at a cluster address ("172-30-0-1.sslip.io"). Its name carries
//     no namespace the resolver honoured, so it cannot be attributed to the
//     caller; name classification alone would miss it, which is why the
//     resolved address decides (ADR-0003 c).
//
// An external target that resolves externally (the public Internet, the
// company network outside the cluster, an OpenShift Route's public host) is
// left to the target policy, unchanged. resolver is the backend's own
// (net.DefaultResolver when nil); only a scoped caller's external-looking
// name is resolved, so single-tenant installs and administrators never pay a
// lookup.
func targetInScope(ctx context.Context, resolver netguard.Resolver, p auth.Principal, host string) error {
	if p.SeesAll() || host == "" {
		return nil
	}
	if netguard.InCluster(host) {
		ns, ok := netguard.Namespace(host)
		if !ok {
			return &scopeRefusal{"the target is a cluster-internal address whose namespace cannot be read from its name: name the Service as service.namespace.svc so its namespace can be checked against your scope, or ask an administrator"}
		}
		if !p.CanRead(ns) {
			return &scopeRefusal{fmt.Sprintf("the target is in namespace %q, which is outside your namespaces", ns)}
		}
		return nil
	}
	if netguard.ResolvesInCluster(ctx, resolver, host) {
		return &scopeRefusal{"the target is an external name that resolves to a cluster-internal address; name the Service as service.namespace.svc so its namespace can be checked against your scope, or ask an administrator"}
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
