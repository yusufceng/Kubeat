package api

import (
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// frontendTokenHeader carries the secret by which the UI proves it is the
// UI (Options.FrontendToken, UPTIME_FRONTEND_TOKEN): only a request that
// carries it may name its client in X-Forwarded-For.
const frontendTokenHeader = "X-Frontend-Token"

// clientIP is the address a guess at a password comes from: a sign-in, a
// password change or a status-page unlock, the surface s. It keys the
// throttle (see authLogin and beginGuess) so that one source's failed
// guesses throttle only that source, not the username, the account or the
// page for everyone: this is what keeps a known name, such as the
// ever-present "admin", from being locked out of a public login, and a
// protected status page from being locked for all its visitors, by anyone
// who knows it (CWE-307, ADR-0004).
//
// With TrustProxyHops 0 (the default) it is the peer that connected;
// X-Forwarded-For is not trusted, since any client may send one. Above 0
// the client is read from X-Forwarded-For, trusting only the rightmost
// TrustProxyHops entries, which the proxies in front appended, and ignoring
// whatever a client put to their left. Behind an ingress or a load balancer
// the peer is the proxy, the same address for every client, so
// TrustProxyHops must be set or the throttle cannot tell sources apart and
// falls back to one shared lock: per name, per page or per account
// (README, "Hesaplar ve giriş").
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
// X-Forwarded-For arriving while TrustProxyHops is 0, a request that did
// not come from the UI (or no UPTIME_FRONTEND_TOKEN to tell) while it is
// above 0, and a forwarded chain shorter than the trusted hops. In the last
// two it also keeps to the safe side, falling back to the peer rather than
// to a client-settable entry (see below). A warning names the surface of
// the request that met the misconfiguration, but the three surfaces share
// its once-a-minute limit: a status page's visitors add no lines to those
// the sign-ins leave. now is the request's clock, threaded in so the
// rate-limiting is testable. No warning names the token, nor the value a
// request presented for it.
func (h *handler) clientIP(r *http.Request, s surface, now time.Time) string {
	peer := hostOnly(r.RemoteAddr)
	xff := forwardedFor(r.Header.Values("X-Forwarded-For"))
	if h.opts.TrustProxyHops <= 0 {
		if len(xff) > 0 {
			// A proxy in front, or Next.js in the UI, set X-Forwarded-For, but
			// we are told to trust none of it (the default), so the throttle
			// sees the one peer address for every client and falls back to a
			// single shared lock: the published DoS fix is silently
			// ineffective here. Warn, naming the address keyed on so the
			// operator can see it is the proxy's (or the UI pod's), but keep
			// to the peer, since the header is a client's to forge without a
			// trusted proxy to vouch for it.
			h.proxyWarn.warn(now, "xff-hops-zero",
				fmt.Sprintf("X-Forwarded-For seen on a %s but UPTIME_TRUST_PROXY_HOPS=0: the %s throttle keys on the "+
					"peer %q for every client and falls back to one shared %s lock; set it to the "+
					"X-Forwarded-For entries your proxies in front of the UI write, 1 with none and the UI reached "+
					"only through a port-forward (README, \"Hesaplar ve giriş\", \"Vekil-hop rehberi\")",
					s.name, s.name, peer, s.shared))
		}
		return peer
	}
	if h.opts.FrontendToken == "" {
		// Hops are trusted, but there is no token to tell the UI by, so no
		// peer may name its client: every browser keys on the UI pod, one
		// shared lock (the backend said so at start, too).
		h.proxyWarn.warn(now, "xff-no-frontend-token",
			fmt.Sprintf("UPTIME_TRUST_PROXY_HOPS=%d but UPTIME_FRONTEND_TOKEN is not set: X-Forwarded-For is trusted "+
				"from no peer, so the %s throttle keys on the peer %q, the UI pod for every browser, and falls "+
				"back to one shared %s lock; give the backend UPTIME_FRONTEND_TOKEN and the UI FRONTEND_TOKEN "+
				"the same random value, as the chart does (README, \"Hesaplar ve giriş\", \"Güvenilir eş\")",
				h.opts.TrustProxyHops, s.name, peer, s.shared))
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
			fmt.Sprintf("%s from a peer that did not present the UI's %s while UPTIME_TRUST_PROXY_HOPS=%d: its "+
				"X-Forwarded-For is not trusted (%s), so the %s throttle keys on the peer %q; expected for a "+
				"%s sent to the backend directly (a script, a port-forward to the backend) or one with a header "+
				"its sender wrote; if it came through the UI, give the UI's FRONTEND_TOKEN the value of "+
				"UPTIME_FRONTEND_TOKEN (README, \"Hesaplar ve giriş\", \"Güvenilir eş\")",
				s.name, frontendTokenHeader, h.opts.TrustProxyHops, came, s.name, peer, s.name))
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
				"shorter than the trusted hops, so the %s throttle's source falls back to the peer %q rather than a "+
				"client-settable entry; set UPTIME_TRUST_PROXY_HOPS to the X-Forwarded-For entries your proxies in "+
				"front of the UI write, 1 with none and the UI reached only through a port-forward, or this %s "+
				"reached the UI without passing the proxies in front of it (README, \"Hesaplar ve giriş\", "+
				"\"Vekil-hop rehberi\")",
				h.opts.TrustProxyHops, s.name, peer, s.name))
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
// signing in from another address is never shut out (ADR-0004). The
// status-page and password-change keys the same throttle holds have the
// same shape (sourceKey), but their scope has a colon, which no username
// has.
func signInIPKey(ip, username string) string {
	return ip + "\n" + signInKey(username)
}

