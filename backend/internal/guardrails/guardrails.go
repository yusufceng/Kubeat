// Package guardrails holds the rules that keep monitors from putting load
// on other systems, as far as they are decided when a monitor is made or
// changed: which targets are outside the cluster, the shortest interval
// such a target may be checked at, how many monitors one external host
// may have, and how many monitors may exist in all and in one namespace.
// The API and the annotation syncer are both held to them, and both
// answer with the same sentences, so that a reader of either learns the
// rule and the setting behind it. The ceiling on checks a minute is the
// scheduler's, and the account's creations a minute the API's: neither
// is about a monitor, so neither is here. Zero turns each limit off.
package guardrails

import (
	"fmt"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/netguard"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// Limits are the bounds on monitors as they are made.
type Limits struct {
	// MinInterval holds for every monitor; MinIntervalExternal for those
	// with an external target, when it is longer than MinInterval (zero:
	// no separate rule).
	MinInterval, MinIntervalExternal time.Duration
	// MaxMonitors and MaxMonitorsPerNamespace are quotas on the monitors
	// that exist, bound when one is made or moved to another namespace.
	// MaxMonitorsPerHost is how many monitors may check one external host.
	MaxMonitors, MaxMonitorsPerNamespace, MaxMonitorsPerHost int
}

// External reports whether the monitor checks a host outside the cluster
// (netguard.InCluster). Monitors that make no request are not external,
// nor is a target the type's validation would refuse.
func External(m store.Monitor) bool {
	host := m.Host()
	return host != "" && !netguard.InCluster(host)
}

// ExternalRule ends the sentence that refuses an interval for being
// shorter than the external minimum, naming the rule and its setting.
const ExternalRule = " for a target outside the cluster (UPTIME_MIN_CHECK_INTERVAL_EXTERNAL)"

// MinIntervalFor returns the shortest interval a monitor of the type and
// target may have, and the end of the sentence naming the rule when it is
// the external one: "" when the general rule applies, which needs no
// naming.
func (l Limits) MinIntervalFor(typ store.MonitorType, target string) (time.Duration, string) {
	if l.MinIntervalExternal > l.MinInterval && External(store.Monitor{Type: typ, Target: target}) {
		return l.MinIntervalExternal, ExternalRule
	}
	return l.MinInterval, ""
}

// Kind says which rule refused a monitor.
type Kind int

const (
	// Quota: the installation, or the namespace, is full; the monitor
	// itself is fine.
	Quota Kind = iota + 1
	// HostCap: this target may not have another monitor.
	HostCap
)

// Refusal is why a monitor may not be made, as both the API and the
// syncer say it.
type Refusal struct {
	Kind    Kind
	Message string
}

func (r *Refusal) Error() string { return r.Message }

// Admit says whether m may be made, or changed into, given existing, every
// monitor there is: within the quota on monitors, the quota on its
// namespace and, for an external target, the cap on monitors per host. It
// returns nil when it may, else the refusal. For a change (m.ID set) m
// itself is not counted; the quotas then bind only when the monitor moves
// to another namespace, that namespace's quota.
func (l Limits) Admit(m store.Monitor, existing []store.Monitor) *Refusal {
	others, inNamespace, onHost := 0, 0, 0
	host := ""
	if External(m) {
		host = m.Host()
	}
	var previous *store.Monitor
	for i := range existing {
		e := &existing[i]
		if m.ID != 0 && e.ID == m.ID {
			previous = e
			continue
		}
		others++
		if e.Namespace == m.Namespace {
			inNamespace++
		}
		if host != "" && e.Host() == host {
			onHost++
		}
	}
	isNew := m.ID == 0
	moved := previous != nil && previous.Namespace != m.Namespace
	if isNew {
		if limit := l.MaxMonitors; limit > 0 && others >= limit {
			return &Refusal{Quota, fmt.Sprintf("the limit of %d monitors is reached (UPTIME_MAX_MONITORS); delete monitors or raise the limit", limit)}
		}
	}
	if isNew || moved {
		if limit := l.MaxMonitorsPerNamespace; limit > 0 && inNamespace >= limit {
			where := fmt.Sprintf("the namespace %q", m.Namespace)
			if m.Namespace == "" {
				where = "monitors without a namespace"
			}
			return &Refusal{Quota, fmt.Sprintf("%s is at the limit of %d monitors (UPTIME_MAX_MONITORS_PER_NAMESPACE)", where, limit)}
		}
	}
	if limit := l.MaxMonitorsPerHost; host != "" && limit > 0 && onHost >= limit {
		return &Refusal{HostCap, fmt.Sprintf("at most %d monitors may check the host %s (UPTIME_MAX_MONITORS_PER_HOST); it has %d already", limit, host, onHost)}
	}
	return nil
}
