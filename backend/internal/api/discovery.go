package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/discovery"
	"github.com/yusufceng/Kubeat/backend/internal/k8s"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

type discoveryStatusResponse struct {
	Enabled bool `json:"enabled"`
	// Detail is how the cluster is reached when enabled, and why discovery
	// is unavailable otherwise; empty for all but administrators.
	Detail string `json:"detail"`
	// Sync is the state of the monitors kept in step with annotated
	// objects; null when that is off. Its counts and skipped objects cover
	// only the namespaces the user may see, unless they see all.
	Sync *discovery.Status `json:"sync"`
}

func (h *handler) discoveryStatus(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	resp := discoveryStatusResponse{Enabled: h.opts.Discovery != nil, Detail: h.clusterDetail(p)}
	if h.opts.Sync != nil {
		st := h.opts.Sync.Status()
		if !p.SeesAll() {
			st = st.Within(p.CanRead)
		}
		if st.Error != "" && !p.IsAdmin() {
			st.Error = upstreamReason(p, st.Cause)
		}
		resp.Sync = &st
	}
	writeJSON(w, http.StatusOK, resp)
}

// discovery answers 503 and returns false when no cluster is available.
func (h *handler) discovery(w http.ResponseWriter, r *http.Request) (Discovery, bool) {
	if h.opts.Discovery == nil {
		writeError(w, http.StatusServiceUnavailable, withDetail("discovery is not available", h.clusterDetail(principal(r))))
		return nil, false
	}
	return h.opts.Discovery, true
}

// clusterError reports a failed call to the cluster API, typically missing
// permissions or an unreachable API server, in as much detail as the
// principal may see (see upstreamReason).
func (h *handler) clusterError(w http.ResponseWriter, r *http.Request, err error) {
	h.opts.Logger.Error("cluster request failed", "path", r.URL.Path, "error", err)
	writeError(w, http.StatusBadGateway, "cluster request failed: "+upstreamReason(principal(r), err))
}

func (h *handler) discoveryNamespaces(w http.ResponseWriter, r *http.Request) {
	d, ok := h.discovery(w, r)
	if !ok {
		return
	}
	namespaces, err := d.Namespaces(r.Context())
	if err != nil {
		h.clusterError(w, r, err)
		return
	}
	p := principal(r)
	visible := make([]string, 0, len(namespaces))
	for _, ns := range namespaces {
		if p.CanRead(ns) {
			visible = append(visible, ns)
		}
	}
	writeJSON(w, http.StatusOK, visible)
}

type candidateResponse struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Port      int32  `json:"port"` // 0 for ingresses and routes
	Source    string `json:"source"`
	Type      string `json:"type"`
	Target    string `json:"target"`
	Reason    string `json:"reason"`
	// NoEndpoints marks a service whose port routes to nothing ready: a
	// monitor made from it would open an incident at once.
	NoEndpoints bool `json:"no_endpoints"`
	// MonitorID is set when the candidate is already monitored.
	MonitorID *int64 `json:"monitor_id"`
}

// monitored maps the source of every discovered monitor in a namespace to its ID.
func (h *handler) monitored(r *http.Request, namespace string) (map[string]int64, error) {
	monitors, err := h.opts.Store.ListMonitors(r.Context())
	if err != nil {
		return nil, err
	}
	bySource := make(map[string]int64)
	for _, m := range monitors {
		if m.Namespace == namespace && m.Source != "" {
			bySource[m.Source] = m.ID
		}
	}
	return bySource, nil
}

func (h *handler) discoveryCandidates(w http.ResponseWriter, r *http.Request) {
	d, ok := h.discovery(w, r)
	if !ok {
		return
	}
	namespace := r.URL.Query().Get("namespace")
	if namespace == "" {
		writeError(w, http.StatusBadRequest, "namespace is required")
		return
	}
	if !principal(r).CanRead(namespace) {
		forbidden(w)
		return
	}

	candidates, err := d.Candidates(r.Context(), namespace)
	if err != nil {
		h.clusterError(w, r, err)
		return
	}
	bySource, err := h.monitored(r, namespace)
	if err != nil {
		h.internalError(w, r, err)
		return
	}

	resp := make([]candidateResponse, 0, len(candidates))
	for _, c := range candidates {
		cr := candidateResponse{
			Kind: string(c.Kind), Namespace: c.Namespace, Name: c.Name, Port: c.Port,
			Source: c.Source(), Type: string(c.Type), Target: c.Target, Reason: c.Reason,
			NoEndpoints: c.NoEndpoints,
		}
		if id, ok := bySource[c.Source()]; ok {
			cr.MonitorID = &id
		}
		resp = append(resp, cr)
	}
	writeJSON(w, http.StatusOK, resp)
}

type importRequest struct {
	Namespace string `json:"namespace"`
	// Sources selects candidates by their source; the targets themselves are
	// taken from the cluster, not from the request.
	Sources          []string `json:"sources"`
	IntervalSeconds  int      `json:"interval_seconds"`
	TimeoutSeconds   int      `json:"timeout_seconds"`
	FailureThreshold int      `json:"failure_threshold"`
}

type importResponse struct {
	Created []monitorResponse `json:"created"`
	Skipped []importSkip      `json:"skipped"`
}

