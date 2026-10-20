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
	MonitorWorkload  MonitorType = "workload"  // the ready pods of a Kubernetes workload
)

// MonitorTypes lists every type.
var MonitorTypes = []MonitorType{
	MonitorHTTP, MonitorTCP, MonitorPing, MonitorDNS, MonitorPush, MonitorGRPC, MonitorWebSocket,
	MonitorPostgres, MonitorMySQL, MonitorRedis, MonitorWorkload,
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
	// Managed marks a monitor that discovery created from an annotated
	// cluster object and keeps in step with it; it is removed with the
	// annotation. Set at creation and never changed.
	Managed bool
	// Tags are free-form labels, sorted and without duplicates.
	Tags []string
	// FailureThreshold is how many consecutive failed checks open an incident.
	FailureThreshold int
	// Inverted turns the outcome around: the monitor is up while its target
	// fails, for things that must not be reachable.
	Inverted bool
	// EachPod makes an HTTP or TCP monitor discovered from a service port
	// check every ready pod behind it instead of the service's address.
	EachPod bool
	Enabled bool
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
	Workload  WorkloadOptions
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

// WorkloadOptions configures the check of a Kubernetes workload, whose
// target is namespace/kind/name.
type WorkloadOptions struct {
	// MinReady is how many pods must be ready. Zero means all the workload
	// is meant to have, so that any pod missing is a failure.
	MinReady int `json:"min_ready,omitempty"`
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
	// Diagnostics says why a failed check failed, as far as the check could
	// tell; nil for a successful one, which keeps none.
	Diagnostics *Diagnostics
}

// Diagnostics is what a failed check found out about why it failed: what
// the failure most likely means and, for an HTTP(S) or WebSocket check,
// where the time went and who answered how. It never holds any of an
// answer's body, and only a few of its headers (see the checker): a target
// is set by whoever may edit a monitor, and its results must not become a
// way to read what an internal service answers.
type Diagnostics struct {
	// Explanation is what the failure most likely means; nil when nothing
	// recognized it.
	Explanation *Explanation `json:"explanation,omitempty"`
	// Timing is where the time of the request went; nil for checks that are
	// not HTTP(S) requests.
	Timing *Timing `json:"timing,omitempty"`
	// RemoteAddr is the address the check connected to ("ip:port"), or the
	// last one it tried when it could not connect.
	RemoteAddr string `json:"remote_addr,omitempty"`
	// TLS describes the TLS connection and the certificate the server
	// presented, one that failed verification included; nil for a plain
	// connection.
	TLS *TLSDetails `json:"tls,omitempty"`
	// StatusLine is the status line of the answer that was judged, such as
	// "HTTP/1.1 503 Service Unavailable"; empty when none came.
	StatusLine string `json:"status_line,omitempty"`
	// Headers are the few headers of that answer that are kept, by their
	// lower-case names.
	Headers map[string]string `json:"headers,omitempty"`
	// Redirects is how many redirects were followed before that answer.
	Redirects int `json:"redirects,omitempty"`
	// Pod is the pod these details are about, for a monitor that checks
	// each pod behind its service: the first of those that failed.
	Pod string `json:"pod,omitempty"`
}

// Explanation is what a failure most likely means: a stable code (see the
// explain package) with the few values its wording needs, so that each
// reader can have it in their own language.
type Explanation struct {
	Code   string            `json:"code"`
	Params map[string]string `json:"params,omitempty"`
}

// Timing is where the time of an HTTP(S) request went, in milliseconds. The
// phases are those of the last request, the one whose answer was judged
// when redirects were followed; a phase that did not happen is nil: no name
// lookup for an IP address, no connection for one that was reused, no
// handshake for plain HTTP, no first byte when no answer came.
type Timing struct {
	DNS     *float64 `json:"dns_ms,omitempty"`
	Connect *float64 `json:"connect_ms,omitempty"`
	TLS     *float64 `json:"tls_ms,omitempty"`
	// FirstByte is from the start of the request until the first byte of
	// its answer, the phases above included.
	FirstByte *float64 `json:"first_byte_ms,omitempty"`
	// Total is the whole check, redirects included.
	Total float64 `json:"total_ms"`
}

// TLSDetails describes a TLS connection and the leaf certificate of its
// server.
type TLSDetails struct {
	// Version is the negotiated protocol, such as "TLS 1.3"; empty when the
	// handshake failed before one was agreed.
	Version string `json:"version,omitempty"`
	// Subject and Issuer are the common names of the certificate and of
	// whoever signed it.
	Subject string `json:"subject,omitempty"`
	Issuer  string `json:"issuer,omitempty"`
	// SANs are the first few names and addresses the certificate is valid
	// for; MoreSANs counts the rest.
	SANs     []string  `json:"sans,omitempty"`
	MoreSANs int       `json:"more_sans,omitempty"`
	NotAfter time.Time `json:"not_after,omitzero"`
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
	// MonitorNamespace is the namespace or group of the monitor, filled in
	// when read, for scoping what a user may see.
	MonitorNamespace string
	StartedAt        time.Time
	ResolvedAt       time.Time // zero while the incident is open
	Cause            string
	// LastNotifiedAt is when a notification about the incident was last
	// sent; zero if unknown.
	LastNotifiedAt time.Time
	// OpenedAt is when Kubeat opened the incident: the check that reached
	// the failure threshold, while StartedAt is the first failure of that
	// streak. Zero for incidents opened before it was recorded.
	OpenedAt time.Time
}

// Open reports whether the incident is still ongoing.
func (i Incident) Open() bool { return i.ResolvedAt.IsZero() }

// IncidentSort is what a list of incidents is ordered by.
type IncidentSort string

const (
	SortByStart    IncidentSort = "started_at" // when they began
	SortByDuration IncidentSort = "duration"   // how long they lasted, or have lasted so far
	SortByMonitor  IncidentSort = "monitor"    // the name of their monitor
)

// IncidentSorts lists every order.
var IncidentSorts = []IncidentSort{SortByStart, SortByDuration, SortByMonitor}

// IncidentFilter selects incidents and orders them. Its zero value selects
// every incident, oldest first.
type IncidentFilter struct {
	// Namespaces, unless nil, limits the incidents to those of monitors in
	// these namespaces or groups ("" for monitors without one). Empty but
	// not nil, it selects none: that is what a user whose scope and filter
	// do not meet may see.
	Namespaces []string
	MonitorID  int64        // 0 for every monitor
	Kind       IncidentKind // "" for both kinds
	// Open selects the open incidents (true) or the resolved ones (false);
	// nil selects both.
	Open *bool
	// MonitorName selects the monitors whose name contains it, whatever
	// the case of its ASCII letters.
	MonitorName string
	// From and To select the incidents that overlap [From, To); a zero
	// bound leaves that side open.
	From, To time.Time
	// Sort is SortByStart when empty; Desc puts the largest first. Ties
	// are broken by start, newest first, so that pages do not shift.
	Sort IncidentSort
	Desc bool
	// Limit and Offset page through the list; a zero Limit has no bound.
	Limit, Offset int
	// Now is where open incidents end when they are ordered by duration.
	Now time.Time
}

// Delivery is one attempt to tell a channel about an incident, as it ended
// after its retries: who heard what about it, and when.
type Delivery struct {
	ID         int64
	IncidentID int64
	// MonitorID is the incident's monitor; written from the incident.
	MonitorID int64
	// The channel, as it was: the record outlives a channel deleted later.
	ChannelID   int64
	ChannelName string
	ChannelType ChannelType
	// Event is what the message reported (down, up, reminder,
	// cert_expiring or cert_renewed), EventAt when that happened as Kubeat
	// saw it. GroupSize is how many monitors the message was about: more
	// than one for a grouped message, which is a row for each of them.
	Event     string
	EventAt   time.Time
	GroupSize int
	// At is when the attempt ended. Error says why it failed, redacted of
	// the channel's address and secret; empty when Delivered.
	At        time.Time
	Delivered bool
	Error     string
}

// Ends splits keep between the oldest and the newest of total entries: all
// of them when they fit, otherwise the oldest half of keep (rounded up) and
// the newest half. A long outage is told by how it began and how it ended;
// the rest is left out between the two.
func Ends(total, keep int) (oldest, newest int) {
	if total <= keep {
		return total, 0
	}
	oldest = (keep + 1) / 2
	return oldest, keep - oldest
}

// IncidentNote is a note someone left on an incident.
type IncidentNote struct {
	ID         int64
	IncidentID int64
	// AuthorID is the account that wrote the note: zero for the API token,
	// and once the account is deleted. Author is its username then.
	AuthorID int64
	Author   string
	Text     string
	// ShowOnStatusPage lets the status pages that show the incident's
	// monitor show the note too.
	ShowOnStatusPage bool
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

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
	// empty means every monitor. Tags narrows that further to monitors
	// carrying at least one of them; empty means any.
	Namespaces []string
	Tags       []string
	// Events limits what the channel hears about, from ChannelEvents;
	// empty means everything.
	Events    []string
	Enabled   bool
	CreatedAt time.Time
	// The last delivery attempt: when (zero before the first), whether it
	// was delivered, and the error when it was not. Kept by the alert
	// engine, not set by writers of the channel.
	LastAttemptAt time.Time
	LastOK        bool
	LastError     string
}

// ChannelEvents are the kinds of notification a channel can be limited to:
// an outage opening, an outage ending, the reminders while it lasts, and
// the certificate warnings.
var ChannelEvents = []string{"down", "up", "reminder", "cert"}

// Covers reports whether notifications about the monitor go to the channel.
func (c Channel) Covers(m Monitor) bool {
	if len(c.Namespaces) > 0 && !slices.Contains(c.Namespaces, m.Namespace) {
		return false
	}
	if len(c.Tags) == 0 {
		return true
	}
	for _, tag := range m.Tags {
		if slices.Contains(c.Tags, tag) {
			return true
		}
	}
	return false
}

// Wants reports whether the channel hears about this kind of event (one of
// ChannelEvents); an empty kind, such as a test, always passes.
func (c Channel) Wants(kind string) bool {
	return kind == "" || len(c.Events) == 0 || slices.Contains(c.Events, kind)
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

// MonitorReport summarizes a monitor over a period: its checks from the
// hourly rollups, and its outages.
type MonitorReport struct {
	Monitor
	Checks   int
	UpChecks int
	// AvgLatency is over successful checks; zero if none.
	AvgLatency time.Duration
	// Outages counts the "down" incidents that overlapped the period;
	// Downtime is the part of them inside the period, up to now for one
	// still going, LongestOutage the longest such part. OpenOutage says one
	// is still going.
	Outages       int
	Downtime      time.Duration
	LongestOutage time.Duration
	OpenOutage    bool
	// Monitored is how long the monitor was being checked in the period:
	// the hours with checks, from its creation and up to now. Time up is
	// what of it was not downtime; a paused monitor is not up, nor down.
	Monitored time.Duration
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
	// MonitorName and MonitorNamespace are filled in when read; ignored
	// when written.
	MonitorName      string
	MonitorNamespace string
	CreatedAt        time.Time
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

// Role is what a user may do. Admins do everything; editors change and
// viewers only see the monitors of the namespaces they were given.
type Role string

const (
	RoleAdmin  Role = "admin"
	RoleEditor Role = "editor"
	RoleViewer Role = "viewer"
)

// Roles lists every role.
var Roles = []Role{RoleAdmin, RoleEditor, RoleViewer}

// AllNamespaces, as the only entry of a Namespaces list, means every
// namespace and group, including monitors without one.
const AllNamespaces = "*"

// Where an account comes from.
const (
	UserLocal = "local" // a password kept here
	UserLDAP  = "ldap"  // checked against the directory
	UserOIDC  = "oidc"  // signed in through the OpenID Connect provider
)

// User is an account.
type User struct {
	ID          int64
	Username    string
	DisplayName string
	Email       string
	Source      string
	// PasswordHash is set for local users; it never leaves the store's
	// callers.
	PasswordHash string
	Role         Role
	// Namespaces are the namespaces and groups an editor or viewer may see;
	// nil for an admin, who sees everything.
	Namespaces  []string
	Enabled     bool
	CreatedAt   time.Time
	LastLoginAt time.Time // zero if never
}

// Session is a signed-in browser.
type Session struct {
	// TokenHash is the SHA-256 of the token the browser holds, as hex.
	TokenHash  string
	UserID     int64
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastSeenAt time.Time
}

// LDAPGroupRole gives the members of a directory group a role.
type LDAPGroupRole struct {
	ID         int64
	GroupDN    string
	Role       Role
	Namespaces []string
	CreatedAt  time.Time
}

// AuditEntry records one thing a user did.
type AuditEntry struct {
	ID       int64
	At       time.Time
	UserID   int64 // 0 for the service token
	Username string
	// Action is "create", "update", "delete", "login" and the like;
	// ObjectType says what it was done to.
	Action     string
	ObjectType string
	ObjectID   int64
	Summary    string
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
	// SetPushToken gives the push monitor with the id a new token, and the
	// old one stops working. It returns ErrNotFound when there is no push
	// monitor with the id, ErrConflict when another has the token.
	SetPushToken(ctx context.Context, id int64, token string) error
	// RecordPush stores a push monitor's report, replacing its previous one.
	RecordPush(ctx context.Context, report PushReport) error
	// LastPush returns ErrNotFound when the monitor never reported.
	LastPush(ctx context.Context, monitorID int64) (PushReport, error)

	// InsertCheckResults writes the batch in a single transaction.
	InsertCheckResults(ctx context.Context, results []CheckResult) error
	// ListCheckResults returns up to limit results of a monitor, newest first.
	ListCheckResults(ctx context.Context, monitorID int64, limit int) ([]CheckResult, error)
	// SummarizeChecks counts the checks of a monitor in [from, to).
	SummarizeChecks(ctx context.Context, monitorID int64, from, to time.Time) (CheckSummary, error)
	// ListFailedChecks returns up to limit failed checks in [from, to), newest first.
	ListFailedChecks(ctx context.Context, monitorID int64, from, to time.Time, limit int) ([]CheckResult, error)
	// FailedChecksEnds returns the failed checks in [from, to), oldest
	// first, and how many there are. Of more than keep, it returns the
	// oldest half of keep (rounded up) and the newest half: how an outage
	// began and how it ended.
	FailedChecksEnds(ctx context.Context, monitorID int64, from, to time.Time, keep int) ([]CheckResult, int, error)
	// RecentCheckResults returns up to limit results, newest first, for each
	// of the given monitors, keyed by monitor ID. They come without their
	// diagnostics: a history of many monitors at once has no use for them.
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
	// ListIncidentsBetween returns the incidents of a monitor overlapping
	// [from, to), newest first, at most limit.
	ListIncidentsBetween(ctx context.Context, monitorID int64, from, to time.Time, limit int) ([]Incident, error)
	// GetIncident returns ErrNotFound for an unknown id.
	GetIncident(ctx context.Context, id int64) (Incident, error)
	// FilterIncidents returns the incidents f selects, in its order and
	// page; CountIncidents how many it selects over all pages.
	FilterIncidents(ctx context.Context, f IncidentFilter) ([]Incident, error)
	CountIncidents(ctx context.Context, f IncidentFilter) (int, error)

	// RecordDeliveries adds delivery attempts to the log, in one
	// transaction. An attempt about an incident that is gone (its monitor
	// was deleted meanwhile) is left out: there is no page to show it on.
	RecordDeliveries(ctx context.Context, deliveries []Delivery) error
	// DeliveriesEnds returns the delivery log of an incident, oldest first,
	// and how long it is; of more than keep entries, the oldest half of
	// keep (rounded up) and the newest half.
	DeliveriesEnds(ctx context.Context, incidentID int64, keep int) ([]Delivery, int, error)
	// DeleteDeliveriesBefore removes log entries older than cutoff and
	// reports how many rows were deleted.
	DeleteDeliveriesBefore(ctx context.Context, cutoff time.Time) (int64, error)

	// CreateIncidentNote returns ErrNotFound when the incident is gone.
	CreateIncidentNote(ctx context.Context, n IncidentNote) (IncidentNote, error)
	GetIncidentNote(ctx context.Context, id int64) (IncidentNote, error)
	// ListIncidentNotes returns up to limit notes of an incident, oldest first.
	ListIncidentNotes(ctx context.Context, incidentID int64, limit int) ([]IncidentNote, error)
	// UpdateIncidentNote changes the text and the status page flag of the
	// note with n.ID, and marks it updated now.
	UpdateIncidentNote(ctx context.Context, n IncidentNote) (IncidentNote, error)
	DeleteIncidentNote(ctx context.Context, id int64) error

	CreateMaintenance(ctx context.Context, m Maintenance) (Maintenance, error)
	// ListMaintenance returns up to limit windows: the repeating ones, then
	// the others, latest start first.
	ListMaintenance(ctx context.Context, limit int) ([]Maintenance, error)
	// ListMaintenanceBetween returns the windows that may have an
	// occurrence overlapping [from, to): every repeating one, and the
	// others that overlap it. Maintenance.Occurrences tells which do.
	ListMaintenanceBetween(ctx context.Context, from, to time.Time) ([]Maintenance, error)
	DeleteMaintenance(ctx context.Context, id int64) error
	// InMaintenance reports whether a window covers the monitor at the
	// given time.
	InMaintenance(ctx context.Context, m Monitor, at time.Time) (bool, error)

	CreateChannel(ctx context.Context, c Channel) (Channel, error)
	// RecordChannelDelivery notes the outcome of a delivery attempt;
	// deliveryError is empty when it was delivered.
	RecordChannelDelivery(ctx context.Context, id int64, at time.Time, deliveryError string) error
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
	// Report summarizes every monitor over [from, to); monitors without
	// checks in the period are included with zero counts.
	Report(ctx context.Context, from, to time.Time) ([]MonitorReport, error)
	// StatusPageDailyUptime summarizes the checks of the page's monitors per
	// UTC day in [from, to), keyed by monitor; days without checks are left
	// out. Only Start, Checks and UpChecks of the points are set.
	StatusPageDailyUptime(ctx context.Context, pageID int64, from, to time.Time) (map[int64][]SeriesPoint, error)
	// MonitorIsPublic reports whether a published status page shows the monitor.
	MonitorIsPublic(ctx context.Context, monitorID int64) (bool, error)

	// CreateUser returns ErrConflict when the username is taken (whatever
	// its case).
	CreateUser(ctx context.Context, u User) (User, error)
	// CreateFirstUser creates u only while there is no account at all, and
	// returns ErrConflict once there is one. Checking and creating are one
	// step: of two first accounts created at the same time, one fails.
	CreateFirstUser(ctx context.Context, u User) (User, error)
	GetUser(ctx context.Context, id int64) (User, error)
	GetUserByUsername(ctx context.Context, username string) (User, error)
	ListUsers(ctx context.Context) ([]User, error)
	CountUsers(ctx context.Context) (int, error)
	// UpdateUser changes everything about the user with u.ID except the
	// password hash and the creation time.
	UpdateUser(ctx context.Context, u User) (User, error)
	SetPassword(ctx context.Context, id int64, hash string) error
	// DeleteUser removes the user and their sessions.
	DeleteUser(ctx context.Context, id int64) error

	CreateSession(ctx context.Context, s Session) error
	// GetSession returns ErrNotFound for an unknown or expired token.
	GetSession(ctx context.Context, tokenHash string, now time.Time) (Session, error)
	TouchSession(ctx context.Context, tokenHash string, at time.Time) error
	DeleteSession(ctx context.Context, tokenHash string) error
	DeleteUserSessions(ctx context.Context, userID int64) error
	DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error)

	// CreateLDAPGroupRole returns ErrConflict when the group is mapped already.
	CreateLDAPGroupRole(ctx context.Context, g LDAPGroupRole) (LDAPGroupRole, error)
	ListLDAPGroupRoles(ctx context.Context) ([]LDAPGroupRole, error)
	UpdateLDAPGroupRole(ctx context.Context, g LDAPGroupRole) (LDAPGroupRole, error)
	DeleteLDAPGroupRole(ctx context.Context, id int64) error

	AppendAudit(ctx context.Context, e AuditEntry) error
	// ListAudit returns up to limit entries, newest first.
	ListAudit(ctx context.Context, limit int) ([]AuditEntry, error)
	DeleteAuditBefore(ctx context.Context, cutoff time.Time) (int64, error)

	Close() error
}
