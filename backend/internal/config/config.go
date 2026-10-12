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

	HTTPInsecureSkipVerify bool // UPTIME_HTTP_INSECURE_SKIP_VERIFY

	MinCheckInterval time.Duration // UPTIME_MIN_CHECK_INTERVAL
	StatsWindow      time.Duration // UPTIME_STATS_WINDOW
	MaxResults       int           // UPTIME_MAX_RESULTS
}

// Load reads the configuration from the environment, falling back to
// defaults for unset variables. Durations use Go syntax, e.g. "5s" or "1m30s".
func Load() (Config, error) {
	cfg := Config{
		Addr:   envString("UPTIME_ADDR", ":8080"),
		DBPath: envString("UPTIME_DB_PATH", "uptime.db"),
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
		{&cfg.MinCheckInterval, "UPTIME_MIN_CHECK_INTERVAL", 5 * time.Second},
		{&cfg.StatsWindow, "UPTIME_STATS_WINDOW", 24 * time.Hour},
	}
	for _, d := range durations {
		v, err := envDuration(d.key, d.def)
		if err != nil {
			return Config{}, err
		}
		*d.dst = v
	}

	var err error
	if cfg.Workers, err = envPositiveInt("UPTIME_WORKERS", 20); err != nil {
		return Config{}, err
	}
	if cfg.ResultBatchSize, err = envPositiveInt("UPTIME_RESULT_BATCH_SIZE", 100); err != nil {
		return Config{}, err
	}
	if cfg.MaxResults, err = envPositiveInt("UPTIME_MAX_RESULTS", 1000); err != nil {
		return Config{}, err
	}
	if cfg.MinCheckInterval < time.Second {
		// Intervals are stored in whole seconds.
		return Config{}, fmt.Errorf("UPTIME_MIN_CHECK_INTERVAL: must be at least 1s, got %s", cfg.MinCheckInterval)
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
