package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// Monitor states reported by the API.
const (
	statusUp      = "up"
	statusDown    = "down"
	statusPending = "pending" // no check has run yet
	statusPaused  = "paused"  // disabled by a user; not being checked
)

type monitorResponse struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Target string `json:"target"`
	// Namespace and Source are empty for monitors entered by hand.
	Namespace        string    `json:"namespace"`
	Source           string    `json:"source"`
	IntervalSeconds  int       `json:"interval_seconds"`
	TimeoutSeconds   int       `json:"timeout_seconds"`
	FailureThreshold int       `json:"failure_threshold"`
	Enabled          bool      `json:"enabled"`
	CreatedAt        time.Time `json:"created_at"`

	// HTTP holds the check options of an HTTP monitor; null for other types.
	HTTP *httpOptions `json:"http"`

	Status string          `json:"status"`
	Last   *resultResponse `json:"last_result"`
	// OpenIncident is set while the monitor is in an alerting state.
	OpenIncident *incidentResponse `json:"open_incident"`
	// CertWarning is true while the target's certificate is close to expiry.
	CertWarning bool `json:"cert_warning"`
	// MaintenanceUntil is set while a maintenance window covers the monitor;
	// no incident is opened for it until then.
	MaintenanceUntil *time.Time `json:"maintenance_until"`

	// Statistics over the last WindowSeconds. UptimePercent is null when no
	// check ran in the window.
	WindowSeconds int      `json:"window_seconds"`
	Checks        int      `json:"checks"`
	UptimePercent *float64 `json:"uptime_percent"`
	AvgLatencyMS  int64    `json:"avg_latency_ms"`
}

type resultResponse struct {
	CheckedAt  time.Time `json:"checked_at"`
	Up         bool      `json:"up"`
	LatencyMS  int64     `json:"latency_ms"`
	StatusCode *int      `json:"status_code"`
	Error      *string   `json:"error"`
	// CertExpiresAt is set when the check saw a TLS certificate.
	CertExpiresAt *time.Time `json:"cert_expires_at"`
}

func toResultResponse(r store.CheckResult) resultResponse {
	resp := resultResponse{CheckedAt: r.CheckedAt, Up: r.Up, LatencyMS: r.Latency.Milliseconds()}
	if r.StatusCode != 0 {
		resp.StatusCode = &r.StatusCode
	}
	if r.Error != "" {
		resp.Error = &r.Error
	}
	if !r.CertExpiresAt.IsZero() {
		resp.CertExpiresAt = &r.CertExpiresAt
	}
	return resp
}

func toMonitorResponse(st store.MonitorStatus, window time.Duration) monitorResponse {
	resp := monitorResponse{
		ID:              st.ID,
		Name:            st.Name,
		Type:            string(st.Type),
		Target:          st.Target,
		IntervalSeconds: int(st.Interval / time.Second),
		TimeoutSeconds:  int(st.Timeout / time.Second),
		Enabled:         st.Enabled,
		CreatedAt:       st.CreatedAt,
		Status:          statusPending,
		WindowSeconds:   int(window / time.Second),
		Checks:          st.Checks,
		AvgLatencyMS:    st.AvgLatency.Milliseconds(),

		FailureThreshold: st.FailureThreshold,
		CertWarning:      st.CertWarning,
		Namespace:        st.Namespace,
		Source:           st.Source,
	}
	if st.Type == store.MonitorHTTP {
		resp.HTTP = toHTTPOptions(st.HTTP)
	}
	if !st.MaintenanceUntil.IsZero() {
		resp.MaintenanceUntil = &st.MaintenanceUntil
	}
	if st.Incident != nil {
		incident := toIncidentResponse(*st.Incident)
		resp.OpenIncident = &incident
	}
	if st.Last != nil {
		last := toResultResponse(*st.Last)
		resp.Last = &last
		resp.Status = statusDown
		if st.Last.Up {
			resp.Status = statusUp
		}
	}
	if !st.Enabled {
		resp.Status = statusPaused
	}
	if st.Checks > 0 {
		pct := 100 * float64(st.UpChecks) / float64(st.Checks)
		resp.UptimePercent = &pct
	}
	return resp
}

// window reads the optional ?window= duration, defaulting to StatsWindow.
func (h *handler) window(r *http.Request) (time.Duration, error) {
	v := r.URL.Query().Get("window")
	if v == "" {
		return h.opts.StatsWindow, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("window must be a positive duration such as 1h or 24h")
	}
	return d, nil
}

