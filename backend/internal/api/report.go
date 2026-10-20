package api

import (
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
	Uptime          *float64 `json:"uptime"`
	Outages         int      `json:"outages"`
	DowntimeSeconds int      `json:"downtime_seconds"`
	// MonitoredSeconds adds up how long each monitor was checked; MTTR and
	// MTBF are over all of them (see reliability), null without outages.
	MonitoredSeconds int64           `json:"monitored_seconds"`
	MTTRSeconds      *int64          `json:"mttr_seconds"`
	MTBFSeconds      *int64          `json:"mtbf_seconds"`
	Monitors         []monitorReport `json:"monitors"`

	// The exact sums the figures above are made from; not sent.
	downtime, monitored time.Duration
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
	// MonitoredSeconds is how long the monitor was checked in the period;
	// MTTRSeconds and MTBFSeconds are null when it had no outage.
	MonitoredSeconds int64  `json:"monitored_seconds"`
	MTTRSeconds      *int64 `json:"mttr_seconds"`
	MTBFSeconds      *int64 `json:"mtbf_seconds"`
}

func percent(up, checks int) *float64 {
	if checks == 0 {
		return nil
	}
	v := 100 * float64(up) / float64(checks)
	return &v
}

// seconds is a duration in whole seconds, rounded.
func seconds(d time.Duration) int64 {
	return int64(d.Round(time.Second) / time.Second)
}

// reliability is MTTR and MTBF over a period, in seconds. MTTR, the mean
// time to recovery, is the downtime per outage; MTBF, the mean time
// between failures, the time up per outage, where time up is what of the
// monitored time was not downtime. Outages are the "down" incidents that
// overlapped the period, clipped to it. Without one both are nil: there
// was no failure to measure from, which the UI calls "no failures".
func reliability(outages int, downtime, monitored time.Duration) (mttr, mtbf *int64) {
	if outages == 0 {
		return nil, nil
	}
	repair := seconds(downtime / time.Duration(outages))
	between := seconds(max(monitored-downtime, 0) / time.Duration(outages))
	return &repair, &between
}

// reportPeriod reads ?days= (1..maxReportDays, def when absent) and returns
// the period of that many days that ends with the current hour, in whole
// hours so that the hourly rollups line up with it.
func reportPeriod(r *http.Request, def int) (from, to time.Time, days int, err error) {
	days = def
	if v := r.URL.Query().Get("days"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxReportDays {
			return from, to, 0, fmt.Errorf("days must be between 1 and %d", maxReportDays)
		}
		days = n
	}
	to = time.Now().UTC().Truncate(time.Hour).Add(time.Hour)
	return to.Add(-time.Duration(days) * 24 * time.Hour), to, days, nil
}

// report summarizes the monitors the caller may see over the last N days,
// as JSON or, with format=csv, one row per monitor.
func (h *handler) report(w http.ResponseWriter, r *http.Request) {
	from, to, days, err := reportPeriod(r, 30)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

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
		mttr, mtbf := reliability(row.Outages, row.Downtime, row.Monitored)
		g.Monitors = append(g.Monitors, monitorReport{
			ID: row.ID, Name: row.Name, Type: string(row.Type), Target: row.Target, Enabled: row.Enabled,
			Checks: row.Checks, UpChecks: row.UpChecks, Uptime: percent(row.UpChecks, row.Checks),
			AvgLatencyMS: row.AvgLatency.Milliseconds(),
			Outages:      row.Outages, DowntimeSeconds: int(row.Downtime / time.Second),
			LongestOutageSeconds: int(row.LongestOutage / time.Second), OpenOutage: row.OpenOutage,
			MonitoredSeconds: seconds(row.Monitored), MTTRSeconds: mttr, MTBFSeconds: mtbf,
		})
		g.Outages += row.Outages
		g.DowntimeSeconds += int(row.Downtime / time.Second)
		g.downtime += row.Downtime
		g.monitored += row.Monitored
	}
	resp := reportResponse{From: from, To: to, Days: days, Namespaces: make([]namespaceReport, 0, len(groups))}
	for _, g := range groups {
		checks, up := 0, 0
		for _, m := range g.Monitors {
			checks, up = checks+m.Checks, up+m.UpChecks
		}
		g.Uptime = percent(up, checks)
		g.MonitoredSeconds = seconds(g.monitored)
		g.MTTRSeconds, g.MTBFSeconds = reliability(g.Outages, g.downtime, g.monitored)
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

// optionalSeconds is a cell for a figure that may be missing: empty then.
func optionalSeconds(v *int64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatInt(*v, 10)
}

func writeReportCSV(w http.ResponseWriter, resp reportResponse) {
	out := newCSVDownload(w, fmt.Sprintf("kubeat-report-%dd-%s.csv", resp.Days, resp.To.Format("2006-01-02")))
	// The columns added later go last, where a reader of the older ones
	// does not see them move.
	out.row("namespace", "monitor", "type", "target", "enabled", "checks", "up_checks", "uptime_percent", "avg_latency_ms", "outages",
		"downtime_seconds", "longest_outage_seconds", "open_outage", "from", "to", "monitored_seconds", "mttr_seconds", "mtbf_seconds")
	for _, g := range resp.Namespaces {
		for _, m := range g.Monitors {
			uptime := ""
			if m.Uptime != nil {
				uptime = strconv.FormatFloat(*m.Uptime, 'f', 3, 64)
			}
			out.row(
				g.Namespace, m.Name, m.Type, m.Target, strconv.FormatBool(m.Enabled),
				strconv.Itoa(m.Checks), strconv.Itoa(m.UpChecks), uptime, strconv.FormatInt(m.AvgLatencyMS, 10),
				strconv.Itoa(m.Outages), strconv.Itoa(m.DowntimeSeconds), strconv.Itoa(m.LongestOutageSeconds), strconv.FormatBool(m.OpenOutage),
				resp.From.Format(time.RFC3339), resp.To.Format(time.RFC3339),
				strconv.FormatInt(m.MonitoredSeconds, 10), optionalSeconds(m.MTTRSeconds), optionalSeconds(m.MTBFSeconds),
			)
		}
	}
	out.flush()
}
