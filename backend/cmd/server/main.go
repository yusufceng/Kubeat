// Command server runs the uptime monitoring API.
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
	// Time zones for repeating maintenance windows, for images without them.
	_ "time/tzdata"

	"github.com/yusufceng/Kubeat/backend/internal/alert"
	"github.com/yusufceng/Kubeat/backend/internal/api"
	"github.com/yusufceng/Kubeat/backend/internal/auth"
	"github.com/yusufceng/Kubeat/backend/internal/backup"
	"github.com/yusufceng/Kubeat/backend/internal/checker"
	"github.com/yusufceng/Kubeat/backend/internal/config"
	"github.com/yusufceng/Kubeat/backend/internal/discovery"
	"github.com/yusufceng/Kubeat/backend/internal/heartbeat"
	"github.com/yusufceng/Kubeat/backend/internal/k8s"
	"github.com/yusufceng/Kubeat/backend/internal/ldapauth"
	"github.com/yusufceng/Kubeat/backend/internal/license"
	"github.com/yusufceng/Kubeat/backend/internal/metrics"
	"github.com/yusufceng/Kubeat/backend/internal/netguard"
	"github.com/yusufceng/Kubeat/backend/internal/oidcauth"
	"github.com/yusufceng/Kubeat/backend/internal/prom"
	"github.com/yusufceng/Kubeat/backend/internal/scheduler"
	"github.com/yusufceng/Kubeat/backend/internal/secretbox"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	privateFiles()

	cfg, err := config.Load()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	for _, warning := range cfg.Warnings {
		logger.Warn(warning)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// A database created before files were made private would keep its
	// mode, and pass it on: its journal files are created with the mode
	// of the database file.
	if err := os.Chmod(cfg.DBPath, 0o600); err != nil && !errors.Is(err, fs.ErrNotExist) {
		logger.Warn("the database file could not be made private", "path", cfg.DBPath, "error", err)
	}
	db, err := store.OpenSQLite(ctx, cfg.DBPath, cfg.DBBusyTimeout)
	if err != nil {
		logger.Error("open database failed", "path", cfg.DBPath, "error", err)
		os.Exit(1)
	}
	defer db.Close()
	if err := protectSecrets(ctx, db, cfg.SecretKey, cfg.AllowPlaintextSecrets, logger); err != nil {
		logger.Error("the secrets in the database cannot be used", "error", err)
		db.Close()
		os.Exit(1)
	}
	if err := bootstrapAdmin(ctx, db, cfg, logger); err != nil {
		logger.Error("the administrator account could not be set up", "error", err)
		db.Close()
		os.Exit(1)
	}
	ldapClient, err := ldapauth.New(ldapauth.Config{
		URL: cfg.LDAPURL, StartTLS: cfg.LDAPStartTLS, CAFile: cfg.LDAPCAFile, InsecureSkipVerify: cfg.LDAPInsecureSkipVerify,
		BindDN: cfg.LDAPBindDN, BindPassword: cfg.LDAPBindPassword, BaseDN: cfg.LDAPBaseDN, UserFilter: cfg.LDAPUserFilter,
		DisplayNameAttr: cfg.LDAPDisplayNameAttr, EmailAttr: cfg.LDAPEmailAttr, GroupAttr: cfg.LDAPGroupAttr,
		GroupBaseDN: cfg.LDAPGroupBaseDN, GroupFilter: cfg.LDAPGroupFilter, Timeout: cfg.LDAPTimeout,
	})
	if err != nil {
		logger.Error("invalid LDAP configuration", "error", err)
		db.Close()
		os.Exit(1)
	}
	// An interface holding a nil *Client would not be nil; keep it nil.
	var ldap api.LDAP
	if ldapClient != nil {
		ldap = ldapClient
		logger.Info("directory sign-in enabled", "url", cfg.LDAPURL)
	}
	oidc, oidcProblem := setUpOIDC(cfg, logger)

	// Background work stops on the same signal as the HTTP server and is
	// waited for before the database is closed.
	var background sync.WaitGroup
	// What checks and notifications call themselves: the software, where
	// it comes from and, when set, where this installation is reached, so
	// that the operator of a checked site can tell who is calling.
	userAgent := cfg.UserAgentFor(version)

	meter := metrics.New(version)
	lic := license.NewManager(cfg.LicenseFile, license.PublicKey)
	reportLicense(lic.Status(), meter, logger)
	if (cfg.LDAPURL != "" || cfg.OIDCIssuer != "") && !lic.Allows(license.SSO) {
		logger.Warn("LDAP or OIDC is configured, but signing in through it needs a Kubeat Enterprise license with the sso feature; only local accounts can sign in")
	}
	meter.WatchMonitors(db, cfg.MetricsTimeout, logger)

	// Checks and notifications connect only where the target policy allows:
	// a monitor's target and a channel's address are set by whoever may
	// edit them, and must not lead to the backend's own loopback or to the
	// cloud's metadata service. Load has checked both lists already.
	guard, err := netguard.New(splitList(cfg.TargetDenyCIDRs), splitList(cfg.TargetAllowCIDRs))
	if err != nil {
		logger.Error("invalid target policy", "error", err)
		db.Close()
		os.Exit(1)
	}
	// The proxies of HTTP_PROXY and HTTPS_PROXY are the operator's own, and
	// are reached at their exact address, wherever that is: one on the
	// pod's loopback needs no exception. The mail server, Telegram and
	// PagerDuty are trusted the same way by their own senders alone.
	guard = guard.Trust(netguard.EnvironmentProxies()...)
	logger.Info("target policy in effect", "target_policy", guard)

	senders := alert.NewRouter(guard, userAgent, alert.SMTPConfig{
		Host:     cfg.SMTPHost,
		Port:     cfg.SMTPPort,
		Username: cfg.SMTPUsername,
		Password: cfg.SMTPPassword,
		From:     cfg.SMTPFrom,
		Security: cfg.SMTPSecurity,
	}, alert.Endpoints{Telegram: cfg.TelegramAPIURL, PagerDuty: cfg.PagerDutyEventsURL})
	alerts := alert.New(db, senders, alert.Options{
		RecoveryThreshold: cfg.RecoveryThreshold,
		CertWarning:       cfg.CertWarning,
		ReminderInterval:  cfg.ReminderInterval,
		QueueSize:         cfg.NotifyQueueSize,
		Attempts:          cfg.NotifyAttempts,
		RetryDelay:        cfg.NotifyRetryDelay,
		SendTimeout:       cfg.NotifyTimeout,
		GroupWait:         cfg.NotifyGroupWait,
		Language:          cfg.Language,
		PublicURL:         cfg.PublicURL,
		Metrics:           meter,
		Logger:            logger,
	})
	// Notifications raised by the scheduler's final flush must still go out,
	// so delivery stops only after the scheduler has.
	alertCtx, stopAlerts := context.WithCancel(context.Background())
	defer stopAlerts()

	// The cluster, for discovery, the pods behind a monitor and workload
	// checks. These stay nil interfaces when unavailable, which their users
	// check for.
	var (
		cluster    api.Discovery
		workloads  api.Workloads
		replicas   checker.Workloads
		endpoints  checker.Endpoints
		podMetrics api.PodMetrics
		sync       api.Sync
	)
	discoveryStatus := "disabled by UPTIME_DISCOVERY_ENABLED"
	if cfg.DiscoveryEnabled {
		client, err := k8s.Connect(k8s.Options{
			Kubeconfig: cfg.Kubeconfig, Context: cfg.KubeContext, Timeout: cfg.KubeTimeout,
			Namespaces: splitList(cfg.DiscoveryNamespaces), EventCache: cfg.EventCache,
		})
		if err != nil {
			discoveryStatus = err.Error()
			logger.Info("cluster discovery unavailable", "reason", err.Error())
		} else {
			cluster, workloads, discoveryStatus = client, client, client.Mode
			replicas, endpoints = workloadReplicas{client}, podEndpoints{client}
			logger.Info("cluster discovery enabled", "mode", client.Mode)
			if cfg.DiscoverySyncInterval > 0 {
				syncer := discovery.New(discovery.Options{
					Cluster: client, Store: db, Interval: cfg.DiscoverySyncInterval,
					DefaultInterval: cfg.DiscoveryDefaultInterval, DefaultTimeout: cfg.DiscoveryDefaultTimeout,
					DefaultFailureThreshold: cfg.DefaultFailureThreshold, MinInterval: cfg.MinCheckInterval,
					MinIntervalExternal: cfg.MinCheckIntervalExternal, MaxMonitors: cfg.MaxMonitors,
					MaxMonitorsPerNamespace: cfg.MaxMonitorsPerNamespace, MaxMonitorsPerHost: cfg.MaxMonitorsPerHost,
					Logger: logger,
				})
				sync = syncer
				background.Add(1)
				go func() {
					defer background.Done()
					syncer.Run(ctx)
				}()
				logger.Info("annotation discovery enabled", "interval", cfg.DiscoverySyncInterval.String())
			}
		}
	}
	sched := scheduler.New(db, checker.New(checker.Options{
		InsecureSkipVerify: cfg.HTTPInsecureSkipVerify,
		UserAgent:          userAgent,
		BodyLimit:          int64(cfg.KeywordBodyLimit),
		Pushes:             db,
		Workloads:          replicas,
		Endpoints:          endpoints,
		RolloutGrace:       cfg.WorkloadRolloutGrace,
		Guard:              guard,
		Logger:             logger,
	}), scheduler.Options{
		Tick:                 cfg.SchedulerTick,
		MaxJitter:            cfg.MaxJitter,
		Workers:              cfg.Workers,
		BatchSize:            cfg.ResultBatchSize,
		FlushInterval:        cfg.ResultFlushInterval,
		OnResults:            alerts.Evaluate,
		TargetMaxRate:        cfg.TargetMaxRate,
		ClusterTargetMaxRate: cfg.ClusterTargetMaxRate,
		AddressMaxRate:       cfg.TargetAddressMaxRate,
		Metrics:              meter,
		Logger:               logger,
	})
	logger.Info("target guardrails in effect", "user_agent", userAgent,
		"min_check_interval_external", cfg.MinCheckIntervalExternal.String(),
		"target_max_rate", cfg.TargetMaxRate, "cluster_target_max_rate", cfg.ClusterTargetMaxRate,
		"target_address_max_rate", cfg.TargetAddressMaxRate,
		"max_monitors_per_host", cfg.MaxMonitorsPerHost, "max_monitors", cfg.MaxMonitors,
		"max_monitors_per_namespace", cfg.MaxMonitorsPerNamespace, "monitor_create_per_minute", cfg.MonitorCreatePerMinute)
	if cfg.BackupDir != "" {
		background.Add(1)
		go func() {
			defer background.Done()
			backup.Run(ctx, db, backup.Options{
				Dir:      cfg.BackupDir,
				Interval: cfg.BackupInterval,
				Keep:     cfg.BackupKeep,
				// A backup is only kept if it opens as a database of the
				// version this binary writes.
				Verify: func(ctx context.Context, path string) error {
					v, err := store.SchemaVersion(ctx, path)
					if err == nil && v != store.CurrentSchemaVersion() {
						err = fmt.Errorf("schema version %d, want %d", v, store.CurrentSchemaVersion())
					}
					return err
				},
				Metrics: meter,
				Logger:  logger,
			})
		}()
		logger.Info("backups enabled", "dir", cfg.BackupDir, "interval", cfg.BackupInterval.String(), "keep", cfg.BackupKeep)
	}
	if cfg.HeartbeatURL != "" {
		background.Add(1)
		go func() {
			defer background.Done()
			heartbeat.Run(ctx, heartbeat.Options{
				URL:      cfg.HeartbeatURL,
				Interval: cfg.HeartbeatInterval,
				Timeout:  cfg.HeartbeatTimeout,
				// Alive means the scheduler read its monitors and queued the
				// due ones at least once since the previous heartbeat.
				Healthy: func() error {
					if age := time.Since(sched.LastTick()); age > cfg.HeartbeatInterval {
						return fmt.Errorf("the scheduler has not completed a tick for %s", age.Round(time.Second))
					}
					return nil
				},
				Client:  &http.Client{},
				Metrics: meter,
				Logger:  logger,
			})
		}()
		logger.Info("heartbeat enabled", "interval", cfg.HeartbeatInterval.String())
	}
	background.Add(4)
	go func() {
		defer background.Done()
		scheduler.RunRollups(ctx, db, scheduler.RollupOptions{
			RawRetention: cfg.Retention,
			Interval:     cfg.RollupInterval,
			Retention:    cfg.RollupRetention,
			Logger:       logger,
		})
	}()
	go func() {
		defer background.Done()
		sched.Run(ctx)
		stopAlerts()
	}()
	go func() {
		defer background.Done()
		alerts.Run(alertCtx)
	}()
	go func() {
		defer background.Done()
		scheduler.RunRetention(ctx, db, cfg.Retention, cfg.RetentionInterval, logger)
	}()
	background.Add(1)
	go func() {
		defer background.Done()
		scheduler.RunDeliveryRetention(ctx, db, cfg.DeliveryRetention, cfg.RetentionInterval, logger)
	}()
	background.Add(1)
	go func() {
		defer background.Done()
		runSessionCleanup(ctx, db, cfg.AuditRetention, logger)
	}()
	background.Add(1)
	go func() {
		defer background.Done()
		watchLicense(ctx, lic, cfg.LicenseReloadInterval, meter, logger)
	}()
	throttle := &auth.Throttle{MaxFailures: cfg.LoginMaxFailures, Lockout: cfg.LoginLockout, MaxKeys: cfg.LoginThrottleMaxKeys, Logger: logger}
	background.Add(1)
	go func() {
		defer background.Done()
		sweepThrottle(ctx, throttle)
	}()

	if cfg.PrometheusURL != "" {
		client, err := prom.New(prom.Options{
			URL:                cfg.PrometheusURL,
			TokenFile:          cfg.PrometheusTokenFile,
			CAFile:             cfg.PrometheusCAFile,
			InsecureSkipVerify: cfg.PrometheusInsecureSkipVerify,
			Timeout:            cfg.PrometheusTimeout,
			RateWindow:         cfg.PrometheusRateWindow,
			CPUQuery:           cfg.PrometheusCPUQuery,
			MemoryQuery:        cfg.PrometheusMemoryQuery,
		})
		if err != nil {
			logger.Error("invalid metrics server configuration", "error", err)
			os.Exit(1)
		}
		podMetrics = client
		logger.Info("pod metrics enabled", "url", cfg.PrometheusURL)
	}

	// The dummy hash a sign-in of an unknown name is compared with takes as
	// long as a real comparison to make; better now than in a sign-in.
	auth.PrepareDummy()
	srv := &http.Server{
		Addr: cfg.Addr,
		Handler: api.NewHandler(api.Options{
			Version:          version,
			Store:            db,
			MinCheckInterval: cfg.MinCheckInterval,
			StatsWindow:      cfg.StatsWindow,
			MaxResults:       cfg.MaxResults,

			MinCheckIntervalExternal: cfg.MinCheckIntervalExternal,
			MaxMonitorsPerHost:       cfg.MaxMonitorsPerHost,
			MaxMonitors:              cfg.MaxMonitors,
			MaxMonitorsPerNamespace:  cfg.MaxMonitorsPerNamespace,
			MonitorCreatePerMinute:   cfg.MonitorCreatePerMinute,
			Throttles:                sched,

			StatusPageDays:           cfg.StatusPageDays,
			StatusPageIncidentWindow: cfg.StatusPageIncidentWindow,
			StatusPageUpdates:        cfg.StatusPageUpdates,
			StatusPageUpcoming:       cfg.StatusPageUpcoming,
			StatusPageSeries:         cfg.StatusPageSeries,
			StatusPageSession:        cfg.StatusPageSession,
			StatusPageTokenKey:       statusPageTokenKey(cfg.SecretKey, logger),
			Token:                    cfg.APIToken,
			SessionTTL:               cfg.SessionTTL,
			Throttle:                 throttle,
			PasswordMinLength:        cfg.PasswordMinLength,
			LoginConcurrency:         cfg.LoginConcurrency,
			LoginQueueWait:           cfg.LoginQueueWait,
			LoginAuditPerMinute:      cfg.LoginAuditPerMinute,
			LDAP:                     ldap,
			LDAPDefaultRole:          store.Role(cfg.LDAPDefaultRole),
			LDAPDefaultNamespaces:    splitList(cfg.LDAPDefaultNamespaces),
			OIDC:                     oidc,
			OIDCProblem:              oidcProblem,
			OIDCName:                 cfg.OIDCName,
			OIDCDefaultRole:          store.Role(cfg.OIDCDefaultRole),
			OIDCDefaultNamespaces:    splitList(cfg.OIDCDefaultNamespaces),
			Notifier:                 alerts,
			Metrics:                  meter.Handler(),
			MetricsToken:             cfg.MetricsToken,
			EmailEnabled:             senders.EmailEnabled(),
			Discovery:                cluster,
			Sync:                     sync,
			Language:                 cfg.Language,
			Workloads:                workloads,
			PodMetrics:               podMetrics,
			EventWindow:              cfg.EventWindow,
			EventLimit:               cfg.EventLimit,
			TimelineLimit:            cfg.IncidentTimelineLimit,
			DiscoveryStatus:          discoveryStatus,
			License:                  lic,
			Logger:                   logger,

			DefaultFailureThreshold: cfg.DefaultFailureThreshold,
			RawRetention:            cfg.Retention,
			HistoryRetention:        cfg.RollupRetention,
			SeriesPoints:            cfg.SeriesPoints,
		}),
		// Reading a request and writing its answer are bounded, and so is
		// an idle connection's life: a client that sends a body slowly, or
		// keeps connections open without using them, cannot hold them for
		// good. Without a ReadTimeout only the headers had a deadline.
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.HTTPReadTimeout,
		WriteTimeout:      cfg.HTTPWriteTimeout,
		IdleTimeout:       cfg.HTTPIdleTimeout,
		MaxHeaderBytes:    cfg.HTTPMaxHeaderBytes,
	}

	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("shutdown failed", "error", err)
		}
	}()

	if cfg.APIToken == "" {
		logger.Info("UPTIME_API_TOKEN is not set: the API accepts signed-in users only; set a token for scripts and automation")
	}
	if cfg.MetricsToken == "" {
		logger.Info("UPTIME_METRICS_TOKEN is not set: /metrics answers anyone who can reach the port")
	}
	logger.Info("server listening", "addr", cfg.Addr, "version", version, "db", cfg.DBPath)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server failed", "error", err)
		stop()
		background.Wait()
		db.Close()
		os.Exit(1)
	}
	// ListenAndServe returns as soon as Shutdown starts; wait for it to drain.
	<-shutdownDone
	background.Wait()
}