type importSkip struct {
	Source string `json:"source"`
	Reason string `json:"reason"`
}

// MonitorName is the name given to a monitor created from a candidate.
func monitorName(c k8s.Candidate) string {
	return c.Namespace + "/" + c.Source()
}

func (h *handler) discoveryImport(w http.ResponseWriter, r *http.Request) {
	d, ok := h.discovery(w, r)
	if !ok {
		return
	}
	var req importRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Namespace == "" {
		writeError(w, http.StatusBadRequest, "namespace is required")
		return
	}
	if p := principal(r); !p.CanEdit(req.Namespace) {
		scopeError(w, p)
		return
	}
	if len(req.Sources) == 0 {
		writeError(w, http.StatusBadRequest, "sources must not be empty")
		return
	}
	// Reuse the monitor rules for the settings shared by every new monitor;
	// the in-cluster probe target holds them to the general minimum
	// interval, each candidate is held to its own below.
	settings := createMonitorRequest{
		Name: "x", Type: string(store.MonitorTCP), Target: "x:1",
		IntervalSeconds: req.IntervalSeconds, TimeoutSeconds: req.TimeoutSeconds, FailureThreshold: req.FailureThreshold,
	}
	if err := settings.validate(h.limits(), h.opts.DefaultFailureThreshold); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	candidates, err := d.Candidates(r.Context(), req.Namespace)
	if err != nil {
		h.clusterError(w, r, err)
		return
	}
	bySource := make(map[string]k8s.Candidate, len(candidates))
	for _, c := range candidates {
		bySource[c.Source()] = c
	}
	// Every monitor made here is held to the quotas and the cap per host
	// (admit) like one made by hand, counted among those made before it.
	existing, err := h.opts.Store.ListMonitors(r.Context())
	if err != nil {
		h.internalError(w, r, err)
		return
	}

	resp := importResponse{Created: []monitorResponse{}, Skipped: []importSkip{}}
	seen := make(map[string]bool, len(req.Sources))
	for _, source := range req.Sources {
		if seen[source] {
			continue
		}
		seen[source] = true

		c, found := bySource[source]
		if !found {
			resp.Skipped = append(resp.Skipped, importSkip{Source: source, Reason: "not found in the cluster"})
			continue
		}
		// A Route or an Ingress may point outside the cluster, where the
		// shortest interval is longer: the candidate's own target decides.
		probe := settings
		probe.Type, probe.Target = string(c.Type), c.Target
		if err := probe.validate(h.limits(), h.opts.DefaultFailureThreshold); err != nil {
			resp.Skipped = append(resp.Skipped, importSkip{Source: source, Reason: err.Error()})
			continue
		}
		// The check options are the defaults of a monitor made by hand.
		monitor := defaultOptions(c.Type)
		monitor.Name = monitorName(c)
		monitor.Type = c.Type
		monitor.Target = c.Target
		monitor.GRPC.Service = c.GRPCService
		monitor.Namespace = c.Namespace
		monitor.Source = source
		monitor.Interval = time.Duration(settings.IntervalSeconds) * time.Second
		monitor.Timeout = time.Duration(settings.TimeoutSeconds) * time.Second
		monitor.FailureThreshold = settings.FailureThreshold
		monitor.Enabled = true
		if _, msg := h.admit(monitor, existing); msg != "" {
			resp.Skipped = append(resp.Skipped, importSkip{Source: source, Reason: msg})
			continue
		}
		// Each monitor of an import is a creation of the account's minute.
		ok, retryAfter, release := h.creationAllowed(r)
		if !ok {
			resp.Skipped = append(resp.Skipped, importSkip{Source: source, Reason: h.creationRefused(retryAfter)})
			continue
		}
		m, err := h.opts.Store.CreateMonitor(r.Context(), monitor)
		if err != nil {
			release() // not a creation
		}
		if errors.Is(err, store.ErrConflict) {
			resp.Skipped = append(resp.Skipped, importSkip{Source: source, Reason: "already monitored"})
			continue
		}
		if err != nil {
			h.internalError(w, r, err)
			return
		}
		existing = append(existing, m)
		h.audit(r, "create", "monitor", m.ID, m.Name)
		resp.Created = append(resp.Created, h.monitorResponse(principal(r), store.MonitorStatus{Monitor: m}, h.opts.StatsWindow))
	}
	writeJSON(w, http.StatusOK, resp)
}

type restoreRequest struct {
	Namespace string `json:"namespace"`
	Source    string `json:"source"`
}

// discoveryRestore undoes the deletion of an annotated object's monitor:
// the next sync creates it again.
func (h *handler) discoveryRestore(w http.ResponseWriter, r *http.Request) {
	var req restoreRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil || req.Source == "" {
		writeError(w, http.StatusBadRequest, "namespace and source are required")
		return
	}
	if p := principal(r); !p.CanEdit(req.Namespace) {
		scopeError(w, p)
		return
	}
	err := h.opts.Store.RestoreSource(r.Context(), req.Namespace, req.Source)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no deleted monitor for that object")
		return
	}
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	h.audit(r, "restore", "monitor", 0, req.Namespace+"/"+req.Source)
	w.WriteHeader(http.StatusNoContent)
}
