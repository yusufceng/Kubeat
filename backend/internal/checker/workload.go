package checker

import (
	"context"
	"fmt"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// Workloads reads the state of a Kubernetes workload.
type Workloads interface {
	// Replicas returns how many pods the workload in target
	// (namespace/kind/name) is meant to have and how many are ready.
	Replicas(ctx context.Context, target string) (desired, ready int, err error)
}

// checkWorkload asks the cluster how many pods of a deployment, statefulset
// or daemonset are ready. It needs no connection to the pods themselves, so
// it also covers workloads without a service: workers, consumers, agents.
func (c *Checker) checkWorkload(ctx context.Context, m store.Monitor) outcome {
	if c.opts.Workloads == nil {
		return failedf("workload monitors need a connection to the cluster")
	}
	desired, ready, err := c.opts.Workloads.Replicas(ctx, m.Target)
	if err != nil {
		return failed(err)
	}

	needed := m.Workload.MinReady
	if needed == 0 {
		if desired == 0 {
			return failedf("the workload is scaled to zero")
		}
		needed = desired
	}
	if ready < needed {
		if needed == desired {
			return failedf("%d of %d pods are ready", ready, desired)
		}
		return failedf("%d of %d pods are ready, at least %d are needed", ready, desired, needed)
	}
	return outcome{detail: fmt.Sprintf("%d of %d pods are ready", ready, desired)}
}
