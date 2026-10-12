package api

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"
)

// maxReportDays bounds the period of a report; the hourly rollups it reads
// are kept for UPTIME_ROLLUP_RETENTION anyway.
const maxReportDays = 365

type reportResponse struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
	Days int       `json:"days"`
	// Namespaces groups the monitors the caller may see, by namespace (the
	// empty one holds the monitors made by hand), each with its totals.
	Namespaces []namespaceReport `json:"namespaces"`
}

type namespaceReport struct {
	Namespace string `json:"namespace"`
	// Uptime is the share of successful checks over the namespace's
	// monitors, 0..100; null when none ran.
	Uptime          *float64        `json:"uptime"`
	Outages         int             `json:"outages"`
	DowntimeSeconds int             `json:"downtime_seconds"`
	Monitors        []monitorReport `json:"monitors"`
}

type monitorReport struct {
	ID                   int64    `json:"id"`
	Name                 string   `json:"name"`
	Type                 string   `json:"type"`
	Target               string   `json:"target"`
	Enabled              bool     `json:"enabled"`
	Checks               int      `json:"checks"`
	UpChecks             int      `json:"up_checks"`
	Uptime               *float64 `json:"uptime"`
	AvgLatencyMS         int64    `json:"avg_latency_ms"`
	Outages              int      `json:"outages"`
	DowntimeSeconds      int      `json:"downtime_seconds"`
	LongestOutageSeconds int      `json:"longest_outage_seconds"`
	OpenOutage           bool     `json:"open_outage"`
}

func percent(up, checks int) *float64 {
	if checks == 0 {
		return nil
	}
	v := 100 * float64(up) / float64(checks)
	return &v
}

// report summarizes the monitors the caller may see over the last N days,
// as JSON or, with format=csv, one row per monitor.
func (h *handler) report(w http.ResponseWriter, r *http.Request) {
	days := 30
	if v := r.URL.Query().Get("days"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxReportDays {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("days must be between 1 and %d", maxReportDays))
			return
		}
		days = n
	}
	// Whole hours, so that the rollups line up with the period.
	to := time.Now().UTC().Truncate(time.Hour).Add(time.Hour)
	from := to.Add(-time.Duration(days) * 24 * time.Hour)

	rows, err := h.opts.Store.Report(r.Context(), from, to)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	p := principal(r)
	only, filtered := r.URL.Query().Get("namespace"), r.URL.Query().Has("namespace")

	groups := make(map[string]*namespaceReport)
	for _, row := range rows {
		if !p.CanRead(row.Namespace) || (filtered && row.Namespace != only) {
			continue
		}
		g, ok := groups[row.Namespace]
		if !ok {
			g = &namespaceReport{Namespace: row.Namespace, Monitors: []monitorReport{}}
			groups[row.Namespace] = g
		}
		g.Monitors = append(g.Monitors, monitorReport{
			ID: row.ID, Name: row.Name, Type: string(row.Type), Target: row.Target, Enabled: row.Enabled,
			Checks: row.Checks, UpChecks: row.UpChecks, Uptime: percent(row.UpChecks, row.Checks),
			AvgLatencyMS: row.AvgLatency.Milliseconds(),
			Outages:      row.Outages, DowntimeSeconds: int(row.Downtime / time.Second),
			LongestOutageSeconds: int(row.LongestOutage / time.Second), OpenOutage: row.OpenOutage,
		})
		g.Outages += row.Outages
		g.DowntimeSeconds += int(row.Downtime / time.Second)
	}
	resp := reportResponse{From: from, To: to, Days: days, Namespaces: make([]namespaceReport, 0, len(groups))}
	for _, g := range groups {
		checks, up := 0, 0
		for _, m := range g.Monitors {
			checks, up = checks+m.Checks, up+m.UpChecks
		}
		g.Uptime = percent(up, checks)
		// Worst first within a namespace: the report is for finding trouble.
		sort.SliceStable(g.Monitors, func(i, j int) bool {
			a, b := g.Monitors[i], g.Monitors[j]
			if (a.Uptime == nil) != (b.Uptime == nil) {
				return a.Uptime != nil
			}
			if a.Uptime != nil && *a.Uptime != *b.Uptime {
				return *a.Uptime < *b.Uptime
			}
			return a.Name < b.Name
		})
		resp.Namespaces = append(resp.Namespaces, *g)
	}
	sort.Slice(resp.Namespaces, func(i, j int) bool { return resp.Namespaces[i].Namespace < resp.Namespaces[j].Namespace })

	if r.URL.Query().Get("format") == "csv" {
		writeReportCSV(w, resp)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func writeReportCSV(w http.ResponseWriter, resp reportResponse) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="kubeat-report-%dd-%s.csv"`, resp.Days, resp.To.Format("2006-01-02")))
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"namespace", "monitor", "type", "target", "enabled", "checks", "up_checks", "uptime_percent", "avg_latency_ms", "outages", "downtime_seconds", "longest_outage_seconds", "open_outage", "from", "to"})
	for _, g := range resp.Namespaces {
		for _, m := range g.Monitors {
			uptime := ""
			if m.Uptime != nil {
				uptime = strconv.FormatFloat(*m.Uptime, 'f', 3, 64)
			}
			_ = cw.Write([]string{
				g.Namespace, m.Name, m.Type, m.Target, strconv.FormatBool(m.Enabled),
				strconv.Itoa(m.Checks), strconv.Itoa(m.UpChecks), uptime, strconv.FormatInt(m.AvgLatencyMS, 10),
				strconv.Itoa(m.Outages), strconv.Itoa(m.DowntimeSeconds), strconv.Itoa(m.LongestOutageSeconds), strconv.FormatBool(m.OpenOutage),
				resp.From.Format(time.RFC3339), resp.To.Format(time.RFC3339),
			})
		}
	}
	cw.Flush()
}
