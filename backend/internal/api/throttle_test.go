package api

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// clientIP reads the source the throttle keys on: the peer without a
// trusted proxy, and the right entry of X-Forwarded-For with one, ignoring
// whatever a client put to the left of the hops it trusts. When the
// forwarded chain is shorter than the trusted hops the leftmost is a
// client's to set, so clientIP falls back to the peer rather than trust it.
func TestClientIP(t *testing.T) {
	for _, tc := range []struct {
		name string
		hops int
		peer string
		xff  []string
		want string
	}{
		{name: "no proxy trusts the peer, not the header", hops: 0, peer: "198.51.100.9:4321", xff: []string{"203.0.113.1"}, want: "198.51.100.9"},
		{name: "peer without a port", hops: 0, peer: "198.51.100.9", want: "198.51.100.9"},
		{name: "one hop reads the client the proxy saw", hops: 1, peer: "10.0.0.1:5000", xff: []string{"203.0.113.7"}, want: "203.0.113.7"},
		{name: "one hop ignores a spoofed left entry", hops: 1, peer: "10.0.0.1:5000", xff: []string{"1.2.3.4, 203.0.113.7"}, want: "203.0.113.7"},
		{name: "two hops step back two from us", hops: 2, peer: "10.0.0.1:5000", xff: []string{"203.0.113.7, 10.0.0.2"}, want: "203.0.113.7"},
		{name: "several header lines join in order", hops: 1, peer: "10.0.0.1:5000", xff: []string{"1.2.3.4", "203.0.113.7"}, want: "203.0.113.7"},
		{name: "fewer entries than hops falls back to the peer, not the leftmost", hops: 3, peer: "10.0.0.1:5000", xff: []string{"203.0.113.7"}, want: "10.0.0.1"},
		{name: "a hop but no header falls back to the peer", hops: 1, peer: "10.0.0.1:5000", want: "10.0.0.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &handler{opts: Options{TrustProxyHops: tc.hops}}
			r := &http.Request{RemoteAddr: tc.peer, Header: http.Header{}}
			for _, v := range tc.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			if got := h.clientIP(r, time.Now()); got != tc.want {
				t.Errorf("clientIP(hops=%d, peer=%q, xff=%v) = %q, want %q", tc.hops, tc.peer, tc.xff, got, tc.want)
			}
		})
	}
}

// proxyWarnHint is the value both X-Forwarded-For warnings ask for: README's
// proxy-hop guide counts the entries the proxies in front of the UI write
// (1 with none), not the proxies, since the UI passes the header on and
// takes the place of the proxy nearest to the backend.
const proxyWarnHint = "the X-Forwarded-For entries your proxies in front of the UI write, 1 with none"

