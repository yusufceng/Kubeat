package guardrails

import (
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

func web(id int64, namespace, target string) store.Monitor {
	return store.Monitor{ID: id, Type: store.MonitorHTTP, Target: target, Namespace: namespace}
}

// The quota on a namespace binds a new monitor and one moved into the
// namespace; a change that stays in its namespace is not counted, nor is
// the monitor itself. The total quota binds new monitors only; the cap per
// host binds every change of host.
func TestAdmit(t *testing.T) {
	l := Limits{MaxMonitors: 10, MaxMonitorsPerNamespace: 2, MaxMonitorsPerHost: 1}
	existing := []store.Monitor{
		web(1, "q", "http://api.q.svc/"),
		web(2, "q", "http://db.q.svc/"),
		web(3, "p", "https://example.com/"),
	}
	if r := l.Admit(web(0, "q", "http://x.q.svc/"), existing); r == nil || r.Kind != Quota {
		t.Errorf("a new monitor in a full namespace: %+v, want the quota's refusal", r)
	}
	moved := existing[2]
	moved.Namespace = "q"
	if r := l.Admit(moved, existing); r == nil || r.Kind != Quota || r.Message != `the namespace "q" is at the limit of 2 monitors (UPTIME_MAX_MONITORS_PER_NAMESPACE)` {
		t.Errorf("a monitor moved into a full namespace: %+v, want the quota's refusal", r)
	}
	renamed := existing[0]
	renamed.Target = "http://api.q.svc/v2"
	if r := l.Admit(renamed, existing); r != nil {
		t.Errorf("a change within a full namespace: %v, want none", r)
	}
	if r := l.Admit(web(0, "p", "https://example.com/other"), existing); r == nil || r.Kind != HostCap ||
		r.Message != "at most 1 monitors may check the host example.com (UPTIME_MAX_MONITORS_PER_HOST); it has 1 already" {
		t.Errorf("a second monitor on a capped host: %+v, want the cap's refusal", r)
	}
	ownHost := existing[2]
	ownHost.Target = "https://example.com/still"
	if r := l.Admit(ownHost, existing); r != nil {
		t.Errorf("a monitor staying on its host: %v, want none", r)
	}
	full := Limits{MaxMonitors: 3}
	if r := full.Admit(web(0, "", "http://x.q.svc/"), existing); r == nil || r.Kind != Quota {
		t.Errorf("a new monitor past the total quota: %+v, want the quota's refusal", r)
	}
	if r := full.Admit(moved, existing); r != nil {
		t.Errorf("a move past the total quota: %v, want none: the count does not grow", r)
	}
	if r := (Limits{}).Admit(web(0, "q", "https://example.com/"), existing); r != nil {
		t.Errorf("with every limit off: %v", r)
	}
}

// The external minimum holds for an external target when it is longer
// than the general one, and names its rule.
func TestMinIntervalFor(t *testing.T) {
	l := Limits{MinInterval: 5 * time.Second, MinIntervalExternal: 30 * time.Second}
	if d, rule := l.MinIntervalFor(store.MonitorHTTP, "https://example.com/"); d != 30*time.Second || rule != ExternalRule {
		t.Errorf("external: %s %q", d, rule)
	}
	if d, rule := l.MinIntervalFor(store.MonitorHTTP, "http://api.shop.svc/"); d != 5*time.Second || rule != "" {
		t.Errorf("in-cluster: %s %q", d, rule)
	}
	if d, _ := l.MinIntervalFor(store.MonitorPush, ""); d != 5*time.Second {
		t.Errorf("push: %s", d)
	}
	off := Limits{MinInterval: 5 * time.Second}
	if d, rule := off.MinIntervalFor(store.MonitorHTTP, "https://example.com/"); d != 5*time.Second || rule != "" {
		t.Errorf("without an external rule: %s %q", d, rule)
	}
	if !External(store.Monitor{Type: store.MonitorTCP, Target: "example.com:443"}) || External(store.Monitor{Type: store.MonitorTCP, Target: "10.0.0.1:443"}) || External(store.Monitor{Type: store.MonitorPush}) {
		t.Error("External misclassified a target")
	}
}
