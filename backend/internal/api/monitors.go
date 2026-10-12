package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
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
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"`
	// Target is empty for push monitors.
	Target string `json:"target"`
	// Namespace and Source are empty for monitors entered by hand.
	Namespace       string `json:"namespace"`
	Source          string `json:"source"`
	IntervalSeconds int    `json:"interval_seconds"`
	TimeoutSeconds  int    `json:"timeout_seconds"`
	// RetryIntervalSeconds is 0 when failed checks are not repeated early.
	RetryIntervalSeconds int       `json:"retry_interval_seconds"`
	FailureThreshold     int       `json:"failure_threshold"`
	Inverted             bool      `json:"inverted"`
	Enabled              bool      `json:"enabled"`
	CreatedAt            time.Time `json:"created_at"`
	// PushToken is the secret of a push monitor's report URL,
	// /api/push/{token}; absent for other types.
	PushToken string `json:"push_token,omitempty"`

	// The check options of the monitor's type; the others are null.
	checkOptions

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

	// History holds the most recent checks, newest first; only present when
	// the request asked for it with ?history=N.
	History []historyPoint `json:"history,omitempty"`
}

// historyPoint is one past check in the compact form used when many
// monitors are listed with their history.
type historyPoint struct {
	T     int64  `json:"t"` // when it ran, Unix milliseconds
	Up    bool   `json:"up"`
	MS    int64  `json:"ms"` // how long it took
	Error string `json:"error,omitempty"`
}

