package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yusufceng/Kubeat/backend/internal/metrics"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// postAs answers a POST to path with body from peer: with token in
// X-Frontend-Token unless it is empty, xff as X-Forwarded-For unless it is
// empty, and bearer as the session unless it is empty.
func postAs(srv *httptest.Server, peer, token, xff, bearer, path, body string) int {
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.RemoteAddr = peer
	if token != "" {
		req.Header.Set(frontendTokenHeader, token)
	}
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	srv.Config.Handler.ServeHTTP(rec, req)
	return rec.Code
}

// A throttledSurface is one of the three passwords the throttle guards per
// source, as a request reaches it: prepare sets it up on a fresh server and
// returns a wrong guess at it from peer, with token, xff and, where it needs
// one, a session (see postAs).
type throttledSurface struct {
	name, metric string
	prepare      func(t *testing.T, srv *httptest.Server, db *store.SQLite) func(peer, token, xff string) int
}

var throttledSurfaces = []throttledSurface{
	{"sign-in", metrics.SurfaceSignIn, func(t *testing.T, srv *httptest.Server, _ *store.SQLite) func(peer, token, xff string) int {
		t.Helper()
		if code := postAs(srv, frontendPod, testFrontendToken, "", "", "/api/v1/auth/setup",
			`{"username":"admin","password":"parola-123"}`); code != http.StatusOK {
			t.Fatalf("setup: status = %d, want 200", code)
		}
		return func(peer, token, xff string) int {
			return postAs(srv, peer, token, xff, "", "/api/v1/auth/login", `{"username":"admin","password":"yanlis-parola"}`)
		}
	}},
	{"password change", metrics.SurfacePasswordChange, func(t *testing.T, srv *httptest.Server, _ *store.SQLite) func(peer, token, xff string) int {
		t.Helper()
		rec := fromBrowserAt(srv, "203.0.113.200", "", "POST", "/api/v1/auth/setup", `{"username":"admin","password":"parola-123"}`)
		var sess sessionResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &sess); rec.Code != http.StatusOK || err != nil || sess.Token == "" {
			t.Fatalf("setup: status = %d, body = %q", rec.Code, rec.Body.String())
		}
		return func(peer, token, xff string) int {
			return postAs(srv, peer, token, xff, sess.Token, "/api/v1/auth/password",
				`{"current_password":"yanlis-parola","new_password":"parola-123"}`)
		}
	}},
	{"status-page unlock", metrics.SurfaceStatusPage, func(t *testing.T, srv *httptest.Server, db *store.SQLite) func(peer, token, xff string) int {
		t.Helper()
		statusPagePassword(t, srv, db)
		return func(peer, token, xff string) int {
			return postAs(srv, peer, token, xff, "", "/api/v1/status-pages/by-slug/gizli/unlock", `{"password":"yanlis-parola"}`)
		}
	}},
}

// degradedSeries is the series of kubeat_signin_source_degraded_total for
// reason and surface, as Gather names it.
func degradedSeries(reason, surface string) string {
	return "kubeat_signin_source_degraded_total{reason=" + reason + "}{surface=" + surface + "}"
}

