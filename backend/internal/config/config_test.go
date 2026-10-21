package config

import (
	"os"
	"path/filepath"
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

		HTTPReadTimeout:    30 * time.Second,
		HTTPWriteTimeout:   2 * time.Minute,
		HTTPIdleTimeout:    2 * time.Minute,
		HTTPMaxHeaderBytes: 1 << 20,

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

		DeliveryRetention:     365 * 24 * time.Hour,
		IncidentTimelineLimit: 100,

		KeywordBodyLimit: 1 << 20,

		MinCheckInterval: 5 * time.Second,
		StatsWindow:      24 * time.Hour,
		MaxResults:       1000,

		StatusPageDays:           90,
		StatusPageIncidentWindow: 14 * 24 * time.Hour,
		StatusPageUpdates:        20,
		StatusPageUpcoming:       7 * 24 * time.Hour,
		StatusPageSession:        12 * time.Hour,
		StatusPageSeries:         24 * time.Hour,

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

		LoginThrottleMaxKeys: 100000,
		LoginConcurrency:     0, // half the CPUs
		LoginQueueWait:       3 * time.Second,
		LoginAuditPerMinute:  60,

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
	t.Setenv("UPTIME_METRICS_TOKEN", "scrape-token")

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
	if got.MetricsToken != "scrape-token" {
		t.Errorf("MetricsToken = %q, want scrape-token", got.MetricsToken)
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

func TestLoginConcurrencyMayBeZero(t *testing.T) {
	for value, want := range map[string]int{"": 0, "0": 0, "4": 4} {
		t.Setenv("UPTIME_LOGIN_CONCURRENCY", value)
		cfg, err := Load()
		if err != nil || cfg.LoginConcurrency != want {
			t.Errorf("UPTIME_LOGIN_CONCURRENCY=%q: %d, %v; want %d", value, cfg.LoginConcurrency, err, want)
		}
	}
	for _, bad := range []string{"-1", "many"} {
		t.Setenv("UPTIME_LOGIN_CONCURRENCY", bad)
		if _, err := Load(); err == nil {
			t.Errorf("UPTIME_LOGIN_CONCURRENCY=%q was accepted", bad)
		}
	}
}

// The key can come from a file, a mounted Secret for instance, but not
// from both places at once. The file is taken byte for byte, as the
// variable is, its trailing newline too: the same Secret gives the same
// key either way.
func TestSecretKeyFile(t *testing.T) {
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "key")
	const written = "0123456789abcdef0123456789abcdef-from-file\n"
	if err := os.WriteFile(keyFile, []byte(written), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UPTIME_SECRET_KEY", "")
	t.Setenv("UPTIME_SECRET_KEY_FILE", keyFile)
	cfg, err := Load()
	if err != nil || cfg.SecretKey != written {
		t.Errorf("key from a file = %q, %v; want %q", cfg.SecretKey, err, written)
	}
	t.Setenv("UPTIME_SECRET_KEY_FILE", "")
	t.Setenv("UPTIME_SECRET_KEY", written)
	if fromEnv, err := Load(); err != nil || fromEnv.SecretKey != cfg.SecretKey {
		t.Errorf("the same bytes in the variable = %q, %v; want the file's key", fromEnv.SecretKey, err)
	}
	t.Setenv("UPTIME_SECRET_KEY", "")
	t.Setenv("UPTIME_SECRET_KEY_FILE", keyFile)

	t.Setenv("UPTIME_SECRET_KEY", "0123456789abcdef0123456789abcdef-from-env")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "both") {
		t.Errorf("both set: err = %v", err)
	}
	t.Setenv("UPTIME_SECRET_KEY", "")

	empty := filepath.Join(dir, "empty")
	os.WriteFile(empty, []byte("\n"), 0o600)
	for _, bad := range []string{empty, filepath.Join(dir, "missing")} {
		t.Setenv("UPTIME_SECRET_KEY_FILE", bad)
		if _, err := Load(); err == nil || !strings.HasPrefix(err.Error(), "UPTIME_SECRET_KEY_FILE") {
			t.Errorf("UPTIME_SECRET_KEY_FILE=%s: err = %v", bad, err)
		}
	}

	t.Setenv("UPTIME_SECRET_KEY_FILE", "")
	t.Setenv("UPTIME_ALLOW_PLAINTEXT_SECRETS", "yes")
	if _, err := Load(); err == nil {
		t.Error("UPTIME_ALLOW_PLAINTEXT_SECRETS=yes was accepted")
	}
	t.Setenv("UPTIME_ALLOW_PLAINTEXT_SECRETS", "true")
	if cfg, err := Load(); err != nil || !cfg.AllowPlaintextSecrets || cfg.SecretKey != "" {
		t.Errorf("plaintext allowed: %+v, %v", cfg.AllowPlaintextSecrets, err)
	}
}

func TestLoadRejectsSubSecondMinInterval(t *testing.T) {
	t.Setenv("UPTIME_MIN_CHECK_INTERVAL", "500ms")
	if _, err := Load(); err == nil {
		t.Error("UPTIME_MIN_CHECK_INTERVAL=500ms was accepted")
	}
}

// An incident's timeline shows how it began and how it ended: that takes
// room for two entries at least.
func TestIncidentTimelineLimit(t *testing.T) {
	t.Setenv("UPTIME_INCIDENT_TIMELINE_LIMIT", "1")
	if _, err := Load(); err == nil {
		t.Error("UPTIME_INCIDENT_TIMELINE_LIMIT=1 was accepted")
	}
	t.Setenv("UPTIME_INCIDENT_TIMELINE_LIMIT", "2")
	t.Setenv("UPTIME_DELIVERY_RETENTION", "720h")
	got, err := Load()
	if err != nil || got.IncidentTimelineLimit != 2 || got.DeliveryRetention != 30*24*time.Hour {
		t.Errorf("Load() = limit %d, retention %s, %v", got.IncidentTimelineLimit, got.DeliveryRetention, err)
	}
}

func TestLoadRejectsUnknownSMTPSecurity(t *testing.T) {
	t.Setenv("UPTIME_SMTP_SECURITY", "ssl")
	if _, err := Load(); err == nil {
		t.Error("UPTIME_SMTP_SECURITY=ssl was accepted")
	}
}

// The OIDC redirect URL comes from the configuration only: set, or made
// from the public address, and checked at start. One that cannot be used
// turns SSO off, with the reason, but never stops the server.
func TestOIDCRedirectURL(t *testing.T) {
	load := func() Config {
		t.Helper()
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load: %v; an SSO setting must not stop the server", err)
		}
		return cfg
	}
	t.Setenv("UPTIME_PUBLIC_URL", "https://kubeat.apps.example/")
	t.Setenv("UPTIME_OIDC_REDIRECT_URL", "")
	if cfg := load(); cfg.OIDCRedirectURL != "" || cfg.OIDCProblem != nil {
		t.Errorf("OIDC off: redirect = %q, %v; want none", cfg.OIDCRedirectURL, cfg.OIDCProblem)
	}

	t.Setenv("UPTIME_OIDC_ISSUER", "https://sso.example.com/realms/corp")
	t.Setenv("UPTIME_OIDC_CLIENT_ID", "kubeat")
	if cfg := load(); cfg.OIDCProblem != nil || cfg.OIDCRedirectURL != "https://kubeat.apps.example/auth/callback" {
		t.Errorf("from the public URL: %q, %v", cfg.OIDCRedirectURL, cfg.OIDCProblem)
	}
	t.Setenv("UPTIME_OIDC_REDIRECT_URL", "https://sso-entry.example/auth/callback")
	if cfg := load(); cfg.OIDCProblem != nil || cfg.OIDCRedirectURL != "https://sso-entry.example/auth/callback" {
		t.Errorf("set: %q, %v; it wins over the public URL", cfg.OIDCRedirectURL, cfg.OIDCProblem)
	}
	t.Setenv("UPTIME_PUBLIC_URL", "")
	t.Setenv("UPTIME_OIDC_REDIRECT_URL", "")
	if cfg := load(); cfg.OIDCProblem != nil || cfg.OIDCRedirectURL != "" {
		t.Errorf("neither set: %q, %v; main warns about it", cfg.OIDCRedirectURL, cfg.OIDCProblem)
	}

	// Plain HTTP only on this machine; private addresses and internal
	// names are where many installs serve the UI.
	for _, good := range []string{
		"http://localhost:3000/auth/callback", "http://127.0.0.1:3000/auth/callback", "http://[::1]:3000/auth/callback",
		"https://10.0.0.5/auth/callback", "https://kubeat.shop.svc:3000/auth/callback",
	} {
		t.Setenv("UPTIME_OIDC_REDIRECT_URL", good)
		if cfg := load(); cfg.OIDCProblem != nil {
			t.Errorf("UPTIME_OIDC_REDIRECT_URL=%q: %v", good, cfg.OIDCProblem)
		}
	}
	for _, bad := range []string{
		"http://kubeat.apps.example/auth/callback", "kubeat.apps.example/auth/callback", "/auth/callback", "https:///auth/callback",
		"https://kubeat.apps.example/", "https://kubeat.apps.example/auth/callback/", "https://kubeat.apps.example/auth/callback?x=1",
		"https://kubeat.apps.example/auth/callback#x", "https://user@kubeat.apps.example/auth/callback",
	} {
		t.Setenv("UPTIME_OIDC_REDIRECT_URL", bad)
		if p := load().OIDCProblem; p == nil || !strings.HasPrefix(p.Error(), "UPTIME_OIDC_REDIRECT_URL: ") {
			t.Errorf("UPTIME_OIDC_REDIRECT_URL=%q: problem = %v, want one naming it", bad, p)
		}
	}
	// A public URL that cannot make one is a problem as well, with what to set.
	t.Setenv("UPTIME_OIDC_REDIRECT_URL", "")
	for _, bad := range []string{"http://kubeat.apps.example", "https://example.com/kubeat"} {
		t.Setenv("UPTIME_PUBLIC_URL", bad)
		if p := load().OIDCProblem; p == nil || !strings.HasPrefix(p.Error(), "UPTIME_PUBLIC_URL") || !strings.Contains(p.Error(), "set UPTIME_OIDC_REDIRECT_URL") {
			t.Errorf("UPTIME_PUBLIC_URL=%q: problem = %v", bad, p)
		}
	}
	// So is an issuer without a client id.
	t.Setenv("UPTIME_PUBLIC_URL", "https://kubeat.apps.example")
	t.Setenv("UPTIME_OIDC_CLIENT_ID", "")
	if p := load().OIDCProblem; p == nil || !strings.Contains(p.Error(), "UPTIME_OIDC_CLIENT_ID") {
		t.Errorf("no client id: problem = %v", p)
	}
	// Without OIDC neither is used, so neither is checked.
	t.Setenv("UPTIME_OIDC_ISSUER", "")
	t.Setenv("UPTIME_OIDC_REDIRECT_URL", "http://kubeat.apps.example/auth/callback")
	if p := load().OIDCProblem; p != nil {
		t.Errorf("unused redirect URL is a problem: %v", p)
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
