package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	for _, k := range os.Environ() {
		if name, _, _ := strings.Cut(k, "="); strings.HasPrefix(name, "UPTIME_") {
			t.Setenv(name, "")
		}
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Config{
		Addr:              ":8080",
		DBPath:            "uptime.db",
		DBBusyTimeout:     5 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		ShutdownTimeout:   10 * time.Second,

		SchedulerTick:       time.Second,
		MaxJitter:           5 * time.Second,
		Workers:             20,
		ResultBatchSize:     100,
		ResultFlushInterval: time.Second,
		Retention:           7 * 24 * time.Hour,
		RetentionInterval:   time.Hour,
		RollupInterval:      time.Minute,
		RollupRetention:     365 * 24 * time.Hour,
		SeriesPoints:        120,

		KeywordBodyLimit: 1 << 20,

		MinCheckInterval: 5 * time.Second,
		StatsWindow:      24 * time.Hour,
		MaxResults:       1000,

		StatusPageDays:           90,
		StatusPageIncidentWindow: 14 * 24 * time.Hour,

		DefaultFailureThreshold: 3,
		RecoveryThreshold:       1,
		CertWarning:             14 * 24 * time.Hour,
		NotifyQueueSize:         100,
		NotifyAttempts:          3,
		NotifyRetryDelay:        5 * time.Second,
		NotifyTimeout:           5 * time.Second,

		PrometheusTimeout:    10 * time.Second,
		PrometheusRateWindow: 5 * time.Minute,

		BackupInterval: 24 * time.Hour,
		BackupKeep:     7,

		HeartbeatInterval: time.Minute,
		HeartbeatTimeout:  10 * time.Second,
		MetricsTimeout:    5 * time.Second,

		SMTPPort:     587,
		SMTPSecurity: "starttls",

		TelegramAPIURL:     "https://api.telegram.org",
		PagerDutyEventsURL: "https://events.pagerduty.com/v2/enqueue",

		SessionTTL:        12 * time.Hour,
		LoginMaxFailures:  10,
		LoginLockout:      15 * time.Minute,
		PasswordMinLength: 8,
		AuditRetention:    90 * 24 * time.Hour,

		LDAPUserFilter:       "(uid={username})",
		LDAPDisplayNameAttr:  "cn",
		LDAPEmailAttr:        "mail",
		LDAPGroupAttr:        "memberOf",
		LDAPTimeout:          10 * time.Second,
		OIDCScopes:           "openid profile email",
		OIDCUsernameClaim:    "preferred_username",
		OIDCDisplayNameClaim: "name",
		OIDCEmailClaim:       "email",
		OIDCGroupsClaim:      "groups",
		OIDCName:             "SSO",
		OIDCTimeout:          10 * time.Second,
		Language:             "en",

		DiscoveryEnabled: true,
		KubeTimeout:      10 * time.Second,
		EventWindow:      2 * time.Hour,
		EventLimit:       20,
		EventCache:       10 * time.Second,

		WorkloadRolloutGrace: 10 * time.Minute,
		NotifyGroupWait:      30 * time.Second,

		DiscoverySyncInterval:    time.Minute,
		DiscoveryDefaultInterval: 30 * time.Second,
		DiscoveryDefaultTimeout:  5 * time.Second,
	}
	if got != want {
		t.Errorf("Load() = %+v, want %+v", got, want)
	}
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("UPTIME_ADDR", "127.0.0.1:9000")
	t.Setenv("UPTIME_DB_PATH", "/data/uptime.db")
	t.Setenv("UPTIME_DB_BUSY_TIMEOUT", "250ms")
	t.Setenv("UPTIME_READ_HEADER_TIMEOUT", "2s")
	t.Setenv("UPTIME_SHUTDOWN_TIMEOUT", "1m")
	t.Setenv("UPTIME_WORKERS", "50")
	t.Setenv("UPTIME_RETENTION", "48h")
	t.Setenv("UPTIME_HTTP_INSECURE_SKIP_VERIFY", "true")

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Config{
		Addr:              "127.0.0.1:9000",
		DBPath:            "/data/uptime.db",
		DBBusyTimeout:     250 * time.Millisecond,
		ReadHeaderTimeout: 2 * time.Second,
		ShutdownTimeout:   time.Minute,
	}
	if got.Workers != 50 || got.Retention != 48*time.Hour || !got.HTTPInsecureSkipVerify {
		t.Errorf("Workers=%d Retention=%s HTTPInsecureSkipVerify=%v", got.Workers, got.Retention, got.HTTPInsecureSkipVerify)
	}
	got = Config{Addr: got.Addr, DBPath: got.DBPath, DBBusyTimeout: got.DBBusyTimeout,
		ReadHeaderTimeout: got.ReadHeaderTimeout, ShutdownTimeout: got.ShutdownTimeout}
	if got != want {
		t.Errorf("Load() = %+v, want %+v", got, want)
	}
}

