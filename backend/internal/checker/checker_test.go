package checker

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

func TestHTTPCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			if r.Header.Get("User-Agent") != "kubeat/test" {
				w.WriteHeader(http.StatusBadRequest)
			}
		case "/redirect":
			http.Redirect(w, r, "/ok", http.StatusFound)
		case "/fail":
			w.WriteHeader(http.StatusServiceUnavailable)
		case "/slow":
			select {
			case <-time.After(5 * time.Second):
			case <-r.Context().Done():
			}
		}
	}))
	defer srv.Close()
	c := New(Options{UserAgent: "kubeat/test"})

	tests := []struct {
		path       string
		wantUp     bool
		wantStatus int
	}{
		{"/ok", true, 200},
		{"/redirect", true, 200},
		{"/fail", false, 503},
		{"/slow", false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			got := c.Check(ctx, store.Monitor{ID: 7, Type: store.MonitorHTTP, Target: srv.URL + tt.path})

			if got.Up != tt.wantUp || got.StatusCode != tt.wantStatus {
				t.Errorf("Up=%v StatusCode=%d Error=%q, want Up=%v StatusCode=%d",
					got.Up, got.StatusCode, got.Error, tt.wantUp, tt.wantStatus)
			}
			if got.Up == (got.Error != "") {
				t.Errorf("Up=%v but Error=%q", got.Up, got.Error)
			}
			if got.MonitorID != 7 || got.CheckedAt.IsZero() || got.Latency <= 0 {
				t.Errorf("result metadata not set: %+v", got)
			}
		})
	}
}

func TestHTTPCheckTLSVerification(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	m := store.Monitor{Type: store.MonitorHTTP, Target: srv.URL}

	if got := New(Options{}).Check(context.Background(), m); got.Up {
		t.Error("self-signed certificate was accepted with verification on")
	}
	if got := New(Options{InsecureSkipVerify: true}).Check(context.Background(), m); !got.Up {
		t.Errorf("check failed with verification off: %s", got.Error)
	}
}

func TestTCPCheck(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	c := New(Options{})

	if got := c.Check(context.Background(), store.Monitor{Type: store.MonitorTCP, Target: addr}); !got.Up {
		t.Errorf("open port reported down: %s", got.Error)
	}

	ln.Close()
	if got := c.Check(context.Background(), store.Monitor{Type: store.MonitorTCP, Target: addr}); got.Up {
		t.Error("closed port reported up")
	}
}

func TestUnsupportedType(t *testing.T) {
	got := New(Options{}).Check(context.Background(), store.Monitor{Type: "icmp"})
	if got.Up || got.Error == "" {
		t.Errorf("unsupported type: %+v", got)
	}
}
