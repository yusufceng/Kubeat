package config

import (
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// proxyEnv are the variables net/http reads a proxy from. A proxy in the
// environment the tests run in would add its warning to every Load (S81),
// so they start without one; a test that needs one sets it.
var proxyEnv = []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy"}

func TestMain(m *testing.M) {
	for _, key := range proxyEnv {
		os.Unsetenv(key)
	}
	os.Exit(m.Run())
}

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
		CheckShowValue:   true,

		TargetResolveTimeout: 2 * time.Second,
		// TargetScopeTrustProxy defaults to false: a proxied NXDOMAIN is refused.

		MinCheckInterval: 5 * time.Second,
		StatsWindow:      24 * time.Hour,
		MaxResults:       1000,

		MinCheckIntervalExternal: 30 * time.Second,
		TargetMaxRate:            60,
		ClusterTargetMaxRate:     600,
		TargetAddressMaxRate:     600,
		MaxMonitorsPerHost:       5,
		MaxMonitors:              1000,
		MaxMonitorsPerNamespace:  200,
		MonitorCreatePerMinute:   30,

		StatusPageDays:           90,
		StatusPageIncidentWindow: 14 * 24 * time.Hour,
		StatusPageUpdates:        20,
		StatusPageUpcoming:       7 * 24 * time.Hour,
		StatusPageSession:        12 * time.Hour,
		StatusPageSeries:         24 * time.Hour,

		DefaultFailureThreshold: 3,
		RecoveryThreshold:       1,
		CertWarning:             14 * 24 * time.Hour,
		NotifyQueueSize:         1000,
		NotifyAttempts:          3,
		NotifyRetryDelay:        5 * time.Second,
		NotifyTimeout:           5 * time.Second,
		NotifyRetryMax:          15 * time.Minute,
		NotifyRetryFor:          time.Hour,

		PrometheusTimeout:    10 * time.Second,
		PrometheusRateWindow: 5 * time.Minute,

		BackupInterval: 24 * time.Hour,
		BackupKeep:     7,

		LicenseReloadInterval: time.Minute,

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

		LoginThrottleMaxKeys:    100000,
		LoginConcurrency:        0, // half the CPUs
		LoginQueueWait:          3 * time.Second,
		LoginAuditPerMinute:     60,
		LoginAccountMaxFailures: 50,
		// TrustProxyHops defaults to 0: no trusted proxy, use the peer.

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
	if !reflect.DeepEqual(got, want) {
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
	if !reflect.DeepEqual(got, want) {
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

// The ceiling on one source's failures across names is off unless set, and
// off at 0; whether it was given at all is kept apart, for the start to warn
// of it left unset with LDAP on and only note a 0 given on purpose.
func TestLoginSourceMaxFailures(t *testing.T) {
	for _, tt := range []struct {
		value string
		want  int
		set   bool
	}{{"", 0, false}, {"0", 0, true}, {"20", 20, true}} {
		t.Setenv("UPTIME_LOGIN_SOURCE_MAX_FAILURES", tt.value)
		cfg, err := Load()
		if err != nil || cfg.LoginSourceMaxFailures != tt.want || cfg.LoginSourceMaxFailuresSet != tt.set {
			t.Errorf("UPTIME_LOGIN_SOURCE_MAX_FAILURES=%q: %d (set %v), %v; want %d (set %v)", tt.value,
				cfg.LoginSourceMaxFailures, cfg.LoginSourceMaxFailuresSet, err, tt.want, tt.set)
		}
	}
	for _, bad := range []string{"-1", "many"} {
		t.Setenv("UPTIME_LOGIN_SOURCE_MAX_FAILURES", bad)
		if _, err := Load(); err == nil || !strings.HasPrefix(err.Error(), "UPTIME_LOGIN_SOURCE_MAX_FAILURES: ") {
			t.Errorf("UPTIME_LOGIN_SOURCE_MAX_FAILURES=%q: err = %v, want a refusal naming the variable", bad, err)
		}
	}
}

// The UI's token tells it from other peers: X-Forwarded-For is read only
// from a request that carries it. With hops trusted and no token, no peer
// is trusted, and the start says so once, as it does of a token a header
// cannot carry (which the UI would never send as it is). Neither stops the
// backend.
func TestFrontendToken(t *testing.T) {
	warnsOfNoToken := func(cfg Config) bool {
		for _, w := range cfg.Warnings {
			if strings.Contains(w, "UPTIME_FRONTEND_TOKEN is not set") {
				return true
			}
		}
		return false
	}
	for _, tc := range []struct {
		name, hops, token string
		want              string
		noTokenWarning    bool
		unsendableWarning bool
	}{
		{name: "defaults", want: ""},
		{name: "the chart's install: a token, hops 0", token: "Gh7k2Lm9", want: "Gh7k2Lm9"},
		{name: "a token and hops", hops: "1", token: "Gh7k2Lm9", want: "Gh7k2Lm9"},
		{name: "surrounding spaces and a newline go", hops: "1", token: " Gh7k2Lm9\n", want: "Gh7k2Lm9"},
		{name: "hops without a token", hops: "2", want: "", noTokenWarning: true},
		{name: "hops with a token of spaces", hops: "1", token: "   ", want: "", noTokenWarning: true},
		{name: "a space inside", hops: "1", token: "Gh7k 2Lm9", want: "", noTokenWarning: true, unsendableWarning: true},
		{name: "a byte outside ASCII", hops: "1", token: "Gh7kç2Lm9", want: "", noTokenWarning: true, unsendableWarning: true},
		{name: "a control character inside, hops 0", token: "Gh7k\x012Lm9", want: "", unsendableWarning: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("UPTIME_TRUST_PROXY_HOPS", tc.hops)
			t.Setenv("UPTIME_FRONTEND_TOKEN", tc.token)
			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.FrontendToken != tc.want {
				t.Errorf("FrontendToken = %q, want %q", cfg.FrontendToken, tc.want)
			}
			if got := warnsOfNoToken(cfg); got != tc.noTokenWarning {
				t.Errorf("warns of no token = %v, want %v: %q", got, tc.noTokenWarning, cfg.Warnings)
			}
			unsendable := false
			for _, w := range cfg.Warnings {
				if strings.Contains(w, "a character a header cannot carry") {
					unsendable = true
				}
				// The token is a secret: no warning repeats it.
				if tc.token != "" && strings.TrimSpace(tc.token) != "" && strings.Contains(w, strings.TrimSpace(tc.token)) {
					t.Errorf("a warning names the token: %q", w)
				}
			}
			if unsendable != tc.unsendableWarning {
				t.Errorf("warns of an unsendable token = %v, want %v: %q", unsendable, tc.unsendableWarning, cfg.Warnings)
			}
		})
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

// The external minimum is a stricter rule on top of the general one, so
// it cannot be the shorter; zero means there is no separate rule.
func TestExternalMinInterval(t *testing.T) {
	t.Setenv("UPTIME_MIN_CHECK_INTERVAL", "10s")
	t.Setenv("UPTIME_MIN_CHECK_INTERVAL_EXTERNAL", "5s")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "UPTIME_MIN_CHECK_INTERVAL_EXTERNAL") || !strings.Contains(err.Error(), "10s") {
		t.Errorf("5s under a 10s minimum: err = %v, want a refusal naming both", err)
	}
	t.Setenv("UPTIME_MIN_CHECK_INTERVAL_EXTERNAL", "0")
	cfg, err := Load()
	if err != nil || cfg.MinCheckIntervalExternal != 0 {
		t.Errorf("0: cfg.MinCheckIntervalExternal = %s, err = %v; want 0 (off) and no error", cfg.MinCheckIntervalExternal, err)
	}
	t.Setenv("UPTIME_MIN_CHECK_INTERVAL_EXTERNAL", "10s")
	if cfg, err = Load(); err != nil || cfg.MinCheckIntervalExternal != 10*time.Second {
		t.Errorf("equal to the minimum: cfg.MinCheckIntervalExternal = %s, err = %v", cfg.MinCheckIntervalExternal, err)
	}
}

// The discovery default interval is for in-cluster targets and may be
// shorter than the external minimum, which an annotated Ingress or Route
// without an interval of its own then falls under: Load says so, and
// does not refuse, since the monitors there are keep running.
func TestDiscoveryDefaultIntervalUnderTheExternalMinimumWarns(t *testing.T) {
	t.Setenv("UPTIME_DISCOVERY_DEFAULT_INTERVAL", "15s")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("15s under the external 30s: %v", err)
	}
	if len(cfg.Warnings) != 1 || !strings.Contains(cfg.Warnings[0], "UPTIME_DISCOVERY_DEFAULT_INTERVAL (15s)") ||
		!strings.Contains(cfg.Warnings[0], "UPTIME_MIN_CHECK_INTERVAL_EXTERNAL (30s)") {
		t.Errorf("warnings = %q, want one naming both settings", cfg.Warnings)
	}
	t.Setenv("UPTIME_MIN_CHECK_INTERVAL_EXTERNAL", "0")
	if cfg, err := Load(); err != nil || len(cfg.Warnings) != 0 {
		t.Errorf("without the external rule: warnings = %q, err = %v; want none", cfg.Warnings, err)
	}
	t.Setenv("UPTIME_MIN_CHECK_INTERVAL_EXTERNAL", "15s")
	if cfg, err := Load(); err != nil || len(cfg.Warnings) != 0 {
		t.Errorf("default equal to the external minimum: warnings = %q, err = %v; want none", cfg.Warnings, err)
	}
}

// UPTIME_TARGET_SCOPE_TRUST_PROXY is off by default, which keeps 0.49's
// refusal of an external name only the proxy resolves. The start warns once
// when a proxy is set and the setting is off, naming the setting and the
// condition for turning it on (a proxy outside the cluster), and when the
// setting is on with no proxy, where it does nothing (S81).
func TestTargetScopeTrustProxy(t *testing.T) {
	load := func() Config {
		t.Helper()
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		return cfg
	}
	if cfg := load(); cfg.TargetScopeTrustProxy || len(cfg.Warnings) != 0 {
		t.Errorf("default: trust proxy = %v, warnings = %q; want off and none", cfg.TargetScopeTrustProxy, cfg.Warnings)
	}

	for _, key := range proxyEnv {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "http://proxy.corp.example:3128")
			cfg := load()
			if len(cfg.Warnings) != 1 || !strings.Contains(cfg.Warnings[0], "UPTIME_TARGET_SCOPE_TRUST_PROXY is off") ||
				!strings.Contains(cfg.Warnings[0], "outside the cluster") || !strings.Contains(cfg.Warnings[0], "UPTIME_TARGET_SCOPE_TRUST_PROXY=true") {
				t.Errorf("%s set, setting off: warnings = %q, want one naming the setting and the condition", key, cfg.Warnings)
			}
			for _, off := range []string{"false", "0"} {
				t.Setenv("UPTIME_TARGET_SCOPE_TRUST_PROXY", off)
				if cfg := load(); cfg.TargetScopeTrustProxy || len(cfg.Warnings) != 1 {
					t.Errorf("%s set, setting %q: trust proxy = %v, warnings = %q; want off and the warning", key, off, cfg.TargetScopeTrustProxy, cfg.Warnings)
				}
			}
			t.Setenv("UPTIME_TARGET_SCOPE_TRUST_PROXY", "true")
			if cfg := load(); !cfg.TargetScopeTrustProxy || len(cfg.Warnings) != 0 {
				t.Errorf("%s set, setting on: trust proxy = %v, warnings = %q; want on and none", key, cfg.TargetScopeTrustProxy, cfg.Warnings)
			}
		})
	}

	t.Setenv("UPTIME_TARGET_SCOPE_TRUST_PROXY", "true")
	if cfg := load(); !cfg.TargetScopeTrustProxy || len(cfg.Warnings) != 1 || !strings.Contains(cfg.Warnings[0], "changes nothing") {
		t.Errorf("setting on, no proxy: trust proxy = %v, warnings = %q; want on and one saying it does nothing", cfg.TargetScopeTrustProxy, cfg.Warnings)
	}
	t.Setenv("UPTIME_TARGET_SCOPE_TRUST_PROXY", "sometimes")
	if _, err := Load(); err == nil {
		t.Error("UPTIME_TARGET_SCOPE_TRUST_PROXY=sometimes was accepted")
	}
}

