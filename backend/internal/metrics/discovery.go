package metrics

import "github.com/prometheus/client_golang/prometheus"

// Reasons for kubeat_discovery_sync_errors_total, and in the discovery
// status: why the annotation sync could not read a namespace, or the
// cluster. The set is fixed and small, and no label names a namespace: the
// names are in the discovery status and the backend's log, where whoever
// may read them can.
const (
	// DiscoveryForbidden: the cluster API refused Kubeat's service account
	// (403), as it does in a namespace whose Role was taken away or which
	// was deleted together with its RoleBinding.
	DiscoveryForbidden = "forbidden"
	// DiscoveryNotFound: the cluster API does not serve the kind of object
	// asked for (404); a namespace that does not exist lists as empty.
	DiscoveryNotFound = "not_found"
	// DiscoveryTimeout: no answer within UPTIME_KUBE_TIMEOUT, or the API
	// server's own timeout.
	DiscoveryTimeout = "timeout"
	// DiscoveryOther: any other failure: the API server unreachable, its
	// certificate untrusted, Kubeat's credentials refused (401), an error
	// on the server's side.
	DiscoveryOther = "other"
)

// discoveryMetrics are the series of the annotation sync (discovery.Syncer).
type discoveryMetrics struct {
	syncErrors        *prometheus.CounterVec
	namespacesSkipped prometheus.Gauge
}

// newDiscoveryMetrics makes the annotation sync's series and registers them;
// the reasons are there from the start, at 0, so that a first failure
// reads as an increase.
func newDiscoveryMetrics(registry *prometheus.Registry) discoveryMetrics {
	d := discoveryMetrics{
		syncErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kubeat_discovery_sync_errors_total",
			Help: "Reads of the cluster the annotation sync could not do, by reason: \"forbidden\" (403: a namespace's Role taken away, or the namespace deleted with its RoleBinding), \"not_found\" (404: a kind of object the cluster does not serve), \"timeout\" (no answer within UPTIME_KUBE_TIMEOUT) or \"other\" (the API server unreachable, credentials refused, a failure on its side). With discovery limited to namespaces (UPTIME_DISCOVERY_NAMESPACES), one per namespace skipped in a run; otherwise, and for a run that got no answer at all, one per run, which changed nothing. The namespaces are named in the discovery status (GET /api/v1/discovery) and the backend's log, not here.",
		}, []string{"reason"}),
		namespacesSkipped: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "kubeat_discovery_namespaces_skipped",
			Help: "Namespaces the last annotation sync run could not read and skipped, with discovery limited to namespaces (UPTIME_DISCOVERY_NAMESPACES): their monitors stay as they were, new annotations there are not picked up, removed ones keep their monitors. 0 when it read them all, and always with discovery over the whole cluster; a run that got no answer at all leaves it as it was. The namespaces and why are in the discovery status (GET /api/v1/discovery) and the backend's log.",
		}),
	}
	for _, reason := range []string{DiscoveryForbidden, DiscoveryNotFound, DiscoveryTimeout, DiscoveryOther} {
		d.syncErrors.WithLabelValues(reason)
	}
	registry.MustRegister(d.syncErrors, d.namespacesSkipped)
	return d
}

// DiscoveryNamespacesSkipped records an annotation sync run that got answers
// from the cluster: the reason (a Discovery* reason) of each namespace it
// skipped, none when it read them all.
func (m *Metrics) DiscoveryNamespacesSkipped(reasons []string) {
	if m == nil {
		return
	}
	for _, reason := range reasons {
		m.discovery.syncErrors.WithLabelValues(reason).Inc()
	}
	m.discovery.namespacesSkipped.Set(float64(len(reasons)))
}

// DiscoverySyncFailed records an annotation sync run that could not read
// the cluster at all, by reason (a Discovery* reason). The namespaces it
// skipped are not known then, and their gauge stays as it was.
func (m *Metrics) DiscoverySyncFailed(reason string) {
	if m != nil {
		m.discovery.syncErrors.WithLabelValues(reason).Inc()
	}
}
