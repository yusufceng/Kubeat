package checker

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// httpMonitor is an HTTP monitor with the default options, as the store
// returns it.
func httpMonitor(target string) store.Monitor {
	return store.Monitor{ID: 7, Type: store.MonitorHTTP, Target: target,
		HTTP: store.HTTPOptions{Method: store.DefaultHTTPMethod, ExpectedStatus: store.DefaultExpectedStatus}}
}

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
	c := New(Options{UserAgent: "kubeat/test", KeywordBodyLimit: 1 << 20})

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
			got := c.Check(ctx, httpMonitor(srv.URL+tt.path))

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
	m := httpMonitor(srv.URL)

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

func TestParseStatusSet(t *testing.T) {
	set, err := ParseStatusSet(" 200-299, 401 ,404 ")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for code, want := range map[int]bool{199: false, 200: true, 250: true, 299: true, 300: false, 401: true, 403: false, 404: true} {
		if got := set.Contains(code); got != want {
			t.Errorf("Contains(%d) = %v, want %v", code, got, want)
		}
	}
	for _, bad := range []string{"", "abc", "200-", "-200", "300-200", "99", "600", "200,,204", "200-299-300", "2xx"} {
		if _, err := ParseStatusSet(bad); err == nil {
			t.Errorf("ParseStatusSet(%q) was accepted", bad)
		}
	}
}

func TestHTTPOptions(t *testing.T) {
	var gotMethod, gotAuth, gotHost string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotAuth, gotHost = r.Method, r.Header.Get("Authorization"), r.Host
		switch r.URL.Path {
		case "/unauthorized":
			w.WriteHeader(http.StatusUnauthorized)
		case "/late-keyword":
			w.Write(bytes.Repeat([]byte("x"), 100))
			w.Write([]byte("ready"))
		default:
			w.Write([]byte(`{"status":"ready","db":"ok"}`))
		}
	}))
	defer srv.Close()
	c := New(Options{KeywordBodyLimit: 50})
	ctx := context.Background()

	check := func(path string, edit func(*store.HTTPOptions)) store.CheckResult {
		m := httpMonitor(srv.URL + path)
		edit(&m.HTTP)
		return c.Check(ctx, m)
	}

	// An expected non-2xx status is a success.
	if got := check("/unauthorized", func(o *store.HTTPOptions) { o.ExpectedStatus = "401" }); !got.Up || got.StatusCode != 401 {
		t.Errorf("expected 401: %+v", got)
	}
	// And a 200 outside the expected set is a failure.
	if got := check("/", func(o *store.HTTPOptions) { o.ExpectedStatus = "204" }); got.Up || got.StatusCode != 200 {
		t.Errorf("200 when 204 expected: %+v", got)
	}

	if got := check("/", func(o *store.HTTPOptions) { o.Keyword = `"db":"ok"` }); !got.Up {
		t.Errorf("keyword present: %+v", got)
	}
	if got := check("/", func(o *store.HTTPOptions) { o.Keyword = `"db":"down"` }); got.Up || got.StatusCode != 200 || !strings.Contains(got.Error, "keyword") {
		t.Errorf("keyword absent: %+v", got)
	}
	// Only the configured prefix of the body is searched.
	if got := check("/late-keyword", func(o *store.HTTPOptions) { o.Keyword = "ready" }); got.Up {
		t.Errorf("keyword beyond the body limit was found: %+v", got)
	}

	got := check("/", func(o *store.HTTPOptions) {
		o.Method = "HEAD"
		o.Headers = map[string]string{"Authorization": "Bearer t0ken", "host": "internal.example"}
	})
	if !got.Up || gotMethod != "HEAD" || gotAuth != "Bearer t0ken" || gotHost != "internal.example" {
		t.Errorf("method/headers: up=%v method=%q auth=%q host=%q", got.Up, gotMethod, gotAuth, gotHost)
	}

	if got := check("/", func(o *store.HTTPOptions) { o.ExpectedStatus = "nonsense" }); got.Up || !strings.Contains(got.Error, "expected status") {
		t.Errorf("invalid expected status: %+v", got)
	}
}

func TestPerMonitorTLSSkip(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	c := New(Options{}) // verification on globally

	m := httpMonitor(srv.URL)
	if got := c.Check(context.Background(), m); got.Up {
		t.Error("self-signed certificate accepted without the monitor opting out")
	}
	m.HTTP.SkipTLSVerify = true
	if got := c.Check(context.Background(), m); !got.Up {
		t.Errorf("monitor that skips verification failed: %s", got.Error)
	}
}
