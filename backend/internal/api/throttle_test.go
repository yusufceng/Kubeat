package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/auth"
	"github.com/yusufceng/Kubeat/backend/internal/metrics"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// fromUI is a request as the UI sends it: from peer, with the UI's token
// and the X-Forwarded-For lines given.
func fromUI(peer string, xff ...string) *http.Request {
	r := &http.Request{RemoteAddr: peer, Header: http.Header{}}
	r.Header.Set(frontendTokenHeader, testFrontendToken)
	for _, v := range xff {
		r.Header.Add("X-Forwarded-For", v)
	}
	return r
}

// clientIP reads the source the throttle keys on: the peer without a
// trusted proxy, and the right entry of X-Forwarded-For with one, ignoring
// whatever a client put to the left of the hops it trusts. When the
// forwarded chain is shorter than the trusted hops the leftmost is a
// client's to set, so clientIP falls back to the peer rather than trust it.
// These requests come from the UI, the one peer whose header is read.
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
		{name: "an IPv6 client", hops: 1, peer: "[fd00::15]:41234", xff: []string{"2001:db8::7"}, want: "2001:db8::7"},
		{name: "fewer entries than hops falls back to the peer, not the leftmost", hops: 3, peer: "10.0.0.1:5000", xff: []string{"203.0.113.7"}, want: "10.0.0.1"},
		{name: "a hop but no header falls back to the peer", hops: 1, peer: "10.0.0.1:5000", want: "10.0.0.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &handler{opts: Options{TrustProxyHops: tc.hops, FrontendToken: testFrontendToken}}
			if got := h.clientIP(fromUI(tc.peer, tc.xff...), signInSurface, time.Now()); got != tc.want {
				t.Errorf("clientIP(hops=%d, peer=%q, xff=%v) = %q, want %q", tc.hops, tc.peer, tc.xff, got, tc.want)
			}
		})
	}
}

// attackerPod is a pod of another tenant that reaches the backend's Service
// directly, never through the UI.
const attackerPod = "10.128.5.9:39000"

// forgedXFF are the forms an X-Forwarded-For may take, each made up by a
// peer that is not the UI: none of them may name the source its sign-ins
// count against.
var forgedXFF = []struct {
	name  string
	lines []string
}{
	{"one address", []string{"203.0.113.7"}},
	{"a comma-separated list", []string{"198.51.100.1, 203.0.113.7"}},
	{"a list without spaces", []string{"198.51.100.1,198.51.100.2,203.0.113.7"}},
	{"several header lines", []string{"198.51.100.1", "203.0.113.7"}},
	{"an IPv6 address", []string{"2001:db8::7"}},
	{"a bracketed IPv6 address with a port", []string{"[2001:db8::7]:4711"}},
	{"an address with a port", []string{"203.0.113.7:4711"}},
	{"garbage", []string{"not-an-address"}},
	{"empty entries", []string{", ,", " "}},
	{"an entry far too long", []string{strings.Repeat("9", 4096)}},
}

// The ways a peer may fail to prove it is the UI: no token, a wrong one, a
// near miss, or the right one twice or beside a wrong one, which the UI
// never sends.
var notTheUI = []struct {
	name   string
	tokens []string
}{
	{"no token", nil},
	{"a wrong token", []string{"guess"}},
	{"an empty token", []string{""}},
	{"the token's prefix", []string{testFrontendToken[:len(testFrontendToken)-1]}},
	{"the token with a byte more", []string{testFrontendToken + "x"}},
	{"the token in another case", []string{strings.ToUpper(testFrontendToken)}},
	{"the token beside a wrong one", []string{"guess", testFrontendToken}},
	{"the token twice", []string{testFrontendToken, testFrontendToken}},
}

