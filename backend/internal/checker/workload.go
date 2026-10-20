package checker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/k8s"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// WorkloadStatus is what the cluster says about a workload's pods.
type WorkloadStatus struct {
	Desired, Ready int
	// Rolling is set while an update is being rolled out; Stalled when the
	// cluster has given up on it (a Deployment past its progress deadline).
	Rolling, Stalled bool
}

// Workloads reads the state of a Kubernetes workload.
type Workloads interface {
	// Replicas returns how many pods the workload in target
	// (namespace/kind/name) is meant to have, how many are ready, and
	// whether it is being rolled out.
	Replicas(ctx context.Context, target string) (WorkloadStatus, error)
}

// checkWorkload asks the cluster how many pods of a deployment, statefulset
// or daemonset are ready. It needs no connection to the pods themselves, so
// it also covers workloads without a service: workers, consumers, agents.
//
// A rollout replaces pods on purpose, so missing pods during one are not a
// failure until the rollout has gone on for longer than RolloutGrace or the
// cluster has given up on it.
func (c *Checker) checkWorkload(ctx context.Context, m store.Monitor) outcome {
	if c.opts.Workloads == nil {
		return failedf("workload monitors need a connection to the cluster")
	}
	st, err := c.opts.Workloads.Replicas(ctx, m.Target)
	if err != nil {
		return failed(c.clusterFailure(m, err))
	}
	rollingFor, rolling := c.rollingFor(m.Target, st.Rolling)

	needed := m.Workload.MinReady
	if needed == 0 {
		if st.Desired == 0 {
			return failedf("the workload is scaled to zero")
		}
		needed = st.Desired
	}
	if st.Ready < needed {
		if rolling && !st.Stalled && rollingFor < c.opts.RolloutGrace {
			return outcome{detail: fmt.Sprintf("rollout in progress for %s: %d of %d pods are ready", rollingFor.Truncate(time.Second), st.Ready, st.Desired)}
		}
		switch {
		case st.Stalled:
			return failedf("%d of %d pods are ready; the rollout has stalled", st.Ready, st.Desired)
		case rolling:
			return failedf("%d of %d pods are ready; the rollout has been going on for %s", st.Ready, st.Desired, rollingFor.Truncate(time.Second))
		case needed == st.Desired:
			return failedf("%d of %d pods are ready", st.Ready, st.Desired)
		default:
			return failedf("%d of %d pods are ready, at least %d are needed", st.Ready, st.Desired, needed)
		}
	}
	if rolling {
		return outcome{detail: fmt.Sprintf("%d of %d pods are ready, rollout in progress", st.Ready, st.Desired)}
	}
	return outcome{detail: fmt.Sprintf("%d of %d pods are ready", st.Ready, st.Desired)}
}

// clusterFailure is what a check stores when its request to the cluster API
// fails: what kind of failure it was (k8s.FailureReason). Every reader of
// the monitor reads its results, and its incidents and notifications carry
// them; the error itself, which names Kubeat's service account or the API
// server's address, goes to the log.
func (c *Checker) clusterFailure(m store.Monitor, err error) error {
	reason := k8s.FailureReason(err)
	if reason != err.Error() {
		c.opts.Logger.Warn("a check's request to the cluster API failed", "monitor_id", m.ID, "monitor", m.Name, "error", err)
	}
	return errors.New(reason)
}

// rollingFor returns how long the workload has been seen rolling out. The
// start is kept in memory from the first check that saw the rollout, so a
// restart of Kubeat starts the clock again.
func (c *Checker) rollingFor(target string, rolling bool) (time.Duration, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !rolling {
		delete(c.rollouts, target)
		return 0, false
	}
	now := c.now()
	since, seen := c.rollouts[target]
	if !seen {
		since = now
		c.rollouts[target] = since
	}
	return now.Sub(since), true
}
