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
	ID        int64
	Name      string
	Type      MonitorType
	Target    string
	Interval  time.Duration
	Timeout   time.Duration
	Enabled   bool
	CreatedAt time.Time
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

// MonitorStatus is a monitor together with its latest result and its
// statistics over a time window.
type MonitorStatus struct {
	Monitor
	Last       *CheckResult // nil until the first check has run
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

	Close() error
}