// Every request whose source the throttle could not key on its client is
// counted, by its reason and surface, on each of the three surfaces: a log
// line a minute is no alert, and an install whose notes no one reads (Argo
// CD, Terraform) has nothing else to tell it by. The counter counts each
// request, not each warning, and carries no address. A request read as
// configured counts nowhere.
func TestSourceDegradedCountsEveryRequest(t *testing.T) {
	const client = "203.0.113.9"
	for _, branch := range []struct {
		reason    string
		customize func(*Options)
		// peer, token and xff of the guess.
		peer, token, xff string
		warn             string
	}{
		{metrics.DegradedHopsZero, func(o *Options) { o.TrustProxyHops = 0 },
			frontendPod, testFrontendToken, client, "but UPTIME_TRUST_PROXY_HOPS=0"},
		{metrics.DegradedNoFrontendToken, func(o *Options) { o.FrontendToken = "" },
			frontendPod, testFrontendToken, client, "UPTIME_FRONTEND_TOKEN is not set"},
		{metrics.DegradedUntrustedPeer, nil,
			attackerPod, "", client, "did not present the UI's X-Frontend-Token"},
		{metrics.DegradedHopsTooHigh, func(o *Options) { o.TrustProxyHops = 2 },
			frontendPod, testFrontendToken, client, "fewer entries than UPTIME_TRUST_PROXY_HOPS=2"},
		{metrics.DegradedHopsTooLow, func(o *Options) { o.PublicAddresses = refreshedPublicAddresses(t, lbAddr) },
			frontendPod, testFrontendToken, client + ", " + lbAddr, "UPTIME_TRUST_PROXY_HOPS=1 is lower than needed"},
		{"", nil, frontendPod, testFrontendToken, client, ""},
	} {
		for _, s := range throttledSurfaces {
			name := branch.reason
			if name == "" {
				name = "read as configured"
			}
			t.Run(name+", "+s.name, func(t *testing.T) {
				meter := metrics.New("test")
				srv, db, _, logs := throttledAPI(t, 0, func(o *Options) {
					o.SignIns = meter
					if branch.customize != nil {
						branch.customize(o)
					}
				})
				guess := s.prepare(t, srv, db)
				// Three guesses within the minute; the third is the last the
				// source may make before its lock.
				for i := range 3 {
					if code := guess(branch.peer, branch.token, branch.xff); code == http.StatusTooManyRequests {
						t.Fatalf("guess %d: 429 before the lock", i+1)
					}
				}
				got, err := meter.Gather()
				if err != nil {
					t.Fatal(err)
				}
				for _, reason := range []string{metrics.DegradedHopsZero, metrics.DegradedNoFrontendToken,
					metrics.DegradedUntrustedPeer, metrics.DegradedHopsTooHigh, metrics.DegradedHopsTooLow} {
					for _, other := range throttledSurfaces {
						want := 0.0
						if reason == branch.reason && other.metric == s.metric {
							want = 3
						}
						if key := degradedSeries(reason, other.metric); got[key] != want {
							t.Errorf("%s = %v, want %v", key, got[key], want)
						}
					}
				}
				text := logs.String()
				if branch.reason == "" {
					if strings.Contains(text, "level=WARN") {
						t.Errorf("warned with the hops right: %q", text)
					}
					return
				}
				// One warning for the three requests: the counter is not held
				// to the warning's minute.
				if n := strings.Count(text, "level=WARN"); n != 1 || !strings.Contains(text, branch.warn) {
					t.Errorf("want one WARN saying %q, got %d: %q", branch.warn, n, text)
				}
				// The exposition carries no address: neither the client's,
				// the peer's nor the load balancer's.
				rec := httptest.NewRecorder()
				meter.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
				body := rec.Body.String()
				if !strings.Contains(body, `kubeat_signin_source_degraded_total{reason="`+branch.reason+`",surface="`+s.metric+`"} 3`) {
					t.Errorf("the exposition lacks the series at 3:\n%s", grepLines(body, "kubeat_signin_source_degraded_total"))
				}
				for _, addr := range []string{client, lbAddr, "10.128."} {
					if strings.Contains(body, addr) {
						t.Errorf("the exposition carries the address %s", addr)
					}
				}
			})
		}
	}
}

