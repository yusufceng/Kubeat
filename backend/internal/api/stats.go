package api

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// statsResponse sums up the monitors a user may see over the last days:
// the figures the overview shows beside the live state.
type statsResponse struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
	Days int       `json:"days"`
	// Monitors is how many monitors the figures cover.
	Monitors int `json:"monitors"`
	// Uptime is the share of successful checks, 0..100; null when none ran.
	Checks   int      `json:"checks"`
	UpChecks int      `json:"up_checks"`
	Uptime   *float64 `json:"uptime"`
	// Outages are the "down" incidents that overlapped the period, and
	// Downtime the part of them inside it. MonitoredSeconds adds up how
	// long each monitor was checked. MTTR and MTBF follow from these (see
	// reliability); both are null when there was no outage.
	Outages          int    `json:"outages"`
	DowntimeSeconds  int64  `json:"downtime_seconds"`
	MonitoredSeconds int64  `json:"monitored_seconds"`
	MTTRSeconds      *int64 `json:"mttr_seconds"`
	MTBFSeconds      *int64 `json:"mtbf_seconds"`
	// What is open now, whatever the period: OpenIncidents is both kinds,
	// outages and certificates about to expire.
	OpenIncidents    int `json:"open_incidents"`
	OpenOutages      int `json:"open_outages"`
	OpenCertWarnings int `json:"open_cert_warnings"`
}

// stats answers GET /api/v1/stats?days=7: uptime, outages, MTTR and MTBF
// over the monitors of the namespaces the caller may read, or of those of
// them named with ?namespace= (repeatable; empty for the monitors without
// one), or of the one monitor named with ?monitor_id= (404 for one the
// caller may not read), and the incidents open now. The period ends with
// the current hour, as the report's does, and is read from the same hourly
// rollups.
func (h *handler) stats(w http.ResponseWriter, r *http.Request) {
	from, to, days, err := reportPeriod(r, 7)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	wanted, asked := r.URL.Query()["namespace"], r.URL.Query().Has("namespace")
	if len(wanted) > maxFilterNamespaces {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("namespace: at most %d", maxFilterNamespaces))
		return
	}
	p := principal(r)
	namespaces := readableNamespaces(p, wanted, asked)
	var monitorID int64
	if v := r.URL.Query().Get("monitor_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id < 1 {
			writeError(w, http.StatusBadRequest, "monitor_id must be a positive integer")
			return
		}
		m, err := h.opts.Store.GetMonitor(r.Context(), id)
		if errors.Is(err, store.ErrNotFound) || (err == nil && !p.CanRead(m.Namespace)) {
			writeError(w, http.StatusNotFound, "monitor not found")
			return
		}
		if err != nil {
			h.internalError(w, r, err)
			return
		}
		monitorID = id
	}

	rows, err := h.opts.Store.Report(r.Context(), from, to)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	resp := statsResponse{From: from, To: to, Days: days}
	var downtime, monitored time.Duration
	for _, row := range rows {
		if !p.CanRead(row.Namespace) || (namespaces != nil && !slices.Contains(namespaces, row.Namespace)) || (monitorID != 0 && row.ID != monitorID) {
			continue
		}
		resp.Monitors++
		resp.Checks += row.Checks
		resp.UpChecks += row.UpChecks
		resp.Outages += row.Outages
		downtime += row.Downtime
		monitored += row.Monitored
	}
	resp.Uptime = percent(resp.UpChecks, resp.Checks)
	resp.DowntimeSeconds, resp.MonitoredSeconds = seconds(downtime), seconds(monitored)
	resp.MTTRSeconds, resp.MTBFSeconds = reliability(resp.Outages, downtime, monitored)

	open := true
	for kind, count := range map[store.IncidentKind]*int{store.IncidentDown: &resp.OpenOutages, store.IncidentCert: &resp.OpenCertWarnings} {
		n, err := h.opts.Store.CountIncidents(r.Context(), store.IncidentFilter{Namespaces: namespaces, MonitorID: monitorID, Kind: kind, Open: &open})
		if err != nil {
			h.internalError(w, r, err)
			return
		}
		*count = n
	}
	resp.OpenIncidents = resp.OpenOutages + resp.OpenCertWarnings
	writeJSON(w, http.StatusOK, resp)
}
