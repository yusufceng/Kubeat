package checker

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// httpMonitor is an HTTP monitor with the default options, as the store
// returns it.
func httpMonitor(target string) store.Monitor {
	return store.Monitor{ID: 7, Type: store.MonitorHTTP, Target: target,
		HTTP: store.HTTPOptions{Method: store.DefaultHTTPMethod, ExpectedStatus: store.DefaultExpectedStatus,
			MaxRedirects: store.DefaultMaxRedirects}}
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
	c := New(Options{UserAgent: "kubeat/test", BodyLimit: 1 << 20})

	tests := []struct {
		path       string
		wantUp     bool
		wantStatus int
		// wantHops is what the check made of the target: a redirect
		// followed is a request more, for the ceiling on checks per host.
		wantHops int
	}{
		{"/ok", true, 200, 1},
		{"/redirect", true, 200, 2},
		{"/fail", false, 503, 1},
		{"/slow", false, 0, 1},
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
			if len(got.Hops) != tt.wantHops {
				t.Errorf("Hops = %v, want %d", got.Hops, tt.wantHops)
			}
			// Every hop went to the test server, by the name its URL gave
			// and at the address that answered.
			for _, hop := range got.Hops {
				if hop.Host != "127.0.0.1" || hop.Addr != "127.0.0.1" {
					t.Errorf("hop = %+v, want host and address 127.0.0.1", hop)
				}
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
	c := New(Options{BodyLimit: 50})
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

// The monitor's headers and credentials go to the server it checks, along
// its redirects there, and to no other: not another port of the same host,
// not another host. A redirect that names the monitor's own server by its
// address stays on the monitor's site there. The request body goes to no
// other server either: a redirect that would send it there is not followed.
func TestRedirectToAnotherServerDropsTheMonitorsHeaders(t *testing.T) {
	type request struct{ path, host, key, auth, body string }
	var (
		mu   sync.Mutex
		seen []request
	)
	record := func(r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, request{r.URL.Path, r.Host, r.Header.Get("X-Api-Key"), r.Header.Get("Authorization"), string(body)})
	}
	other := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { record(r) }))
	defer other.Close()
	var front *httptest.Server
	front = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record(r)
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, "/same", http.StatusFound) // the same site
		case "/same":
			http.Redirect(w, r, front.URL+"/by-address", http.StatusFound) // the same server, by its address
		case "/by-address":
			http.Redirect(w, r, other.URL+"/away", http.StatusFound) // another port
		case "/plain":
			http.Redirect(w, r, front.URL+"/plain-end", http.StatusFound)
		case "/post":
			http.Redirect(w, r, front.URL+"/post-here", http.StatusTemporaryRedirect) // the body along
		case "/post-here":
			http.Redirect(w, r, other.URL+"/post-away", http.StatusTemporaryRedirect)
		case "/post-dropped":
			http.Redirect(w, r, other.URL+"/post-away", http.StatusSeeOther) // a GET, without the body
		}
	}))
	defer front.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	check := func(path string, edit func(*store.HTTPOptions)) (store.CheckResult, []request) {
		mu.Lock()
		seen = nil
		mu.Unlock()
		m := httpMonitor(front.URL + path)
		m.HTTP.Auth, m.HTTP.AuthSecret = store.HTTPAuthBearer, "TOKEN"
		edit(&m.HTTP)
		got := New(Options{}).Check(ctx, m)
		mu.Lock()
		defer mu.Unlock()
		return got, seen
	}
	headers := func(h map[string]string) func(*store.HTTPOptions) {
		return func(o *store.HTTPOptions) { o.Headers = h }
	}
	posting := func(o *store.HTTPOptions) {
		o.Method, o.Body, o.Headers = "POST", `{"password":"S3cr3t"}`, map[string]string{"X-Api-Key": "KEY"}
	}

	frontHost, otherHost := front.Listener.Addr().String(), other.Listener.Addr().String()
	for _, tc := range []struct {
		path    string
		edit    func(*store.HTTPOptions)
		up      bool
		want    []request
		failure string
	}{
		{"/start", headers(map[string]string{"X-Api-Key": "KEY", "Host": "api.shop.example"}), true, []request{
			{"/start", "api.shop.example", "KEY", "Bearer TOKEN", ""},
			{"/same", "api.shop.example", "KEY", "Bearer TOKEN", ""},
			{"/by-address", "api.shop.example", "KEY", "Bearer TOKEN", ""},
			{"/away", otherHost, "", "", ""},
		}, ""},
		// Without a Host header the address is the site: a redirect on it is
		// the same server, whatever the path.
		{"/plain", headers(map[string]string{"X-Api-Key": "KEY"}), true, []request{
			{"/plain", frontHost, "KEY", "Bearer TOKEN", ""},
			{"/plain-end", frontHost, "KEY", "Bearer TOKEN", ""},
		}, ""},
		{"/post", posting, false, []request{
			{"/post", frontHost, "KEY", "Bearer TOKEN", `{"password":"S3cr3t"}`},
			{"/post-here", frontHost, "KEY", "Bearer TOKEN", `{"password":"S3cr3t"}`},
		}, `Post "` + other.URL + `/post-away": the redirect would send the request body to another server`},
		{"/post-dropped", posting, true, []request{
			{"/post-dropped", frontHost, "KEY", "Bearer TOKEN", `{"password":"S3cr3t"}`},
			{"/post-away", otherHost, "", "", ""},
		}, ""},
	} {
		got, requests := check(tc.path, tc.edit)
		if got.Up != tc.up || got.Error != tc.failure {
			t.Errorf("%s: %+v", tc.path, got)
		}
		if fmt.Sprint(requests) != fmt.Sprint(tc.want) {
			t.Errorf("%s: requests:\n got  %v\n want %v", tc.path, requests, tc.want)
		}
	}
}