// protectSecrets sets the store up to encrypt secrets with key and brings a
// database written before the key was configured in line with it. Without
// a key the server does not start: the secrets would sit readable in the
// database file and every backup of it. allowPlaintext lets it start all
// the same, but not when the database already holds encrypted secrets:
// starting then would only fail later, one monitor at a time.
func protectSecrets(ctx context.Context, db *store.SQLite, key string, allowPlaintext bool, logger *slog.Logger) error {
	var box *secretbox.Box
	if key != "" {
		var err error
		if box, err = secretbox.New(key); err != nil {
			return fmt.Errorf("UPTIME_SECRET_KEY: %w", err)
		}
	}
	db.UseSecrets(box)

	counts, err := db.CountSecrets(ctx)
	if err != nil {
		return err
	}
	if box == nil {
		if counts.Sealed > 0 {
			return errors.New("the database holds encrypted secrets; set UPTIME_SECRET_KEY to the key they were encrypted with")
		}
		if !allowPlaintext {
			return errors.New("UPTIME_SECRET_KEY is not set: set it, or UPTIME_SECRET_KEY_FILE, to a random key of 32 characters or more " +
				"(openssl rand -base64 36) and keep a copy with the backups; to store tokens and passwords unencrypted instead, set UPTIME_ALLOW_PLAINTEXT_SECRETS=true")
		}
		logger.Warn("UPTIME_SECRET_KEY is not set: tokens and passwords are stored unencrypted in the database")
		return nil
	}
	// Reading everything once tells a wrong key now, with a clear message.
	if _, err := db.ListMonitors(ctx); err != nil {
		return wrongKeyHint(err)
	}
	if _, err := db.ListChannels(ctx); err != nil {
		return wrongKeyHint(err)
	}
	owed, earlier, err := db.CompactionPending(ctx)
	if err != nil {
		return err
	}
	pending := earlier
	if counts.Plain > 0 {
		if counts.Sealed == 0 && !pending {
			// The plain values the database was written with stay in the
			// free space of its file and in the write-ahead log until the
			// file is rewritten. That is owed from now on, to this start
			// or, if it is cut short, to the next ones.
			if err := db.SetCompactionPending(ctx, time.Now()); err != nil {
				return err
			}
			pending = true
		}
		if err := db.EncryptSecrets(ctx); err != nil {
			return err
		}
		logger.Info("secrets stored before the key was configured are now encrypted", "count", counts.Plain)
	}
	if !pending {
		// Nothing plain, or a database that had the key already, with a
		// value only now counted among the secrets (request bodies):
		// rewriting the rows zeroed the plain values in place, and nothing
		// else was stored without the key. Older copies of those values
		// may linger until overwritten, which is not worth compacting for
		// at every upgrade.
		return nil
	}
	compact(ctx, db, owed, earlier, logger)
	return nil
}

