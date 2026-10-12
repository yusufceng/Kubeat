// Package store is the persistence layer. Callers depend on the Store
// interface so the SQLite implementation can later be swapped for PostgreSQL.
package store

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrNotFound is returned when a requested row does not exist.
	ErrNotFound = errors.New("store: not found")
	// ErrConflict is returned when a row violates a uniqueness rule, such as
	// two monitors with the same name.
	ErrConflict = errors.New("store: conflict")
)

// MonitorType is the kind of check a monitor performs.
type MonitorType string

const (
	MonitorHTTP MonitorType = "http"
	MonitorTCP  MonitorType = "tcp"
)

// Monitor is an endpoint that is checked periodically.
type Monitor struct {
	ID       int64
	Name     string
	Type     MonitorType
	Target   string
	Interval time.Duration
	Timeout  time.Duration
	// FailureThreshold is how many consecutive failed checks open an incident.
	FailureThreshold int
	Enabled          bool
	CreatedAt        time.Time
}

// CheckResult is the outcome of a single check of a monitor.
type CheckResult struct {
	MonitorID  int64
	CheckedAt  time.Time
	Up         bool
	Latency    time.Duration
	StatusCode int    // 0 when not applicable
	Error      string // empty on success
}

// Incident is a period during which a monitor was considered down.
type Incident struct {
	ID          int64
	MonitorID   int64
	MonitorName string // filled in when read; ignored when written
	StartedAt   time.Time
	ResolvedAt  time.Time // zero while the incident is open
	Cause       string
}

// Open reports whether the incident is still ongoing.
func (i Incident) Open() bool { return i.ResolvedAt.IsZero() }

// ChannelType is the kind of destination a notification channel sends to.
type ChannelType string

const ChannelWebhook ChannelType = "webhook"

// Channel is a destination for incident notifications.
type Channel struct {
	ID        int64
	Name      string
	Type      ChannelType
	URL       string
	Enabled   bool
	CreatedAt time.Time
}

// MonitorStatus is a monitor together with its latest result and its
// statistics over a time window.
type MonitorStatus struct {
	Monitor
	Last       *CheckResult // nil until the first check has run
	Incident   *Incident    // the open incident, if any
	Checks     int          // results in the window
	UpChecks   int          // of which successful
	AvgLatency time.Duration
}

// Store persists monitors and their check results.
type Store interface {
	// CreateMonitor inserts m and returns it with ID and CreatedAt set.
	CreateMonitor(ctx context.Context, m Monitor) (Monitor, error)
	GetMonitor(ctx context.Context, id int64) (Monitor, error)
	ListMonitors(ctx context.Context) ([]Monitor, error)
	// ListMonitorStatuses returns every monitor with its latest result and
	// statistics computed over results at or after since.
	ListMonitorStatuses(ctx context.Context, since time.Time) ([]MonitorStatus, error)
	// GetMonitorStatus is ListMonitorStatuses for a single monitor.
	GetMonitorStatus(ctx context.Context, id int64, since time.Time) (MonitorStatus, error)
	// DeleteMonitor removes the monitor and all of its check results.
	DeleteMonitor(ctx context.Context, id int64) error

	// InsertCheckResults writes the batch in a single transaction.
	InsertCheckResults(ctx context.Context, results []CheckResult) error
	// ListCheckResults returns up to limit results of a monitor, newest first.
	ListCheckResults(ctx context.Context, monitorID int64, limit int) ([]CheckResult, error)
	// DeleteCheckResultsBefore removes results older than cutoff and reports
	// how many rows were deleted.
	DeleteCheckResultsBefore(ctx context.Context, cutoff time.Time) (int64, error)

	// OpenIncident records a new open incident. It returns ErrConflict when
	// the monitor already has one.
	OpenIncident(ctx context.Context, i Incident) (Incident, error)
	ResolveIncident(ctx context.Context, id int64, resolvedAt time.Time) error
	// GetOpenIncident returns ErrNotFound when the monitor has none.
	GetOpenIncident(ctx context.Context, monitorID int64) (Incident, error)
	// ListIncidents returns up to limit incidents, newest first, for one
	// monitor or for all when monitorID is 0.
	ListIncidents(ctx context.Context, monitorID int64, limit int) ([]Incident, error)

	CreateChannel(ctx context.Context, c Channel) (Channel, error)
	GetChannel(ctx context.Context, id int64) (Channel, error)
	ListChannels(ctx context.Context) ([]Channel, error)
	DeleteChannel(ctx context.Context, id int64) error

	Close() error
}