// A published source-address throttle must not go silently ineffective, so
// clientIP warns when X-Forwarded-For cannot be trusted: an X-Forwarded-For
// arriving while the header is trusted none (hops 0), and a forwarded chain
// shorter than the trusted hops.
func TestClientIPWarns(t *testing.T) {
	req := func(peer string, xff ...string) *http.Request {
		r := &http.Request{RemoteAddr: peer, Header: http.Header{}}
		for _, v := range xff {
			r.Header.Add("X-Forwarded-For", v)
		}
		return r
	}
	t0 := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

	t.Run("hops=0 and a header warns and keeps the peer", func(t *testing.T) {
		var logs bytes.Buffer
		h := &handler{opts: Options{TrustProxyHops: 0}, proxyWarn: newProxyWarn(slog.New(slog.NewTextHandler(&logs, nil)))}
		if got := h.clientIP(req("198.51.100.9:4321", "203.0.113.1"), t0); got != "198.51.100.9" {
			t.Errorf("source = %q, want the peer 198.51.100.9", got)
		}
		if !strings.Contains(logs.String(), "UPTIME_TRUST_PROXY_HOPS=0") || !strings.Contains(logs.String(), "level=WARN") {
			t.Errorf("want a WARN naming the setting, got %q", logs.String())
		}
	})

	t.Run("hops=0 without a header stays quiet", func(t *testing.T) {
		var logs bytes.Buffer
		h := &handler{opts: Options{TrustProxyHops: 0}, proxyWarn: newProxyWarn(slog.New(slog.NewTextHandler(&logs, nil)))}
		h.clientIP(req("198.51.100.9:4321"), t0)
		if logs.Len() != 0 {
			t.Errorf("warned with no X-Forwarded-For: %q", logs.String())
		}
	})

	t.Run("a chain shorter than hops warns and falls back to the peer", func(t *testing.T) {
		var logs bytes.Buffer
		h := &handler{opts: Options{TrustProxyHops: 2}, proxyWarn: newProxyWarn(slog.New(slog.NewTextHandler(&logs, nil)))}
		// Only one forwarded entry, which a client could have set, so the
		// source must not be read from it: a forged X-Forwarded-For must
		// neither slip the throttle nor lock a victim out.
		if got := h.clientIP(req("10.0.0.1:5000", "1.2.3.4"), t0); got != "10.0.0.1" {
			t.Errorf("source = %q, want the peer 10.0.0.1, not the client-set entry", got)
		}
		if !strings.Contains(logs.String(), "UPTIME_TRUST_PROXY_HOPS=2") || !strings.Contains(logs.String(), "level=WARN") {
			t.Errorf("want a WARN naming the configured hops, got %q", logs.String())
		}
		// The value it asks for is the guide's: the entries the proxies in
		// front of the UI write, not a count of proxies.
		if !strings.Contains(logs.String(), proxyWarnHint) {
			t.Errorf("want the WARN to ask for %q, got %q", proxyWarnHint, logs.String())
		}
	})

	t.Run("a full-length chain reads the client and stays quiet", func(t *testing.T) {
		var logs bytes.Buffer
		h := &handler{opts: Options{TrustProxyHops: 2}, proxyWarn: newProxyWarn(slog.New(slog.NewTextHandler(&logs, nil)))}
		if got := h.clientIP(req("10.0.0.1:5000", "203.0.113.7, 10.0.0.2"), t0); got != "203.0.113.7" {
			t.Errorf("source = %q, want the client 203.0.113.7", got)
		}
		if logs.Len() != 0 {
			t.Errorf("warned on a full-length chain: %q", logs.String())
		}
	})

	t.Run("the warning is rate-limited to once a minute per key", func(t *testing.T) {
		var logs bytes.Buffer
		h := &handler{opts: Options{TrustProxyHops: 0}, proxyWarn: newProxyWarn(slog.New(slog.NewTextHandler(&logs, nil)))}
		h.clientIP(req("198.51.100.9:4321", "203.0.113.1"), t0)
		h.clientIP(req("198.51.100.9:4321", "203.0.113.2"), t0.Add(30*time.Second))
		if n := strings.Count(logs.String(), "level=WARN"); n != 1 {
			t.Errorf("warned %d times within a minute, want 1", n)
		}
		// A new minute warns again.
		h.clientIP(req("198.51.100.9:4321", "203.0.113.3"), t0.Add(61*time.Second))
		if n := strings.Count(logs.String(), "level=WARN"); n != 2 {
			t.Errorf("warned %d times across the minute boundary, want 2", n)
		}
	})

	t.Run("the two warnings keep separate keys", func(t *testing.T) {
		var logs bytes.Buffer
		h := &handler{opts: Options{TrustProxyHops: 0}, proxyWarn: newProxyWarn(slog.New(slog.NewTextHandler(&logs, nil)))}
		h.clientIP(req("198.51.100.9:4321", "203.0.113.1"), t0) // hops=0 branch
		h.opts.TrustProxyHops = 2
		h.clientIP(req("10.0.0.1:5000", "1.2.3.4"), t0) // short-chain branch
		if n := strings.Count(logs.String(), "level=WARN"); n != 2 {
			t.Errorf("distinct misconfigurations warned %d times, want 2", n)
		}
	})
}

// frontendPod is the peer of every sign-in a browser makes: the Route or
// Ingress sends browsers to the UI, never to the backend, and the UI pod
// calls the backend on their behalf.
const frontendPod = "10.128.2.15:41234"

// syncBuffer is a log the handler writes and the test reads.
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