// maxCompactionAttempts is how many starts in a row may try the compaction
// owed after encrypting a database's secrets and not finish it. One that
// takes longer than a start is given, by the chart's startupProbe or by a
// liveness probe without one, is cut short every time; past this bound
// the next start skips it, so that the backend comes up, and logs what to
// do. The start after that tries once more (a raised startup budget gets
// its chance), and if that one is cut short too, the next skips again: a
// restart costs at most one start cut short.
const maxCompactionAttempts = 3

// compact does the compaction the database is owed after its secrets were
// encrypted (see protectSecrets), or skips it. Not finishing it only
// postpones it: the start goes on whatever happens. Each attempt is
// counted before it begins, so that one cut short counts too; earlier says
// whether an earlier start owed it already.
func compact(ctx context.Context, db *store.SQLite, owed store.PendingCompaction, earlier bool, logger *slog.Logger) {
	if owed.Attempts >= maxCompactionAttempts {
		logger.Error("the database is still owed a compaction after encrypting its secrets, and the last starts tried it and did not finish: "+
			"cut short (by the startup or liveness probe, most likely) or failed (see their warnings). This start skips it, so that "+
			"the backend comes up; unencrypted copies of the secrets may remain in the free space of the database file. Give a start "+
			"the time it needs (in the chart, raise backend.startupProbe.failureThreshold) and free disk about the size of the database, "+
			"or compact it offline with VACUUM while the backend is stopped (see the README); the next start tries once more",
			"pending_since", owed.Since, "attempts", owed.Attempts)
		if err := db.SetCompactionAttempts(ctx, maxCompactionAttempts-1); err != nil {
			logger.Warn("the attempts at the compaction could not be counted afresh: the next start skips it as well", "error", err)
		}
		return
	}
	if earlier {
		logger.Warn("an earlier start did not finish compacting the database after encrypting its secrets: "+
			"unencrypted copies may remain in the free space of its file; compacting now, which takes a while for a large one",
			"pending_since", owed.Since, "attempt", owed.Attempts+1, "max_attempts", maxCompactionAttempts)
	} else {
		logger.Info("compacting the database, so that no unencrypted copy of the secrets is left in it; this takes a while for a large one")
	}
	if err := db.SetCompactionAttempts(ctx, owed.Attempts+1); err != nil {
		// Uncounted, an attempt cut short could be made by every start.
		logger.Warn("the database is not compacted: the attempt could not be counted; the next start tries again", "error", err)
		return
	}
	if err := db.Vacuum(ctx); err != nil {
		logger.Warn("the database could not be compacted: unencrypted copies of the secrets may remain in the free space of its file; "+
			"the next start tries again", "error", err)
	}
}

