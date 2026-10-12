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
	Addr              string        // UPTIME_ADDR
	DBPath            string        // UPTIME_DB_PATH
	DBBusyTimeout     time.Duration // UPTIME_DB_BUSY_TIMEOUT
	ReadHeaderTimeout time.Duration // UPTIME_READ_HEADER_TIMEOUT
	ShutdownTimeout   time.Duration // UPTIME_SHUTDOWN_TIMEOUT

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

	DefaultFailureThreshold int           // UPTIME_DEFAULT_FAILURE_THRESHOLD
	RecoveryThreshold       int           // UPTIME_RECOVERY_THRESHOLD
	CertWarning             time.Duration // UPTIME_CERT_WARNING
	ReminderInterval        time.Duration // UPTIME_REMINDER_INTERVAL; 0 = off
	NotifyQueueSize         int           // UPTIME_NOTIFY_QUEUE_SIZE
	NotifyAttempts          int           // UPTIME_NOTIFY_ATTEMPTS
	NotifyRetryDelay        time.Duration // UPTIME_NOTIFY_RETRY_DELAY
	NotifyTimeout           time.Duration // UPTIME_NOTIFY_TIMEOUT

	// SMTP settings for e-mail channels; e-mail is off unless both the host
	// and the sender address are set.
	SMTPHost     string // UPTIME_SMTP_HOST
	SMTPPort     int    // UPTIME_SMTP_PORT
	SMTPUsername string // UPTIME_SMTP_USERNAME
	SMTPPassword string // UPTIME_SMTP_PASSWORD
	SMTPFrom     string // UPTIME_SMTP_FROM
	SMTPSecurity string // UPTIME_SMTP_SECURITY: starttls, tls or none

	// Discovery connects with the pod's service account when running in a
	// cluster. Outside one, it needs both a kubeconfig and a context.
	DiscoveryEnabled bool          // UPTIME_DISCOVERY_ENABLED
	Kubeconfig       string        // UPTIME_KUBECONFIG
	KubeContext      string        // UPTIME_KUBE_CONTEXT
	KubeTimeout      time.Duration // UPTIME_KUBE_TIMEOUT
}

// Load reads the configuration from the environment, falling back to
// defaults for unset variables. Durations use Go syntax, e.g. "5s" or "1m30s".
func Load() (Config, error) {
	cfg := Config{
		Addr:   envString("UPTIME_ADDR", ":8080"),
		DBPath: envString("UPTIME_DB_PATH", "uptime.db"),

		SMTPHost:     envString("UPTIME_SMTP_HOST", ""),
		SMTPUsername: envString("UPTIME_SMTP_USERNAME", ""),
		SMTPPassword: envString("UPTIME_SMTP_PASSWORD", ""),
		SMTPFrom:     envString("UPTIME_SMTP_FROM", ""),
		SMTPSecurity: envString("UPTIME_SMTP_SECURITY", "starttls"),

		Kubeconfig:  envString("UPTIME_KUBECONFIG", ""),
		KubeContext: envString("UPTIME_KUBE_CONTEXT", ""),
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
		{&cfg.StatsWindow, "UPTIME_STATS_WINDOW", 24 * time.Hour},
		{&cfg.CertWarning, "UPTIME_CERT_WARNING", 14 * 24 * time.Hour},
		{&cfg.NotifyRetryDelay, "UPTIME_NOTIFY_RETRY_DELAY", 5 * time.Second},
		{&cfg.NotifyTimeout, "UPTIME_NOTIFY_TIMEOUT", 5 * time.Second},
		{&cfg.KubeTimeout, "UPTIME_KUBE_TIMEOUT", 10 * time.Second},
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
		{&cfg.SeriesPoints, "UPTIME_SERIES_POINTS", 120},
		{&cfg.KeywordBodyLimit, "UPTIME_KEYWORD_BODY_LIMIT_BYTES", 1 << 20},
		{&cfg.DefaultFailureThreshold, "UPTIME_DEFAULT_FAILURE_THRESHOLD", 3},
		{&cfg.RecoveryThreshold, "UPTIME_RECOVERY_THRESHOLD", 1},
		{&cfg.NotifyQueueSize, "UPTIME_NOTIFY_QUEUE_SIZE", 100},
		{&cfg.NotifyAttempts, "UPTIME_NOTIFY_ATTEMPTS", 3},
		{&cfg.SMTPPort, "UPTIME_SMTP_PORT", 587},
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
	if cfg.MinCheckInterval < time.Second {
		// Intervals are stored in whole seconds.
		return Config{}, fmt.Errorf("UPTIME_MIN_CHECK_INTERVAL: must be at least 1s, got %s", cfg.MinCheckInterval)
	}
	if cfg.ReminderInterval, err = envDurationOrZero("UPTIME_REMINDER_INTERVAL", 0); err != nil {
		return Config{}, err
	}
	if cfg.DiscoveryEnabled, err = envBool("UPTIME_DISCOVERY_ENABLED", true); err != nil {
		return Config{}, err
	}
	if cfg.HTTPInsecureSkipVerify, err = envBool("UPTIME_HTTP_INSECURE_SKIP_VERIFY", false); err != nil {
		return Config{}, err
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
