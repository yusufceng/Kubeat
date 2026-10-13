package api

import (
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/k8s"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// windowResponse is what happened to a monitor between two moments: the
// chart's selected range, or the span of an incident a notification linked
// to. It is read from what Kubeat stored (checks, incidents) and from what
// the cluster still remembers (events).
type windowResponse struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
	// Checks is null when the whole window lies before the raw retention:
	// only the incidents are still known then.
	Checks *checkSummaryResponse `json:"checks"`
	// Failures are the failed checks, newest first, at most MaxResults;
	// FailuresTruncated says when there were more.
	Failures          []resultResponse   `json:"failures"`
	FailuresTruncated bool               `json:"failures_truncated"`
	Incidents         []incidentResponse `json:"incidents"`
	// Events are the cluster's events about the pods in the window, as far
	// as the cluster still keeps them, newest first and at most EventLimit;
	// EventsTruncated says when there were more, EventsReason why there are
	// none when the cluster could not be asked.
	Events          []eventResponse `json:"events"`
	EventsTruncated bool            `json:"events_truncated"`
	EventsReason    string          `json:"events_reason,omitempty"`
}

type checkSummaryResponse struct {
	Total        int     `json:"total"`
	Failed       int     `json:"failed"`
	AvgLatencyMS float64 `json:"avg_latency_ms"`
	MaxLatencyMS int64   `json:"max_latency_ms"`
	// Partial says the window begins before the raw retention: the counts
	// cover only its later part.
	Partial bool `json:"partial"`
}

// monitorWindow answers GET /api/v1/monitors/{id}/window?from=&to=, both
// RFC 3339; to defaults to now.
func (h *handler) monitorWindow(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "monitor not found")
		return
	}
	now := time.Now().UTC()
	from, err := time.Parse(time.RFC3339, r.URL.Query().Get("from"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "from must be an RFC 3339 time")
		return
	}
	to := now
	if v := r.URL.Query().Get("to"); v != "" {
		if to, err = time.Parse(time.RFC3339, v); err != nil {
			writeError(w, http.StatusBadRequest, "to must be an RFC 3339 time")
			return
		}
	}
	from, to = from.UTC(), to.UTC()
	if to.After(now) {
		to = now
	}
	if !to.After(from) {
		writeError(w, http.StatusBadRequest, "to must be after from")
		return
	}

	m, err := h.opts.Store.GetMonitor(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) || (err == nil && !principal(r).CanRead(m.Namespace)) {
		writeError(w, http.StatusNotFound, "monitor not found")
		return
	}
	if err != nil {
		h.internalError(w, r, err)
		return
	}

	resp := windowResponse{From: from, To: to, Failures: []resultResponse{}, Incidents: []incidentResponse{}, Events: []eventResponse{}}

	// Raw results reach back RawRetention; a window entirely before that
	// has only its incidents left, one that straddles it a partial count.
	if retained := now.Add(-h.opts.RawRetention); to.After(retained) {
		sum, err := h.opts.Store.SummarizeChecks(r.Context(), id, from, to)
		if err != nil {
			h.internalError(w, r, err)
			return
		}
		resp.Checks = &checkSummaryResponse{
			Total: sum.Total, Failed: sum.Failed, Partial: from.Before(retained),
			AvgLatencyMS: float64(sum.AvgLatency) / float64(time.Millisecond), MaxLatencyMS: sum.MaxLatency.Milliseconds(),
		}
		failures, err := h.opts.Store.ListFailedChecks(r.Context(), id, from, to, h.opts.MaxResults)
		if err != nil {
			h.internalError(w, r, err)
			return
		}
		for _, f := range failures {
			resp.Failures = append(resp.Failures, toResultResponse(f))
		}
		resp.FailuresTruncated = sum.Failed > len(failures)
	}

	incidents, err := h.opts.Store.ListIncidentsBetween(r.Context(), id, from, to, h.opts.MaxResults)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	for _, i := range incidents {
		resp.Incidents = append(resp.Incidents, toIncidentResponse(i))
	}

	// An incident begins at its first failed check; what caused it happened
	// up to one check interval earlier, so the events start that much sooner.
	resp.Events, resp.EventsTruncated, resp.EventsReason = h.eventsBetween(r, m, from.Add(-m.Interval), to)
	writeJSON(w, http.StatusOK, resp)
}

// eventsBetween reads the cluster's events about the monitor's workload in
// the window, oldest first: the story of what happened, cut at EventLimit
// with a word that it was. The cluster keeps events for a limited time (an
// hour upstream, three on OpenShift), so an older window finds nothing;
// the reason names why the cluster could not be asked at all.
func (h *handler) eventsBetween(r *http.Request, m store.Monitor, from, to time.Time) (events []eventResponse, truncated bool, reason string) {
	none := []eventResponse{}
	namespace, source := m.Namespace, m.Source
	if m.Type == store.MonitorWorkload {
		ns, kind, name, err := k8s.ParseWorkloadTarget(m.Target)
		if err != nil {
			return none, false, err.Error()
		}
		if !principal(r).CanRead(ns) {
			return none, false, "the monitor's workload is outside your namespaces"
		}
		namespace, source = ns, string(kind)+"/"+name
	}
	switch {
	case source == "":
		return none, false, "the monitor was not created from a cluster object"
	case h.opts.Workloads == nil:
		return none, false, "the backend is not connected to a cluster: " + h.opts.DiscoveryStatus
	}
	workload, err := h.opts.Workloads.Workload(r.Context(), namespace, source)
	if err != nil {
		if !errors.Is(err, k8s.ErrNoWorkload) {
			h.opts.Logger.Warn("reading workload failed", "monitor", m.Name, "error", err)
		}
		return none, false, "the cluster could not be read: " + err.Error()
	}
	found, err := h.opts.Workloads.Events(r.Context(), namespace, workload, from, to, 0)
	if err != nil {
		h.opts.Logger.Warn("reading events failed", "monitor", m.Name, "error", err)
		return none, false, "the cluster's events could not be read: " + err.Error()
	}
	slices.Reverse(found) // oldest first: how it began
	if limit := h.opts.EventLimit; limit > 0 && len(found) > limit {
		found, truncated = found[:limit], true
	}
	events = make([]eventResponse, 0, len(found))
	for _, e := range found {
		events = append(events, eventResponse{At: e.At, Type: e.Type, Reason: e.Reason, Object: e.Object, Message: e.Message, Count: e.Count})
	}
	return events, truncated, ""
}