// setUpOIDC makes the client for sign-in through an OpenID Connect
// provider, or says why there is none although an issuer is set. Settings
// with which SSO cannot work (see config.Config.OIDCProblem, or a CA file
// that cannot be read) turn SSO off: its button is not offered, the reason
// is logged and shown to administrators, and everything else runs. A
// sign-in setting must not take the monitoring down.
func setUpOIDC(cfg config.Config, logger *slog.Logger) (oidc api.OIDC, problem string) {
	if cfg.OIDCIssuer == "" {
		return nil, ""
	}
	err := cfg.OIDCProblem
	var client *oidcauth.Client
	if err == nil {
		client, err = oidcauth.New(oidcauth.Config{
			Issuer: cfg.OIDCIssuer, ClientID: cfg.OIDCClientID, ClientSecret: cfg.OIDCClientSecret, RedirectURL: cfg.OIDCRedirectURL,
			Scopes: strings.Fields(cfg.OIDCScopes), UsernameClaim: cfg.OIDCUsernameClaim, DisplayNameClaim: cfg.OIDCDisplayNameClaim,
			EmailClaim: cfg.OIDCEmailClaim, GroupsClaim: cfg.OIDCGroupsClaim, CAFile: cfg.OIDCCAFile,
			InsecureSkipVerify: cfg.OIDCInsecureSkipVerify, Timeout: cfg.OIDCTimeout,
			// Starting a sign-in needs no credentials, like trying a
			// password: the same bound keeps a flood of either from growing
			// the memory without end.
			MaxPending: cfg.LoginThrottleMaxKeys, Logger: logger,
		})
	}
	if err != nil {
		logger.Error("SSO is off: its settings cannot be used; fix them and restart, monitoring and the other sign-ins are not affected",
			"issuer", cfg.OIDCIssuer, "error", err)
		return nil, err.Error()
	}
	logger.Info("OIDC sign-in enabled", "issuer", cfg.OIDCIssuer, "name", cfg.OIDCName, "redirect_url", cfg.OIDCRedirectURL)
	if cfg.OIDCRedirectURL == "" {
		// The frontend's own idea of its address is trusted only on
		// localhost; behind a Route it is the pod's 0.0.0.0:3000.
		logger.Warn("OIDC sign-in works only for a frontend on localhost: set UPTIME_PUBLIC_URL to the address users reach Kubeat at, or UPTIME_OIDC_REDIRECT_URL to its /auth/callback")
	}
	return client, ""
}

