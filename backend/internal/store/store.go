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
	// Namespace groups the monitor; Source identifies the cluster object it
	// was discovered from. Both are empty for monitors entered by hand.
	Namespace string
	Source    string
	// FailureThreshold is how many consecutive failed checks open an incident.
	FailureThreshold int
	Enabled          bool
	CreatedAt        time.Time
	// HTTP holds the options of an HTTP check; unused for other types.
	HTTP HTTPOptions
}

// Defaults of HTTPOptions, applied by the database to monitors that do not
// set them.
const (
	DefaultHTTPMethod     = "GET"
	DefaultExpectedStatus = "200-399"
)

// HTTPOptions configures how an HTTP monitor is checked.
type HTTPOptions struct {
	// Method is GET or HEAD.
	Method string
	// ExpectedStatus lists the status codes that count as success, as codes
	// and ranges: "200-299,401".
	ExpectedStatus string
	// Keyword, if set, must appear in the response body.
	Keyword string
	// Headers are added to the request. Values may be credentials.
	Headers map[string]string
	// SkipTLSVerify accepts any certificate, for targets with a private CA.
	SkipTLSVerify bool
}

// CheckResult is the outcome of a single check of a monitor.
type CheckResult struct {
	MonitorID  int64
	CheckedAt  time.Time
	Up         bool
	Latency    time.Duration
	StatusCode int    // 0 when not applicable
	Error      string // empty on success
	// CertExpiresAt is when the target's TLS certificate expires; zero when
	// the check saw no certificate.
	CertExpiresAt time.Time
}

// IncidentKind says what an incident is about.
type IncidentKind string

const (
	IncidentDown IncidentKind = "down" // the target is failing its checks
	IncidentCert IncidentKind = "cert" // the target's certificate expires soon
)

// IncidentKinds lists every kind.
var IncidentKinds = []IncidentKind{IncidentDown, IncidentCert}

// Incident is a period during which something was wrong with a monitor.
type Incident struct {
	ID        int64
	MonitorID int64
	// Kind defaults to IncidentDown when written empty.
	Kind        IncidentKind
	MonitorName string // filled in when read; ignored when written
	StartedAt   time.Time
	ResolvedAt  time.Time // zero while the incident is open
	Cause       string
	// LastNotifiedAt is when a notification about the incident was last
	// sent; zero if unknown.
	LastNotifiedAt time.Time
}

// Open reports whether the incident is still ongoing.
func (i Incident) Open() bool { return i.ResolvedAt.IsZero() }

// ChannelType is the kind of destination a notification channel sends to.
type ChannelType string

const (
	ChannelWebhook ChannelType = "webhook" // JSON POST to any URL
	ChannelSlack   ChannelType = "slack"   // Slack incoming webhook
	ChannelTeams   ChannelType = "teams"   // Microsoft Teams workflow webhook
	ChannelEmail   ChannelType = "email"   // e-mail through the configured SMTP server
)

// Channel is a destination for incident notifications.
type Channel struct {
	ID   int64
	Name string
	Type ChannelType
	// Target is a URL, or for e-mail a comma-separated list of addresses.
	Target    string
	Enabled   bool
	CreatedAt time.Time
}

// RollupBucket is the size of a stored summary bucket.
const RollupBucket = time.Hour

// SeriesPoint summarizes the checks of one monitor in one time bucket.
type SeriesPoint struct {
	Start    time.Time
	Checks   int
	UpChecks int
	// AvgLatency and MaxLatency cover successful checks only; both are zero
	// when UpChecks is zero.
	AvgLatency time.Duration
	MaxLatency time.Duration
}

// SeriesSource selects which table a series is read from.
type SeriesSource int

const (
	// SeriesRaw reads individual check results; any bucket size works but
	// only the raw retention period is available.
	SeriesRaw SeriesSource = iota
	// SeriesRollup reads hourly summaries; the bucket must be a multiple of
	// RollupBucket.
	SeriesRollup
)

// Maintenance is a period during which no incident is opened for the
// monitors it covers: one monitor, one namespace, or all of them.
type Maintenance struct {
	ID       int64
	Name     string
	StartsAt time.Time
	EndsAt   time.Time
	// Namespace limits the window to one namespace and MonitorID to one
	// monitor; at most one of them is set, and neither means everything.
	Namespace string
	MonitorID int64
	// MonitorName is filled in when read; ignored when written.
	MonitorName string
	CreatedAt   time.Time
}

