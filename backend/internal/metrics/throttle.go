package metrics

import "github.com/prometheus/client_golang/prometheus"

// The surfaces whose passwords the throttle guards per source: the fixed
// values of the surface label of kubeat_signin_source_degraded_total and
// kubeat_throttled_total.
const (
	SurfaceSignIn         = "sign_in"
	SurfacePasswordChange = "password_change"
	SurfaceStatusPage     = "status_page"
)

// Why the throttle could not key a request on its client: the fixed values
// of the reason label of kubeat_signin_source_degraded_total. All but
// DegradedUntrustedPeer leave every browser on one address, and the throttle
// on one shared lock per name, page or account.
const (
	// DegradedHopsZero: X-Forwarded-For came, but UPTIME_TRUST_PROXY_HOPS is
	// 0, so the throttle keys on the peer, the UI pod or a proxy.
	DegradedHopsZero = "hops_zero"
	// DegradedNoFrontendToken: the hops are above 0, but there is no
	// UPTIME_FRONTEND_TOKEN to tell the UI by, so no peer's header is read.
	DegradedNoFrontendToken = "no_frontend_token"
	// DegradedUntrustedPeer: the hops are above 0 and the request did not
	// come from the UI, so it keys on its peer: expected for a script or a
	// port-forward to the backend.
	DegradedUntrustedPeer = "untrusted_peer"
	// DegradedHopsTooHigh: the UI's forwarded chain is shorter than the
	// hops, so the throttle falls back to the peer.
	DegradedHopsTooHigh = "hops_too_high"
	// DegradedHopsTooLow: the source keyed on is an address Kubeat's own
	// public URL resolves to, its load balancer rather than a client.
	DegradedHopsTooLow = "hops_too_low"
)

var (
	surfaces        = []string{SurfaceSignIn, SurfacePasswordChange, SurfaceStatusPage}
	degradedReasons = []string{DegradedHopsZero, DegradedNoFrontendToken, DegradedUntrustedPeer, DegradedHopsTooHigh, DegradedHopsTooLow}
	// guessSurfaces are those beside the sign-in, whose 429s
	// kubeat_throttled_total counts; the sign-in's are
	// kubeat_signin_throttled_total's.
	guessSurfaces = []string{SurfacePasswordChange, SurfaceStatusPage}
)

// throttleMetrics are the throttle's counters beside the sign-in's own
// (kubeat_signin_throttled_total): the requests whose source it could not
// tell apart, and the 429s of the password change and the status-page
// unlock.
type throttleMetrics struct {
	sourceDegraded *prometheus.CounterVec
	throttled      *prometheus.CounterVec
}

// newThrottleMetrics registers the throttle's counters with reg, each series
// there from the start at 0, so that an alert on increase() sees the first
// request: the chart's KubeatSignInSourceDegraded watches them.
func newThrottleMetrics(reg *prometheus.Registry) throttleMetrics {
	t := throttleMetrics{
		sourceDegraded: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kubeat_signin_source_degraded_total",
			Help: "Sign-ins, password changes and status-page unlocks (surface \"sign_in\", \"password_change\", \"status_page\") whose source the throttle could not key on the client, by reason: \"hops_zero\" (X-Forwarded-For came but UPTIME_TRUST_PROXY_HOPS is 0), \"no_frontend_token\" (the hops are above 0 but UPTIME_FRONTEND_TOKEN is not set), \"hops_too_high\" (the UI's forwarded chain is shorter than the hops), \"hops_too_low\" (the source is an address UPTIME_PUBLIC_URL's host resolves to, Kubeat's own load balancer) and \"untrusted_peer\" (the hops are above 0 and the request did not come from the UI; expected for a script or a port-forward to the backend). All but untrusted_peer leave every browser on one address and the throttle on one shared lock. Every such request counts, whatever the once-a-minute WARN logs; no address is carried.",
		}, []string{"reason", "surface"}),
		throttled: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kubeat_throttled_total",
			Help: "Password changes and status-page unlocks (surface \"password_change\", \"status_page\") the throttle refused with 429, by kind: \"source\" (the source's lock, UPTIME_LOGIN_MAX_FAILURES) or \"account\" (the ceiling on the account's or the page's failures across all sources, UPTIME_LOGIN_ACCOUNT_MAX_FAILURES: a lock for everyone). Sign-ins are counted by kubeat_signin_throttled_total. The client response is the same 429 for any key.",
		}, []string{"surface", "kind"}),
	}
	for _, surface := range surfaces {
		for _, reason := range degradedReasons {
			t.sourceDegraded.WithLabelValues(reason, surface)
		}
	}
	for _, surface := range guessSurfaces {
		for _, kind := range []string{ThrottleSource, ThrottleAccount} {
			t.throttled.WithLabelValues(surface, kind)
		}
	}
	reg.MustRegister(t.sourceDegraded, t.throttled)
	return t
}

// SourceDegraded records a request at a throttled password on surface (a
// Surface* value) that the throttle could not key on its client, for reason
// (a Degraded* value). It counts every request, unlike the once-a-minute
// WARN beside it.
func (m *Metrics) SourceDegraded(reason, surface string) {
	if m != nil {
		m.throttle.sourceDegraded.WithLabelValues(reason, surface).Inc()
	}
}

// Throttled records a password change or a status-page unlock (surface
// SurfacePasswordChange or SurfaceStatusPage) the throttle refused with 429,
// by kind (ThrottleSource or ThrottleAccount).
func (m *Metrics) Throttled(surface, kind string) {
	if m != nil {
		m.throttle.throttled.WithLabelValues(surface, kind).Inc()
	}
}
