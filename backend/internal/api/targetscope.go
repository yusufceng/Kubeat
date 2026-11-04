package api

import (
	"errors"
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

// targetInScope reports why host — a monitor's target host or a channel
// webhook's host — is outside the caller's namespaces, or nil when the caller
// may aim a check or a delivery at it.
//
// A caller who sees every namespace (an administrator, the service token, an
// editor or a viewer of all) reaches any target, as before, so a
// single-tenant install is unchanged. A caller limited to some namespaces may
// aim a check or a delivery at a cluster-internal target (netguard.InCluster)
// only when its namespace can be told from the host and is one of theirs:
// that keeps one tenant from pointing a monitor or a webhook at the API
// server or another team's Service. A cluster-internal address with no
// namespace in its name — a bare ClusterIP or pod IP, a short Service name in
// Kubeat's own namespace — is refused, since it cannot be attributed to the
// caller's scope. An external target (the public Internet, or the company
// network outside the cluster, which includes an OpenShift Route's public
// host) is left to the target policy, unchanged.
func targetInScope(p auth.Principal, host string) error {
	if p.SeesAll() || host == "" || !netguard.InCluster(host) {
		return nil
	}
	ns, ok := netguard.Namespace(host)
	if !ok {
		return errors.New("the target is a cluster-internal address with no namespace in its name: name the Service as service.namespace.svc so its namespace can be checked against your scope, or ask an administrator")
	}
	if !p.CanRead(ns) {
		return fmt.Errorf("the target is in namespace %q, which is outside your namespaces", ns)
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
