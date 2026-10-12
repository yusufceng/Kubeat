// Package heartbeat tells an external dead man's switch that the server is
// alive and doing its job. The switch raises the alarm when the heartbeats
// stop, which is the one failure the server cannot report itself.
package heartbeat

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/metrics"
)

// Options configures Run.
type Options struct {
	// URL is requested with GET on every beat. Services such as
	// healthchecks.io, Uptime Kuma push monitors and Alertmanager webhooks
	// accept that.
	URL      string
	Interval time.Duration
	Timeout  time.Duration
	// Healthy reports why the server should not be considered working, or
	// nil. A heartbeat is only sent while it returns nil, so a process that
	// is up but no longer checking anything also trips the switch.
	Healthy func() error
	Client  *http.Client
	Metrics *metrics.Metrics
	Logger  *slog.Logger
}

// Run sends a heartbeat every Interval until ctx is cancelled.
func Run(ctx context.Context, opts Options) {
	ticker := time.NewTicker(opts.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			beat(ctx, opts)
		}
	}
}

func beat(ctx context.Context, opts Options) {
	now := time.Now()
	if err := opts.Healthy(); err != nil {
		opts.Metrics.Heartbeat(metrics.HeartbeatWithheld, now)
		opts.Logger.Error("heartbeat withheld", "reason", err)
		return
	}
	if err := send(ctx, opts); err != nil {
		if ctx.Err() != nil {
			return // shutting down
		}
		opts.Metrics.Heartbeat(metrics.HeartbeatFailed, now)
		opts.Logger.Warn("heartbeat failed", "error", redact(err, opts.URL))
		return
	}
	opts.Metrics.Heartbeat(metrics.HeartbeatSent, now)
}

func send(ctx context.Context, opts Options) error {
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, opts.URL, nil)
	if err != nil {
		return err
	}
	resp, err := opts.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("heartbeat URL answered %d", resp.StatusCode)
	}
	return nil
}

// redact keeps the heartbeat URL, which usually embeds a secret key, out of
// the logs.
func redact(err error, url string) string {
	msg := err.Error()
	for i := 0; i+len(url) <= len(msg); i++ {
		if msg[i:i+len(url)] == url {
			return msg[:i] + "<heartbeat url>" + msg[i+len(url):]
		}
	}
	return msg
}