func TestLoadRejectsInvalidDuration(t *testing.T) {
	for _, v := range []string{"abc", "10", "0s", "-5s"} {
		t.Setenv("UPTIME_SHUTDOWN_TIMEOUT", v)
		if _, err := Load(); err == nil {
			t.Errorf("UPTIME_SHUTDOWN_TIMEOUT=%q was accepted", v)
		}
	}
}

func TestLoadRejectsInvalidNumbersAndBools(t *testing.T) {
	for _, v := range []string{"abc", "0", "-3", "1.5"} {
		t.Setenv("UPTIME_WORKERS", v)
		if _, err := Load(); err == nil {
			t.Errorf("UPTIME_WORKERS=%q was accepted", v)
		}
	}
	t.Setenv("UPTIME_WORKERS", "")

	t.Setenv("UPTIME_HTTP_INSECURE_SKIP_VERIFY", "maybe")
	if _, err := Load(); err == nil {
		t.Error("UPTIME_HTTP_INSECURE_SKIP_VERIFY=maybe was accepted")
	}
}

func TestLoadRejectsSubSecondMinInterval(t *testing.T) {
	t.Setenv("UPTIME_MIN_CHECK_INTERVAL", "500ms")
	if _, err := Load(); err == nil {
		t.Error("UPTIME_MIN_CHECK_INTERVAL=500ms was accepted")
	}
}

func TestLoadRejectsUnknownSMTPSecurity(t *testing.T) {
	t.Setenv("UPTIME_SMTP_SECURITY", "ssl")
	if _, err := Load(); err == nil {
		t.Error("UPTIME_SMTP_SECURITY=ssl was accepted")
	}
}

func TestReminderInterval(t *testing.T) {
	for value, want := range map[string]time.Duration{"": 0, "0": 0, "0s": 0, "1h": time.Hour} {
		t.Setenv("UPTIME_REMINDER_INTERVAL", value)
		cfg, err := Load()
		if err != nil || cfg.ReminderInterval != want {
			t.Errorf("UPTIME_REMINDER_INTERVAL=%q: %s, %v; want %s", value, cfg.ReminderInterval, err, want)
		}
	}
	for _, bad := range []string{"-1h", "soon"} {
		t.Setenv("UPTIME_REMINDER_INTERVAL", bad)
		if _, err := Load(); err == nil {
			t.Errorf("UPTIME_REMINDER_INTERVAL=%q was accepted", bad)
		}
	}
}

func TestHeartbeatIntervalMustAllowATick(t *testing.T) {
	t.Setenv("UPTIME_HEARTBEAT_URL", "https://hc.example.com/ping/x")
	t.Setenv("UPTIME_SCHEDULER_TICK", "10s")
	t.Setenv("UPTIME_HEARTBEAT_INTERVAL", "15s")
	if _, err := Load(); err == nil {
		t.Error("a heartbeat interval shorter than two scheduler ticks was accepted")
	}
	t.Setenv("UPTIME_HEARTBEAT_INTERVAL", "20s")
	if _, err := Load(); err != nil {
		t.Errorf("interval of exactly two ticks rejected: %v", err)
	}
	// Without a URL the interval is not used, so it is not checked.
	t.Setenv("UPTIME_HEARTBEAT_URL", "")
	t.Setenv("UPTIME_HEARTBEAT_INTERVAL", "1s")
	if _, err := Load(); err != nil {
		t.Errorf("unused heartbeat interval rejected: %v", err)
	}
}

func TestTargetCIDRs(t *testing.T) {
	t.Setenv("UPTIME_TARGET_DENY_CIDRS", "10.0.0.0/8, fd00::/8")
	t.Setenv("UPTIME_TARGET_ALLOW_CIDRS", "127.0.0.1/32,,169.254.20.10/32")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.TargetDenyCIDRs != "10.0.0.0/8, fd00::/8" || cfg.TargetAllowCIDRs != "127.0.0.1/32,,169.254.20.10/32" {
		t.Errorf("deny=%q allow=%q", cfg.TargetDenyCIDRs, cfg.TargetAllowCIDRs)
	}

	// A mistake names its variable and, for a bare address, the CIDR meant.
	for _, key := range []string{"UPTIME_TARGET_DENY_CIDRS", "UPTIME_TARGET_ALLOW_CIDRS"} {
		for value, want := range map[string]string{
			"10.0.0.0/8,localhost": `"localhost" is not a CIDR`,
			"10.0.0.0/40":          `"10.0.0.0/40" is not a CIDR`,
			"127.0.0.1":            "write it as 127.0.0.1/32",
			"fe80::1%eth0":         "write it as fe80::1/128",
		} {
			t.Setenv(key, value)
			_, err := Load()
			if err == nil || !strings.HasPrefix(err.Error(), key+": ") || !strings.Contains(err.Error(), want) {
				t.Errorf("%s=%q: err = %v, want one naming the variable and saying %q", key, value, err, want)
			}
		}
		t.Setenv(key, "")
	}
}
