package api

import (
	"net/http"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// gridResponse is a monitor's check history as a grid of time buckets, for
// a calendar-like view: each cell is a bucket, coloured by how its checks
// went, and carries the error most of its failures gave.
type gridResponse struct {
	RangeSeconds  int        `json:"range_seconds"`
	BucketSeconds int        `json:"bucket_seconds"`
	From          time.Time  `json:"from"`
	To            time.Time  `json:"to"`
	Cells         []gridCell `json:"cells"` // empty buckets are left out
	// Errors are the most frequent errors of the range, most frequent first.
	Errors []gridError `json:"errors"`
}

type gridCell struct {
	Start        time.Time `json:"start"`
	Checks       int       `json:"checks"`
	Failed       int       `json:"failed"`
	AvgLatencyMS *float64  `json:"avg_latency_ms"` // null when no check succeeded
	Error        string    `json:"error,omitempty"`
}

type gridError struct {
	Error   string    `json:"error"`
	Count   int       `json:"count"`
	FirstAt time.Time `json:"first_at"`
	LastAt  time.Time `json:"last_at"`
}

// gridErrors is how many distinct errors the grid's summary lists.
const gridErrors = 5

// gridBuckets are the cell sizes of the day view: each divides the hour,
// so that a column is an hour. The smallest one not shorter than the
// monitor's interval is used, which keeps every cell from being empty.
var gridBuckets = []time.Duration{5 * time.Minute, 10 * time.Minute, 15 * time.Minute, 20 * time.Minute, 30 * time.Minute, time.Hour}

// gridBucket picks the cell size: an hour beyond a day, else by interval.
func gridBucket(span, interval time.Duration) time.Duration {
	if span > 24*time.Hour {
		return time.Hour
	}
	for _, b := range gridBuckets {
		if b >= interval {
			return b
		}
	}
	return time.Hour
}

// monitorGrid answers GET /api/v1/monitors/{id}/grid?range=24h. The range
// is cut to what the raw results still cover.
func (h *handler) monitorGrid(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "monitor not found")
		return
	}
	span, err := h.historyDuration("range", r.URL.Query().Get("range"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	m, err := h.opts.Store.GetMonitor(r.Context(), id)
	if err != nil || !principal(r).CanRead(m.Namespace) {
		if err != nil && err != store.ErrNotFound {
			h.internalError(w, r, err)
			return
		}
		writeError(w, http.StatusNotFound, "monitor not found")
		return
	}
	span = min(span, h.opts.RawRetention)

	now := time.Now().UTC()
	bucket := gridBucket(span, m.Interval)
	from := now.Add(-span).Truncate(bucket)
	cells, err := h.opts.Store.CheckGrid(r.Context(), id, from, now, bucket)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	top, err := h.opts.Store.TopErrors(r.Context(), id, from, now, gridErrors)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	resp := gridResponse{
		RangeSeconds: int(span / time.Second), BucketSeconds: int(bucket / time.Second), From: from, To: now,
		Cells: make([]gridCell, 0, len(cells)), Errors: make([]gridError, 0, len(top)),
	}
	for _, c := range cells {
		cell := gridCell{Start: c.Start, Checks: c.Checks, Failed: c.Failed, Error: c.Error}
		if c.Checks > c.Failed {
			avg := float64(c.AvgLatency) / float64(time.Millisecond)
			cell.AvgLatencyMS = &avg
		}
		resp.Cells = append(resp.Cells, cell)
	}
	for _, e := range top {
		resp.Errors = append(resp.Errors, gridError{Error: e.Error, Count: e.Count, FirstAt: e.First, LastAt: e.Last})
	}
	writeJSON(w, http.StatusOK, resp)
}
