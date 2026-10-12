package api

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// Where a maintenance window is relative to now.
const (
	maintenanceActive   = "active"
	maintenanceUpcoming = "upcoming"
	maintenancePast     = "past"
)

type maintenanceResponse struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	// StartsAt and EndsAt bound the window. For one that repeats they are
	// the occurrence going on now or, when none is, the next one.
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
	State    string    `json:"state"`
	// Repeat is "", "daily", "weekly" or "monthly"; Timezone is the zone in
	// which a repeating window keeps its time of day.
	Repeat   string `json:"repeat"`
	Timezone string `json:"timezone"`
	// Scope: one monitor, one namespace, or (both empty) every monitor.
	Namespace   string `json:"namespace"`
	MonitorID   *int64 `json:"monitor_id"`
	MonitorName string `json:"monitor_name"`
}

func toMaintenanceResponse(m store.Maintenance, now time.Time) maintenanceResponse {
	resp := maintenanceResponse{
		ID: m.ID, Name: m.Name, StartsAt: m.StartsAt, EndsAt: m.EndsAt, Repeat: string(m.Repeat), Timezone: m.Timezone,
		State: maintenanceActive, Namespace: m.Namespace, MonitorName: m.MonitorName,
	}
	start, end, more := m.Occurrence(now)
	switch {
	case !more:
		resp.State = maintenancePast
	case now.Before(start):
		resp.State = maintenanceUpcoming
		resp.StartsAt, resp.EndsAt = start, end
	default:
		resp.StartsAt, resp.EndsAt = start, end
	}
	if m.MonitorID != 0 {
		resp.MonitorID = &m.MonitorID
	}
	return resp
}

func (h *handler) listMaintenance(w http.ResponseWriter, r *http.Request) {
	limit, err := h.limit(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	windows, err := h.opts.Store.ListMaintenance(r.Context(), limit)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	now := time.Now()
	p := principal(r)
	resp := make([]maintenanceResponse, 0, len(windows))
	for _, m := range windows {
		if !maintenanceVisible(p, m) {
			continue
		}
		resp = append(resp, toMaintenanceResponse(m, now))
	}
	writeJSON(w, http.StatusOK, resp)
}

type createMaintenanceRequest struct {
	Name string `json:"name"`
	// StartsAt defaults to now.
	StartsAt *time.Time `json:"starts_at"`
	// Exactly one of EndsAt and DurationSeconds says when the window ends.
	EndsAt          *time.Time `json:"ends_at"`
	DurationSeconds int        `json:"duration_seconds"`
	// At most one of Namespace and MonitorID; neither means every monitor.
	Namespace string `json:"namespace"`
	MonitorID int64  `json:"monitor_id"`
	// Repeat makes the window come back: "daily", "weekly" or "monthly",
	// at the same time of day in Timezone (an IANA name, UTC if empty).
	Repeat   string `json:"repeat"`
	Timezone string `json:"timezone"`
}

func (h *handler) createMaintenance(w http.ResponseWriter, r *http.Request) {
	var req createMaintenanceRequest
	if !decodeBody(w, r, &req) {
		return
	}

	now := time.Now().UTC()
	m := store.Maintenance{
		Name:      strings.TrimSpace(req.Name),
		StartsAt:  now,
		Namespace: strings.TrimSpace(req.Namespace),
		MonitorID: req.MonitorID,
		Repeat:    store.Repeat(req.Repeat),
	}
	switch m.Repeat {
	case store.RepeatNone:
		if req.Timezone != "" {
			writeError(w, http.StatusBadRequest, "timezone applies only to a window that repeats")
			return
		}
	case store.RepeatDaily, store.RepeatWeekly, store.RepeatMonthly:
		m.Timezone = strings.TrimSpace(req.Timezone)
		if m.Timezone == "" {
			m.Timezone = "UTC"
		}
		// "Local" would be the server's zone, which no user means.
		if _, err := time.LoadLocation(m.Timezone); err != nil || m.Timezone == "Local" {
			writeError(w, http.StatusBadRequest, "timezone must be an IANA name such as Europe/Istanbul")
			return
		}
	default:
		writeError(w, http.StatusBadRequest, "repeat must be daily, weekly, monthly or empty")
		return
	}
	if req.StartsAt != nil {
		m.StartsAt = *req.StartsAt
	}
	switch {
	case m.Name == "":
		writeError(w, http.StatusBadRequest, "name is required")
		return
	case (req.EndsAt == nil) == (req.DurationSeconds == 0):
		writeError(w, http.StatusBadRequest, "give either ends_at or duration_seconds")
		return
	case req.DurationSeconds < 0:
		writeError(w, http.StatusBadRequest, "duration_seconds must be positive")
		return
	case m.Namespace != "" && m.MonitorID != 0:
		writeError(w, http.StatusBadRequest, "give a namespace or a monitor_id, not both")
		return
	case m.MonitorID < 0:
		writeError(w, http.StatusBadRequest, "monitor_id must be positive")
		return
	}
	if req.EndsAt != nil {
		m.EndsAt = *req.EndsAt
	} else {
		m.EndsAt = m.StartsAt.Add(time.Duration(req.DurationSeconds) * time.Second)
	}
	if !m.EndsAt.After(m.StartsAt) {
		writeError(w, http.StatusBadRequest, "the window must end after it starts")
		return
	}
	if m.Repeat == store.RepeatNone && !m.EndsAt.After(now) {
		writeError(w, http.StatusBadRequest, "the window is already over")
		return
	}
	if period := m.Repeat.Period(); period > 0 && m.EndsAt.Sub(m.StartsAt) >= period {
		writeError(w, http.StatusBadRequest, "a repeating window must be shorter than the time between two occurrences")
		return
	}
	if m.MonitorID != 0 {
		monitor, err := h.opts.Store.GetMonitor(r.Context(), m.MonitorID)
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusBadRequest, "monitor_id does not exist")
			return
		}
		if err != nil {
			h.internalError(w, r, err)
			return
		}
		m.MonitorName, m.MonitorNamespace = monitor.Name, monitor.Namespace
	}
	if p := principal(r); !canEditMaintenance(p, m) {
		scopeError(w, p)
		return
	}

	created, err := h.opts.Store.CreateMaintenance(r.Context(), m)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	created.MonitorName = m.MonitorName
	h.audit(r, "create", "maintenance", created.ID, created.Name)
	writeJSON(w, http.StatusCreated, toMaintenanceResponse(created, now))
}

func (h *handler) deleteMaintenance(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "maintenance window not found")
		return
	}
	// Which window it is: the list is bounded, and windows are few.
	windows, err := h.opts.Store.ListMaintenance(r.Context(), h.opts.MaxResults)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	i := slices.IndexFunc(windows, func(m store.Maintenance) bool { return m.ID == id })
	p := principal(r)
	if i < 0 || !maintenanceVisible(p, windows[i]) {
		writeError(w, http.StatusNotFound, "maintenance window not found")
		return
	}
	if !canEditMaintenance(p, windows[i]) {
		scopeError(w, p)
		return
	}
	h.audit(r, "delete", "maintenance", id, windows[i].Name)
	err = h.opts.Store.DeleteMaintenance(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "maintenance window not found")
		return
	}
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