// wrongKeyHint adds to a refusal of the key what most often explains one
// that looks right: every byte of it counts, a trailing newline too.
func wrongKeyHint(err error) error {
	if !errors.Is(err, secretbox.ErrWrongKey) {
		return err
	}
	return fmt.Errorf("%w; every byte of the key counts, a trailing newline too: a key file written with one "+
		"gives another key than the same text without it", err)
}

// adminUsername is the account UPTIME_ADMIN_PASSWORD creates and resets.
const adminUsername = "admin"

// bootstrapAdmin creates the administrator "admin" from UPTIME_ADMIN_PASSWORD
// when there is no account yet, which is how a Helm install gets its first
// sign-in. With UPTIME_ADMIN_PASSWORD_RESET it also resets that account's
// password, for an operator who is locked out.
//
// The password is held to the rules of every other password, but only when
// it is used. One that breaks them stops the start: going on without the
// account would leave the first-run page open to whoever comes first.
func bootstrapAdmin(ctx context.Context, db *store.SQLite, cfg config.Config, logger *slog.Logger) error {
	if cfg.AdminPassword == "" {
		return nil
	}
	n, err := db.CountUsers(ctx)
	if err != nil {
		return err
	}
	if n > 0 && !cfg.AdminPasswordReset {
		return nil
	}
	if err := auth.ValidatePassword(adminUsername, cfg.AdminPassword, cfg.PasswordMinLength); err != nil {
		return fmt.Errorf("UPTIME_ADMIN_PASSWORD: %w", err)
	}
	hash, err := auth.HashPassword(cfg.AdminPassword)
	if err != nil {
		return err
	}
	if n == 0 {
		displayName := "Administrator"
		if cfg.Language == "tr" {
			displayName = "Yönetici"
		}
		if _, err := db.CreateFirstUser(ctx, store.User{Username: adminUsername, DisplayName: displayName, Source: store.UserLocal, PasswordHash: hash, Role: store.RoleAdmin, Enabled: true}); err != nil {
			return err
		}
		logger.Info("administrator account created from UPTIME_ADMIN_PASSWORD", "user", adminUsername)
		return nil
	}
	admin, err := db.GetUserByUsername(ctx, adminUsername)
	if err != nil {
		return fmt.Errorf("reset the admin password: %w", err)
	}
	if err := db.SetPassword(ctx, admin.ID, hash); err != nil {
		return err
	}
	if err := db.DeleteUserSessions(ctx, admin.ID); err != nil {
		return err
	}
	logger.Warn("the admin password was reset from UPTIME_ADMIN_PASSWORD; unset UPTIME_ADMIN_PASSWORD_RESET again")
	return nil
}

