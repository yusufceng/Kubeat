// Package store is the persistence layer. Callers depend on the Store
// interface so the SQLite implementation can later be swapped for PostgreSQL.
package store

import (
	"context"
	"errors"
	"slices"
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
	MonitorHTTP      MonitorType = "http"      // an HTTP(S) request
	MonitorTCP       MonitorType = "tcp"       // a TCP connection, optionally with a TLS handshake
	MonitorPing      MonitorType = "ping"      // an ICMP echo
	MonitorDNS       MonitorType = "dns"       // a DNS lookup
	MonitorPush      MonitorType = "push"      // the target reports in by itself
	MonitorGRPC      MonitorType = "grpc"      // the gRPC health checking protocol
	MonitorWebSocket MonitorType = "websocket" // a WebSocket handshake
	MonitorPostgres  MonitorType = "postgres"  // a query on a PostgreSQL server
	MonitorMySQL     MonitorType = "mysql"     // a query on a MySQL or MariaDB server
	MonitorRedis     MonitorType = "redis"     // a PING to a Redis server
)

// MonitorTypes lists every type.
var MonitorTypes = []MonitorType{
	MonitorHTTP, MonitorTCP, MonitorPing, MonitorDNS, MonitorPush, MonitorGRPC, MonitorWebSocket,
	MonitorPostgres, MonitorMySQL, MonitorRedis,
}

// IsDatabase reports whether monitors of the type log in to a database.
func (t MonitorType) IsDatabase() bool {
	return t == MonitorPostgres || t == MonitorMySQL || t == MonitorRedis
}

// Monitor is an endpoint that is checked periodically.
type Monitor struct {
	ID          int64
	Name        string
	Description string
	Type        MonitorType
	// Target is what is checked: a URL, host:port or a name, depending on
	// the type. Push monitors have none.
	Target   string
	Interval time.Duration
	// Timeout bounds one check. For a push monitor it is how late a report
	// may be before the monitor counts as failing.
	Timeout time.Duration
	// RetryInterval is how soon a failed check is repeated while fewer than
	// FailureThreshold checks have failed in a row; zero means Interval.
	RetryInterval time.Duration
	// Namespace groups the monitor: the namespace of the cluster object it
	// was discovered from or, for a monitor entered by hand, a group name of
	// the user's choosing (empty for none). Source identifies that cluster
	// object; it is empty for monitors entered by hand.
	Namespace string
	Source    string
	// Tags are free-form labels, sorted and without duplicates.
	Tags []string
	// FailureThreshold is how many consecutive failed checks open an incident.
	FailureThreshold int
	// Inverted turns the outcome around: the monitor is up while its target
	// fails, for things that must not be reachable.
	Inverted bool
	Enabled  bool
	// PushToken is the secret in the URL a push monitor's source reports to;
	// empty for other types.
	PushToken string
	CreatedAt time.Time

	// The options of the check. Only the part of the monitor's own type is
	// used and stored.
	HTTP      HTTPOptions
	TCP       TCPOptions
	DNS       DNSOptions
	GRPC      GRPCOptions
	WebSocket WebSocketOptions
	Database  DatabaseOptions
}

// Defaults of the check options, applied to monitors that do not set them.
const (
	DefaultHTTPMethod     = "GET"
	DefaultExpectedStatus = "200-399"
	DefaultMaxRedirects   = 10
	DefaultDNSRecordType  = "A"
	DefaultDatabaseQuery  = "SELECT 1"
)

// HTTPAuth is how an HTTP monitor authenticates.
type HTTPAuth string

const (
	HTTPAuthNone   HTTPAuth = ""
	HTTPAuthBasic  HTTPAuth = "basic"  // AuthUser and AuthSecret as user and password
	HTTPAuthBearer HTTPAuth = "bearer" // AuthSecret as the token
)

