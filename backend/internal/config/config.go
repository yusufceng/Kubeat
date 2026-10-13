// Package config loads the server configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds every tunable of the server. Each field maps to one
// environment variable; see Load for names and defaults.
type Config struct {
	Addr string // UPTIME_ADDR
	// APIToken, if set, is required as a bearer token on API requests.
	APIToken string // UPTIME_API_TOKEN
	// SecretKey, if set, encrypts the secrets kept in the database.
	SecretKey string // UPTIME_SECRET_KEY

	// Accounts. AdminPassword creates the administrator "admin" when there
	// is no account yet; with AdminPasswordReset it also resets admin's
	// password, for an operator locked out.
	AdminPassword      string        // UPTIME_ADMIN_PASSWORD
	AdminPasswordReset bool          // UPTIME_ADMIN_PASSWORD_RESET
	SessionTTL         time.Duration // UPTIME_SESSION_TTL
	LoginMaxFailures   int           // UPTIME_LOGIN_MAX_FAILURES
	LoginLockout       time.Duration // UPTIME_LOGIN_LOCKOUT
	PasswordMinLength  int           // UPTIME_PASSWORD_MIN_LENGTH
	AuditRetention     time.Duration // UPTIME_AUDIT_RETENTION

	// LDAP; see ldapauth.Config. Empty LDAPURL means local accounts only.
	LDAPURL                string        // UPTIME_LDAP_URL
	LDAPStartTLS           bool          // UPTIME_LDAP_START_TLS
	LDAPCAFile             string        // UPTIME_LDAP_CA_FILE
	LDAPInsecureSkipVerify bool          // UPTIME_LDAP_INSECURE_SKIP_VERIFY
	LDAPBindDN             string        // UPTIME_LDAP_BIND_DN
	LDAPBindPassword       string        // UPTIME_LDAP_BIND_PASSWORD
	LDAPBaseDN             string        // UPTIME_LDAP_BASE_DN
	LDAPUserFilter         string        // UPTIME_LDAP_USER_FILTER
	LDAPDisplayNameAttr    string        // UPTIME_LDAP_DISPLAY_NAME_ATTR
	LDAPEmailAttr          string        // UPTIME_LDAP_EMAIL_ATTR
	LDAPGroupAttr          string        // UPTIME_LDAP_GROUP_ATTR
	LDAPGroupBaseDN        string        // UPTIME_LDAP_GROUP_BASE_DN
	LDAPGroupFilter        string        // UPTIME_LDAP_GROUP_FILTER
	LDAPTimeout            time.Duration // UPTIME_LDAP_TIMEOUT
	// LDAPDefaultRole is what a directory user gets when none of their
	// groups is mapped; empty refuses them. LDAPDefaultNamespaces is the
	// scope of that role, as a comma-separated list.
	LDAPDefaultRole       string // UPTIME_LDAP_DEFAULT_ROLE
	LDAPDefaultNamespaces string // UPTIME_LDAP_DEFAULT_NAMESPACES

	// OIDC sign-in through an OpenID Connect provider; off unless the
	// issuer is set. The redirect URL is the frontend's /auth/callback as
	// the provider knows it.
	OIDCIssuer             string        // UPTIME_OIDC_ISSUER
	OIDCClientID           string        // UPTIME_OIDC_CLIENT_ID
	OIDCClientSecret       string        // UPTIME_OIDC_CLIENT_SECRET
	OIDCRedirectURL        string        // UPTIME_OIDC_REDIRECT_URL
	OIDCScopes             string        // UPTIME_OIDC_SCOPES, space-separated
	OIDCUsernameClaim      string        // UPTIME_OIDC_USERNAME_CLAIM
	OIDCDisplayNameClaim   string        // UPTIME_OIDC_DISPLAY_NAME_CLAIM
	OIDCEmailClaim         string        // UPTIME_OIDC_EMAIL_CLAIM
	OIDCGroupsClaim        string        // UPTIME_OIDC_GROUPS_CLAIM
	OIDCName               string        // UPTIME_OIDC_NAME, the button's label
	OIDCCAFile             string        // UPTIME_OIDC_CA_FILE
	OIDCInsecureSkipVerify bool          // UPTIME_OIDC_INSECURE_SKIP_VERIFY
	OIDCTimeout            time.Duration // UPTIME_OIDC_TIMEOUT
	OIDCDefaultRole        string        // UPTIME_OIDC_DEFAULT_ROLE
	OIDCDefaultNamespaces  string        // UPTIME_OIDC_DEFAULT_NAMESPACES
	DBPath                 string        // UPTIME_DB_PATH
	DBBusyTimeout          time.Duration // UPTIME_DB_BUSY_TIMEOUT
	ReadHeaderTimeout      time.Duration // UPTIME_READ_HEADER_TIMEOUT
	ShutdownTimeout        time.Duration // UPTIME_SHUTDOWN_TIMEOUT

	SchedulerTick       time.Duration // UPTIME_SCHEDULER_TICK
	MaxJitter           time.Duration // UPTIME_MAX_JITTER
	Workers             int           // UPTIME_WORKERS
	ResultBatchSize     int           // UPTIME_RESULT_BATCH_SIZE
	ResultFlushInterval time.Duration // UPTIME_RESULT_FLUSH_INTERVAL
	Retention           time.Duration // UPTIME_RETENTION
	RetentionInterval   time.Duration // UPTIME_RETENTION_INTERVAL
	RollupInterval      time.Duration // UPTIME_ROLLUP_INTERVAL
	RollupRetention     time.Duration // UPTIME_ROLLUP_RETENTION
	SeriesPoints        int           // UPTIME_SERIES_POINTS

	HTTPInsecureSkipVerify bool // UPTIME_HTTP_INSECURE_SKIP_VERIFY
	KeywordBodyLimit       int  // UPTIME_KEYWORD_BODY_LIMIT_BYTES

	MinCheckInterval time.Duration // UPTIME_MIN_CHECK_INTERVAL
	StatsWindow      time.Duration // UPTIME_STATS_WINDOW
	MaxResults       int           // UPTIME_MAX_RESULTS

	StatusPageDays           int           // UPTIME_STATUS_PAGE_DAYS
	StatusPageIncidentWindow time.Duration // UPTIME_STATUS_PAGE_INCIDENT_WINDOW

	DefaultFailureThreshold int           // UPTIME_DEFAULT_FAILURE_THRESHOLD
	RecoveryThreshold       int           // UPTIME_RECOVERY_THRESHOLD
	CertWarning             time.Duration // UPTIME_CERT_WARNING
	ReminderInterval        time.Duration // UPTIME_REMINDER_INTERVAL; 0 = off
	NotifyQueueSize         int           // UPTIME_NOTIFY_QUEUE_SIZE
	NotifyGroupWait         time.Duration // UPTIME_NOTIFY_GROUP_WAIT; 0 = no grouping
	NotifyAttempts          int           // UPTIME_NOTIFY_ATTEMPTS
	NotifyRetryDelay        time.Duration // UPTIME_NOTIFY_RETRY_DELAY
	NotifyTimeout           time.Duration // UPTIME_NOTIFY_TIMEOUT

	// A Prometheus-compatible server (on OpenShift, Thanos Querier) from
	// which pod CPU and memory usage is read; off while the URL is empty.
	PrometheusURL                string        // UPTIME_PROMETHEUS_URL
	PrometheusTokenFile          string        // UPTIME_PROMETHEUS_TOKEN_FILE
	PrometheusCAFile             string        // UPTIME_PROMETHEUS_CA_FILE
	PrometheusInsecureSkipVerify bool          // UPTIME_PROMETHEUS_INSECURE_SKIP_VERIFY
	PrometheusTimeout            time.Duration // UPTIME_PROMETHEUS_TIMEOUT
	PrometheusRateWindow         time.Duration // UPTIME_PROMETHEUS_RATE_WINDOW
	PrometheusCPUQuery           string        // UPTIME_PROMETHEUS_CPU_QUERY
	PrometheusMemoryQuery        string        // UPTIME_PROMETHEUS_MEMORY_QUERY

	// Periodic copies of the database; off while the directory is empty.
	BackupDir      string        // UPTIME_BACKUP_DIR
	BackupInterval time.Duration // UPTIME_BACKUP_INTERVAL
	BackupKeep     int           // UPTIME_BACKUP_KEEP

	// Heartbeats go to an external dead man's switch; off while the URL is
	// empty. MetricsTimeout bounds the database read done for a scrape.
	HeartbeatURL      string        // UPTIME_HEARTBEAT_URL
	HeartbeatInterval time.Duration // UPTIME_HEARTBEAT_INTERVAL
	HeartbeatTimeout  time.Duration // UPTIME_HEARTBEAT_TIMEOUT
	MetricsTimeout    time.Duration // UPTIME_METRICS_TIMEOUT

	// SMTP settings for e-mail channels; e-mail is off unless both the host
	// and the sender address are set.
	SMTPHost     string // UPTIME_SMTP_HOST
	SMTPPort     int    // UPTIME_SMTP_PORT
	SMTPUsername string // UPTIME_SMTP_USERNAME
	SMTPPassword string // UPTIME_SMTP_PASSWORD
	SMTPFrom     string // UPTIME_SMTP_FROM
	SMTPSecurity string // UPTIME_SMTP_SECURITY: starttls, tls or none

	// The hosted services notifications go to; change them to send through
	// a proxy of your own.
	TelegramAPIURL     string // UPTIME_TELEGRAM_API_URL
	PagerDutyEventsURL string // UPTIME_PAGERDUTY_EVENTS_URL

	// Discovery connects with the pod's service account when running in a
	// cluster. Outside one, it needs both a kubeconfig and a context.
	DiscoveryEnabled bool          // UPTIME_DISCOVERY_ENABLED
	Kubeconfig       string        // UPTIME_KUBECONFIG
	KubeContext      string        // UPTIME_KUBE_CONTEXT
	KubeTimeout      time.Duration // UPTIME_KUBE_TIMEOUT
	// DiscoveryNamespaces limits discovery to these namespaces (comma
	// separated); empty means the whole cluster.
	DiscoveryNamespaces string // UPTIME_DISCOVERY_NAMESPACES
	// The cluster events shown with a monitor's pods: how far back and how
	// many.
	EventWindow time.Duration // UPTIME_EVENT_WINDOW
	EventLimit  int           // UPTIME_EVENT_LIMIT
	// Language the notifications and the first administrator's name are
	// written in: "en" or "tr".
	Language string // UPTIME_LANGUAGE
	// WorkloadRolloutGrace is how long a workload monitor puts up with
	// missing pods while a rollout is in progress; zero means not at all.
	WorkloadRolloutGrace time.Duration // UPTIME_WORKLOAD_ROLLOUT_GRACE
	// Discovery sync keeps monitors in step with objects annotated
	// kubeat.io/monitor=true; zero turns it off. The defaults apply to
	// monitors whose annotations do not set them.
	DiscoverySyncInterval    time.Duration // UPTIME_DISCOVERY_SYNC_INTERVAL
	DiscoveryDefaultInterval time.Duration // UPTIME_DISCOVERY_DEFAULT_INTERVAL
	DiscoveryDefaultTimeout  time.Duration // UPTIME_DISCOVERY_DEFAULT_TIMEOUT
}

