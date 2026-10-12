package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/k8s"
	"github.com/yusufceng/Kubeat/backend/internal/prom"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// Workloads reads the pods behind a discovered monitor from the cluster.
type Workloads interface {
	Workload(ctx context.Context, namespace, source string) (k8s.Workload, error)
}

// PodMetrics reads pod resource usage from the cluster's metrics.
type PodMetrics interface {
	PodUsage(ctx context.Context, namespace string, pods []string) (map[string]prom.Usage, error)
}

type workloadResponse struct {
	// Available is false when there are no pods to show; Reason says why.
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`

	Service string        `json:"service,omitempty"`
	Ready   int           `json:"ready"`
	Total   int           `json:"total"`
	Pods    []podResponse `json:"pods"`
	// Metrics says whether CPU and memory could be read. The pod list itself
	// comes from the cluster API and does not depend on it.
	Metrics workloadMetrics `json:"metrics"`
}

type workloadMetrics struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

type podResponse struct {
	Name            string     `json:"name"`
	Ready           bool       `json:"ready"`
	ReadyContainers int        `json:"ready_containers"`
	Containers      int        `json:"containers"`
	Restarts        int32      `json:"restarts"`
	Status          string     `json:"status"`
	StartedAt       *time.Time `json:"started_at"`
	Node            string     `json:"node"`
	// Null when the metrics server has no value for the pod.
	CPUCores    *float64 `json:"cpu_cores"`
	MemoryBytes *float64 `json:"memory_bytes"`
}

func unavailable(reason string) workloadResponse {
	return workloadResponse{Reason: reason, Pods: []podResponse{}, Metrics: workloadMetrics{Reason: reason}}
}

// monitorWorkload returns the pods behind a discovered monitor with their
// readiness, restarts and, if a metrics server is configured, resource usage.
func (h *handler) monitorWorkload(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "monitor not found")
		return
	}
	m, err := h.opts.Store.GetMonitor(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "monitor not found")
		return
	}
	if err != nil {
		h.internalError(w, r, err)
		return
	}

	switch {
	case m.Source == "":
		writeJSON(w, http.StatusOK, unavailable("the monitor was not created from a cluster object"))
		return
	case h.opts.Workloads == nil:
		writeJSON(w, http.StatusOK, unavailable("the backend is not connected to a cluster: "+h.opts.DiscoveryStatus))
		return
	}

	workload, err := h.opts.Workloads.Workload(r.Context(), m.Namespace, m.Source)
	if errors.Is(err, k8s.ErrNoWorkload) {
		writeJSON(w, http.StatusOK, unavailable(err.Error()))
		return
	}
	if err != nil {
		h.clusterError(w, r, err)
		return
	}

	resp := workloadResponse{Available: true, Service: workload.Service, Total: len(workload.Pods), Pods: make([]podResponse, 0, len(workload.Pods))}
	names := make([]string, 0, len(workload.Pods))
	for _, p := range workload.Pods {
		names = append(names, p.Name)
	}

	// Usage is an extra: failing to read it must not hide the pod list.
	var usage map[string]prom.Usage
	switch {
	case h.opts.PodMetrics == nil:
		resp.Metrics.Reason = "no metrics server is configured (UPTIME_PROMETHEUS_URL)"
	default:
		usage, err = h.opts.PodMetrics.PodUsage(r.Context(), m.Namespace, names)
		if err != nil {
			usage = nil // never show values from a failed read
			h.opts.Logger.Warn("reading pod metrics failed", "monitor", m.Name, "error", err)
			resp.Metrics.Reason = "the metrics server could not be queried: " + err.Error()
		} else {
			resp.Metrics.Available = true
		}
	}

	for _, p := range workload.Pods {
		pr := podResponse{
			Name: p.Name, Ready: p.Ready, ReadyContainers: p.ReadyContainers, Containers: p.Containers,
			Restarts: p.Restarts, Status: p.Status, Node: p.Node,
			CPUCores: usage[p.Name].CPUCores, MemoryBytes: usage[p.Name].MemoryBytes,
		}
		if !p.StartedAt.IsZero() {
			pr.StartedAt = &p.StartedAt
		}
		if p.Ready {
			resp.Ready++
		}
		resp.Pods = append(resp.Pods, pr)
	}
	writeJSON(w, http.StatusOK, resp)
}
