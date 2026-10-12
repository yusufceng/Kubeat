// Package api exposes the HTTP API consumed by the frontend.
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/alert"
	"github.com/yusufceng/Kubeat/backend/internal/k8s"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// maxBodyBytes bounds the size of a request body.
const maxBodyBytes = 1 << 20

// Store is the part of the persistence layer the API needs.
type Store interface {
	CreateMonitor(ctx context.Context, m store.Monitor) (store.Monitor, error)
	ListMonitors(ctx context.Context) ([]store.Monitor, error)
	GetMonitor(ctx context.Context, id int64) (store.Monitor, error)
	UpdateMonitor(ctx context.Context, m store.Monitor) (store.Monitor, error)
	DeleteMonitor(ctx context.Context, id int64) error
	GetOpenIncident(ctx context.Context, monitorID int64, kind store.IncidentKind) (store.Incident, error)
	ResolveIncident(ctx context.Context, id int64, resolvedAt time.Time) error
	ListMonitorStatuses(ctx context.Context, since time.Time) ([]store.MonitorStatus, error)
	GetMonitorStatus(ctx context.Context, id int64, since time.Time) (store.MonitorStatus, error)
	ListCheckResults(ctx context.Context, monitorID int64, limit int) ([]store.CheckResult, error)
	Series(ctx context.Context, monitorID int64, source store.SeriesSource, from, to time.Time, bucket time.Duration) ([]store.SeriesPoint, error)
	ListIncidents(ctx context.Context, monitorID int64, limit int) ([]store.Incident, error)
	CreateMaintenance(ctx context.Context, m store.Maintenance) (store.Maintenance, error)
	ListMaintenance(ctx context.Context, limit int) ([]store.Maintenance, error)
	DeleteMaintenance(ctx context.Context, id int64) error
	CreateChannel(ctx context.Context, c store.Channel) (store.Channel, error)
	GetChannel(ctx context.Context, id int64) (store.Channel, error)
	ListChannels(ctx context.Context) ([]store.Channel, error)
	DeleteChannel(ctx context.Context, id int64) error
}

// Discovery lists what could be monitored in the cluster.
type Discovery interface {
	Namespaces(ctx context.Context) ([]string, error)
	Candidates(ctx context.Context, namespace string) ([]k8s.Candidate, error)
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
	// RawRetention is how far back individual results exist; beyond it only
	// hourly summaries do, for up to HistoryRetention.
	RawRetention     time.Duration
	HistoryRetention time.Duration
	// SeriesPoints is the number of buckets a series aims for.
	SeriesPoints int
	// DefaultFailureThreshold applies to monitors created without one.
	DefaultFailureThreshold int
	Notifier                Notifier
	// Metrics, if set, is served at /metrics.
	Metrics http.Handler
	// EmailEnabled says whether an SMTP server is configured; e-mail
	// channels cannot be created without one.
	EmailEnabled bool
	// Discovery is nil when no cluster is available; DiscoveryStatus then
	// says why. Otherwise DiscoveryStatus describes the connection.
	Discovery       Discovery
	DiscoveryStatus string
	Logger          *slog.Logger
}

// Revision identifies the shape of the API. Increase it whenever a response
// gains something the frontend depends on, and raise the frontend's required
// revision in the same change; a frontend talking to an older backend can
// then say so instead of failing on a missing field.
const Revision = 7

// Health is the response body of the health endpoint.
type Health struct {
	Status   string    `json:"status"`
	Version  string    `json:"version"`
	Revision int       `json:"api_revision"`
	Time     time.Time `json:"time"`
}

type handler struct {
	opts Options
}

// NewHandler returns the root HTTP handler of the server.
func NewHandler(opts Options) http.Handler {
	h := &handler{opts: opts}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", h.health)
	if opts.Metrics != nil {
		mux.Handle("GET /metrics", opts.Metrics)
	}
	mux.HandleFunc("GET /api/v1/monitors", h.listMonitors)
	mux.HandleFunc("POST /api/v1/monitors", h.createMonitor)
	mux.HandleFunc("GET /api/v1/monitors/{id}", h.getMonitor)
	mux.HandleFunc("PATCH /api/v1/monitors/{id}", h.updateMonitor)
	mux.HandleFunc("DELETE /api/v1/monitors/{id}", h.deleteMonitor)
	mux.HandleFunc("GET /api/v1/monitors/{id}/results", h.listResults)
	mux.HandleFunc("GET /api/v1/monitors/{id}/series", h.monitorSeries)
	mux.HandleFunc("GET /api/v1/monitors/{id}/uptime", h.monitorUptime)
	mux.HandleFunc("GET /api/v1/discovery", h.discoveryStatus)
	mux.HandleFunc("GET /api/v1/discovery/namespaces", h.discoveryNamespaces)
	mux.HandleFunc("GET /api/v1/discovery/candidates", h.discoveryCandidates)
	mux.HandleFunc("POST /api/v1/discovery/import", h.discoveryImport)
	mux.HandleFunc("GET /api/v1/incidents", h.listIncidents)
	mux.HandleFunc("GET /api/v1/maintenance", h.listMaintenance)
	mux.HandleFunc("POST /api/v1/maintenance", h.createMaintenance)
	mux.HandleFunc("DELETE /api/v1/maintenance/{id}", h.deleteMaintenance)
	mux.HandleFunc("GET /api/v1/channels", h.listChannels)
	mux.HandleFunc("POST /api/v1/channels", h.createChannel)
	mux.HandleFunc("DELETE /api/v1/channels/{id}", h.deleteChannel)
	mux.HandleFunc("POST /api/v1/channels/{id}/test", h.testChannel)
	return mux
}

func (h *handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, Health{
		Status:   "ok",
		Version:  h.opts.Version,
		Revision: Revision,
		Time:     time.Now().UTC(),
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