type resultResponse struct {
	CheckedAt  time.Time `json:"checked_at"`
	Up         bool      `json:"up"`
	LatencyMS  int64     `json:"latency_ms"`
	StatusCode *int      `json:"status_code"`
	Error      *string   `json:"error"`
	// Detail is what a successful check found, when there is something to say.
	Detail *string `json:"detail"`
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
	if r.Detail != "" {
		resp.Detail = &r.Detail
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
		Description:     st.Description,
		Type:            string(st.Type),
		Target:          st.Target,
		IntervalSeconds: int(st.Interval / time.Second),
		TimeoutSeconds:  int(st.Timeout / time.Second),
		Inverted:        st.Inverted,
		Enabled:         st.Enabled,
		CreatedAt:       st.CreatedAt,
		PushToken:       st.PushToken,
		checkOptions:    toCheckOptions(st.Monitor),
		Status:          statusPending,
		WindowSeconds:   int(window / time.Second),
		Checks:          st.Checks,
		AvgLatencyMS:    st.AvgLatency.Milliseconds(),

		RetryIntervalSeconds: int(st.RetryInterval / time.Second),
		FailureThreshold:     st.FailureThreshold,
		CertWarning:          st.CertWarning,
		Namespace:            st.Namespace,
		Source:               st.Source,
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

// maxListIDs bounds the ?ids= filter of the monitor list.
const maxListIDs = 500

// listMonitors returns the monitors with their status. Optional filters:
// ?namespace=x (empty for monitors without one) and ?ids=1,2,3. With
// ?history=N each monitor carries its N most recent checks, so a page that
// draws history for many monitors needs one request, not one per monitor.
func (h *handler) listMonitors(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	window, err := h.window(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	history := 0
	if v := query.Get("history"); v != "" {
		if history, err = strconv.Atoi(v); err != nil || history < 1 {
			writeError(w, http.StatusBadRequest, "history must be a positive integer")
			return
		}
		history = min(history, h.opts.MaxResults)
	}
	var ids map[int64]bool
	if v := query.Get("ids"); v != "" {
		parts := strings.Split(v, ",")
		if len(parts) > maxListIDs {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("ids: at most %d", maxListIDs))
			return
		}
		ids = make(map[int64]bool, len(parts))
		for _, part := range parts {
			id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
			if err != nil || id < 1 {
				writeError(w, http.StatusBadRequest, "ids must be a comma-separated list of monitor ids")
				return
			}
			ids[id] = true
		}
	}
	namespace, byNamespace := query.Get("namespace"), query.Has("namespace")

	statuses, err := h.opts.Store.ListMonitorStatuses(r.Context(), time.Now().Add(-window))
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	resp := make([]monitorResponse, 0, len(statuses))
	selected := make([]int64, 0, len(statuses))
	for _, st := range statuses {
		if (byNamespace && st.Namespace != namespace) || (ids != nil && !ids[st.ID]) {
			continue
		}
		resp = append(resp, toMonitorResponse(st, window))
		selected = append(selected, st.ID)
	}

	if history > 0 {
		recent, err := h.opts.Store.RecentCheckResults(r.Context(), selected, history)
		if err != nil {
			h.internalError(w, r, err)
			return
		}
		for i := range resp {
			results := recent[resp[i].ID]
			resp[i].History = make([]historyPoint, len(results))
			for j, res := range results {
				resp[i].History[j] = historyPoint{T: res.CheckedAt.UnixMilli(), Up: res.Up, MS: res.Latency.Milliseconds(), Error: res.Error}
			}
		}
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
	Description     string `json:"description"`
	Type            string `json:"type"`
	Target          string `json:"target"`
	IntervalSeconds int    `json:"interval_seconds"`
	TimeoutSeconds  int    `json:"timeout_seconds"`
	// RetryIntervalSeconds is optional; 0 repeats failed checks at the
	// normal interval.
	RetryIntervalSeconds int `json:"retry_interval_seconds"`
	// FailureThreshold is optional; 0 means the server default.
	FailureThreshold int  `json:"failure_threshold"`
	Inverted         bool `json:"inverted"`
	// Options of the monitor's own type; optional.
	checkOptions
}

const maxDescriptionLength = 500

// hostName matches a DNS name, with the underscores service records use.
var hostName = regexp.MustCompile(`^[A-Za-z0-9_]([A-Za-z0-9_.-]*[A-Za-z0-9_])?\.?$`)

func validHost(host string) bool {
	return net.ParseIP(host) != nil || (len(host) <= 253 && hostName.MatchString(host))
}

func validPort(port string) bool {
	n, err := strconv.Atoi(port)
	return err == nil && n >= 1 && n <= 65535
}

// validateTarget checks that target is what a monitor of the given type
// checks.
func validateTarget(typ store.MonitorType, target string) error {
	switch typ {
	case store.MonitorHTTP:
		u, err := url.Parse(target)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return errors.New("target must be an http:// or https:// URL")
		}
	case store.MonitorWebSocket:
		u, err := url.Parse(target)
		if err != nil || (u.Scheme != "ws" && u.Scheme != "wss") || u.Host == "" {
			return errors.New("target must be a ws:// or wss:// URL")
		}
	case store.MonitorTCP, store.MonitorGRPC:
		host, port, err := net.SplitHostPort(target)
		if err != nil || host == "" {
			return errors.New("target must be host:port")
		}
		if !validPort(port) {
			return errors.New("target port must be between 1 and 65535")
		}
	case store.MonitorPing, store.MonitorDNS:
		if !validHost(target) {
			return errors.New("target must be a host name or an IP address, without a scheme or port")
		}
	case store.MonitorPush:
		if target != "" {
			return errors.New("a push monitor has no target: its source reports to the monitor's push URL")
		}
	default:
		types := make([]string, len(store.MonitorTypes))
		for i, t := range store.MonitorTypes {
			types[i] = string(t)
		}
		return fmt.Errorf("type must be one of %s", strings.Join(types, ", "))
	}
	return nil
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
	req.Description = strings.TrimSpace(req.Description)
	req.Target = strings.TrimSpace(req.Target)

	if req.Name == "" {
		return errors.New("name is required")
	}
	if len(req.Description) > maxDescriptionLength {
		return fmt.Errorf("description must be at most %d bytes", maxDescriptionLength)
	}
	if err := validateTarget(store.MonitorType(req.Type), req.Target); err != nil {
		return err
	}
	minSeconds := int(minInterval / time.Second)
	if req.IntervalSeconds < minSeconds {
		return fmt.Errorf("interval_seconds must be at least %d", minSeconds)
	}
	if req.TimeoutSeconds < 1 {
		return errors.New("timeout_seconds must be at least 1")
	}
	if req.TimeoutSeconds > req.IntervalSeconds {
		return errors.New("timeout_seconds must not exceed interval_seconds")
	}
	switch {
	case req.RetryIntervalSeconds == 0:
	case store.MonitorType(req.Type) == store.MonitorPush:
		return errors.New("retry_interval_seconds does not apply to push monitors: nothing is checked that could be retried")
	case req.RetryIntervalSeconds < minSeconds:
		return fmt.Errorf("retry_interval_seconds must be 0 or at least %d", minSeconds)
	case req.RetryIntervalSeconds > req.IntervalSeconds:
		return errors.New("retry_interval_seconds must not exceed interval_seconds")
	}
	return nil
}

// monitor returns the monitor a validated request describes, on top of
// base: a monitor's current state, or the defaults of a new one.
func (req *createMonitorRequest) monitor(base store.Monitor) store.Monitor {
	base.Name = req.Name
	base.Description = req.Description
	base.Type = store.MonitorType(req.Type)
	base.Target = req.Target
	base.Interval = time.Duration(req.IntervalSeconds) * time.Second
	base.Timeout = time.Duration(req.TimeoutSeconds) * time.Second
	base.RetryInterval = time.Duration(req.RetryIntervalSeconds) * time.Second
	base.FailureThreshold = req.FailureThreshold
	base.Inverted = req.Inverted
	return base
}

// newPushToken returns the random secret of a push monitor's URL.
func newPushToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// decodeBody reads a JSON request body into v, answering 400 if it is not
// one.
func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

// storeMonitor creates m and answers with it, or with why it could not be
// created.
func (h *handler) storeMonitor(w http.ResponseWriter, r *http.Request, m store.Monitor) {
	if m.Type == store.MonitorPush {
		m.PushToken = newPushToken()
	}
	created, err := h.opts.Store.CreateMonitor(r.Context(), m)
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "a monitor with this name already exists")
		return
	}
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toMonitorResponse(store.MonitorStatus{Monitor: created}, h.opts.StatsWindow))
}

