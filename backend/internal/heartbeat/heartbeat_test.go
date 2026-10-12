package heartbeat

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/metrics"
)

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// run starts Run and returns a function that stops it and waits.
func run(opts Options) (stop func()) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		Run(ctx, opts)
	}()
	return func() {
		cancel()
		<-done
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestHeartbeat(t *testing.T) {
	var (
		hits   atomic.Int32
		status atomic.Int32
		method atomic.Value
	)
	status.Store(http.StatusOK)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method.Store(r.Method)
		hits.Add(1)
		w.WriteHeader(int(status.Load()))
	}))
	defer srv.Close()

	var healthy atomic.Bool
	healthy.Store(true)
	logs := &syncBuffer{}
	m := metrics.New("test")
	url := srv.URL + "/ping/SECRET-KEY"
	stop := run(Options{
		URL: url, Interval: 5 * time.Millisecond, Timeout: time.Second,
		Healthy: func() error {
			if healthy.Load() {
				return nil
			}
			return errors.New("scheduler has not ticked")
		},
		Client: srv.Client(), Metrics: m, Logger: slog.New(slog.NewTextHandler(logs, nil)),
	})
	defer stop()

	count := func(result string) float64 {
		got, err := m.Gather()
		if err != nil {
			t.Fatal(err)
		}
		return got["kubeat_heartbeats_total{result="+result+"}"]
	}

	waitFor(t, "heartbeats to arrive", func() bool { return hits.Load() >= 3 && count(metrics.HeartbeatSent) >= 3 })
	if method.Load() != http.MethodGet {
		t.Errorf("method = %v, want GET", method.Load())
	}

	// Unhealthy: nothing is sent, so the external switch will trip.
	healthy.Store(false)
	waitFor(t, "withheld heartbeats", func() bool { return count(metrics.HeartbeatWithheld) >= 3 })
	before := hits.Load()
	time.Sleep(40 * time.Millisecond)
	if after := hits.Load(); after > before+1 { // one request may have been in flight
		t.Errorf("%d heartbeats sent while unhealthy", after-before)
	}

	// Healthy again but the switch rejects the request: counted as failed.
	healthy.Store(true)
	status.Store(http.StatusInternalServerError)
	waitFor(t, "failed heartbeats", func() bool { return count(metrics.HeartbeatFailed) >= 2 })

	if out := logs.String(); !strings.Contains(out, "heartbeat withheld") || !strings.Contains(out, "scheduler has not ticked") {
		t.Errorf("log does not explain the withheld heartbeat:\n%s", out)
	}
}

func TestHeartbeatFailureDoesNotLogTheURL(t *testing.T) {
	logs := &syncBuffer{}
	m := metrics.New("test")
	url := "http://127.0.0.1:1/ping/SECRET-KEY"
	stop := run(Options{
		URL: url, Interval: 5 * time.Millisecond, Timeout: time.Second,
		Healthy: func() error { return nil },
		Client:  &http.Client{}, Metrics: m, Logger: slog.New(slog.NewTextHandler(logs, nil)),
	})
	waitFor(t, "a failed heartbeat to be logged", func() bool { return strings.Contains(logs.String(), "heartbeat failed") })
	stop()

	if strings.Contains(logs.String(), "SECRET-KEY") {
		t.Errorf("the heartbeat URL leaked into the log:\n%s", logs.String())
	}
}