// A surface is a password the throttle guards per source: a sign-in's, a
// status page's, or the current one asked for to change it. name is what
// the X-Forwarded-For warnings call it; shared is the one lock every client
// falls back to when the throttle cannot tell sources apart.
type surface struct{ name, shared string }

var (
	signInSurface     = surface{name: "sign-in", shared: "per-name"}
	passwordSurface   = surface{name: "password change", shared: "per-account"}
	statusPageSurface = surface{name: "status-page unlock", shared: "per-page"}
)

// statusPageScope and passwordScope are the scopes of the passwords other
// than a sign-in's, as the throttle keys them: a status page's by its slug,
// the current password asked for to change one by the account's ID. The
// colon keeps them apart from the usernames, which have none, under both
// throttles.
func statusPageScope(slug string) string { return "sp:" + slug }
func passwordScope(userID int64) string  { return "pw:" + strconv.FormatInt(userID, 10) }

// sourceKey is the per-source throttle key of a guess at scope: the source
// address and the scope, shaped as a sign-in's (signInIPKey) is.
func sourceKey(source, scope string) string { return source + "\n" + scope }

// A guess is an attempt at a status page's password or at the current
// password asked for to change one, as the throttle holds it: under its
// source's key (sourceKey) and, while AccountThrottle is set, under the
// scope's own key at the loose ceiling across all sources. It is begun
// before the password is checked (beginGuess), then taken back (undo) when
// it turns out not to be a guess, or forgotten with the failures before it
// (succeeded) when the password was right; a wrong one stays counted.
type guess struct {
	h            *handler
	key, ceiling string
	at           time.Time
	// source is the address it was keyed on, for the operator's log, and
	// attempt its number from that source within the lockout period.
	source  string
	attempt int
}

// beginGuess reserves an attempt at scope from r's source (clientIP, which
// names surface s in its warnings), counted as a failure from the start as
// a sign-in's is, so parallel guesses cannot all be tried before the first
// failure is recorded. The throttle is the sign-in's (ADR-0004): one
// source's wrong guesses lock that source only, at UPTIME_LOGIN_MAX_FAILURES,
// so nobody can lock a status page for all its visitors, nor an account's
// password change for its owner, who may need it to shut a stolen session
// out (CWE-307). The loose ceiling on the scope's failures from all sources
// (UPTIME_LOGIN_ACCOUNT_MAX_FAILURES; no AccountThrottle when it is 0) keeps
// a distributed guess from running forever. Past either it answers 429 with
// Retry-After, the same for both, and returns false.
func (h *handler) beginGuess(w http.ResponseWriter, r *http.Request, s surface, scope string, now time.Time) (guess, bool) {
	source := h.clientIP(r, s, now)
	g := guess{h: h, key: sourceKey(source, scope), ceiling: scope, at: now, source: source}
	attempt, locked, ok := h.opts.Throttle.Begin(g.key, now)
	if !ok {
		tooManyAttempts(w, locked)
		return guess{}, false
	}
	g.attempt = attempt
	if h.opts.AccountThrottle != nil {
		if _, locked, ok := h.opts.AccountThrottle.Begin(scope, now); !ok {
			h.opts.Throttle.Undo(g.key, now)
			tooManyAttempts(w, locked)
			return guess{}, false
		}
	}
	return g, true
}

// undo takes the attempt back: it was no guess, because the page is not
// there or has no password, or the password could not be checked.
func (g guess) undo() {
	g.h.opts.Throttle.Undo(g.key, g.at)
	if g.h.opts.AccountThrottle != nil {
		g.h.opts.AccountThrottle.Undo(g.ceiling, g.at)
	}
}

// succeeded forgets the failures before a right password: its source's and,
// as a sign-in forgets its name's, the scope's under the ceiling. A page's
// password is shared by its visitors; were the ceiling kept, the typos of
// many visitors opening the page at once, in an incident, would add up to
// it and lock the page for everyone. The ceiling thus bounds the failures
// since the last right password within the lockout period.
func (g guess) succeeded() {
	g.h.opts.Throttle.Reset(g.key)
	if g.h.opts.AccountThrottle != nil {
		g.h.opts.AccountThrottle.Reset(g.ceiling)
	}
}
