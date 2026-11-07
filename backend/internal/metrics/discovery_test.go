package metrics

import (
	"strings"
	"testing"
)

// The annotation sync's series are there from the start, at 0, one per
// reason and none per namespace. A run that got answers sets the gauge to
// the namespaces it skipped and counts each by reason; one that got none
// counts once and leaves the gauge as it was. A nil *Metrics records
// nothing.
func TestDiscoverySyncMetrics(t *testing.T) {
	var none *Metrics
	none.DiscoveryNamespacesSkipped([]string{DiscoveryForbidden})
	none.DiscoverySyncFailed(DiscoveryOther)

	m := New("test")
	want := map[string]float64{
		"kubeat_discovery_namespaces_skipped":                                   0,
		"kubeat_discovery_sync_errors_total{reason=" + DiscoveryForbidden + "}": 0,
		"kubeat_discovery_sync_errors_total{reason=" + DiscoveryNotFound + "}":  0,
		"kubeat_discovery_sync_errors_total{reason=" + DiscoveryTimeout + "}":   0,
		"kubeat_discovery_sync_errors_total{reason=" + DiscoveryOther + "}":     0,
	}
	check := func(when string) {
		t.Helper()
		got, err := m.Gather()
		if err != nil {
			t.Fatal(err)
		}
		for key, value := range want {
			if v, ok := got[key]; !ok || v != value {
				t.Errorf("%s: %s = %v (present: %v), want %v", when, key, v, ok, value)
			}
		}
		n := 0
		for key := range got {
			if strings.HasPrefix(key, "kubeat_discovery_") {
				n++
			}
		}
		if n != len(want) {
			t.Errorf("%s: %d discovery series, want %d: %v", when, n, len(want), got)
		}
	}
	check("at the start")

	m.DiscoveryNamespacesSkipped([]string{DiscoveryForbidden, DiscoveryForbidden, DiscoveryNotFound})
	want["kubeat_discovery_namespaces_skipped"] = 3
	want["kubeat_discovery_sync_errors_total{reason=forbidden}"] = 2
	want["kubeat_discovery_sync_errors_total{reason=not_found}"] = 1
	check("three namespaces skipped")

	m.DiscoverySyncFailed(DiscoveryTimeout)
	want["kubeat_discovery_sync_errors_total{reason=timeout}"] = 1
	check("a run without an answer")

	m.DiscoveryNamespacesSkipped(nil)
	want["kubeat_discovery_namespaces_skipped"] = 0
	check("a run that read everything")
}