// grepLines are the lines of text that contain s.
func grepLines(text, s string) string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, s) {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// refreshedPublicAddresses are the public URL "https://kubeat.example.com"'s
// addresses, resolved once to addrs.
func refreshedPublicAddresses(t *testing.T, addrs ...string) *PublicAddresses {
	t.Helper()
	resolver := newFakeHostResolver()
	resolver.set("kubeat.example.com.", addrs...)
	p := NewPublicAddresses("https://kubeat.example.com", resolver, nil)
	p.Refresh(context.Background())
	return p
}

// Behind a load balancer that appends its own address to X-Forwarded-For
// after the client's (a GCE Ingress, a GKE Gateway), one hop too few keys
// every browser on the load balancer: the address the public URL's host
// resolves to. That is counted as hops_too_low and warned of, naming the
// address, while the source stays as configured; the right hops read the
// client and say nothing. A host behind a CNAME, or one that does not
// resolve, says nothing either, at any hops: no false alarm.
func TestSourceDegradedHopsTooLow(t *testing.T) {
	const client = "203.0.113.9"
	xff := client + ", " + lbAddr
	for _, tc := range []struct {
		name    string
		hops    int
		public  func(t *testing.T) *PublicAddresses
		counted bool
		source  string
	}{
		{"one hop too few", 1, func(t *testing.T) *PublicAddresses { return refreshedPublicAddresses(t, lbAddr) }, true, lbAddr},
		{"the right hops", 2, func(t *testing.T) *PublicAddresses { return refreshedPublicAddresses(t, lbAddr) }, false, client},
		{"behind a CNAME", 1, func(t *testing.T) *PublicAddresses {
			resolver := newFakeHostResolver()
			resolver.set("kubeat.example.com.", lbAddr)
			resolver.cname("kubeat.example.com.", "edge.cdn.example.net.")
			p := NewPublicAddresses("https://kubeat.example.com", resolver, nil)
			p.Refresh(context.Background())
			return p
		}, false, lbAddr},
		{"a host that does not resolve", 1, func(t *testing.T) *PublicAddresses { return refreshedPublicAddresses(t) }, false, lbAddr},
		{"no public URL", 1, func(*testing.T) *PublicAddresses { return nil }, false, lbAddr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			meter := metrics.New("test")
			srv, db, _, logs := throttledAPI(t, 0, func(o *Options) {
				o.SignIns = meter
				o.TrustProxyHops = tc.hops
				o.PublicAddresses = tc.public(t)
			})
			guess := throttledSurfaces[0].prepare(t, srv, db)
			if code := guess(frontendPod, testFrontendToken, xff); code != http.StatusUnauthorized {
				t.Fatalf("guess: status = %d, want 401", code)
			}
			got, err := meter.Gather()
			if err != nil {
				t.Fatal(err)
			}
			want := 0.0
			if tc.counted {
				want = 1
			}
			if key := degradedSeries(metrics.DegradedHopsTooLow, metrics.SurfaceSignIn); got[key] != want {
				t.Errorf("%s = %v, want %v", key, got[key], want)
			}
			text := logs.String()
			if !strings.Contains(text, "source="+tc.source) {
				t.Errorf("the sign-in keyed on another source than %s: %q", tc.source, text)
			}
			warned := strings.Contains(text, "lower than needed")
			if warned != tc.counted {
				t.Errorf("warned %v, want %v: %q", warned, tc.counted, text)
			}
			if tc.counted && (!strings.Contains(text, `\"`+lbAddr+`\"`) || !strings.Contains(text, "one shared per-name lock")) {
				t.Errorf("the WARN names neither the load balancer's address nor the shared lock: %q", text)
			}
		})
	}
}

// The 429s of the password change and the status-page unlock are counted in
// kubeat_throttled_total, by surface and kind: the source's lock and the
// ceiling across all sources, each as it happens. The sign-in's counter
// keeps its meaning: none of them count there.
func TestGuessThrottledCounted(t *testing.T) {
	const ceiling = 5
	for _, tc := range guardedPasswords {
		t.Run(tc.name, func(t *testing.T) {
			meter := metrics.New("test")
			srv, db, _, _ := throttledAPI(t, ceiling, func(o *Options) { o.SignIns = meter })
			g := tc.make(t, srv, db)
			surface := map[string]string{statusPageSurface.name: metrics.SurfaceStatusPage, passwordSurface.name: metrics.SurfacePasswordChange}[g.surfaceName]
			// Three guesses lock the attacker's source; two more 429s.
			for i := range 3 {
				if rec := g.try("203.0.113.1", "yanlis-parola"); rec.Code != g.wrong {
					t.Fatalf("guess %d: status = %d, want %d", i+1, rec.Code, g.wrong)
				}
			}
			for range 2 {
				wantThrottled(t, "the locked source", g.try("203.0.113.1", "yanlis-parola"), guessLockout)
			}
			// Two more sources fill the ceiling at five; a sixth source is
			// refused by it.
			for i := range 2 {
				if rec := g.try(fmt.Sprintf("198.51.100.%d", i+1), "yanlis-parola"); rec.Code != g.wrong {
					t.Fatalf("guess from another source %d: status = %d, want %d", i+1, rec.Code, g.wrong)
				}
			}
			wantThrottled(t, "a fresh source past the ceiling", g.try("198.51.100.99", "yanlis-parola"), guessLockout)

			got, err := meter.Gather()
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]float64{
				"kubeat_throttled_total{kind=source}{surface=" + surface + "}":  2,
				"kubeat_throttled_total{kind=account}{surface=" + surface + "}": 1,
				"kubeat_signin_throttled_total{kind=source}":                    0,
				"kubeat_signin_throttled_total{kind=account}":                   0,
				"kubeat_signin_throttled_total{kind=source_total}":              0,
			}
			for _, other := range []string{metrics.SurfaceStatusPage, metrics.SurfacePasswordChange} {
				if other != surface {
					want["kubeat_throttled_total{kind=source}{surface="+other+"}"] = 0
					want["kubeat_throttled_total{kind=account}{surface="+other+"}"] = 0
				}
			}
			for key, value := range want {
				if got[key] != value {
					t.Errorf("%s = %v, want %v", key, got[key], value)
				}
			}
		})
	}
}