// Any pod in the cluster can reach the backend's Service: with hops above 0
// one that is not the UI keys on its own address whatever X-Forwarded-For
// it writes, in every form, and whatever it presents for the UI's token
// (CWE-307: it cannot spread its guesses over made-up sources, nor take a
// victim's address).
func TestClientIPUntrustedPeerKeysOnThePeer(t *testing.T) {
	for _, hops := range []int{1, 2} {
		for _, who := range notTheUI {
			for _, form := range forgedXFF {
				t.Run(fmt.Sprintf("hops=%d, %s, %s", hops, who.name, form.name), func(t *testing.T) {
					h := &handler{opts: Options{TrustProxyHops: hops, FrontendToken: testFrontendToken}}
					r := &http.Request{RemoteAddr: attackerPod, Header: http.Header{}}
					for _, v := range who.tokens {
						r.Header.Add(frontendTokenHeader, v)
					}
					for _, v := range form.lines {
						r.Header.Add("X-Forwarded-For", v)
					}
					if got := h.clientIP(r, signInSurface, time.Now()); got != "10.128.5.9" {
						t.Errorf("source = %q, want the peer 10.128.5.9", got)
					}
				})
			}
		}
	}
	// The same from an IPv6 peer: the port goes, the address stays.
	h := &handler{opts: Options{TrustProxyHops: 1, FrontendToken: testFrontendToken}}
	r := &http.Request{RemoteAddr: "[fd00::9]:39000", Header: http.Header{"X-Forwarded-For": {"203.0.113.7"}}}
	if got := h.clientIP(r, signInSurface, time.Now()); got != "fd00::9" {
		t.Errorf("an IPv6 peer: source = %q, want fd00::9", got)
	}
}

// proxyWarnHint is the value both X-Forwarded-For warnings ask for: README's
// proxy-hop guide counts the entries the proxies in front of the UI write
// (1 with none), not the proxies, since the UI passes the header on and
// takes the place of the proxy nearest to the backend.
const proxyWarnHint = "the X-Forwarded-For entries your proxies in front of the UI write, 1 with none"