func (h *handler) createMonitor(w http.ResponseWriter, r *http.Request) {
	var req createMonitorRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if err := req.validate(h.opts.MinCheckInterval, h.opts.DefaultFailureThreshold); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	m := req.monitor(defaultOptions(store.MonitorType(req.Type)))
	m.Enabled = true
	if err := req.checkOptions.apply(&m); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.storeMonitor(w, r, m)
}

type cloneMonitorRequest struct {
	Name string `json:"name"`
}

// cloneMonitor creates a copy of a monitor under a new name. It exists
// because a client cannot copy one itself: the secrets of a monitor never
// leave the server.
func (h *handler) cloneMonitor(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "monitor not found")
		return
	}
	var req cloneMonitorRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if req.Name = strings.TrimSpace(req.Name); req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
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
	// The copy stays in the namespace, but it is not the discovered object:
	// that can be monitored only once.
	m.ID, m.Name, m.Source, m.Enabled = 0, req.Name, "", true
	h.storeMonitor(w, r, m)
}

// updateMonitorRequest carries only the fields to change.
type updateMonitorRequest struct {
	Name                 *string `json:"name"`
	Description          *string `json:"description"`
	Target               *string `json:"target"`
	IntervalSeconds      *int    `json:"interval_seconds"`
	TimeoutSeconds       *int    `json:"timeout_seconds"`
	RetryIntervalSeconds *int    `json:"retry_interval_seconds"`
	FailureThreshold     *int    `json:"failure_threshold"`
	Inverted             *bool   `json:"inverted"`
	Enabled              *bool   `json:"enabled"`
	// The options of the monitor's type, when present, replace all of them.
	checkOptions
}

func (h *handler) updateMonitor(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "monitor not found")
		return
	}
	var req updateMonitorRequest
	if !decodeBody(w, r, &req) {
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
		Name:                 current.Name,
		Description:          current.Description,
		Type:                 string(current.Type),
		Target:               current.Target,
		IntervalSeconds:      int(current.Interval / time.Second),
		TimeoutSeconds:       int(current.Timeout / time.Second),
		RetryIntervalSeconds: int(current.RetryInterval / time.Second),
		FailureThreshold:     current.FailureThreshold,
		Inverted:             current.Inverted,
	}
	if req.Name != nil {
		merged.Name = *req.Name
	}
	if req.Description != nil {
		merged.Description = *req.Description
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
	if req.RetryIntervalSeconds != nil {
		merged.RetryIntervalSeconds = *req.RetryIntervalSeconds
	}
	if req.Inverted != nil {
		merged.Inverted = *req.Inverted
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

	change := merged.monitor(current)
	if err := req.checkOptions.apply(&change); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
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
