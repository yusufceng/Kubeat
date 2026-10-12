// Package config loads the server configuration from environment variables.
package config

import (
	"fmt"
	"os"
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
}

// Load reads the configuration from the environment, falling back to
// defaults for unset variables. Durations use Go syntax, e.g. "5s" or "1m30s".
func Load() (Config, error) {
	cfg := Config{
		Addr:   envString("UPTIME_ADDR", ":8080"),
		DBPath: envString("UPTIME_DB_PATH", "uptime.db"),
	}

	var err error
	if cfg.DBBusyTimeout, err = envDuration("UPTIME_DB_BUSY_TIMEOUT", 5*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.ReadHeaderTimeout, err = envDuration("UPTIME_READ_HEADER_TIMEOUT", 5*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.ShutdownTimeout, err = envDuration("UPTIME_SHUTDOWN_TIMEOUT", 10*time.Second); err != nil {
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