// A published source-address throttle must not go silently ineffective, so
// clientIP warns when X-Forwarded-For cannot be trusted: an X-Forwarded-For
// arriving while the header is trusted none (hops 0), a sign-in from a peer
// that is not the UI, or with no token to tell the UI by, while hops are
// trusted, and a forwarded chain shorter than the trusted hops. No warning
// names the token, nor what a request presented for it.
func TestClientIPWarns(t *testing.T) {
	req := func(peer string, xff ...string) *http.Request {
		r := &http.Request{RemoteAddr: peer, Header: http.Header{}}
		for _, v := range xff {
			r.Header.Add("X-Forwarded-For", v)
		}
		return r
	}
	logged := func(opts Options) (*handler, *bytes.Buffer) {
		var logs bytes.Buffer
		return &handler{opts: opts, proxyWarn: newProxyWarn(slog.New(slog.NewTextHandler(&logs, nil)))}, &logs
	}
	t0 := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

	t.Run("hops=0 and a header warns and keeps the peer", func(t *testing.T) {
		h, logs := logged(Options{TrustProxyHops: 0})
		if got := h.clientIP(req("198.51.100.9:4321", "203.0.113.1"), signInSurface, t0); got != "198.51.100.9" {
			t.Errorf("source = %q, want the peer 198.51.100.9", got)
		}
		if !strings.Contains(logs.String(), "UPTIME_TRUST_PROXY_HOPS=0") || !strings.Contains(logs.String(), "level=WARN") {
			t.Errorf("want a WARN naming the setting, got %q", logs.String())
		}
	})

	t.Run("hops=0 without a header stays quiet", func(t *testing.T) {
		h, logs := logged(Options{TrustProxyHops: 0})
		h.clientIP(req("198.51.100.9:4321"), signInSurface, t0)
		if logs.Len() != 0 {
			t.Errorf("warned with no X-Forwarded-For: %q", logs.String())
		}
	})

	t.Run("a peer that is not the UI warns and keys on the peer", func(t *testing.T) {
		h, logs := logged(Options{TrustProxyHops: 1, FrontendToken: testFrontendToken})
		r := req(attackerPod, "203.0.113.7")
		r.Header.Set(frontendTokenHeader, "a-guess-at-the-token")
		if got := h.clientIP(r, signInSurface, t0); got != "10.128.5.9" {
			t.Errorf("source = %q, want the peer 10.128.5.9", got)
		}
		got := logs.String()
		if !strings.Contains(got, "level=WARN") || !strings.Contains(got, "did not present the UI's X-Frontend-Token") ||
			!strings.Contains(got, "UPTIME_TRUST_PROXY_HOPS=1") || !strings.Contains(got, `\"10.128.5.9\"`) ||
			!strings.Contains(got, "it came with one, ignored") {
			t.Errorf("want the xff-untrusted-peer WARN naming the hops and the peer, got %q", got)
		}
		// The secret stays out of the log, and so does what was presented
		// for it, which may be a near miss.
		if strings.Contains(got, testFrontendToken) || strings.Contains(got, "a-guess-at-the-token") {
			t.Errorf("the WARN names a token: %q", got)
		}
	})

	t.Run("a sign-in sent to the backend directly warns that it came without a header", func(t *testing.T) {
		h, logs := logged(Options{TrustProxyHops: 1, FrontendToken: testFrontendToken})
		if got := h.clientIP(req("10.128.7.3:51000"), signInSurface, t0); got != "10.128.7.3" {
			t.Errorf("source = %q, want the peer 10.128.7.3", got)
		}
		if got := logs.String(); !strings.Contains(got, "did not present the UI's X-Frontend-Token") ||
			!strings.Contains(got, "it came with none") {
			t.Errorf("want the xff-untrusted-peer WARN saying the request had no X-Forwarded-For, got %q", got)
		}
	})

	t.Run("hops above 0 without a frontend token trusts no peer and warns", func(t *testing.T) {
		h, logs := logged(Options{TrustProxyHops: 1})
		// Whatever the request presents: there is nothing to compare it with.
		for _, presented := range []string{"", testFrontendToken} {
			r := req(frontendPod, "203.0.113.7")
			if presented != "" {
				r.Header.Set(frontendTokenHeader, presented)
			}
			if got := h.clientIP(r, signInSurface, t0); got != "10.128.2.15" {
				t.Errorf("presenting %q: source = %q, want the peer 10.128.2.15", presented, got)
			}
		}
		got := logs.String()
		if !strings.Contains(got, "UPTIME_TRUST_PROXY_HOPS=1 but UPTIME_FRONTEND_TOKEN is not set") ||
			!strings.Contains(got, `\"10.128.2.15\"`) {
			t.Errorf("want the xff-no-frontend-token WARN naming the peer, got %q", got)
		}
		if n := strings.Count(got, "level=WARN"); n != 1 {
			t.Errorf("warned %d times within a minute, want 1", n)
		}
		if strings.Contains(got, testFrontendToken) {
			t.Errorf("the WARN names the presented token: %q", got)
		}
	})

	t.Run("the UI without X-Forwarded-For keys on the peer and warns", func(t *testing.T) {
		h, logs := logged(Options{TrustProxyHops: 1, FrontendToken: testFrontendToken})
		if got := h.clientIP(fromUI(frontendPod), signInSurface, t0); got != "10.128.2.15" {
			t.Errorf("source = %q, want the peer 10.128.2.15", got)
		}
		if got := logs.String(); !strings.Contains(got, "fewer entries than UPTIME_TRUST_PROXY_HOPS=1") {
			t.Errorf("want the xff-hops-too-high WARN, got %q", got)
		}
	})

	t.Run("a chain shorter than hops warns and falls back to the peer", func(t *testing.T) {
		h, logs := logged(Options{TrustProxyHops: 2, FrontendToken: testFrontendToken})
		// Only one forwarded entry, which a client could have set, so the
		// source must not be read from it: a forged X-Forwarded-For must
		// neither slip the throttle nor lock a victim out.
		if got := h.clientIP(fromUI("10.0.0.1:5000", "1.2.3.4"), signInSurface, t0); got != "10.0.0.1" {
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
		if strings.Contains(logs.String(), testFrontendToken) {
			t.Errorf("the WARN names the token: %q", logs.String())
		}
	})

	t.Run("a full-length chain from the UI reads the client and stays quiet", func(t *testing.T) {
		h, logs := logged(Options{TrustProxyHops: 2, FrontendToken: testFrontendToken})
		if got := h.clientIP(fromUI("10.0.0.1:5000", "203.0.113.7, 10.0.0.2"), signInSurface, t0); got != "203.0.113.7" {
			t.Errorf("source = %q, want the client 203.0.113.7", got)
		}
		if logs.Len() != 0 {
			t.Errorf("warned on a full-length chain: %q", logs.String())
		}
	})

	t.Run("the warning is rate-limited to once a minute per key", func(t *testing.T) {
		h, logs := logged(Options{TrustProxyHops: 0})
		h.clientIP(req("198.51.100.9:4321", "203.0.113.1"), signInSurface, t0)
		h.clientIP(req("198.51.100.9:4321", "203.0.113.2"), signInSurface, t0.Add(30*time.Second))
		if n := strings.Count(logs.String(), "level=WARN"); n != 1 {
			t.Errorf("warned %d times within a minute, want 1", n)
		}
		// A new minute warns again.
		h.clientIP(req("198.51.100.9:4321", "203.0.113.3"), signInSurface, t0.Add(61*time.Second))
		if n := strings.Count(logs.String(), "level=WARN"); n != 2 {
			t.Errorf("warned %d times across the minute boundary, want 2", n)
		}
	})

	t.Run("a flood from a peer that is not the UI warns once a minute", func(t *testing.T) {
		h, logs := logged(Options{TrustProxyHops: 1, FrontendToken: testFrontendToken})
		for i := range 100 {
			h.clientIP(req(attackerPod, fmt.Sprintf("198.51.100.%d", i)), signInSurface, t0.Add(time.Duration(i)*100*time.Millisecond))
		}
		if n := strings.Count(logs.String(), "level=WARN"); n != 1 {
			t.Errorf("warned %d times within a minute, want 1", n)
		}
	})

	t.Run("the warnings keep separate keys", func(t *testing.T) {
		h, logs := logged(Options{TrustProxyHops: 0, FrontendToken: testFrontendToken})
		h.clientIP(req("198.51.100.9:4321", "203.0.113.1"), signInSurface, t0) // hops=0 branch
		h.opts.TrustProxyHops = 2
		h.clientIP(fromUI("10.0.0.1:5000", "1.2.3.4"), signInSurface, t0) // short-chain branch
		h.clientIP(req(attackerPod, "203.0.113.7"), signInSurface, t0)    // untrusted-peer branch
		h.opts.FrontendToken = ""
		h.clientIP(req(frontendPod, "203.0.113.7"), signInSurface, t0) // no-token branch
		if n := strings.Count(logs.String(), "level=WARN"); n != 4 {
			t.Errorf("distinct misconfigurations warned %d times, want 4", n)
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

// serveFrom answers a POST to path with body, sent from peer: with token in
// X-Frontend-Token unless it is empty, and with xff as X-Forwarded-For
// unless it is empty. It returns the answer's status.
func serveFrom(srv *httptest.Server, peer, token, xff, path, body string) int {
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.RemoteAddr = peer
	if token != "" {
		req.Header.Set(frontendTokenHeader, token)
	}
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	rec := httptest.NewRecorder()
	srv.Config.Handler.ServeHTTP(rec, req)
	return rec.Code
}

// throughFrontend serves the API as the UI reaches it: every request comes
// from frontendPod with the UI's token (frontend/lib/frontend-token.ts), and
// with the X-Forwarded-For the UI passed on as it got it
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
		return serveFrom(srv, frontendPod, testFrontendToken, xff, path, body)
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

// The attack the trusted peer closes: a pod of another tenant reaches the
// backend's Service directly and writes a fresh X-Forwarded-For with every
// guess. Before, each guess counted against a made-up source of its own, so
// one pod slipped the per-source throttle and filled the name's account
// ceiling, locking the administrator out for everyone. Now its guesses key
// on its own address: the per-source throttle stops it at
// UPTIME_LOGIN_MAX_FAILURES, far below the ceiling, and the administrator
// signs in through the UI (CWE-307, ADR-0004). The defaults are the
// backend's own: ten per source, fifty per name.
func TestSignInUntrustedPeerCannotFillTheAccountCeiling(t *testing.T) {
	for _, who := range []struct {
		name, token string
	}{{"without the UI's token", ""}, {"with a wrong token", "a-guess"}} {
		t.Run(who.name, func(t *testing.T) {
			meter := metrics.New("test")
			logs := &syncBuffer{}
			srv, _ := newTestAPIWith(t, func(o *Options) {
				o.TrustProxyHops = 1
				o.Throttle = &auth.Throttle{MaxFailures: 10, Lockout: time.Minute}
				o.AccountThrottle = &auth.Throttle{MaxFailures: 50, Lockout: time.Minute}
				o.SignIns = meter
				o.Logger = slog.New(slog.NewTextHandler(logs, nil))
			})
			const login = "/api/v1/auth/login"
			if code := serveFrom(srv, frontendPod, testFrontendToken, "", "/api/v1/auth/setup",
				`{"username":"admin","password":"parola-123"}`); code != http.StatusOK {
				t.Fatalf("setup: status = %d, want 200", code)
			}
			// Sixty guesses, more than the ceiling, each claiming a source
			// of its own.
			for i := range 60 {
				want := http.StatusUnauthorized
				if i >= 10 {
					want = http.StatusTooManyRequests
				}
				xff := fmt.Sprintf("198.51.%d.%d", i/250, i%250+1)
				if code := serveFrom(srv, attackerPod, who.token, xff, login, `{"username":"admin","password":"yanlis"}`); code != want {
					t.Fatalf("guess %d from the pod: status = %d, want %d", i+1, code, want)
				}
			}
			// The administrator, through the UI from a source of their own.
			if code := serveFrom(srv, frontendPod, testFrontendToken, "203.0.113.2", login,
				`{"username":"admin","password":"parola-123"}`); code != http.StatusOK {
				t.Errorf("the admin through the UI: status = %d, want 200 (the pod must not reach the account ceiling)", code)
			}
			got, err := meter.Gather()
			if err != nil {
				t.Fatal(err)
			}
			if got["kubeat_signin_throttled_total{kind=account}"] != 0 {
				t.Errorf("kind=account = %v, want 0", got["kubeat_signin_throttled_total{kind=account}"])
			}
			if got["kubeat_signin_throttled_total{kind=source}"] != 50 {
				t.Errorf("kind=source = %v, want 50", got["kubeat_signin_throttled_total{kind=source}"])
			}
			text := logs.String()
			if n := strings.Count(text, "source=10.128.5.9"); n != 10 {
				t.Errorf("%d sign-in refused lines name the pod's address, want 10: %q", n, text)
			}
			if strings.Contains(text, "source=198.51.") {
				t.Errorf("keyed on an address the pod wrote: %q", text)
			}
			if !strings.Contains(text, "did not present the UI's X-Frontend-Token") {
				t.Errorf("want the xff-untrusted-peer WARN, got %q", text)
			}
			if strings.Contains(text, testFrontendToken) || strings.Contains(text, "a-guess") {
				t.Errorf("the log names a token: %q", text)
			}
		})
	}
}

// fakeClock is the handler's clock in a test (Options.now), moved by hand,
// so that a lockout runs out without the test waiting for it.
type fakeClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
}

// guessLockout is the lockout period of the throttled-guess tests, the
// backend's default.
const guessLockout = 15 * time.Minute

// throttledAPI serves the API on a fake clock starting at a fixed time, with
// three failures locking a source for guessLockout, ceiling failures of a
// page or an account from all sources locking it for everyone (zero: no
// ceiling), one X-Forwarded-For entry trusted from the UI, and the options
// then changed by customize unless it is nil. It returns the server, the
// store, the clock and the backend's log.
func throttledAPI(t *testing.T, ceiling int, customize func(*Options)) (*httptest.Server, *store.SQLite, *fakeClock, *syncBuffer) {
	t.Helper()
	clock := &fakeClock{at: time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)}
	logs := &syncBuffer{}
	srv, db := newTestAPIWith(t, func(o *Options) {
		o.now = clock.now
		o.TrustProxyHops = 1
		o.Throttle = &auth.Throttle{MaxFailures: 3, Lockout: guessLockout}
		if ceiling > 0 {
			o.AccountThrottle = &auth.Throttle{MaxFailures: ceiling, Lockout: guessLockout}
		}
		o.Logger = slog.New(slog.NewTextHandler(logs, nil))
		if customize != nil {
			customize(o)
		}
	})
	return srv, db, clock, logs
}

// fromBrowserAt sends a request to the API as the UI sends it on behalf of a
// browser at source: from the UI pod, with the UI's token and with source as
// the X-Forwarded-For the proxy in front of the UI wrote, and with bearer as
// the session unless it is empty.
func fromBrowserAt(srv *httptest.Server, source, bearer, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = frontendPod
	req.Header.Set(frontendTokenHeader, testFrontendToken)
	req.Header.Set("X-Forwarded-For", source)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	srv.Config.Handler.ServeHTTP(rec, req)
	return rec
}

// A guarded is a password other than a sign-in's that the throttle guards
// per source, as a browser tries it: try sends password from source. A
// wrong one is answered with wrong, the right one, password, with right;
// refused is the log line a wrong one leaves.
type guarded struct {
	try                  func(source, password string) *httptest.ResponseRecorder
	password             string
	wrong, right         int
	refused, surfaceName string
}

// The two guarded passwords, each made on a fresh server by its make.
var guardedPasswords = []struct {
	name string
	make func(t *testing.T, srv *httptest.Server, db *store.SQLite) guarded
}{
	{"status-page unlock", statusPagePassword},
	{"password change", currentPassword},
}

// statusPagePassword publishes the page "gizli" with a password and returns
// its unlock.
func statusPagePassword(t *testing.T, srv *httptest.Server, db *store.SQLite) guarded {
	t.Helper()
	web := mkMonitor(t, db, "web", "", "", true)
	if code := do(t, "POST", srv.URL+"/api/v1/status-pages", `{"slug":"gizli","title":"Gizli sayfa","monitors":[{"monitor_id":`+
		itoa(web.ID)+`}],"password":"gizli-parola-1"}`, nil); code != http.StatusCreated {
		t.Fatalf("create the page: status = %d, want 201", code)
	}
	return guarded{
		try: func(source, password string) *httptest.ResponseRecorder {
			return fromBrowserAt(srv, source, "", "POST", "/api/v1/status-pages/by-slug/gizli/unlock", `{"password":"`+password+`"}`)
		},
		password: "gizli-parola-1", wrong: http.StatusUnauthorized, right: http.StatusOK,
		refused: "status page unlock refused", surfaceName: statusPageSurface.name,
	}
}

// currentPassword makes the administrator "admin" and returns the change of
// admin's password, asked for the current one, with admin's session: the
// same session from every source, as one who stole it holds it. The right
// password is changed to itself, so that it stays right.
func currentPassword(t *testing.T, srv *httptest.Server, _ *store.SQLite) guarded {
	t.Helper()
	rec := fromBrowserAt(srv, "203.0.113.200", "", "POST", "/api/v1/auth/setup", `{"username":"admin","password":"parola-123"}`)
	var sess sessionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &sess); rec.Code != http.StatusOK || err != nil || sess.Token == "" {
		t.Fatalf("setup: status = %d, body = %q", rec.Code, rec.Body.String())
	}
	return guarded{
		try: func(source, password string) *httptest.ResponseRecorder {
			return fromBrowserAt(srv, source, sess.Token, "POST", "/api/v1/auth/password",
				`{"current_password":"`+password+`","new_password":"parola-123"}`)
		},
		password: "parola-123", wrong: http.StatusBadRequest, right: http.StatusNoContent,
		refused: "password change refused", surfaceName: passwordSurface.name,
	}
}

// wantThrottled fails unless rec is the throttle's answer for a lock that
// lasts left: 429, Retry-After in whole seconds and the very body any lock
// of that length gets, whichever key it is (enum-safe, #78).
func wantThrottled(t *testing.T, what string, rec *httptest.ResponseRecorder, left time.Duration) {
	t.Helper()
	want := httptest.NewRecorder()
	tooManyAttempts(want, left)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != want.Header().Get("Retry-After") ||
		rec.Body.String() != want.Body.String() {
		t.Errorf("%s: %d, Retry-After %q, body %q; want 429, Retry-After %q, body %q", what, rec.Code,
			rec.Header().Get("Retry-After"), rec.Body.String(), want.Header().Get("Retry-After"), want.Body.String())
	}
}

// One source's wrong guesses at a status page's password, or at the
// current password asked for to change an account's, lock that source
// only: a visitor from another source opens the page, the owner from
// another source changes the password, while the guesser is refused with
// 429 and Retry-After until the lockout runs out (CWE-307, ADR-0004). Before
// 0.51 the lock was the page's and the account's, for everyone.
func TestGuessThrottledPerSource(t *testing.T) {
	const attacker, owner = "203.0.113.1", "203.0.113.2"
	for _, tc := range guardedPasswords {
		t.Run(tc.name, func(t *testing.T) {
			srv, db, clock, logs := throttledAPI(t, 0, nil)
			g := tc.make(t, srv, db)
			for i := range 3 {
				if rec := g.try(attacker, "yanlis-parola"); rec.Code != g.wrong {
					t.Fatalf("attacker guess %d: status = %d, want %d", i+1, rec.Code, g.wrong)
				}
			}
			wantThrottled(t, "the attacker's fourth guess", g.try(attacker, "yanlis-parola"), guessLockout)
			wantThrottled(t, "the attacker with the right password", g.try(attacker, g.password), guessLockout)
			if rec := g.try(owner, g.password); rec.Code != g.right {
				t.Errorf("the right password from another source: status = %d, want %d (one source locks itself only)", rec.Code, g.right)
			}
			// Another source's success does not lift the guesser's lock.
			clock.advance(5 * time.Minute)
			wantThrottled(t, "the attacker five minutes on", g.try(attacker, g.password), guessLockout-5*time.Minute)
			clock.advance(guessLockout - 5*time.Minute)
			if rec := g.try(attacker, g.password); rec.Code != g.right {
				t.Errorf("the attacker's source once the lockout ran out: status = %d, want %d", rec.Code, g.right)
			}
			got := logs.String()
			if n := strings.Count(got, `msg="`+g.refused+`"`); n != 3 {
				t.Errorf("%d %q lines, want 3: %q", n, g.refused, got)
			}
			if n := strings.Count(got, "source="+attacker); n != 3 {
				t.Errorf("%d refusals name the keyed source %s, want 3: %q", n, attacker, got)
			}
			if strings.Contains(got, "source=10.128.2.15") || strings.Contains(got, "level=WARN") {
				t.Errorf("keyed on the UI pod, or warned, with the hops right: %q", got)
			}
		})
	}
}

// The loose ceiling of the sign-in (UPTIME_LOGIN_ACCOUNT_MAX_FAILURES)
// holds a page's and an account's failures from all sources, so a guess
// spread over many sources cannot run forever: past it the page or the
// account is locked for everyone, the right password from a fresh source
// included, with the same 429 as a source's lock, until the lockout runs
// out (the cost ADR-0004 accepts).
func TestGuessCeilingAcrossSources(t *testing.T) {
	const ceiling = 5
	for _, tc := range guardedPasswords {
		t.Run(tc.name, func(t *testing.T) {
			srv, db, clock, _ := throttledAPI(t, ceiling, nil)
			g := tc.make(t, srv, db)
			for i := range ceiling {
				if rec := g.try(fmt.Sprintf("198.51.100.%d", i+1), "yanlis-parola"); rec.Code != g.wrong {
					t.Fatalf("guess %d from a source of its own: status = %d, want %d", i+1, rec.Code, g.wrong)
				}
			}
			wantThrottled(t, "a guess from a fresh source past the ceiling", g.try("198.51.100.99", "yanlis-parola"), guessLockout)
			wantThrottled(t, "the right password from a fresh source past the ceiling", g.try("203.0.113.2", g.password), guessLockout)
			clock.advance(guessLockout)
			if rec := g.try("203.0.113.2", g.password); rec.Code != g.right {
				t.Errorf("the right password once the lockout ran out: status = %d, want %d", rec.Code, g.right)
			}
		})
	}
	// Without the ceiling (0) the same spread guess is held per source only.
	for _, tc := range guardedPasswords {
		t.Run(tc.name+" without a ceiling", func(t *testing.T) {
			srv, db, _, _ := throttledAPI(t, 0, nil)
			g := tc.make(t, srv, db)
			for i := range 2 * ceiling {
				if rec := g.try(fmt.Sprintf("198.51.100.%d", i+1), "yanlis-parola"); rec.Code != g.wrong {
					t.Fatalf("guess %d from a source of its own: status = %d, want %d", i+1, rec.Code, g.wrong)
				}
			}
			if rec := g.try("203.0.113.2", g.password); rec.Code != g.right {
				t.Errorf("the right password: status = %d, want %d", rec.Code, g.right)
			}
		})
	}
}

// A right password forgets the failures before it under the ceiling as a
// sign-in's does: the visitors of a page share its password, and their typos
// while many open it at once, in an incident, must not add up to the
// ceiling and lock the page for all of them.
func TestGuessRightPasswordClearsTheCeiling(t *testing.T) {
	const ceiling = 5
	for _, tc := range guardedPasswords {
		t.Run(tc.name, func(t *testing.T) {
			srv, db, _, _ := throttledAPI(t, ceiling, nil)
			g := tc.make(t, srv, db)
			visitor := 0
			for round := range 4 {
				for range ceiling - 1 {
					visitor++
					if rec := g.try(fmt.Sprintf("198.51.100.%d", visitor), "yanlis-parola"); rec.Code != g.wrong {
						t.Fatalf("round %d, typo of visitor %d: status = %d, want %d", round+1, visitor, rec.Code, g.wrong)
					}
				}
				visitor++
				if rec := g.try(fmt.Sprintf("198.51.100.%d", visitor), g.password); rec.Code != g.right {
					t.Fatalf("round %d, the right password: status = %d, want %d", round+1, rec.Code, g.right)
				}
			}
		})
	}
}

// A request for a page that is not there, or not published, is no guess:
// it is taken back from its source and from the ceiling, so it neither
// locks anyone nor counts toward the page's ceiling.
func TestStatusPageUnknownIsNoGuess(t *testing.T) {
	srv, db, _, _ := throttledAPI(t, 5, nil)
	g := statusPagePassword(t, srv, db)
	for i := range 10 {
		if rec := fromBrowserAt(srv, "203.0.113.1", "", "POST", "/api/v1/status-pages/by-slug/nope/unlock",
			`{"password":"x"}`); rec.Code != http.StatusNotFound {
			t.Fatalf("unknown page, request %d: status = %d, want 404", i+1, rec.Code)
		}
	}
	if rec := g.try("203.0.113.1", g.password); rec.Code != g.right {
		t.Errorf("a real page from the same source: status = %d, want %d", rec.Code, g.right)
	}
}

// With the hops wrong the throttle cannot tell sources apart, and both
// surfaces fall back to one shared lock, as the sign-in does: every browser
// keys on the UI pod and one source's guesses lock the page, or the
// account's password change, for everyone. The backend warns, naming the
// surface and the lock it fell back to.
func TestGuessSharedLockWhenHopsWrong(t *testing.T) {
	const attacker, owner = "203.0.113.1", "203.0.113.2"
	for _, wrong := range []struct {
		name      string
		customize func(*Options)
		warn      string
	}{
		{"hops=0 behind a proxy", func(o *Options) { o.TrustProxyHops = 0 }, "but UPTIME_TRUST_PROXY_HOPS=0"},
		{"hops above the chain", func(o *Options) { o.TrustProxyHops = 2 }, "fewer entries than UPTIME_TRUST_PROXY_HOPS=2"},
		{"no frontend token", func(o *Options) { o.FrontendToken = "" }, "UPTIME_FRONTEND_TOKEN is not set"},
	} {
		for _, tc := range guardedPasswords {
			t.Run(wrong.name+", "+tc.name, func(t *testing.T) {
				srv, db, _, logs := throttledAPI(t, 0, wrong.customize)
				g := tc.make(t, srv, db)
				for i := range 3 {
					if rec := g.try(attacker, "yanlis-parola"); rec.Code != g.wrong {
						t.Fatalf("attacker guess %d: status = %d, want %d", i+1, rec.Code, g.wrong)
					}
				}
				wantThrottled(t, "the right password from another source", g.try(owner, g.password), guessLockout)
				got := logs.String()
				if !strings.Contains(got, "level=WARN") || !strings.Contains(got, wrong.warn) ||
					!strings.Contains(got, "the "+g.surfaceName+" throttle") || !strings.Contains(got, `\"10.128.2.15\"`) {
					t.Errorf("want a WARN naming %q, the %s throttle and the UI pod, got %q", wrong.warn, g.surfaceName, got)
				}
				if strings.Contains(got, "sign-in") {
					t.Errorf("the WARN names the sign-in for a %s: %q", g.surfaceName, got)
				}
				shared := map[string]string{statusPageSurface.name: statusPageSurface.shared, passwordSurface.name: passwordSurface.shared}[g.surfaceName]
				if !strings.Contains(got, "one shared "+shared+" lock") {
					t.Errorf("the WARN does not name the shared %s lock it fell back to: %q", shared, got)
				}
				if n := strings.Count(got, "source=10.128.2.15"); n != 3 {
					t.Errorf("%d refusals name the UI pod they keyed on, want 3: %q", n, got)
				}
			})
		}
	}
}

// The three surfaces share the warnings' once-a-minute limit: the visitors of
// a status page add no lines to those the sign-ins leave. Each line names
// the surface of the request that met the misconfiguration.
func TestGuessWarningsShareTheMinute(t *testing.T) {
	srv, db, clock, logs := throttledAPI(t, 0, func(o *Options) { o.TrustProxyHops = 0 })
	page := statusPagePassword(t, srv, db)
	change := currentPassword(t, srv, db)
	page.try("203.0.113.1", "yanlis-parola")
	fromBrowserAt(srv, "203.0.113.1", "", "POST", "/api/v1/auth/login", `{"username":"admin","password":"yanlis-parola"}`)
	change.try("203.0.113.1", "yanlis-parola")
	got := logs.String()
	if n := strings.Count(got, "level=WARN"); n != 1 || !strings.Contains(got, "seen on a status-page unlock") ||
		!strings.Contains(got, "one shared per-page lock") {
		t.Errorf("want one WARN within the minute, naming the status-page unlock, got %d: %q", n, got)
	}
	clock.advance(61 * time.Second)
	change.try("203.0.113.1", "yanlis-parola")
	got = logs.String()
	if n := strings.Count(got, "level=WARN"); n != 2 || !strings.Contains(got, "seen on a password change") ||
		!strings.Contains(got, "one shared per-account lock") {
		t.Errorf("want a second WARN a minute on, naming the password change, got %d: %q", n, got)
	}
}