// HTTPOptions configures how an HTTP monitor is checked.
type HTTPOptions struct {
	Method string `json:"method"`
	// ExpectedStatus lists the status codes that count as success, as codes
	// and ranges: "200-299,401".
	ExpectedStatus string `json:"expected_status"`
	// Keyword, if set, must appear in the response body, or with
	// InvertKeyword must not.
	Keyword       string `json:"keyword,omitempty"`
	InvertKeyword bool   `json:"invert_keyword,omitempty"`
	// JSONPath, if set, selects a value in a JSON response, such as
	// "status" or "checks[0].state"; it is compared to JSONExpected with
	// JSONOperator (see checker.JSONOperators).
	JSONPath     string `json:"json_path,omitempty"`
	JSONOperator string `json:"json_operator,omitempty"`
	JSONExpected string `json:"json_expected,omitempty"`
	// Headers are added to the request. Values may be credentials.
	Headers map[string]string `json:"headers,omitempty"`
	// Body is sent with the request, as ContentType.
	Body        string `json:"body,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	// Auth, AuthUser and AuthSecret set the Authorization header.
	Auth       HTTPAuth `json:"auth,omitempty"`
	AuthUser   string   `json:"auth_user,omitempty"`
	AuthSecret string   `json:"auth_secret,omitempty"`
	// MaxRedirects is how many redirects are followed. With zero the first
	// response is the one that is judged, redirect or not.
	MaxRedirects int `json:"max_redirects"`
	// SkipTLSVerify accepts any certificate, for targets with a private CA.
	SkipTLSVerify bool `json:"skip_tls_verify,omitempty"`
}

// TCPOptions configures a TCP monitor.
type TCPOptions struct {
	// TLS completes a TLS handshake after connecting, which also tells when
	// the certificate expires.
	TLS           bool `json:"tls,omitempty"`
	SkipTLSVerify bool `json:"skip_tls_verify,omitempty"`
}

// DNSOptions configures a DNS monitor; its target is the name to look up.
type DNSOptions struct {
	// RecordType is one of checker.DNSRecordTypes.
	RecordType string `json:"record_type"`
	// Resolver is the server to ask, as host or host:port; empty uses the
	// resolver of the machine the checks run on.
	Resolver string `json:"resolver,omitempty"`
	// Expected, if set, must appear in at least one of the records found.
	Expected string `json:"expected,omitempty"`
}

// GRPCOptions configures a gRPC health check.
type GRPCOptions struct {
	// Service is the name asked about; empty asks for the server as a whole.
	Service       string `json:"service,omitempty"`
	TLS           bool   `json:"tls,omitempty"`
	SkipTLSVerify bool   `json:"skip_tls_verify,omitempty"`
}

// WebSocketOptions configures a WebSocket handshake check.
type WebSocketOptions struct {
	// Subprotocol, if set, is requested and must be accepted by the server.
	Subprotocol   string `json:"subprotocol,omitempty"`
	SkipTLSVerify bool   `json:"skip_tls_verify,omitempty"`
}

// DatabaseOptions configures a check that logs in to a database server:
// PostgreSQL, MySQL or Redis. The target is the server's host:port.
type DatabaseOptions struct {
	// Username and Password log in. Redis needs neither unless it is set up
	// to; with only a password it is the server's shared one.
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	// Database is the database to connect to; unused for Redis.
	Database string `json:"database,omitempty"`
	// Query is run after logging in, and must succeed; unused for Redis,
	// which is sent a PING.
	Query         string `json:"query,omitempty"`
	TLS           bool   `json:"tls,omitempty"`
	SkipTLSVerify bool   `json:"skip_tls_verify,omitempty"`
}

// PushReport is the latest report of a push monitor's source.
type PushReport struct {
	MonitorID  int64
	ReceivedAt time.Time
	// Up is false when the source itself reported a failure.
	Up      bool
	Message string
	// Latency is a duration the source chose to report; zero if none.
	Latency time.Duration
}

// CheckResult is the outcome of a single check of a monitor.
type CheckResult struct {
	MonitorID  int64
	CheckedAt  time.Time
	Up         bool
	Latency    time.Duration
	StatusCode int    // 0 when not applicable
	Error      string // empty on success
	// Detail is what a successful check found, where that says more than
	// "up": the records of a DNS lookup, the message of a push.
	Detail string
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
	ChannelWebhook    ChannelType = "webhook"    // JSON POST to any URL
	ChannelSlack      ChannelType = "slack"      // Slack incoming webhook
	ChannelTeams      ChannelType = "teams"      // Microsoft Teams workflow webhook
	ChannelEmail      ChannelType = "email"      // e-mail through the configured SMTP server
	ChannelDiscord    ChannelType = "discord"    // Discord webhook
	ChannelMattermost ChannelType = "mattermost" // Mattermost incoming webhook
	ChannelGoogleChat ChannelType = "googlechat" // Google Chat space webhook
	ChannelTelegram   ChannelType = "telegram"   // Telegram bot
	ChannelPagerDuty  ChannelType = "pagerduty"  // PagerDuty Events API v2
	ChannelNtfy       ChannelType = "ntfy"       // an ntfy topic
	ChannelGotify     ChannelType = "gotify"     // a Gotify server
)

// ChannelTypes lists every type.
var ChannelTypes = []ChannelType{
	ChannelWebhook, ChannelSlack, ChannelTeams, ChannelEmail, ChannelDiscord, ChannelMattermost,
	ChannelGoogleChat, ChannelTelegram, ChannelPagerDuty, ChannelNtfy, ChannelGotify,
}

// Channel is a destination for incident notifications.
type Channel struct {
	ID   int64
	Name string
	Type ChannelType
	// Target is where the channel sends to: a URL, for e-mail a
	// comma-separated list of addresses, for Telegram a chat. PagerDuty has
	// none.
	Target string
	// Secret is the credential of providers that have one beside the
	// target: a bot token, a routing key, an access token.
	Secret string
	// Namespaces limits the channel to the monitors of these namespaces;
	// empty means every monitor.
	Namespaces []string
	Enabled    bool
	CreatedAt  time.Time
}

// Covers reports whether notifications about the monitor go to the channel.
func (c Channel) Covers(m Monitor) bool {
	return len(c.Namespaces) == 0 || slices.Contains(c.Namespaces, m.Namespace)
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
	ID   int64
	Name string
	// StartsAt and EndsAt bound the window or, for one that repeats, its
	// first occurrence.
	StartsAt time.Time
	EndsAt   time.Time
	// Repeat says whether and how often the window comes back, at the same
	// time of day in Timezone (an IANA name; empty means UTC).
	Repeat   Repeat
	Timezone string
	// Namespace limits the window to one namespace and MonitorID to one
	// monitor; at most one of them is set, and neither means everything.
	Namespace string
	MonitorID int64
	// MonitorName is filled in when read; ignored when written.
	MonitorName string
	CreatedAt   time.Time
}

// StatusPage is a public view of some monitors: what a visitor without an
// account may see.
type StatusPage struct {
	ID int64
	// Slug is the last part of the page's address.
	Slug        string
	Title       string
	Description string
	// Announcement is a notice shown at the top of the page; empty for none.
	Announcement string
	// Published says whether the page is served.
	Published bool
	// Monitors are shown in this order.
	Monitors  []StatusPageMonitor
	CreatedAt time.Time
}

// StatusPageMonitor is one monitor on a status page.
type StatusPageMonitor struct {
	MonitorID int64
	// DisplayName is shown instead of the monitor's own name, which is
	// often an internal one; empty shows the monitor's name.
	DisplayName string
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
	// UpdateMonitor changes the monitor with m.ID. Its type, source, push
	// token and creation time never change, nor does the namespace of a
	// discovered monitor.
	UpdateMonitor(ctx context.Context, m Monitor) (Monitor, error)
	// DeleteMonitor removes the monitor and all of its check results.
	DeleteMonitor(ctx context.Context, id int64) error

	// GetMonitorByPushToken returns ErrNotFound for an unknown token.
	GetMonitorByPushToken(ctx context.Context, token string) (Monitor, error)
	// RecordPush stores a push monitor's report, replacing its previous one.
	RecordPush(ctx context.Context, report PushReport) error
	// LastPush returns ErrNotFound when the monitor never reported.
	LastPush(ctx context.Context, monitorID int64) (PushReport, error)

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
	// ListMaintenance returns up to limit windows: the repeating ones, then
	// the others, latest start first.
	ListMaintenance(ctx context.Context, limit int) ([]Maintenance, error)
	DeleteMaintenance(ctx context.Context, id int64) error
	// InMaintenance reports whether a window covers the monitor at the
	// given time.
	InMaintenance(ctx context.Context, m Monitor, at time.Time) (bool, error)

	CreateChannel(ctx context.Context, c Channel) (Channel, error)
	GetChannel(ctx context.Context, id int64) (Channel, error)
	// UpdateChannel changes everything about the channel with c.ID except
	// its type and creation time.
	UpdateChannel(ctx context.Context, c Channel) (Channel, error)
	ListChannels(ctx context.Context) ([]Channel, error)
	DeleteChannel(ctx context.Context, id int64) error

	// CreateStatusPage returns ErrConflict when the slug is taken.
	CreateStatusPage(ctx context.Context, p StatusPage) (StatusPage, error)
	GetStatusPage(ctx context.Context, id int64) (StatusPage, error)
	GetStatusPageBySlug(ctx context.Context, slug string) (StatusPage, error)
	// ListStatusPages returns every page, by title.
	ListStatusPages(ctx context.Context) ([]StatusPage, error)
	// UpdateStatusPage changes everything about the page with p.ID,
	// including which monitors it shows.
	UpdateStatusPage(ctx context.Context, p StatusPage) (StatusPage, error)
	DeleteStatusPage(ctx context.Context, id int64) error
	// StatusPageDailyUptime summarizes the checks of the page's monitors per
	// UTC day in [from, to), keyed by monitor; days without checks are left
	// out. Only Start, Checks and UpChecks of the points are set.
	StatusPageDailyUptime(ctx context.Context, pageID int64, from, to time.Time) (map[int64][]SeriesPoint, error)
	// MonitorIsPublic reports whether a published status page shows the monitor.
	MonitorIsPublic(ctx context.Context, monitorID int64) (bool, error)

	Close() error
}