// MonitorStatus is a monitor together with its latest result and its
// statistics over a time window.
type MonitorStatus struct {
	Monitor
	Last     *CheckResult // nil until the first check has run
	Incident *Incident    // the open outage, if any
	// CertWarning reports an open certificate incident.
	CertWarning bool
	// MaintenanceUntil is when the maintenance covering the monitor ends;
	// zero when it is not in maintenance.
	MaintenanceUntil time.Time
	Checks           int // results in the window
	UpChecks         int // of which successful
	// AvgLatency is the average over successful checks; zero if none.
	AvgLatency time.Duration
}

// Store persists monitors and their check results.
type Store interface {
	// CreateMonitor inserts m and returns it with ID and CreatedAt set.
	CreateMonitor(ctx context.Context, m Monitor) (Monitor, error)
	GetMonitor(ctx context.Context, id int64) (Monitor, error)
	ListMonitors(ctx context.Context) ([]Monitor, error)
	// ListMonitorStatuses returns every monitor with its latest result and
	// statistics over the window that starts at since. The statistics are
	// read from the hourly summaries, which keeps this fast however many
	// results there are: the window starts on the hour at or before since,
	// and the newest results count once the summaries are next refreshed.
	ListMonitorStatuses(ctx context.Context, since time.Time) ([]MonitorStatus, error)
	// GetMonitorStatus is ListMonitorStatuses for a single monitor.
	GetMonitorStatus(ctx context.Context, id int64, since time.Time) (MonitorStatus, error)
	// UpdateMonitor changes the editable fields of the monitor with m.ID:
	// name, target, interval, timeout, failure threshold and enabled. Type,
	// namespace and source never change.
	UpdateMonitor(ctx context.Context, m Monitor) (Monitor, error)
	// DeleteMonitor removes the monitor and all of its check results.
	DeleteMonitor(ctx context.Context, id int64) error

	// InsertCheckResults writes the batch in a single transaction.
	InsertCheckResults(ctx context.Context, results []CheckResult) error
	// ListCheckResults returns up to limit results of a monitor, newest first.
	ListCheckResults(ctx context.Context, monitorID int64, limit int) ([]CheckResult, error)
	// RecentCheckResults returns up to limit results, newest first, for each
	// of the given monitors, keyed by monitor ID.
	RecentCheckResults(ctx context.Context, monitorIDs []int64, limit int) (map[int64][]CheckResult, error)
	// DeleteCheckResultsBefore removes results older than cutoff and reports
	// how many rows were deleted.
	DeleteCheckResultsBefore(ctx context.Context, cutoff time.Time) (int64, error)

	// RollupCheckResults (re)computes the hourly summaries of every monitor
	// for the hours in [from, to). from must be on an hour boundary, and the
	// raw results of those hours must still be complete.
	RollupCheckResults(ctx context.Context, from, to time.Time) error
	// DeleteRollupsBefore removes summaries of hours before cutoff.
	DeleteRollupsBefore(ctx context.Context, cutoff time.Time) (int64, error)
	// Series returns one point per non-empty bucket in [from, to), oldest
	// first. Buckets are aligned to multiples of bucket since the Unix epoch.
	Series(ctx context.Context, monitorID int64, source SeriesSource, from, to time.Time, bucket time.Duration) ([]SeriesPoint, error)

	// OpenIncident records a new open incident. It returns ErrConflict when
	// the monitor already has an open one of that kind.
	OpenIncident(ctx context.Context, i Incident) (Incident, error)
	ResolveIncident(ctx context.Context, id int64, resolvedAt time.Time) error
	// MarkIncidentNotified records that a notification about the incident
	// was sent at the given time.
	MarkIncidentNotified(ctx context.Context, id int64, at time.Time) error
	// GetOpenIncident returns ErrNotFound when the monitor has none of that kind.
	GetOpenIncident(ctx context.Context, monitorID int64, kind IncidentKind) (Incident, error)
	// ListIncidents returns up to limit incidents, newest first, for one
	// monitor or for all when monitorID is 0.
	ListIncidents(ctx context.Context, monitorID int64, limit int) ([]Incident, error)

	CreateMaintenance(ctx context.Context, m Maintenance) (Maintenance, error)
	// ListMaintenance returns up to limit windows, latest start first.
	ListMaintenance(ctx context.Context, limit int) ([]Maintenance, error)
	DeleteMaintenance(ctx context.Context, id int64) error
	// InMaintenance reports whether a window covers the monitor at the
	// given time.
	InMaintenance(ctx context.Context, m Monitor, at time.Time) (bool, error)

	CreateChannel(ctx context.Context, c Channel) (Channel, error)
	GetChannel(ctx context.Context, id int64) (Channel, error)
	ListChannels(ctx context.Context) ([]Channel, error)
	DeleteChannel(ctx context.Context, id int64) error

	Close() error
}
