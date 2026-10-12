// Command server runs the uptime monitoring API.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/yusufceng/Kubeat/backend/internal/alert"
	"github.com/yusufceng/Kubeat/backend/internal/api"
	"github.com/yusufceng/Kubeat/backend/internal/checker"
	"github.com/yusufceng/Kubeat/backend/internal/config"
	"github.com/yusufceng/Kubeat/backend/internal/k8s"
	"github.com/yusufceng/Kubeat/backend/internal/scheduler"
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

	// Background work stops on the same signal as the HTTP server and is
	// waited for before the database is closed.
	var background sync.WaitGroup
	userAgent := "kubeat/" + version

	alerts := alert.New(db, &alert.WebhookSender{Client: &http.Client{}, UserAgent: userAgent}, alert.Options{
		RecoveryThreshold: cfg.RecoveryThreshold,
		CertWarning:       cfg.CertWarning,
		QueueSize:         cfg.NotifyQueueSize,
		Attempts:          cfg.NotifyAttempts,
		RetryDelay:        cfg.NotifyRetryDelay,
		SendTimeout:       cfg.NotifyTimeout,
		Logger:            logger,
	})
	// Notifications raised by the scheduler's final flush must still go out,
	// so delivery stops only after the scheduler has.
	alertCtx, stopAlerts := context.WithCancel(context.Background())
	defer stopAlerts()

	sched := scheduler.New(db, checker.New(checker.Options{
		InsecureSkipVerify: cfg.HTTPInsecureSkipVerify,
		UserAgent:          userAgent,
		KeywordBodyLimit:   int64(cfg.KeywordBodyLimit),
	}), scheduler.Options{
		Tick:          cfg.SchedulerTick,
		MaxJitter:     cfg.MaxJitter,
		Workers:       cfg.Workers,
		BatchSize:     cfg.ResultBatchSize,
		FlushInterval: cfg.ResultFlushInterval,
		OnResults:     alerts.Evaluate,
		Logger:        logger,
	})
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

	var discovery api.Discovery
	discoveryStatus := "disabled by UPTIME_DISCOVERY_ENABLED"
	if cfg.DiscoveryEnabled {
		client, err := k8s.Connect(k8s.Options{Kubeconfig: cfg.Kubeconfig, Context: cfg.KubeContext, Timeout: cfg.KubeTimeout})
		if err != nil {
			discoveryStatus = err.Error()
			logger.Info("cluster discovery unavailable", "reason", err.Error())
		} else {
			discovery, discoveryStatus = client, client.Mode
			logger.Info("cluster discovery enabled", "mode", client.Mode)
		}
	}

	srv := &http.Server{
		Addr: cfg.Addr,
		Handler: api.NewHandler(api.Options{
			Version:          version,
			Store:            db,
			MinCheckInterval: cfg.MinCheckInterval,
			StatsWindow:      cfg.StatsWindow,
			MaxResults:       cfg.MaxResults,
			Notifier:         alerts,
			Discovery:        discovery,
			DiscoveryStatus:  discoveryStatus,
			Logger:           logger,

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
