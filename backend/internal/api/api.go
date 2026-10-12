// Package api exposes the HTTP API consumed by the frontend.
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/alert"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// maxBodyBytes bounds the size of a request body.
const maxBodyBytes = 1 << 20

// Store is the part of the persistence layer the API needs.
type Store interface {
	CreateMonitor(ctx context.Context, m store.Monitor) (store.Monitor, error)
	DeleteMonitor(ctx context.Context, id int64) error
	ListMonitorStatuses(ctx context.Context, since time.Time) ([]store.MonitorStatus, error)
	GetMonitorStatus(ctx context.Context, id int64, since time.Time) (store.MonitorStatus, error)
	ListCheckResults(ctx context.Context, monitorID int64, limit int) ([]store.CheckResult, error)
	ListIncidents(ctx context.Context, monitorID int64, limit int) ([]store.Incident, error)
	CreateChannel(ctx context.Context, c store.Channel) (store.Channel, error)
	GetChannel(ctx context.Context, id int64) (store.Channel, error)
	ListChannels(ctx context.Context) ([]store.Channel, error)
	DeleteChannel(ctx context.Context, id int64) error
}

// Notifier sends a single notification to a channel, without retries.
type Notifier interface {
	SendOnce(ctx context.Context, ch store.Channel, n alert.Notification) error
}

// Options configures the API handler.
type Options struct {
	Version string
	Store   Store
	// MinCheckInterval is the shortest interval a monitor may be created with.
	MinCheckInterval time.Duration
	// StatsWindow is the default period over which uptime is computed.
	StatsWindow time.Duration
	// MaxResults caps how many check results or incidents one request may return.
	MaxResults int
	// DefaultFailureThreshold applies to monitors created without one.
	DefaultFailureThreshold int
	Notifier                Notifier
	Logger                  *slog.Logger
}

// Health is the response body of the health endpoint.
type Health struct {
	Status  string    `json:"status"`
	Version string    `json:"version"`
	Time    time.Time `json:"time"`
}

type handler struct {
	opts Options
}

// NewHandler returns the root HTTP handler of the server.
func NewHandler(opts Options) http.Handler {
	h := &handler{opts: opts}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", h.health)
	mux.HandleFunc("GET /api/v1/monitors", h.listMonitors)
	mux.HandleFunc("POST /api/v1/monitors", h.createMonitor)
	mux.HandleFunc("GET /api/v1/monitors/{id}", h.getMonitor)
	mux.HandleFunc("DELETE /api/v1/monitors/{id}", h.deleteMonitor)
	mux.HandleFunc("GET /api/v1/monitors/{id}/results", h.listResults)
	mux.HandleFunc("GET /api/v1/incidents", h.listIncidents)
	mux.HandleFunc("GET /api/v1/channels", h.listChannels)
	mux.HandleFunc("POST /api/v1/channels", h.createChannel)
	mux.HandleFunc("DELETE /api/v1/channels/{id}", h.deleteChannel)
	mux.HandleFunc("POST /api/v1/channels/{id}/test", h.testChannel)
	return mux
}

func (h *handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, Health{
		Status:  "ok",
		Version: h.opts.Version,
		Time:    time.Now().UTC(),
	})
}

type errorBody struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorBody{Error: msg})
}

// internalError logs err and answers 500 without leaking its text.
func (h *handler) internalError(w http.ResponseWriter, r *http.Request, err error) {
	h.opts.Logger.Error("request failed", "method", r.Method, "path", r.URL.Path, "error", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}