// workloadReplicas hands the cluster's workload state to the checker in its
// own terms.
type workloadReplicas struct{ client *k8s.Client }

func (w workloadReplicas) Replicas(ctx context.Context, target string) (checker.WorkloadStatus, error) {
	st, err := w.client.Replicas(ctx, target)
	return checker.WorkloadStatus{Desired: st.Desired, Ready: st.Ready, Rolling: st.Rolling, Stalled: st.Stalled}, err
}

// podEndpoints hands the cluster's endpoints to the checker in its own terms.
type podEndpoints struct{ client *k8s.Client }

func (p podEndpoints) PodEndpoints(ctx context.Context, namespace, source string) ([]checker.Endpoint, error) {
	found, err := p.client.PodEndpoints(ctx, namespace, source)
	if err != nil {
		return nil, err
	}
	out := make([]checker.Endpoint, 0, len(found))
	for _, e := range found {
		out = append(out, checker.Endpoint{Pod: e.Pod, Address: e.Address})
	}
	return out, nil
}

// reportLicense logs where the license stands and sets its metric.
func reportLicense(s license.Status, meter *metrics.Metrics, logger *slog.Logger) {
	meter.License(time.Time{})
	switch s.State {
	case license.Community:
		logger.Info("community edition: no license configured (UPTIME_LICENSE_FILE)")
		return
	case license.Invalid:
		logger.Error("the license cannot be used; Kubeat Enterprise features are off, monitoring goes on", "problem", s.Problem)
		return
	}
	meter.License(s.ExpiresAt)
	attrs := []any{"customer", s.Customer, "id", s.ID, "features", s.Features, "expires", s.ExpiresAt.Format(time.DateOnly)}
	switch s.State {
	case license.Active:
		logger.Info("license in effect", attrs...)
	case license.Expiring:
		logger.Warn("the license ends soon; renew it to keep its features", attrs...)
	case license.Expired:
		logger.Warn("the license has ended; Kubeat Enterprise features are off, monitoring goes on", attrs...)
	}
}