// limit reads the optional ?limit=, capped at and defaulting to MaxResults.
func (h *handler) limit(r *http.Request) (int, error) {
	v := r.URL.Query().Get("limit")
	if v == "" {
		return h.opts.MaxResults, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 0, errors.New("limit must be a positive integer")
	}
	return min(n, h.opts.MaxResults), nil
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

func (h *handler) listMonitors(w http.ResponseWriter, r *http.Request) {
	window, err := h.window(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	statuses, err := h.opts.Store.ListMonitorStatuses(r.Context(), time.Now().Add(-window))
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	resp := make([]monitorResponse, 0, len(statuses))
	for _, st := range statuses {
		resp = append(resp, toMonitorResponse(st, window))
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *handler) getMonitor(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "monitor not found")
		return
	}
	window, err := h.window(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	st, err := h.opts.Store.GetMonitorStatus(r.Context(), id, time.Now().Add(-window))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "monitor not found")
		return
	}
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toMonitorResponse(st, window))
}

type createMonitorRequest struct {
	Name            string `json:"name"`
	Type            string `json:"type"`
	Target          string `json:"target"`
	IntervalSeconds int    `json:"interval_seconds"`
	TimeoutSeconds  int    `json:"timeout_seconds"`
	// FailureThreshold is optional; 0 means the server default.
	FailureThreshold int `json:"failure_threshold"`
	// HTTP is optional and only allowed for HTTP monitors.
	HTTP *httpOptions `json:"http"`
}

// validate normalizes the request and reports the first problem found.
func (req *createMonitorRequest) validate(minInterval time.Duration, defaultThreshold int) error {
	if req.FailureThreshold == 0 {
		req.FailureThreshold = defaultThreshold
	}
	if req.FailureThreshold < 1 {
		return errors.New("failure_threshold must be at least 1")
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Target = strings.TrimSpace(req.Target)

	if req.Name == "" {
		return errors.New("name is required")
	}
	switch store.MonitorType(req.Type) {
	case store.MonitorHTTP:
		u, err := url.Parse(req.Target)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return errors.New("target must be an http:// or https:// URL")
		}
	case store.MonitorTCP:
		host, port, err := net.SplitHostPort(req.Target)
		if err != nil || host == "" {
			return errors.New("target must be host:port")
		}
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			return errors.New("target port must be between 1 and 65535")
		}
	default:
		return errors.New("type must be http or tcp")
	}
	if minSeconds := int(minInterval / time.Second); req.IntervalSeconds < minSeconds {
		return fmt.Errorf("interval_seconds must be at least %d", minSeconds)
	}
	if req.TimeoutSeconds < 1 {
		return errors.New("timeout_seconds must be at least 1")
	}
	if req.TimeoutSeconds > req.IntervalSeconds {
		return errors.New("timeout_seconds must not exceed interval_seconds")
	}
	return nil
}

func (h *handler) createMonitor(w http.ResponseWriter, r *http.Request) {
	var req createMonitorRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := req.validate(h.opts.MinCheckInterval, h.opts.DefaultFailureThreshold); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var httpOpts store.HTTPOptions
	if req.HTTP != nil {
		if store.MonitorType(req.Type) != store.MonitorHTTP {
			writeError(w, http.StatusBadRequest, "http options are only valid for http monitors")
			return
		}
		var err error
		if httpOpts, err = req.HTTP.resolve(nil); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	m, err := h.opts.Store.CreateMonitor(r.Context(), store.Monitor{
		HTTP:     httpOpts,
		Name:     req.Name,
		Type:     store.MonitorType(req.Type),
		Target:   req.Target,
		Interval: time.Duration(req.IntervalSeconds) * time.Second,
		Timeout:  time.Duration(req.TimeoutSeconds) * time.Second,
		Enabled:  true,

		FailureThreshold: req.FailureThreshold,
	})
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "a monitor with this name already exists")
		return
	}
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toMonitorResponse(store.MonitorStatus{Monitor: m}, h.opts.StatsWindow))
}

// updateMonitorRequest carries only the fields to change.
type updateMonitorRequest struct {
	Name             *string `json:"name"`
	Target           *string `json:"target"`
	IntervalSeconds  *int    `json:"interval_seconds"`
	TimeoutSeconds   *int    `json:"timeout_seconds"`
	FailureThreshold *int    `json:"failure_threshold"`
	Enabled          *bool   `json:"enabled"`
	// HTTP, when present, replaces all HTTP options of the monitor.
	HTTP *httpOptions `json:"http"`
}

