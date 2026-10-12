// Command server runs the uptime monitoring API.
package main

import (
	"context"
	"errors"
	"fmt"
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
	"github.com/yusufceng/Kubeat/backend/internal/metrics"
	"github.com/yusufceng/Kubeat/backend/internal/prom"
	"github.com/yusufceng/Kubeat/backend/internal/scheduler"
	"github.com/yusufceng/Kubeat/backend/internal/secretbox"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := store.OpenSQLite(ctx, cfg.DBPath, cfg.DBBusyTimeout)
	if err != nil {
		logger.Error("open database failed", "path", cfg.DBPath, "error", err)
		os.Exit(1)
	}
	defer db.Close()
	if err := protectSecrets(ctx, db, cfg.SecretKey, logger); err != nil {
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

	// Background work stops on the same signal as the HTTP server and is
	// waited for before the database is closed.
	var background sync.WaitGroup
	userAgent := "kubeat/" + version

	meter := metrics.New(version)
	meter.WatchMonitors(db, cfg.MetricsTimeout, logger)

	senders := alert.NewRouter(&http.Client{}, userAgent, alert.SMTPConfig{
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
		podMetrics api.PodMetrics
		sync       api.Sync
	)
	discoveryStatus := "disabled by UPTIME_DISCOVERY_ENABLED"
	if cfg.DiscoveryEnabled {
		client, err := k8s.Connect(k8s.Options{Kubeconfig: cfg.Kubeconfig, Context: cfg.KubeContext, Timeout: cfg.KubeTimeout})
		if err != nil {
			discoveryStatus = err.Error()
			logger.Info("cluster discovery unavailable", "reason", err.Error())
		} else {
			cluster, workloads, replicas, discoveryStatus = client, client, client, client.Mode
			logger.Info("cluster discovery enabled", "mode", client.Mode)
			if cfg.DiscoverySyncInterval > 0 {
				syncer := discovery.New(discovery.Options{
					Cluster: client, Store: db, Interval: cfg.DiscoverySyncInterval,
					DefaultInterval: cfg.DiscoveryDefaultInterval, DefaultTimeout: cfg.DiscoveryDefaultTimeout,
					DefaultFailureThreshold: cfg.DefaultFailureThreshold, MinInterval: cfg.MinCheckInterval,
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
	}), scheduler.Options{
		Tick:          cfg.SchedulerTick,
		MaxJitter:     cfg.MaxJitter,
		Workers:       cfg.Workers,
		BatchSize:     cfg.ResultBatchSize,
		FlushInterval: cfg.ResultFlushInterval,
		OnResults:     alerts.Evaluate,
		Metrics:       meter,
		Logger:        logger,
	})
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
		runSessionCleanup(ctx, db, cfg.AuditRetention, logger)
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

	srv := &http.Server{
		Addr: cfg.Addr,
		Handler: api.NewHandler(api.Options{
			Version:          version,
			Store:            db,
			MinCheckInterval: cfg.MinCheckInterval,
			StatsWindow:      cfg.StatsWindow,
			MaxResults:       cfg.MaxResults,

			StatusPageDays:           cfg.StatusPageDays,
			StatusPageIncidentWindow: cfg.StatusPageIncidentWindow,
			Token:                    cfg.APIToken,
			SessionTTL:               cfg.SessionTTL,
			Throttle:                 &auth.Throttle{MaxFailures: cfg.LoginMaxFailures, Lockout: cfg.LoginLockout},
			PasswordMinLength:        cfg.PasswordMinLength,
			LDAP:                     ldap,
			LDAPDefaultRole:          store.Role(cfg.LDAPDefaultRole),
			LDAPDefaultNamespaces:    splitList(cfg.LDAPDefaultNamespaces),
			Notifier:                 alerts,
			Metrics:                  meter.Handler(),
			EmailEnabled:             senders.EmailEnabled(),
			Discovery:                cluster,
			Sync:                     sync,
			Workloads:                workloads,
			PodMetrics:               podMetrics,
			DiscoveryStatus:          discoveryStatus,
			Logger:                   logger,

			DefaultFailureThreshold: cfg.DefaultFailureThreshold,
			RawRetention:            cfg.Retention,
			HistoryRetention:        cfg.RollupRetention,
			SeriesPoints:            cfg.SeriesPoints,
		}),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
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
		logger.Warn("the API is not protected: UPTIME_API_TOKEN is not set, so anything that can reach this address can change monitors and channels")
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
// a key secrets stay as they are, unless the database already holds
// encrypted ones: starting then would only fail later, one monitor at a time.
func protectSecrets(ctx context.Context, db *store.SQLite, key string, logger *slog.Logger) error {
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
		logger.Warn("UPTIME_SECRET_KEY is not set: tokens and passwords are stored unencrypted in the database")
		return nil
	}
	// Reading everything once tells a wrong key now, with a clear message.
	if _, err := db.ListMonitors(ctx); err != nil {
		return err
	}
	if _, err := db.ListChannels(ctx); err != nil {
		return err
	}
	if counts.Plain > 0 {
		if err := db.EncryptSecrets(ctx); err != nil {
			return err
		}
		logger.Info("secrets stored before the key was configured are now encrypted", "count", counts.Plain)
	}
	return nil
}

// bootstrapAdmin creates the administrator "admin" from UPTIME_ADMIN_PASSWORD
// when there is no account yet, which is how a Helm install gets its first
// sign-in. With UPTIME_ADMIN_PASSWORD_RESET it also resets that account's
// password, for an operator who is locked out.
func bootstrapAdmin(ctx context.Context, db *store.SQLite, cfg config.Config, logger *slog.Logger) error {
	if cfg.AdminPassword == "" {
		return nil
	}
	n, err := db.CountUsers(ctx)
	if err != nil {
		return err
	}
	hash, err := auth.HashPassword(cfg.AdminPassword)
	if err != nil {
		return err
	}
	if n == 0 {
		if _, err := db.CreateUser(ctx, store.User{Username: "admin", DisplayName: "Yönetici", Source: store.UserLocal, PasswordHash: hash, Role: store.RoleAdmin, Enabled: true}); err != nil {
			return err
		}
		logger.Info("administrator account created from UPTIME_ADMIN_PASSWORD", "user", "admin")
		return nil
	}
	if !cfg.AdminPasswordReset {
		return nil
	}
	admin, err := db.GetUserByUsername(ctx, "admin")
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
