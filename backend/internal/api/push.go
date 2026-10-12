package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

const (
	// pushPath is where the sources of push monitors report. The token in
	// the path is the only credential: a job in the cluster has no API token.
	pushPath = "/api/push/"
	// maxPushMessage bounds the message a report may carry.
	maxPushMessage = 250
	// maxPushLatency bounds the duration a report may claim.
	maxPushLatency = 24 * time.Hour
)

type pushResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// push records the report of a push monitor's source:
//
//	/api/push/{token}?status=up|down&msg=text&ping=milliseconds
//
// Every parameter is optional; without status the report means "up". The
// scheduler turns the latest report into a check result once per interval.
func (h *handler) push(w http.ResponseWriter, r *http.Request) {
	m, err := h.opts.Store.GetMonitorByPushToken(r.Context(), r.PathValue("token"))
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, pushResponse{Error: "no monitor has this push token"})
		return
	}
	if err != nil {
		h.internalError(w, r, err)
		return
	}

	query := r.URL.Query()
	report := store.PushReport{MonitorID: m.ID, ReceivedAt: time.Now().UTC(), Up: true}
	switch strings.ToLower(query.Get("status")) {
	case "", "up":
	case "down":
		report.Up = false
	default:
		writeJSON(w, http.StatusBadRequest, pushResponse{Error: "status must be up or down"})
		return
	}
	report.Message = strings.TrimSpace(query.Get("msg"))
	if len(report.Message) > maxPushMessage {
		writeJSON(w, http.StatusBadRequest, pushResponse{Error: "msg is too long"})
		return
	}
	if v := query.Get("ping"); v != "" {
		ms, err := strconv.ParseFloat(v, 64)
		if err != nil || ms < 0 || ms > float64(maxPushLatency/time.Millisecond) {
			writeJSON(w, http.StatusBadRequest, pushResponse{Error: "ping must be a duration in milliseconds"})
			return
		}
		report.Latency = time.Duration(ms * float64(time.Millisecond))
	}

	if err := h.opts.Store.RecordPush(r.Context(), report); err != nil {
		h.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, pushResponse{OK: true})
}