// Every guardrail turns off at zero and refuses a negative number.
func TestGuardrailLimitsMayBeZero(t *testing.T) {
	keys := []string{
		"UPTIME_TARGET_MAX_RATE", "UPTIME_CLUSTER_TARGET_MAX_RATE", "UPTIME_TARGET_ADDRESS_MAX_RATE", "UPTIME_MAX_MONITORS_PER_HOST",
		"UPTIME_MAX_MONITORS", "UPTIME_MAX_MONITORS_PER_NAMESPACE", "UPTIME_MONITOR_CREATE_PER_MINUTE",
	}
	for _, key := range keys {
		t.Setenv(key, "0")
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("all zero: %v", err)
	}
	if cfg.TargetMaxRate != 0 || cfg.ClusterTargetMaxRate != 0 || cfg.TargetAddressMaxRate != 0 || cfg.MaxMonitorsPerHost != 0 ||
		cfg.MaxMonitors != 0 || cfg.MaxMonitorsPerNamespace != 0 || cfg.MonitorCreatePerMinute != 0 {
		t.Errorf("zero did not turn the limits off: %+v", cfg)
	}
	for _, key := range keys {
		t.Setenv(key, "-1")
		if _, err := Load(); err == nil || !strings.HasPrefix(err.Error(), key+": ") {
			t.Errorf("%s=-1: err = %v, want a refusal naming the variable", key, err)
		}
		t.Setenv(key, "")
	}
}

