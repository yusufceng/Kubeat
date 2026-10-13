package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

const (
	// maxUptimeWindows bounds how many windows one uptime request may ask for.
	maxUptimeWindows = 10
	// exactUptimeLimit is the window length from which uptime is computed
	// from hourly summaries instead of individual results.
	exactUptimeLimit = 24 * time.Hour
)

// bucketSizes are the bucket lengths a series may use, so that bucket edges
// fall on round times.
var bucketSizes = []time.Duration{
	time.Second, 5 * time.Second, 10 * time.Second, 15 * time.Second, 30 * time.Second,
	time.Minute, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute, 15 * time.Minute, 30 * time.Minute,
	time.Hour, 2 * time.Hour, 3 * time.Hour, 6 * time.Hour, 12 * time.Hour, 24 * time.Hour,
}

// bucketFor returns the smallest round bucket that covers span in at most
// points buckets.
func bucketFor(span time.Duration, points int) time.Duration {
	for _, size := range bucketSizes {
		if span <= size*time.Duration(points) {
			return size
		}
	}
	// Longer than the list covers: whole days.
	day := 24 * time.Hour
	days := (span/time.Duration(points) + day - 1) / day
	return days * day
}

// historyDuration parses a duration that must be positive and within the
// period for which any history is kept.
func (h *handler) historyDuration(name, value string) (time.Duration, error) {
	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration such as 1h or 168h", name)
	}
	if d > h.opts.HistoryRetention {
		return 0, fmt.Errorf("%s must not exceed the history retention of %s", name, h.opts.HistoryRetention)
	}
	return d, nil
}

// monitorExists answers 404 and returns false for an unknown monitor.
func (h *handler) monitorExists(w http.ResponseWriter, r *http.Request, id int64) bool {
	m, err := h.opts.Store.GetMonitor(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) || (err == nil && !principal(r).CanRead(m.Namespace)) {
		writeError(w, http.StatusNotFound, "monitor not found")
		return false
	}
	if err != nil {
		h.internalError(w, r, err)
		return false
	}
	return true
}

type seriesResponse struct {
	RangeSeconds  int           `json:"range_seconds"`
	BucketSeconds int           `json:"bucket_seconds"`
	From          time.Time     `json:"from"`
	To            time.Time     `json:"to"`
	Points        []seriesPoint `json:"points"`
}

type seriesPoint struct {
	Start    time.Time `json:"start"`
	Checks   int       `json:"checks"`
	UpChecks int       `json:"up_checks"`
	// Latencies cover successful checks only; null when there were none.
	AvgLatencyMS *float64 `json:"avg_latency_ms"`
	MaxLatencyMS *int64   `json:"max_latency_ms"`
}