// Load reads the configuration from the environment, falling back to
// defaults for unset variables. Durations use Go syntax, e.g. "5s" or "1m30s".
func Load() (Config, error) {
	cfg := Config{
		Addr:     envString("UPTIME_ADDR", ":8080"),
		Language: envString("UPTIME_LANGUAGE", "en"),
		DBPath:   envString("UPTIME_DB_PATH", "uptime.db"),

		APIToken:  envString("UPTIME_API_TOKEN", ""),
		SecretKey: envString("UPTIME_SECRET_KEY", ""),

		AdminPassword: envString("UPTIME_ADMIN_PASSWORD", ""),

		LDAPURL:               envString("UPTIME_LDAP_URL", ""),
		LDAPCAFile:            envString("UPTIME_LDAP_CA_FILE", ""),
		LDAPBindDN:            envString("UPTIME_LDAP_BIND_DN", ""),
		LDAPBindPassword:      envString("UPTIME_LDAP_BIND_PASSWORD", ""),
		LDAPBaseDN:            envString("UPTIME_LDAP_BASE_DN", ""),
		LDAPUserFilter:        envString("UPTIME_LDAP_USER_FILTER", "(uid={username})"),
		LDAPDisplayNameAttr:   envString("UPTIME_LDAP_DISPLAY_NAME_ATTR", "cn"),
		LDAPEmailAttr:         envString("UPTIME_LDAP_EMAIL_ATTR", "mail"),
		LDAPGroupAttr:         envString("UPTIME_LDAP_GROUP_ATTR", "memberOf"),
		LDAPGroupBaseDN:       envString("UPTIME_LDAP_GROUP_BASE_DN", ""),
		LDAPGroupFilter:       envString("UPTIME_LDAP_GROUP_FILTER", ""),
		LDAPDefaultRole:       envString("UPTIME_LDAP_DEFAULT_ROLE", ""),
		LDAPDefaultNamespaces: envString("UPTIME_LDAP_DEFAULT_NAMESPACES", ""),

		OIDCIssuer:            envString("UPTIME_OIDC_ISSUER", ""),
		OIDCClientID:          envString("UPTIME_OIDC_CLIENT_ID", ""),
		OIDCClientSecret:      envString("UPTIME_OIDC_CLIENT_SECRET", ""),
		OIDCRedirectURL:       envString("UPTIME_OIDC_REDIRECT_URL", ""),
		OIDCScopes:            envString("UPTIME_OIDC_SCOPES", "openid profile email"),
		OIDCUsernameClaim:     envString("UPTIME_OIDC_USERNAME_CLAIM", "preferred_username"),
		OIDCDisplayNameClaim:  envString("UPTIME_OIDC_DISPLAY_NAME_CLAIM", "name"),
		OIDCEmailClaim:        envString("UPTIME_OIDC_EMAIL_CLAIM", "email"),
		OIDCGroupsClaim:       envString("UPTIME_OIDC_GROUPS_CLAIM", "groups"),
		OIDCName:              envString("UPTIME_OIDC_NAME", "SSO"),
		OIDCCAFile:            envString("UPTIME_OIDC_CA_FILE", ""),
		OIDCDefaultRole:       envString("UPTIME_OIDC_DEFAULT_ROLE", ""),
		OIDCDefaultNamespaces: envString("UPTIME_OIDC_DEFAULT_NAMESPACES", ""),

		HeartbeatURL: envString("UPTIME_HEARTBEAT_URL", ""),
		BackupDir:    envString("UPTIME_BACKUP_DIR", ""),

		PrometheusURL:         envString("UPTIME_PROMETHEUS_URL", ""),
		PrometheusTokenFile:   envString("UPTIME_PROMETHEUS_TOKEN_FILE", ""),
		PrometheusCAFile:      envString("UPTIME_PROMETHEUS_CA_FILE", ""),
		PrometheusCPUQuery:    envString("UPTIME_PROMETHEUS_CPU_QUERY", ""),
		PrometheusMemoryQuery: envString("UPTIME_PROMETHEUS_MEMORY_QUERY", ""),

		SMTPHost:     envString("UPTIME_SMTP_HOST", ""),
		SMTPUsername: envString("UPTIME_SMTP_USERNAME", ""),
		SMTPPassword: envString("UPTIME_SMTP_PASSWORD", ""),
		SMTPFrom:     envString("UPTIME_SMTP_FROM", ""),
		SMTPSecurity: envString("UPTIME_SMTP_SECURITY", "starttls"),

		TelegramAPIURL:     envString("UPTIME_TELEGRAM_API_URL", "https://api.telegram.org"),
		PagerDutyEventsURL: envString("UPTIME_PAGERDUTY_EVENTS_URL", "https://events.pagerduty.com/v2/enqueue"),

		Kubeconfig:          envString("UPTIME_KUBECONFIG", ""),
		KubeContext:         envString("UPTIME_KUBE_CONTEXT", ""),
		DiscoveryNamespaces: envString("UPTIME_DISCOVERY_NAMESPACES", ""),
	}

	durations := []struct {
		dst *time.Duration
		key string
		def time.Duration
	}{
		{&cfg.DBBusyTimeout, "UPTIME_DB_BUSY_TIMEOUT", 5 * time.Second},
		{&cfg.ReadHeaderTimeout, "UPTIME_READ_HEADER_TIMEOUT", 5 * time.Second},
		{&cfg.ShutdownTimeout, "UPTIME_SHUTDOWN_TIMEOUT", 10 * time.Second},
		{&cfg.SchedulerTick, "UPTIME_SCHEDULER_TICK", time.Second},
		{&cfg.MaxJitter, "UPTIME_MAX_JITTER", 5 * time.Second},
		{&cfg.ResultFlushInterval, "UPTIME_RESULT_FLUSH_INTERVAL", time.Second},
		{&cfg.Retention, "UPTIME_RETENTION", 7 * 24 * time.Hour},
		{&cfg.RetentionInterval, "UPTIME_RETENTION_INTERVAL", time.Hour},
		{&cfg.RollupInterval, "UPTIME_ROLLUP_INTERVAL", time.Minute},
		{&cfg.RollupRetention, "UPTIME_ROLLUP_RETENTION", 365 * 24 * time.Hour},
		{&cfg.MinCheckInterval, "UPTIME_MIN_CHECK_INTERVAL", 5 * time.Second},
		{&cfg.EventWindow, "UPTIME_EVENT_WINDOW", 2 * time.Hour},
		{&cfg.DiscoveryDefaultInterval, "UPTIME_DISCOVERY_DEFAULT_INTERVAL", 30 * time.Second},
		{&cfg.DiscoveryDefaultTimeout, "UPTIME_DISCOVERY_DEFAULT_TIMEOUT", 5 * time.Second},
		{&cfg.StatsWindow, "UPTIME_STATS_WINDOW", 24 * time.Hour},
		{&cfg.StatusPageIncidentWindow, "UPTIME_STATUS_PAGE_INCIDENT_WINDOW", 14 * 24 * time.Hour},
		{&cfg.SessionTTL, "UPTIME_SESSION_TTL", 12 * time.Hour},
		{&cfg.LoginLockout, "UPTIME_LOGIN_LOCKOUT", 15 * time.Minute},
		{&cfg.AuditRetention, "UPTIME_AUDIT_RETENTION", 90 * 24 * time.Hour},
		{&cfg.LDAPTimeout, "UPTIME_LDAP_TIMEOUT", 10 * time.Second},
		{&cfg.OIDCTimeout, "UPTIME_OIDC_TIMEOUT", 10 * time.Second},
		{&cfg.CertWarning, "UPTIME_CERT_WARNING", 14 * 24 * time.Hour},
		{&cfg.NotifyRetryDelay, "UPTIME_NOTIFY_RETRY_DELAY", 5 * time.Second},
		{&cfg.NotifyTimeout, "UPTIME_NOTIFY_TIMEOUT", 5 * time.Second},
		{&cfg.KubeTimeout, "UPTIME_KUBE_TIMEOUT", 10 * time.Second},
		{&cfg.HeartbeatInterval, "UPTIME_HEARTBEAT_INTERVAL", time.Minute},
		{&cfg.HeartbeatTimeout, "UPTIME_HEARTBEAT_TIMEOUT", 10 * time.Second},
		{&cfg.MetricsTimeout, "UPTIME_METRICS_TIMEOUT", 5 * time.Second},
		{&cfg.BackupInterval, "UPTIME_BACKUP_INTERVAL", 24 * time.Hour},
		{&cfg.PrometheusTimeout, "UPTIME_PROMETHEUS_TIMEOUT", 10 * time.Second},
		{&cfg.PrometheusRateWindow, "UPTIME_PROMETHEUS_RATE_WINDOW", 5 * time.Minute},
	}
	for _, d := range durations {
		v, err := envDuration(d.key, d.def)
		if err != nil {
			return Config{}, err
		}
		*d.dst = v
	}

	ints := []struct {
		dst *int
		key string
		def int
	}{
		{&cfg.Workers, "UPTIME_WORKERS", 20},
		{&cfg.ResultBatchSize, "UPTIME_RESULT_BATCH_SIZE", 100},
		{&cfg.MaxResults, "UPTIME_MAX_RESULTS", 1000},
		{&cfg.StatusPageDays, "UPTIME_STATUS_PAGE_DAYS", 90},
		{&cfg.LoginMaxFailures, "UPTIME_LOGIN_MAX_FAILURES", 10},
		{&cfg.PasswordMinLength, "UPTIME_PASSWORD_MIN_LENGTH", 8},
		{&cfg.SeriesPoints, "UPTIME_SERIES_POINTS", 120},
		{&cfg.KeywordBodyLimit, "UPTIME_KEYWORD_BODY_LIMIT_BYTES", 1 << 20},
		{&cfg.DefaultFailureThreshold, "UPTIME_DEFAULT_FAILURE_THRESHOLD", 3},
		{&cfg.EventLimit, "UPTIME_EVENT_LIMIT", 20},
		{&cfg.RecoveryThreshold, "UPTIME_RECOVERY_THRESHOLD", 1},
		{&cfg.NotifyQueueSize, "UPTIME_NOTIFY_QUEUE_SIZE", 100},
		{&cfg.NotifyAttempts, "UPTIME_NOTIFY_ATTEMPTS", 3},
		{&cfg.SMTPPort, "UPTIME_SMTP_PORT", 587},
		{&cfg.BackupKeep, "UPTIME_BACKUP_KEEP", 7},
	}
	for _, i := range ints {
		v, err := envPositiveInt(i.key, i.def)
		if err != nil {
			return Config{}, err
		}
		*i.dst = v
	}

	var err error
	switch cfg.SMTPSecurity {
	case "starttls", "tls", "none":
	default:
		return Config{}, fmt.Errorf("UPTIME_SMTP_SECURITY: must be starttls, tls or none, got %q", cfg.SMTPSecurity)
	}
	if cfg.HeartbeatURL != "" && cfg.HeartbeatInterval < 2*cfg.SchedulerTick {
		// A heartbeat needs a scheduler tick since the previous one.
		return Config{}, fmt.Errorf("UPTIME_HEARTBEAT_INTERVAL: must be at least twice UPTIME_SCHEDULER_TICK (%s), got %s",
			cfg.SchedulerTick, cfg.HeartbeatInterval)
	}
	if cfg.MinCheckInterval < time.Second {
		// Intervals are stored in whole seconds.
		return Config{}, fmt.Errorf("UPTIME_MIN_CHECK_INTERVAL: must be at least 1s, got %s", cfg.MinCheckInterval)
	}
	if cfg.ReminderInterval, err = envDurationOrZero("UPTIME_REMINDER_INTERVAL", 0); err != nil {
		return Config{}, err
	}
	if cfg.PrometheusInsecureSkipVerify, err = envBool("UPTIME_PROMETHEUS_INSECURE_SKIP_VERIFY", false); err != nil {
		return Config{}, err
	}
	if cfg.DiscoveryEnabled, err = envBool("UPTIME_DISCOVERY_ENABLED", true); err != nil {
		return Config{}, err
	}
	if cfg.DiscoverySyncInterval, err = envDurationOrZero("UPTIME_DISCOVERY_SYNC_INTERVAL", time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.WorkloadRolloutGrace, err = envDurationOrZero("UPTIME_WORKLOAD_ROLLOUT_GRACE", 10*time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.NotifyGroupWait, err = envDurationOrZero("UPTIME_NOTIFY_GROUP_WAIT", 30*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.DiscoveryDefaultTimeout >= cfg.DiscoveryDefaultInterval {
		return Config{}, fmt.Errorf("UPTIME_DISCOVERY_DEFAULT_TIMEOUT: must be shorter than UPTIME_DISCOVERY_DEFAULT_INTERVAL (%s)", cfg.DiscoveryDefaultInterval)
	}
	if cfg.DiscoveryDefaultInterval < cfg.MinCheckInterval {
		return Config{}, fmt.Errorf("UPTIME_DISCOVERY_DEFAULT_INTERVAL: must be at least UPTIME_MIN_CHECK_INTERVAL (%s)", cfg.MinCheckInterval)
	}
	if cfg.HTTPInsecureSkipVerify, err = envBool("UPTIME_HTTP_INSECURE_SKIP_VERIFY", false); err != nil {
		return Config{}, err
	}
	if cfg.AdminPasswordReset, err = envBool("UPTIME_ADMIN_PASSWORD_RESET", false); err != nil {
		return Config{}, err
	}
	if cfg.LDAPStartTLS, err = envBool("UPTIME_LDAP_START_TLS", false); err != nil {
		return Config{}, err
	}
	if cfg.LDAPInsecureSkipVerify, err = envBool("UPTIME_LDAP_INSECURE_SKIP_VERIFY", false); err != nil {
		return Config{}, err
	}
	if cfg.OIDCInsecureSkipVerify, err = envBool("UPTIME_OIDC_INSECURE_SKIP_VERIFY", false); err != nil {
		return Config{}, err
	}
	if cfg.Language != "en" && cfg.Language != "tr" {
		return Config{}, fmt.Errorf("UPTIME_LANGUAGE: must be en or tr, got %q", cfg.Language)
	}
	if cfg.OIDCIssuer != "" && cfg.OIDCClientID == "" {
		return Config{}, fmt.Errorf("UPTIME_OIDC_ISSUER is set: UPTIME_OIDC_CLIENT_ID is required with it")
	}
	switch cfg.LDAPDefaultRole {
	case "", "admin", "editor", "viewer":
	default:
		return Config{}, fmt.Errorf("UPTIME_LDAP_DEFAULT_ROLE: must be admin, editor, viewer or empty, got %q", cfg.LDAPDefaultRole)
	}
	switch cfg.OIDCDefaultRole {
	case "", "admin", "editor", "viewer":
	default:
		return Config{}, fmt.Errorf("UPTIME_OIDC_DEFAULT_ROLE: must be admin, editor, viewer or empty, got %q", cfg.OIDCDefaultRole)
	}
	return cfg, nil
}

func envString(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envDuration(key string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%s: must be positive, got %s", key, v)
	}
	return d, nil
}

// envDurationOrZero is envDuration for settings where zero means "off".
func envDurationOrZero(key string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	if d < 0 {
		return 0, fmt.Errorf("%s: must not be negative, got %s", key, v)
	}
	return d, nil
}

func envPositiveInt(key string, def int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	if n <= 0 {
		return 0, fmt.Errorf("%s: must be positive, got %s", key, v)
	}
	return n, nil
}

func envBool(key string, def bool) (bool, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("%s: %w", key, err)
	}
	return b, nil
}