func TestUserAgent(t *testing.T) {
	if got, want := DefaultUserAgent("1.2.3", ""), "kubeat/1.2.3 (+https://github.com/yusufceng/Kubeat)"; got != want {
		t.Errorf("without a public URL: %q, want %q", got, want)
	}
	got := DefaultUserAgent("1.2.3", "https://kubeat.apps.example")
	if want := "kubeat/1.2.3 (+https://github.com/yusufceng/Kubeat; https://kubeat.apps.example)"; got != want {
		t.Errorf("with a public URL: %q, want %q", got, want)
	}
	t.Setenv("UPTIME_PUBLIC_URL", "https://kubeat.apps.example/")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if ua := cfg.UserAgentFor("dev"); ua != "kubeat/dev (+https://github.com/yusufceng/Kubeat; https://kubeat.apps.example)" {
		t.Errorf("UserAgentFor = %q", ua)
	}
	t.Setenv("UPTIME_USER_AGENT", " corp-monitor/7 ")
	if cfg, err = Load(); err != nil || cfg.UserAgentFor("dev") != "corp-monitor/7" {
		t.Errorf("override: UserAgentFor = %q, err = %v", cfg.UserAgentFor("dev"), err)
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

// The two settings of sending an outage notification again take a
// duration; zero turns each off.
func TestNotifyRetry(t *testing.T) {
	for _, key := range []string{"UPTIME_NOTIFY_RETRY_MAX", "UPTIME_NOTIFY_RETRY_FOR"} {
		for value, want := range map[string]time.Duration{"0": 0, "0s": 0, "30m": 30 * time.Minute} {
			t.Setenv(key, value)
			cfg, err := Load()
			got := cfg.NotifyRetryMax
			if key == "UPTIME_NOTIFY_RETRY_FOR" {
				got = cfg.NotifyRetryFor
			}
			if err != nil || got != want {
				t.Errorf("%s=%q: %s, %v; want %s", key, value, got, err, want)
			}
		}
		for _, bad := range []string{"-1m", "later"} {
			t.Setenv(key, bad)
			if _, err := Load(); err == nil {
				t.Errorf("%s=%q was accepted", key, bad)
			}
		}
		t.Setenv(key, "")
	}
}

// The cap on the wait between the attempts is at least a minute, and at
// least one attempt: shorter, a channel that is down would be sent to
// without a pause. A shorter cap is raised to that floor with a warning
// that names the value given and the one used, rather than refused: a
// value an earlier version took must not keep the backend from starting.
// Zero still turns the attempts off, and a value that is not a duration is
// still refused (TestNotifyRetry). The end of an outage has no floor.
func TestNotifyRetryMaxIsRaisedToItsFloor(t *testing.T) {
	// load loads with UPTIME_NOTIFY_RETRY_MAX set to value and returns the
	// cap in effect and the warnings.
	load := func(value string) (time.Duration, []string) {
		t.Helper()
		t.Setenv("UPTIME_NOTIFY_RETRY_MAX", value)
		cfg, err := Load()
		if err != nil {
			t.Fatalf("UPTIME_NOTIFY_RETRY_MAX=%q: %v, want the backend to start", value, err)
		}
		return cfg.NotifyRetryMax, cfg.Warnings
	}
	for _, value := range []string{"1s", "10s", "59s"} {
		got, warnings := load(value)
		if got != time.Minute {
			t.Errorf("UPTIME_NOTIFY_RETRY_MAX=%s: %s in effect, want it raised to 1m", value, got)
		}
		if len(warnings) != 1 || !strings.Contains(warnings[0], "UPTIME_NOTIFY_RETRY_MAX ("+value+")") ||
			!strings.Contains(warnings[0], "raised to 1m0s") {
			t.Errorf("UPTIME_NOTIFY_RETRY_MAX=%s: warnings = %q, want one naming %s and 1m0s", value, warnings, value)
		}
	}
	for value, want := range map[string]time.Duration{"1m": time.Minute, "0": 0, "0s": 0, "": 15 * time.Minute} {
		if got, warnings := load(value); got != want || len(warnings) != 0 {
			t.Errorf("UPTIME_NOTIFY_RETRY_MAX=%q: %s, warnings %q; want %s and none", value, got, warnings, want)
		}
	}

	// An attempt of 5 tries of 30s with 10s between them is 200s long.
	t.Setenv("UPTIME_NOTIFY_ATTEMPTS", "5")
	t.Setenv("UPTIME_NOTIFY_TIMEOUT", "30s")
	t.Setenv("UPTIME_NOTIFY_RETRY_DELAY", "10s")
	got, warnings := load("3m")
	if got != 200*time.Second || len(warnings) != 1 || !strings.Contains(warnings[0], "UPTIME_NOTIFY_RETRY_MAX (3m0s)") ||
		!strings.Contains(warnings[0], "= 3m20s") || !strings.Contains(warnings[0], "raised to 3m20s") {
		t.Errorf("UPTIME_NOTIFY_RETRY_MAX=3m with attempts of 3m20s: %s, warnings %q; want 3m20s and one warning naming both", got, warnings)
	}
	for value, want := range map[string]time.Duration{"4m": 4 * time.Minute, "0": 0, "": 15 * time.Minute} {
		if got, warnings := load(value); got != want || len(warnings) != 0 {
			t.Errorf("UPTIME_NOTIFY_RETRY_MAX=%q with attempts of 3m20s: %s, warnings %q; want %s and none", value, got, warnings, want)
		}
	}
	t.Setenv("UPTIME_NOTIFY_RETRY_FOR", "30s")
	if cfg, err := Load(); err != nil || cfg.NotifyRetryFor != 30*time.Second {
		t.Errorf("UPTIME_NOTIFY_RETRY_FOR=30s: %s, %v", cfg.NotifyRetryFor, err)
	}
}

// HUMAN_RETRY_MAX, the shorter cap for the channels people read, defaults to
// 0 (the same as RETRY_MAX), is refused when negative or not a duration, and
// is raised to the same floor as RETRY_MAX when set shorter.
func TestNotifyHumanRetryMax(t *testing.T) {
	load := func(value string) (time.Duration, []string, error) {
		t.Helper()
		t.Setenv("UPTIME_NOTIFY_HUMAN_RETRY_MAX", value)
		cfg, err := Load()
		return cfg.NotifyHumanRetryMax, cfg.Warnings, err
	}
	for value, want := range map[string]time.Duration{"": 0, "0": 0, "0s": 0, "3m": 3 * time.Minute, "2m": 2 * time.Minute} {
		if got, warnings, err := load(value); err != nil || got != want || len(warnings) != 0 {
			t.Errorf("UPTIME_NOTIFY_HUMAN_RETRY_MAX=%q: %s, warnings %q, %v; want %s and none", value, got, warnings, err, want)
		}
	}
	for _, bad := range []string{"-1m", "soon"} {
		if _, _, err := load(bad); err == nil {
			t.Errorf("UPTIME_NOTIFY_HUMAN_RETRY_MAX=%q was accepted", bad)
		}
	}
	// A value under the floor (1m with the default attempt) is raised with a
	// warning that names it and the floor, as RETRY_MAX is.
	got, warnings, err := load("30s")
	if err != nil || got != time.Minute || len(warnings) != 1 ||
		!strings.Contains(warnings[0], "UPTIME_NOTIFY_HUMAN_RETRY_MAX (30s)") || !strings.Contains(warnings[0], "raised to 1m0s") {
		t.Errorf("UPTIME_NOTIFY_HUMAN_RETRY_MAX=30s: %s, warnings %q, %v; want 1m and one warning", got, warnings, err)
	}
}

// The name of the installation in notifications: as given, or else the
// host of the public address, or none.
func TestInstanceName(t *testing.T) {
	for _, tc := range []struct{ name, publicURL, want string }{
		{"", "", ""},
		{"", "https://kubeat.apps.example/", "kubeat.apps.example"},
		{"", "https://kubeat.apps.example:8443", "kubeat.apps.example:8443"},
		{"", "https://user:secret@kubeat.apps.example", "kubeat.apps.example"},
		{"", "kubeat.apps.example", ""}, // no scheme, so no host
		{" prod-eu ", "https://kubeat.apps.example", "prod-eu"},
		{"staging", "", "staging"},
	} {
		t.Setenv("UPTIME_INSTANCE_NAME", tc.name)
		t.Setenv("UPTIME_PUBLIC_URL", tc.publicURL)
		cfg, err := Load()
		if err != nil || cfg.InstanceName != tc.want {
			t.Errorf("UPTIME_INSTANCE_NAME=%q, UPTIME_PUBLIC_URL=%q: %q, %v; want %q", tc.name, tc.publicURL, cfg.InstanceName, err, tc.want)
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
	// Without a URL nothing is sent at that interval, so it is not refused:
	// kubeat_self_healthy, which judges over it, takes it as two ticks.
	t.Setenv("UPTIME_HEARTBEAT_URL", "")
	t.Setenv("UPTIME_HEARTBEAT_INTERVAL", "1s")
	if cfg, err := Load(); err != nil {
		t.Errorf("heartbeat interval without a URL rejected: %v", err)
	} else if got := cfg.HealthWindow(); got != 20*time.Second {
		t.Errorf("HealthWindow = %s with a 1s interval and a 10s tick, want two ticks, 20s", got)
	}
}

// The heartbeat and kubeat_self_healthy judge over the same window: the
// heartbeat interval, which a heartbeat URL holds to two ticks already.
func TestHealthWindow(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.HealthWindow(); got != time.Minute {
		t.Errorf("HealthWindow = %s by default, want UPTIME_HEARTBEAT_INTERVAL's 1m", got)
	}
	t.Setenv("UPTIME_HEARTBEAT_URL", "https://hc.example.com/ping/x")
	t.Setenv("UPTIME_HEARTBEAT_INTERVAL", "5m")
	if cfg, err = Load(); err != nil {
		t.Fatal(err)
	}
	if got := cfg.HealthWindow(); got != 5*time.Minute {
		t.Errorf("HealthWindow = %s with a heartbeat every 5m, want 5m", got)
	}
}

// The notification queue holds, by default, an outage of every monitor the
// default quota lets in; neither setting moves the other.
func TestNotifyQueueSizeDefaultsToTheMonitorQuota(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.NotifyQueueSize != 1000 || cfg.NotifyQueueSize != cfg.MaxMonitors {
		t.Errorf("UPTIME_NOTIFY_QUEUE_SIZE = %d and UPTIME_MAX_MONITORS = %d by default, want both 1000", cfg.NotifyQueueSize, cfg.MaxMonitors)
	}
	t.Setenv("UPTIME_MAX_MONITORS", "5000")
	if cfg, err = Load(); err != nil {
		t.Fatal(err)
	}
	if cfg.NotifyQueueSize != 1000 {
		t.Errorf("UPTIME_NOTIFY_QUEUE_SIZE = %d with UPTIME_MAX_MONITORS=5000, want the default 1000: the queue is not derived", cfg.NotifyQueueSize)
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

// pretendOwnAddrs makes the backend's own addresses those given (or the
// error), as a pod's interfaces would have them, for the rest of the test.
func pretendOwnAddrs(t *testing.T, err error, ips ...string) {
	t.Helper()
	saved := ownAddrs
	t.Cleanup(func() { ownAddrs = saved })
	addrs := make([]netip.Addr, len(ips))
	for i, ip := range ips {
		addrs[i] = netip.MustParseAddr(ip)
	}
	ownAddrs = func() ([]netip.Addr, error) { return addrs, err }
}

// UPTIME_TARGET_SCOPE_EXTERNAL_CIDRS is empty by default and reads nothing
// then. Set, it takes CIDRs only: an entry that is not one, or an IPv4 range
// written as IPv6, keeps the backend from starting, the error naming the
// variable. A range that holds the API server's address
// (KUBERNETES_SERVICE_HOST, in the Service network) or one of the backend's
// own (its pod IP or loopback, in the pod network), or that cannot be checked
// because those cannot be read, does not: it alone is set aside, named with
// why, and the rest stay in effect (S96, PO decision; S80 refused the start).
func TestTargetScopeExternalCIDRs(t *testing.T) {
	const key = "UPTIME_TARGET_SCOPE_EXTERNAL_CIDRS"
	t.Setenv("KUBERNETES_SERVICE_HOST", "172.30.0.1")
	pretendOwnAddrs(t, nil, "127.0.0.1", "::1", "10.128.2.15", "fd01::2:15", "fe80::1%eth0")

	cfg, err := Load()
	if err != nil || cfg.TargetScopeExternalCIDRs != "" || cfg.TargetScopeExternalIgnored != nil {
		t.Fatalf("default: %q, ignored %v, %v; want empty, none ignored and no error", cfg.TargetScopeExternalCIDRs, cfg.TargetScopeExternalIgnored, err)
	}

	// Company ranges clear of the cluster's networks are taken as written.
	t.Setenv(key, "10.50.0.0/16, 192.168.0.0/16,fd50::/16")
	if cfg, err := Load(); err != nil || cfg.TargetScopeExternalCIDRs != "10.50.0.0/16, 192.168.0.0/16,fd50::/16" || cfg.TargetScopeExternalIgnored != nil {
		t.Errorf("company ranges: %q, ignored %v, %v; want them, none ignored and no error", cfg.TargetScopeExternalCIDRs, cfg.TargetScopeExternalIgnored, err)
	}

	// A mistake names the variable, and a bare address the CIDR meant; it
	// stops the start even beside a range the start would set aside.
	for value, want := range map[string]string{
		"10.50.0.0/16,corp": `"corp" is not a CIDR`,
		"10.50.0.0/33":      `"10.50.0.0/33" is not a CIDR`,
		"10.50.0.1":         "write it as 10.50.0.1/32",
		// An IPv4 range written as IPv6 would hold no address for the scope
		// check, nor for this guard, which reads the ranges as it does.
		"::ffff:10.50.0.0/112":            "write it as 10.50.0.0/16",
		"::ffff:0.0.0.0/96":               "write it as 0.0.0.0/0",
		"10.0.0.0/8,::ffff:10.50.0.0/112": "write it as 10.50.0.0/16",
		"10.0.0.0/8,corp":                 `"corp" is not a CIDR`,
	} {
		t.Setenv(key, value)
		if _, err := Load(); err == nil || !strings.HasPrefix(err.Error(), key+": ") || !strings.Contains(err.Error(), want) {
			t.Errorf("%s=%q: err = %v, want one naming the variable and saying %q", key, value, err, want)
		}
	}

	// A range over the API server's address or one of the backend's own is
	// set aside with why; the backend starts.
	for _, tc := range []struct{ apiServer, value, want string }{
		{"172.30.0.1", "172.16.0.0/12", "172.16.0.0/12 holds the API server's address 172.30.0.1 (KUBERNETES_SERVICE_HOST), in the cluster's Service network"},
		{"10.96.0.1", "10.0.0.0/8", "10.0.0.0/8 holds the API server's address 10.96.0.1"},
		{"fd00:10:96::1", "fd00::/8", "fd00::/8 holds the API server's address fd00:10:96::1"},
		{"172.30.0.1", "10.128.0.0/14", "10.128.0.0/14 holds 10.128.2.15, an address of the backend's own pod"},
		{"172.30.0.1", "10.0.0.0/8", "10.0.0.0/8 holds 10.128.2.15"},
		{"172.30.0.1", "fd01::/16", "fd01::/16 holds fd01::2:15"},
		{"172.30.0.1", "127.0.0.0/8", "127.0.0.0/8 holds 127.0.0.1"},
		{"172.30.0.1", "fe80::/10", "fe80::/10 holds fe80::1,"},
		{"172.30.0.1", "0.0.0.0/0", "0.0.0.0/0 holds the API server's address 172.30.0.1"},
		{"172.30.0.1", "10.128.2.15/32", "10.128.2.15/32 holds 10.128.2.15"},
		{"172.30.0.1", "10.130.0.1/14", "10.128.0.0/14 holds 10.128.2.15"}, // named as the scope reads it, masked
		{"", "10.0.0.0/8", "10.0.0.0/8 holds 10.128.2.15"},                 // outside a pod, the own addresses still count
	} {
		t.Setenv("KUBERNETES_SERVICE_HOST", tc.apiServer)
		t.Setenv(key, tc.value)
		cfg, err := Load()
		if err != nil || cfg.TargetScopeExternalCIDRs != "" || len(cfg.TargetScopeExternalIgnored) != 1 ||
			!strings.HasPrefix(cfg.TargetScopeExternalIgnored[0].String(), tc.want) {
			t.Errorf("API server %q, %s=%q: kept %q, ignored %v, err %v; want none kept, one ignored saying %q, no error",
				tc.apiServer, key, tc.value, cfg.TargetScopeExternalCIDRs, cfg.TargetScopeExternalIgnored, err, tc.want)
		}
	}

	// Only the ranges over an address of the cluster's own are set aside,
	// in the order written; the others stay as written.
	t.Setenv("KUBERNETES_SERVICE_HOST", "172.30.0.1")
	t.Setenv(key, "10.50.0.0/16, 172.16.0.0/12 ,10.128.0.0/14,fd50::/16")
	cfg, err = Load()
	if err != nil || cfg.TargetScopeExternalCIDRs != "10.50.0.0/16,fd50::/16" {
		t.Errorf("mixed: kept %q, %v; want the two company ranges and no error", cfg.TargetScopeExternalCIDRs, err)
	}
	wantIgnored := []IgnoredRange{
		{netip.MustParsePrefix("172.16.0.0/12"), "holds the API server's address 172.30.0.1 (KUBERNETES_SERVICE_HOST), in the cluster's Service network: " +
			"a user limited to some namespaces could reach every Service in it by its ClusterIP"},
		{netip.MustParsePrefix("10.128.0.0/14"), "holds 10.128.2.15, an address of the backend's own pod (its pod IP or loopback): " +
			"a user limited to some namespaces could reach the pods in it by address"},
	}
	if !reflect.DeepEqual(cfg.TargetScopeExternalIgnored, wantIgnored) {
		t.Errorf("mixed: ignored %v, want %v", cfg.TargetScopeExternalIgnored, wantIgnored)
	}

	// Emptied, the setting reads nothing again.
	t.Setenv("KUBERNETES_SERVICE_HOST", "10.96.0.1")
	t.Setenv(key, "")
	if cfg, err := Load(); err != nil || cfg.TargetScopeExternalIgnored != nil {
		t.Errorf("setting emptied: ignored %v, %v; want none", cfg.TargetScopeExternalIgnored, err)
	}
	// A KUBERNETES_SERVICE_HOST that is not an address is not compared.
	t.Setenv("KUBERNETES_SERVICE_HOST", "kubernetes.default.svc")
	t.Setenv(key, "10.50.0.0/16")
	if cfg, err := Load(); err != nil || cfg.TargetScopeExternalCIDRs != "10.50.0.0/16" || cfg.TargetScopeExternalIgnored != nil {
		t.Errorf("API server given by name: %q, ignored %v, %v; want the range kept", cfg.TargetScopeExternalCIDRs, cfg.TargetScopeExternalIgnored, err)
	}

	// Own addresses that cannot be read leave no range shown clear of them:
	// every range is set aside, one over the API server's address for that,
	// and the backend starts. They are not read while the setting is empty.
	pretendOwnAddrs(t, errors.New("no interfaces"))
	t.Setenv("KUBERNETES_SERVICE_HOST", "172.30.0.1")
	t.Setenv(key, "10.50.0.0/16,172.16.0.0/12")
	cfg, err = Load()
	if err != nil || cfg.TargetScopeExternalCIDRs != "" || len(cfg.TargetScopeExternalIgnored) != 2 ||
		cfg.TargetScopeExternalIgnored[0].String() != "10.50.0.0/16 cannot be checked: the backend's own addresses, which no range may hold, cannot be read: no interfaces" ||
		!strings.HasPrefix(cfg.TargetScopeExternalIgnored[1].String(), "172.16.0.0/12 holds the API server's address 172.30.0.1") {
		t.Errorf("own addresses unreadable: kept %q, ignored %v, %v; want both ignored with why", cfg.TargetScopeExternalCIDRs, cfg.TargetScopeExternalIgnored, err)
	}
	t.Setenv(key, "")
	if cfg, err := Load(); err != nil || cfg.TargetScopeExternalIgnored != nil {
		t.Errorf("own addresses unreadable, setting empty: ignored %v, %v; want none", cfg.TargetScopeExternalIgnored, err)
	}
}

// UPTIME_TARGET_SCOPE_CLUSTER_CIDRS declares the cluster's pod, Service and
// node networks (the chart's targetScope.clusterNetworks), empty by default.
// Set, it takes CIDRs only: an entry that is not one, or an IPv4 network
// written as IPv6, keeps the backend from starting, the error naming the
// variable. A company range that overlaps a declared network, any part of
// it and of either family, is set aside with why, as one over the API
// server's address is, and the backend starts (S96); a single address, the
// router's VIP in the node network as README's "Şart" writes it, is not
// compared with them. Empty, it changes nothing.
func TestTargetScopeClusterCIDRs(t *testing.T) {
	const key = "UPTIME_TARGET_SCOPE_CLUSTER_CIDRS"
	const external = "UPTIME_TARGET_SCOPE_EXTERNAL_CIDRS"
	// The API server's and the pod's own addresses are clear of every range
	// below, so that only the declaration sets any aside.
	t.Setenv("KUBERNETES_SERVICE_HOST", "172.30.0.1")
	pretendOwnAddrs(t, nil, "127.0.0.1", "::1", "10.128.2.15")

	cfg, err := Load()
	if err != nil || cfg.TargetScopeClusterCIDRs != "" || cfg.TargetScopeExternalIgnored != nil {
		t.Fatalf("default: %q, ignored %v, %v; want empty, none ignored and no error", cfg.TargetScopeClusterCIDRs, cfg.TargetScopeExternalIgnored, err)
	}

	// A mistake names the variable and stops the start, whatever the
	// company ranges are.
	t.Setenv(external, "10.50.0.0/16")
	for value, want := range map[string]string{
		"10.128.0.0/14,corp":     `"corp" is not a CIDR`,
		"10.128.0.0/33":          `"10.128.0.0/33" is not a CIDR`,
		"192.168.40.0":           "write it as 192.168.40.0/32",
		"::ffff:10.128.0.0/110":  "an IPv4 network written as IPv6; write it as 10.128.0.0/14",
		"::ffff:0.0.0.0/96":      "write it as 0.0.0.0/0",
		"fd02::/48,10.128.0.0/x": `"10.128.0.0/x" is not a CIDR`,
	} {
		t.Setenv(key, value)
		if _, err := Load(); err == nil || !strings.HasPrefix(err.Error(), key+": ") || !strings.Contains(err.Error(), want) {
			t.Errorf("%s=%q: err = %v, want one naming the variable and saying %q", key, value, err, want)
		}
	}

	// Declared networks: OpenShift's default pod and Service networks, a
	// node network, and an IPv6 pod network of a dual-stack cluster.
	const declared = "10.128.0.0/14, 172.30.0.0/16,192.168.40.0/24,fd02::/48"
	t.Setenv(key, declared)
	why := func(network string) string {
		return "overlaps " + network + ", a cluster network declared in UPTIME_TARGET_SCOPE_CLUSTER_CIDRS: " +
			"a user limited to some namespaces could reach the pods, Services or nodes in it by address"
	}
	for _, tc := range []struct {
		name, value, kept string
		ignored           []IgnoredRange
	}{
		{"clear of every network", "10.50.0.0/16,fd50::/16", "10.50.0.0/16,fd50::/16", nil},
		// A part of the pod network the pod's address is not in: the drain of
		// sprint 10's rehearsal (c), caught at the first start.
		{"a part of the pod network", "10.130.0.0/16", "", []IgnoredRange{{netip.MustParsePrefix("10.130.0.0/16"), why("10.128.0.0/14")}}},
		{"over the pod network", "10.0.0.0/8,10.50.0.0/16", "10.50.0.0/16", []IgnoredRange{{netip.MustParsePrefix("10.0.0.0/8"), why("10.128.0.0/14")}}},
		{"over the Service network, clear of the API server", "172.30.128.0/17", "", []IgnoredRange{{netip.MustParsePrefix("172.30.128.0/17"), why("172.30.0.0/16")}}},
		{"over the node network", "10.50.0.0/16, 192.168.0.0/16", "10.50.0.0/16", []IgnoredRange{{netip.MustParsePrefix("192.168.0.0/16"), why("192.168.40.0/24")}}},
		{"masked as the scope reads it", "192.168.40.77/28", "", []IgnoredRange{{netip.MustParsePrefix("192.168.40.64/28"), why("192.168.40.0/24")}}},
		{"IPv6 over the pod network", "fd50::/16,fd02:0:0:5::/64", "fd50::/16", []IgnoredRange{{netip.MustParsePrefix("fd02:0:0:5::/64"), why("fd02::/48")}}},
		{"IPv6 holding the pod network", "fd00::/8", "", []IgnoredRange{{netip.MustParsePrefix("fd00::/8"), why("fd02::/48")}}},
		// The router's VIP in the node network, written on purpose as one
		// address, and one IPv6 address: not compared with the declaration.
		{"the router's VIP as a /32", "10.50.0.0/16,192.168.40.5/32", "10.50.0.0/16,192.168.40.5/32", nil},
		{"one IPv6 address", "fd02::5/128", "fd02::5/128", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(external, tc.value)
			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load: %v, want the backend to start", err)
			}
			if cfg.TargetScopeExternalCIDRs != tc.kept || !reflect.DeepEqual(cfg.TargetScopeExternalIgnored, tc.ignored) {
				t.Errorf("%s=%q: kept %q, ignored %v; want kept %q, ignored %v", external, tc.value,
					cfg.TargetScopeExternalCIDRs, cfg.TargetScopeExternalIgnored, tc.kept, tc.ignored)
			}
			if cfg.TargetScopeClusterCIDRs != declared {
				t.Errorf("declared %q, want %q as given", cfg.TargetScopeClusterCIDRs, declared)
			}
		})
	}

	// A range over a declared network that also holds the API server's
	// address is named by the declaration; the other comparisons still set
	// aside a single address over the API server's or the pod's own.
	t.Setenv(external, "172.16.0.0/12,172.30.0.1/32,10.128.2.15/32")
	cfg, err = Load()
	if err != nil || cfg.TargetScopeExternalCIDRs != "" || len(cfg.TargetScopeExternalIgnored) != 3 ||
		cfg.TargetScopeExternalIgnored[0].String() != "172.16.0.0/12 "+why("172.30.0.0/16") ||
		!strings.HasPrefix(cfg.TargetScopeExternalIgnored[1].String(), "172.30.0.1/32 holds the API server's address 172.30.0.1") ||
		!strings.HasPrefix(cfg.TargetScopeExternalIgnored[2].String(), "10.128.2.15/32 holds 10.128.2.15, an address of the backend's own pod") {
		t.Errorf("declared and environment: kept %q, ignored %v, %v", cfg.TargetScopeExternalCIDRs, cfg.TargetScopeExternalIgnored, err)
	}

	// With no company range, a declaration sets nothing aside.
	t.Setenv(external, "")
	if cfg, err := Load(); err != nil || cfg.TargetScopeExternalCIDRs != "" || cfg.TargetScopeExternalIgnored != nil {
		t.Errorf("no company range: kept %q, ignored %v, %v; want none", cfg.TargetScopeExternalCIDRs, cfg.TargetScopeExternalIgnored, err)
	}

	// Emptied, the declaration changes nothing: the ranges it set aside are
	// kept, as without it.
	t.Setenv(key, "")
	t.Setenv(external, "10.130.0.0/16,fd02:0:0:5::/64")
	if cfg, err := Load(); err != nil || cfg.TargetScopeExternalCIDRs != "10.130.0.0/16,fd02:0:0:5::/64" || cfg.TargetScopeExternalIgnored != nil {
		t.Errorf("declaration emptied: kept %q, ignored %v, %v; want both kept", cfg.TargetScopeExternalCIDRs, cfg.TargetScopeExternalIgnored, err)
	}
}

// The backend's own addresses are read from its interfaces, IPv4 ones as
// IPv4, so a range is compared with them as the scope check compares it.
func TestOwnAddrs(t *testing.T) {
	addrs, err := ownAddrs()
	if err != nil {
		t.Skipf("this machine's interfaces cannot be read: %v", err)
	}
	for _, addr := range addrs {
		if !addr.IsValid() || addr.Is4In6() {
			t.Errorf("own address %v: want a valid address, IPv4 unmapped", addr)
		}
	}
}