// monitorSeries returns the monitor's history in round buckets sized so
// that there are about SeriesPoints of them; empty buckets are left out.
// The span is the last ?range=, or the timeframe ?from=&to= (RFC 3339; to
// defaults to now), which is what zooming into the chart asks for.
func (h *handler) monitorSeries(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "monitor not found")
		return
	}
	now := time.Now().UTC()
	var (
		span     time.Duration
		from, to time.Time
		err      error
	)
	if q := r.URL.Query(); q.Get("from") != "" {
		if from, err = time.Parse(time.RFC3339, q.Get("from")); err != nil {
			writeError(w, http.StatusBadRequest, "from must be an RFC 3339 time")
			return
		}
		to = now
		if v := q.Get("to"); v != "" {
			if to, err = time.Parse(time.RFC3339, v); err != nil {
				writeError(w, http.StatusBadRequest, "to must be an RFC 3339 time")
				return
			}
		}
		from, to = from.UTC(), to.UTC()
		if to.After(now) {
			to = now
		}
		// History older than the retention does not exist.
		if oldest := now.Add(-h.opts.HistoryRetention); from.Before(oldest) {
			from = oldest
		}
		if !to.After(from) {
			writeError(w, http.StatusBadRequest, "to must be after from")
			return
		}
		span = to.Sub(from)
	} else {
		if span, err = h.historyDuration("range", r.URL.Query().Get("range")); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		from, to = now.Add(-span), now
	}
	if !h.monitorExists(w, r, id) {
		return
	}

	bucket := bucketFor(span, h.opts.SeriesPoints)
	source := store.SeriesRaw
	// Raw results are exact but only reach back RawRetention; hourly
	// summaries cover the rest and are enough once buckets are that coarse.
	if from.Before(now.Add(-h.opts.RawRetention)) || bucket >= store.RollupBucket {
		source = store.SeriesRollup
		if bucket < store.RollupBucket {
			bucket = store.RollupBucket
		}
		if rem := bucket % store.RollupBucket; rem != 0 {
			bucket += store.RollupBucket - rem
		}
	}
	from = from.Truncate(bucket)

	points, err := h.opts.Store.Series(r.Context(), id, source, from, to, bucket)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	resp := seriesResponse{
		RangeSeconds:  int(span / time.Second),
		BucketSeconds: int(bucket / time.Second),
		From:          from,
		To:            to,
		Points:        make([]seriesPoint, 0, len(points)),
	}
	for _, p := range points {
		sp := seriesPoint{Start: p.Start, Checks: p.Checks, UpChecks: p.UpChecks}
		if p.UpChecks > 0 {
			avg := float64(p.AvgLatency) / float64(time.Millisecond)
			peak := p.MaxLatency.Milliseconds()
			sp.AvgLatencyMS, sp.MaxLatencyMS = &avg, &peak
		}
		resp.Points = append(resp.Points, sp)
	}
	writeJSON(w, http.StatusOK, resp)
}

type uptimeWindow struct {
	WindowSeconds int `json:"window_seconds"`
	Checks        int `json:"checks"`
	UpChecks      int `json:"up_checks"`
	// UptimePercent is null when no check ran in the window.
	UptimePercent *float64 `json:"uptime_percent"`
}

// monitorUptime returns the uptime over each of ?windows=24h,168h,...
// Windows of a day or more are computed from hourly summaries, so their
// start is rounded down to the hour.
func (h *handler) monitorUptime(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "monitor not found")
		return
	}
	raw := strings.Split(r.URL.Query().Get("windows"), ",")
	if len(raw) > maxUptimeWindows {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("at most %d windows", maxUptimeWindows))
		return
	}
	windows := make([]time.Duration, 0, len(raw))
	for _, v := range raw {
		d, err := h.historyDuration("windows", strings.TrimSpace(v))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		windows = append(windows, d)
	}
	if !h.monitorExists(w, r, id) {
		return
	}

	now := time.Now().UTC()
	resp := make([]uptimeWindow, 0, len(windows))
	for _, window := range windows {
		// Short windows are counted exactly from raw results. From a day up
		// the hourly summaries are used: a week of raw results is tens of
		// thousands of rows per monitor, and an hour more or less at the
		// start of such a window does not change the percentage.
		source, from := store.SeriesRaw, now.Add(-window)
		if window >= exactUptimeLimit || window > h.opts.RawRetention {
			source, from = store.SeriesRollup, from.Truncate(store.RollupBucket)
		}
		points, err := h.opts.Store.Series(r.Context(), id, source, from, now, store.RollupBucket)
		if err != nil {
			h.internalError(w, r, err)
			return
		}
		uw := uptimeWindow{WindowSeconds: int(window / time.Second)}
		for _, p := range points {
			uw.Checks += p.Checks
			uw.UpChecks += p.UpChecks
		}
		if uw.Checks > 0 {
			pct := 100 * float64(uw.UpChecks) / float64(uw.Checks)
			uw.UptimePercent = &pct
		}
		resp = append(resp, uw)
	}
	writeJSON(w, http.StatusOK, resp)
}