// throughFrontend serves the API as the UI reaches it: every request comes
// from frontendPod, with the X-Forwarded-For the UI passed on as it got it
// (frontend/lib/forwarded-for.ts): the one a proxy in front of the UI wrote
// or, without one, the one Next.js writes with its own peer. It makes the
// administrator "admin" (password "parola-123") and returns a sign-in as
// "admin" that answers with its status, the backend's log and the API's
// address.
func throughFrontend(t *testing.T, hops int) (signIn func(xff, password string) int, logs *syncBuffer, base string) {
	t.Helper()
	logs = &syncBuffer{}
	srv, _ := newTestAPIWith(t, func(o *Options) {
		o.TrustProxyHops = hops
		o.Logger = slog.New(slog.NewTextHandler(logs, nil))
	}) // three failures lock a source, no account ceiling
	post := func(path, xff, body string) int {
		req := httptest.NewRequest("POST", path, strings.NewReader(body))
		req.RemoteAddr = frontendPod
		if xff != "" {
			req.Header.Set("X-Forwarded-For", xff)
		}
		rec := httptest.NewRecorder()
		srv.Config.Handler.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := post("/api/v1/auth/setup", "", `{"username":"admin","password":"parola-123"}`); code != http.StatusOK {
		t.Fatalf("setup: status = %d, want 200", code)
	}
	signIn = func(xff, password string) int {
		return post("/api/v1/auth/login", xff, `{"username":"admin","password":"`+password+`"}`)
	}
	return signIn, logs, srv.URL
}

// The path a browser's sign-in takes: browser, the Route or Ingress, the UI,
// the backend. The backend's peer is the UI pod, the same for every client,
// so the source must come from the X-Forwarded-For the UI passes on: with
// one hop trusted it is the client the proxy in front of the UI saw, and
// one source's guesses lock that source only; with none trusted every
// client is the UI pod, and the backend says so (CWE-307, ADR-0004).
func TestSignInThroughFrontend(t *testing.T) {
	const attacker, owner = "203.0.113.1", "203.0.113.2"

	t.Run("hops=1 keys on the client the proxy in front of the UI saw", func(t *testing.T) {
		signIn, logs, base := throughFrontend(t, 1)
		for i := range 3 {
			if code := signIn(attacker, "yanlis"); code != http.StatusUnauthorized {
				t.Fatalf("attacker guess %d: status = %d, want 401", i+1, code)
			}
		}
		if code := signIn(attacker, "yanlis"); code != http.StatusTooManyRequests {
			t.Errorf("the attacker after three guesses: status = %d, want 429", code)
		}
		if code := signIn(owner, "parola-123"); code != http.StatusOK {
			t.Errorf("the admin from another source through the same UI pod: status = %d, want 200", code)
		}
		got := logs.String()
		if n := strings.Count(got, "source="+attacker); n != 3 {
			t.Errorf("%d sign-in refused lines name the keyed source %s, want 3: %q", n, attacker, got)
		}
		if strings.Contains(got, "source=10.128.2.15") || strings.Contains(got, "level=WARN") {
			t.Errorf("keyed on the UI pod, or warned, with the hops right: %q", got)
		}
		// The source is the operator's log alone: the audit log does not
		// carry it.
		for _, e := range audited(t, base, "login_failed") {
			if strings.Contains(e, attacker) {
				t.Errorf("the audit log names the source: %q", e)
			}
		}
	})

	t.Run("hops=1 ignores what a client put to the left", func(t *testing.T) {
		signIn, logs, _ := throughFrontend(t, 1)
		// A fresh made-up address each guess does not slip the throttle...
		for i := range 3 {
			if code := signIn(fmt.Sprintf("198.51.100.%d, %s", i+1, attacker), "yanlis"); code != http.StatusUnauthorized {
				t.Fatalf("attacker guess %d: status = %d, want 401", i+1, code)
			}
		}
		// ...and the owner's address put to the left is not the owner's.
		if code := signIn(owner+", "+attacker, "yanlis"); code != http.StatusTooManyRequests {
			t.Errorf("the attacker with the owner's address to the left: status = %d, want 429 (its own source is locked)", code)
		}
		if code := signIn(owner, "parola-123"); code != http.StatusOK {
			t.Errorf("the admin: status = %d, want 200 (a forged entry must not lock the owner out)", code)
		}
		got := logs.String()
		if strings.Contains(got, "source=198.51.100.") || strings.Contains(got, "source="+owner) {
			t.Errorf("keyed on an entry a client wrote: %q", got)
		}
		if n := strings.Count(got, "source="+attacker); n != 3 {
			t.Errorf("%d refusals keyed on the attacker's real address, want 3: %q", n, got)
		}
	})

	// The limit README and ADR-0004 document: a hop count above the true one
	// is not the safe side once the UI passes the header on. Behind a plain
	// Route (one entry, the router's, so 1 is right) set to 2, a client that
	// writes an entry of its own lengthens the chain to the hops, and the
	// entry it wrote is read as its source: a fresh one each guess slips the
	// per-source throttle, a victim's address is taken, and none of it
	// warns. Only a sign-in that does not lengthen the header keeps to the
	// UI pod and warns that the chain is short, which is the operator's
	// sign that the value is too high.
	t.Run("hops=2 behind a plain Route lets a lengthened header pick the source", func(t *testing.T) {
		signIn, logs, _ := throughFrontend(t, 2)
		// As the router appends it: what the attacker wrote, then the
		// attacker. Twice the guesses that lock a source, none locked.
		for i := range 6 {
			if code := signIn(fmt.Sprintf("192.0.2.%d, %s", i+1, attacker), "yanlis"); code != http.StatusUnauthorized {
				t.Fatalf("attacker guess %d with a made-up entry: status = %d, want 401 (each counts against the entry)", i+1, code)
			}
		}
		if code := signIn(owner+", "+attacker, "yanlis"); code != http.StatusUnauthorized {
			t.Fatalf("attacker guess with the owner's address: status = %d, want 401", code)
		}
		got := logs.String()
		for i := range 6 {
			if !strings.Contains(got, fmt.Sprintf("source=192.0.2.%d\n", i+1)) {
				t.Errorf("guess %d not keyed on the entry the attacker wrote: %q", i+1, got)
			}
		}
		if !strings.Contains(got, "source="+owner) {
			t.Errorf("the owner's address the attacker wrote was not taken as the source: %q", got)
		}
		if strings.Contains(got, "source="+attacker) || strings.Contains(got, "level=WARN") {
			t.Errorf("keyed on the attacker's real address, or warned, for a lengthened header: %q", got)
		}
		// A browser that writes nothing: the chain is shorter than the hops,
		// so it keys on the UI pod and warns.
		if code := signIn(owner, "yanlis"); code != http.StatusUnauthorized {
			t.Fatalf("a sign-in with the router's entry alone: status = %d, want 401", code)
		}
		got = logs.String()
		if !strings.Contains(got, "fewer entries than UPTIME_TRUST_PROXY_HOPS=2") || !strings.Contains(got, `\"10.128.2.15\"`) {
			t.Errorf("want the xff-hops-too-high WARN naming the UI pod, got %q", got)
		}
		if !strings.Contains(got, "source=10.128.2.15") {
			t.Errorf("a short chain did not key on the UI pod: %q", got)
		}
	})

	t.Run("hops=0 keys on the UI pod and warns", func(t *testing.T) {
		signIn, logs, _ := throughFrontend(t, 0)
		for i := range 3 {
			if code := signIn(attacker, "yanlis"); code != http.StatusUnauthorized {
				t.Fatalf("attacker guess %d: status = %d, want 401", i+1, code)
			}
		}
		// Every client is the UI pod: the attacker's guesses lock the owner
		// out too, which is what the warning is for.
		if code := signIn(owner, "parola-123"); code != http.StatusTooManyRequests {
			t.Errorf("the admin from another source: status = %d, want 429 (one shared lock on the UI pod)", code)
		}
		got := logs.String()
		if !strings.Contains(got, "level=WARN") || !strings.Contains(got, "UPTIME_TRUST_PROXY_HOPS=0") ||
			!strings.Contains(got, `\"10.128.2.15\"`) {
			t.Errorf("want the xff-hops-zero WARN naming the UI pod, got %q", got)
		}
		// The operator reads the value to set here: a proxy count would
		// key a GCE Ingress on the load balancer, shared and unwarned.
		if !strings.Contains(got, proxyWarnHint) {
			t.Errorf("want the WARN to ask for %q, got %q", proxyWarnHint, got)
		}
		if n := strings.Count(got, "level=WARN"); n != 1 {
			t.Errorf("warned %d times within a minute, want 1", n)
		}
		if n := strings.Count(got, "source=10.128.2.15"); n != 3 {
			t.Errorf("%d sign-in refused lines name the UI pod they keyed on, want 3: %q", n, got)
		}
	})
}