// watchLicense reads the license file again when it changes, and reports
// when the license changes or moves on to ending or ended.
func watchLicense(ctx context.Context, lic *license.Manager, interval time.Duration, meter *metrics.Metrics, logger *slog.Logger) {
	last := lic.Status().State
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		changed := lic.Reload()
		if s := lic.Status(); changed || s.State != last {
			reportLicense(s, meter, logger)
			last = s.State
		}
	}
}

// runSessionCleanup drops expired sessions and old audit entries, hourly.
func runSessionCleanup(ctx context.Context, db *store.SQLite, auditRetention time.Duration, logger *slog.Logger) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		now := time.Now().UTC()
		if _, err := db.DeleteExpiredSessions(ctx, now); err != nil && ctx.Err() == nil {
			logger.Warn("expired sessions not cleaned up", "error", err)
		}
		if _, err := db.DeleteAuditBefore(ctx, now.Add(-auditRetention)); err != nil && ctx.Err() == nil {
			logger.Warn("old audit entries not cleaned up", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// statusPageTokenKey is the key that signs the tokens of visitors who gave
// a protected status page's password: derived from the instance's secret
// key, so that the tokens outlive a restart, and never the key itself.
// Without a secret key (UPTIME_ALLOW_PLAINTEXT_SECRETS) a key is drawn for
// this run, and a restart asks the visitors for the password again.
func statusPageTokenKey(secretKey string, logger *slog.Logger) []byte {
	if secretKey != "" {
		sum := sha256.Sum256([]byte("kubeat status page tokens\x00" + secretKey))
		return sum[:]
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		logger.Error("no key for the status page tokens could be drawn", "error", err)
		os.Exit(1)
	}
	logger.Info("UPTIME_SECRET_KEY is not set: the tokens of password-protected status pages are signed with a key of this run, and a restart asks their visitors for the password again")
	return key
}

// sweepThrottle forgets, every lockout period, the sign-in names whose
// attempts have all aged out; the sweep also logs what the throttle has
// not reported yet.
func sweepThrottle(ctx context.Context, throttle *auth.Throttle) {
	ticker := time.NewTicker(throttle.Lockout)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			throttle.Sweep(now.UTC())
		}
	}
}

// splitList parses a comma-separated list, dropping empty entries.
func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
