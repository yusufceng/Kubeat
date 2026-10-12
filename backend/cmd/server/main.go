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

	"github.com/yusufceng/Kubeat/backend/internal/api"
	"github.com/yusufceng/Kubeat/backend/internal/checker"
	"github.com/yusufceng/Kubeat/backend/internal/config"
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
	sched := scheduler.New(db, checker.New(checker.Options{
		InsecureSkipVerify: cfg.HTTPInsecureSkipVerify,
		UserAgent:          "kubeat/" + version,
	}), scheduler.Options{
		Tick:          cfg.SchedulerTick,
		MaxJitter:     cfg.MaxJitter,
		Workers:       cfg.Workers,
		BatchSize:     cfg.ResultBatchSize,
		FlushInterval: cfg.ResultFlushInterval,
		Logger:        logger,
	})
	background.Add(2)
	go func() {
		defer background.Done()
		sched.Run(ctx)
	}()
	go func() {
		defer background.Done()
		scheduler.RunRetention(ctx, db, cfg.Retention, cfg.RetentionInterval, logger)
	}()

	srv := &http.Server{
		Addr: cfg.Addr,
		Handler: api.NewHandler(api.Options{
			Version:          version,
			Store:            db,
			MinCheckInterval: cfg.MinCheckInterval,
			StatsWindow:      cfg.StatsWindow,
			MaxResults:       cfg.MaxResults,
			Logger:           logger,
		}),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
	}

	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
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