func (h *handler) updateMonitor(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "monitor not found")
		return
	}
	var req updateMonitorRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	current, err := h.opts.Store.GetMonitor(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "monitor not found")
		return
	}
	if err != nil {
		h.internalError(w, r, err)
		return
	}

	// Apply the changes on top of the current values and validate the result
	// with the same rules as a new monitor.
	merged := createMonitorRequest{
		Name:             current.Name,
		Type:             string(current.Type),
		Target:           current.Target,
		IntervalSeconds:  int(current.Interval / time.Second),
		TimeoutSeconds:   int(current.Timeout / time.Second),
		FailureThreshold: current.FailureThreshold,
	}
	if req.Name != nil {
		merged.Name = *req.Name
	}
	if req.Target != nil {
		merged.Target = *req.Target
	}
	if req.IntervalSeconds != nil {
		merged.IntervalSeconds = *req.IntervalSeconds
	}
	if req.TimeoutSeconds != nil {
		merged.TimeoutSeconds = *req.TimeoutSeconds
	}
	if req.FailureThreshold != nil {
		if *req.FailureThreshold < 1 {
			writeError(w, http.StatusBadRequest, "failure_threshold must be at least 1")
			return
		}
		merged.FailureThreshold = *req.FailureThreshold
	}
	if err := merged.validate(h.opts.MinCheckInterval, h.opts.DefaultFailureThreshold); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	change := current
	if req.HTTP != nil {
		if current.Type != store.MonitorHTTP {
			writeError(w, http.StatusBadRequest, "http options are only valid for http monitors")
			return
		}
		if change.HTTP, err = req.HTTP.resolve(current.HTTP.Headers); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	change.Name = merged.Name
	change.Target = merged.Target
	change.Interval = time.Duration(merged.IntervalSeconds) * time.Second
	change.Timeout = time.Duration(merged.TimeoutSeconds) * time.Second
	change.FailureThreshold = merged.FailureThreshold
	if req.Enabled != nil {
		change.Enabled = *req.Enabled
	}

	updated, err := h.opts.Store.UpdateMonitor(r.Context(), change)
	switch {
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "a monitor with this name already exists")
		return
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "monitor not found")
		return
	case err != nil:
		h.internalError(w, r, err)
		return
	}

	// A paused monitor produces no results, so nothing would ever resolve an
	// incident left open on it.
	if current.Enabled && !updated.Enabled {
		if err := h.closeIncident(r, updated); err != nil {
			h.internalError(w, r, err)
			return
		}
	}

	st, err := h.opts.Store.GetMonitorStatus(r.Context(), id, time.Now().Add(-h.opts.StatsWindow))
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toMonitorResponse(st, h.opts.StatsWindow))
}

// closeIncident resolves the monitor's open incidents, if any, without
// notifying: nothing recovered, the target just stopped being watched.
func (h *handler) closeIncident(r *http.Request, m store.Monitor) error {
	for _, kind := range store.IncidentKinds {
		incident, err := h.opts.Store.GetOpenIncident(r.Context(), m.ID, kind)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		h.opts.Logger.Info("incident closed because its monitor was paused", "monitor", m.Name, "incident_id", incident.ID, "kind", kind)
		err = h.opts.Store.ResolveIncident(r.Context(), incident.ID, time.Now().UTC())
		if err != nil && !errors.Is(err, store.ErrNotFound) { // not found: resolved in the meantime
			return err
		}
	}
	return nil
}

func (h *handler) deleteMonitor(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "monitor not found")
		return
	}
	err := h.opts.Store.DeleteMonitor(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "monitor not found")
		return
	}
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) listResults(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "monitor not found")
		return
	}
	limit, err := h.limit(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Distinguish "no results yet" from "no such monitor".
	if _, err := h.opts.Store.GetMonitorStatus(r.Context(), id, time.Now()); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "monitor not found")
		return
	} else if err != nil {
		h.internalError(w, r, err)
		return
	}

	results, err := h.opts.Store.ListCheckResults(r.Context(), id, limit)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	resp := make([]resultResponse, 0, len(results))
	for _, res := range results {
		resp = append(resp, toResultResponse(res))
	}
	writeJSON(w, http.StatusOK, resp)
}
