package api

import (
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// frontendTokenHeader carries the secret by which the UI proves it is the
// UI (Options.FrontendToken, UPTIME_FRONTEND_TOKEN): only a request that
// carries it may name its client in X-Forwarded-For.
const frontendTokenHeader = "X-Frontend-Token"

// clientIP is the address a sign-in comes from. It keys the sign-in
// throttle (see authLogin) so that one source's failed guesses throttle
// only that source, not the username for everyone: this is what keeps a
// known name, such as the ever-present "admin", from being locked out of a
// public login by anyone who knows it (CWE-307, ADR-0004).
//
// With TrustProxyHops 0 (the default) it is the peer that connected;
// X-Forwarded-For is not trusted, since any client may send one. Above 0
// the client is read from X-Forwarded-For, trusting only the rightmost
// TrustProxyHops entries, which the proxies in front appended, and ignoring
// whatever a client put to their left. Behind an ingress or a load balancer
// the peer is the proxy, the same address for every client, so
// TrustProxyHops must be set or the throttle cannot tell sources apart and
// falls back to one shared, per-name lock (README, "Hesaplar ve giriş").
//
// Browsers never reach the backend themselves: the Route or Ingress sends
// them to the UI, whose pod is then the peer here for every client. The UI
// passes on the X-Forwarded-For it got, as it got it, with the throttled
// calls (sign-in, password change, status-page unlock), and Next.js writes
// its own peer there when no proxy did. So the UI takes the place of the
// proxy nearest to us, and the hops count the entries the proxies in front
// of the UI write to the header, or 1 when none does (README, "Hesaplar ve
// giriş", has them per topology). That 1 holds only while no untrusted
// client reaches the UI but through such a proxy (a port-forward): Next.js
// keeps a header a client sent, so with the UI's Service exposed directly
// the hops stay 0, which costs one shared lock rather than a bypass.
//
// The header is read only from the UI. Any pod in the cluster can reach
// the backend's Service, and pod addresses come from one network, so no
// address tells the UI from another pod: the UI presents a secret of its
// own instead (fromFrontend). A request without it, a script's or a pod's
// with a header it wrote itself, keys on its peer whatever the hops, so a
// single pod cannot spread its guesses over made-up sources to slip the
// per-source throttle or fill a name's account ceiling and lock the owner
// out.
//
// A published source-address throttle must not go silently ineffective when
// it is left unconfigured behind a proxy, so clientIP warns (rate-limited,
// see proxyWarn) when the configuration cannot be trusted: an
// X-Forwarded-For arriving while TrustProxyHops is 0, a sign-in that did
// not come from the UI (or no UPTIME_FRONTEND_TOKEN to tell) while it is
// above 0, and a forwarded chain shorter than the trusted hops. In the last
// two it also keeps to the safe side, falling back to the peer rather than
// to a client-settable entry (see below). now is the request's clock,
// threaded in so the rate-limiting is testable. No warning names the
// token, nor the value a request presented for it.
func (h *handler) clientIP(r *http.Request, now time.Time) string {
	peer := hostOnly(r.RemoteAddr)
	xff := forwardedFor(r.Header.Values("X-Forwarded-For"))
	if h.opts.TrustProxyHops <= 0 {
		if len(xff) > 0 {
			// A proxy in front, or Next.js in the UI, set X-Forwarded-For, but
			// we are told to trust none of it (the default), so the throttle
			// sees the one peer address for every client and falls back to a
			// single shared, per-name lock: the published DoS fix is silently
			// ineffective here. Warn, naming the address keyed on so the
			// operator can see it is the proxy's (or the UI pod's), but keep
			// to the peer, since the header is a client's to forge without a
			// trusted proxy to vouch for it.
			h.proxyWarn.warn(now, "xff-hops-zero",
				fmt.Sprintf("X-Forwarded-For seen but UPTIME_TRUST_PROXY_HOPS=0: the sign-in throttle keys on the "+
					"peer %q for every client and falls back to one shared per-name lock; set it to the "+
					"X-Forwarded-For entries your proxies in front of the UI write, 1 with none and the UI reached "+
					"only through a port-forward (README, \"Hesaplar ve giriş\", \"Vekil-hop rehberi\")", peer))
		}
		return peer
	}
	if h.opts.FrontendToken == "" {
		// Hops are trusted, but there is no token to tell the UI by, so no
		// peer may name its client: every browser keys on the UI pod, one
		// shared lock (the backend said so at start, too).
		h.proxyWarn.warn(now, "xff-no-frontend-token",
			fmt.Sprintf("UPTIME_TRUST_PROXY_HOPS=%d but UPTIME_FRONTEND_TOKEN is not set: X-Forwarded-For is trusted "+
				"from no peer, so the sign-in throttle keys on the peer %q, the UI pod for every browser, and falls "+
				"back to one shared per-name lock; give the backend UPTIME_FRONTEND_TOKEN and the UI FRONTEND_TOKEN "+
				"the same random value, as the chart does (README, \"Hesaplar ve giriş\", \"Güvenilir eş\")",
				h.opts.TrustProxyHops, peer))
		return peer
	}
	if !h.fromFrontend(r) {
		// Not the UI: a script or a port-forward to the backend, or a pod
		// with an X-Forwarded-For of its own making. Whatever the header
		// says, the source is the peer, which no client chooses.
		came := "it came with none"
		if len(xff) > 0 {
			came = "it came with one, ignored"
		}
		h.proxyWarn.warn(now, "xff-untrusted-peer",
			fmt.Sprintf("sign-in from a peer that did not present the UI's %s while UPTIME_TRUST_PROXY_HOPS=%d: its "+
				"X-Forwarded-For is not trusted (%s), so the sign-in throttle keys on the peer %q; expected for a "+
				"sign-in sent to the backend directly (a script, a port-forward to the backend) or one with a header "+
				"its sender wrote; if it came through the UI, give the UI's FRONTEND_TOKEN the value of "+
				"UPTIME_FRONTEND_TOKEN (README, \"Hesaplar ve giriş\", \"Güvenilir eş\")",
				frontendTokenHeader, h.opts.TrustProxyHops, came, peer))
		return peer
	}
	// The chain from the client (leftmost) to us: the entries the proxies
	// appended to X-Forwarded-For, then the peer that connected (our
	// nearest proxy).
	chain := append(xff, peer)
	i := len(chain) - 1 - h.opts.TrustProxyHops
	if i < 0 {
		// The forwarded chain is shorter than the hops we trust, so no
		// trusted proxy appended its leftmost entry: it is a client's to
		// set. Reading it would let an attacker spoof a fresh source each
		// request (slipping the throttle) or forge a victim's address (to
		// lock them out). Fall back to the peer, which no client controls,
		// and warn that the hop count is too high for this request, or the
		// request reached the UI without passing the proxies in front of it.
		h.proxyWarn.warn(now, "xff-hops-too-high",
			fmt.Sprintf("X-Forwarded-For has fewer entries than UPTIME_TRUST_PROXY_HOPS=%d: the forwarded chain is "+
				"shorter than the trusted hops, so the source falls back to the peer %q rather than a "+
				"client-settable entry; set UPTIME_TRUST_PROXY_HOPS to the X-Forwarded-For entries your proxies in "+
				"front of the UI write, 1 with none and the UI reached only through a port-forward, or this sign-in "+
				"reached the UI without passing the proxies in front of it (README, \"Hesaplar ve giriş\", "+
				"\"Vekil-hop rehberi\")",
				h.opts.TrustProxyHops, peer))
		return peer
	}
	return chain[i]
}

// fromFrontend says whether r comes from the UI: it carries the UI's token
// in X-Frontend-Token, once, compared in constant time so that the answer
// time says nothing of how much of a guess was right. A request with the
// header twice is not the UI's, whatever the values: the UI sends it once.
func (h *handler) fromFrontend(r *http.Request) bool {
	if h.opts.FrontendToken == "" {
		return false
	}
	got := r.Header.Values(frontendTokenHeader)
	return len(got) == 1 && subtle.ConstantTimeCompare([]byte(got[0]), []byte(h.opts.FrontendToken)) == 1
}

// proxyWarn logs a message about the X-Forwarded-For configuration at most
// once a minute per key, so that a flood of requests each meeting the same
// misconfiguration leaves one line a minute in the log, not one a request.
// A nil receiver and a nil logger log nothing, which keeps the warning out
// of handlers built by hand in tests. It is safe for concurrent use.
type proxyWarn struct {
	logger *slog.Logger
	mu     sync.Mutex
	last   map[string]time.Time
}

// newProxyWarn returns a proxyWarn that logs to logger.
func newProxyWarn(logger *slog.Logger) *proxyWarn {
	return &proxyWarn{logger: logger, last: make(map[string]time.Time)}
}

// warn logs msg under key, unless the same key was logged less than a
// minute before now.
func (p *proxyWarn) warn(now time.Time, key, msg string) {
	if p == nil || p.logger == nil {
		return
	}
	p.mu.Lock()
	if last, ok := p.last[key]; ok && now.Sub(last) < time.Minute {
		p.mu.Unlock()
		return
	}
	p.last[key] = now
	p.mu.Unlock()
	p.logger.Warn(msg)
}

// forwardedFor flattens X-Forwarded-For, which may arrive as one line of
// comma-separated addresses or as several lines, into the addresses in
// order, the client's leftmost. Empty entries are dropped.
func forwardedFor(values []string) []string {
	var out []string
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

// hostOnly drops the port from an "ip:port" address; one without a port is
// returned unchanged.
func hostOnly(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

// signInIPKey is the primary sign-in throttle's key: the source address and
// the username. A name's failed guesses from one source throttle that
// source's attempts at it, not the name everywhere, so the legitimate owner
// signing in from another address is never shut out (ADR-0004). The newline
// keeps it apart from the status-page ("sp:") and password-change ("pw:")
// keys the same throttle holds, which have none.
func signInIPKey(ip, username string) string {
	return ip + "\n" + signInKey(username)
}