// A site that sends plain HTTP on to its own HTTPS address, as an OpenShift
// Route with insecureEdgeTerminationPolicy: Redirect (302) or ingress-nginx's
// ssl-redirect (308) does, is still the monitor's server: its headers,
// credentials, Host header and body go along. Back to plain HTTP is another
// server, and so is another port.
func TestRedirectToTLSOnTheSameHostKeepsTheMonitorsHeaders(t *testing.T) {
	type request struct{ scheme, host, path, key, auth, body string }
	var (
		mu   sync.Mutex
		seen []request
	)
	record := func(r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, request{scheme, r.Host, r.URL.Path, r.Header.Get("X-Api-Key"), r.Header.Get("Authorization"), string(body)})
	}
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record(r)
		switch r.URL.Path {
		case "/downgrade":
			http.Redirect(w, r, "http://example.com/end", http.StatusFound)
		case "/another-port":
			http.Redirect(w, r, "https://example.com:8443/end", http.StatusFound)
		}
	}))
	defer secure.Close()
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record(r)
		if code, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/")); err == nil {
			http.Redirect(w, r, "https://example.com/end", code)
		}
	}))
	defer plain.Close()

	// example.com, which the test certificate names, on its two usual ports
	// and one more; the certificate is verified, as in production.
	routes := map[string]string{
		"example.com:80": plain.Listener.Addr().String(), "example.com:443": secure.Listener.Addr().String(),
		"example.com:8443": secure.Listener.Addr().String(),
	}
	roots := x509.NewCertPool()
	roots.AddCert(secure.Certificate())
	c := New(Options{})
	c.verifying.Proxy = nil
	c.verifying.TLSClientConfig = &tls.Config{RootCAs: roots}
	c.verifying.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, routes[addr])
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	check := func(target string, edit func(*store.HTTPOptions)) []request {
		mu.Lock()
		seen = nil
		mu.Unlock()
		m := httpMonitor(target)
		m.HTTP.Auth, m.HTTP.AuthSecret, m.HTTP.Headers = store.HTTPAuthBearer, "TOKEN", map[string]string{"X-Api-Key": "KEY"}
		edit(&m.HTTP)
		if got := c.Check(ctx, m); !got.Up {
			t.Errorf("%s: %+v", target, got)
		}
		mu.Lock()
		defer mu.Unlock()
		return seen
	}
	as := func(*store.HTTPOptions) {}

	for _, tc := range []struct {
		target string
		edit   func(*store.HTTPOptions)
		want   []request
	}{
		{"http://example.com/301", as, []request{
			{"http", "example.com", "/301", "KEY", "Bearer TOKEN", ""}, {"https", "example.com", "/end", "KEY", "Bearer TOKEN", ""}}},
		{"http://example.com/302", as, []request{
			{"http", "example.com", "/302", "KEY", "Bearer TOKEN", ""}, {"https", "example.com", "/end", "KEY", "Bearer TOKEN", ""}}},
		{"http://example.com:80/308", func(o *store.HTTPOptions) { o.Method, o.Body = "POST", `{"password":"S3cr3t"}` }, []request{
			{"http", "example.com:80", "/308", "KEY", "Bearer TOKEN", `{"password":"S3cr3t"}`},
			{"https", "example.com", "/end", "KEY", "Bearer TOKEN", `{"password":"S3cr3t"}`}}},
		// The site the Host header names, on the same server over TLS.
		{"http://example.com/302", func(o *store.HTTPOptions) { o.Headers["Host"] = "app.example" }, []request{
			{"http", "app.example", "/302", "KEY", "Bearer TOKEN", ""}, {"https", "app.example", "/end", "KEY", "Bearer TOKEN", ""}}},
		{"https://example.com/downgrade", as, []request{
			{"https", "example.com", "/downgrade", "KEY", "Bearer TOKEN", ""}, {"http", "example.com", "/end", "", "", ""}}},
		{"https://example.com/another-port", as, []request{
			{"https", "example.com", "/another-port", "KEY", "Bearer TOKEN", ""}, {"https", "example.com:8443", "/end", "", "", ""}}},
	} {
		if got := check(tc.target, tc.edit); fmt.Sprint(got) != fmt.Sprint(tc.want) {
			t.Errorf("%s: requests:\n got  %v\n want %v", tc.target, got, tc.want)
		}
	}
}

