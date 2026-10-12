// Command server runs the uptime monitoring API.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/piaycoskun/uptime/backend/internal/api"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	addr := os.Getenv("UPTIME_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           api.NewHandler(version),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("shutdown failed", "error", err)
		}
	}()

	logger.Info("server listening", "addr", addr, "version", version)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
	// ListenAndServe returns as soon as Shutdown starts; wait for it to drain.
	<-shutdownDone
}