// The Host header goes out as the monitor's destination counts it: net/http
// would send a padded one empty, to whichever site a router keeps for
// requests without a name.
func TestHostHeaderIsSentTrimmed(t *testing.T) {
	var host string
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { host = r.Host }))
	defer srv.Close()
	for value, want := range map[string]string{
		"api.shop.example ": "api.shop.example", "\tapi.shop.example": "api.shop.example", " ": srv.Listener.Addr().String(),
	} {
		m := httpMonitor(srv.URL)
		m.HTTP.Headers = map[string]string{"Host": value}
		if got := New(Options{}).Check(context.Background(), m); !got.Up || host != want {
			t.Errorf("Host %q: sent %q, %+v", value, host, got)
		}
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
	got := c.Check(context.Background(), m)
	if !got.Up {
		t.Errorf("monitor that skips verification failed: %s", got.Error)
	}
	// The certificate's expiry is reported exactly.
	if want := srv.Certificate().NotAfter; !got.CertExpiresAt.Equal(want) {
		t.Errorf("CertExpiresAt = %s, want %s", got.CertExpiresAt, want)
	}

	// Still reported when the check itself fails on the status.
	m.HTTP.ExpectedStatus = "204"
	if got := c.Check(context.Background(), m); got.Up || got.CertExpiresAt.IsZero() {
		t.Errorf("failed HTTPS check lost the certificate expiry: %+v", got)
	}
}

func TestPlainHTTPHasNoCertificate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	if got := New(Options{}).Check(context.Background(), httpMonitor(srv.URL)); !got.Up || !got.CertExpiresAt.IsZero() {
		t.Errorf("plain HTTP check: %+v", got)
	}
}

func TestShortDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{
		time.Hour:                    "1h",
		90 * time.Minute:             "1h30m",
		5 * time.Minute:              "5m",
		45 * time.Second:             "45s",
		time.Minute + 30*time.Second: "1m30s",
		24 * time.Hour:               "24h",
	} {
		if got := shortDuration(d); got != want {
			t.Errorf("shortDuration(%s) = %q, want %q", d, got, want)
		}
	}
}
