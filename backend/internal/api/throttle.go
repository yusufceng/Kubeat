package api

import (
	"net"
	"net/http"
	"strings"
)

// clientIP is the address a sign-in comes from. It keys the sign-in
// throttle (see authLogin) so that one source's failed guesses throttle
// only that source, not the username for everyone: this is what keeps a
// known name, such as the ever-present "admin", from being locked out of a
// public login by anyone who knows it (CWE-307, ADR-0004).
//
// Without a reverse proxy of your own in front (TrustProxyHops 0, the
// default) it is the peer that connected; X-Forwarded-For is not trusted,
// since any client may send one. TrustProxyHops is how many proxies of your
// own stand in front: the client is then read from X-Forwarded-For,
// trusting only the rightmost that many entries, which those proxies
// appended, and ignoring whatever a client put to their left. Behind an
// ingress or a load balancer the peer is the proxy, the same address for
// every client, so TrustProxyHops must be set or the throttle cannot tell
// sources apart and falls back to one shared, per-name lock (README,
// "Hesaplar ve giriş").
func (h *handler) clientIP(r *http.Request) string {
	peer := hostOnly(r.RemoteAddr)
	if h.opts.TrustProxyHops <= 0 {
		return peer
	}
	// The chain from the client (leftmost) to us: the entries the proxies
	// appended to X-Forwarded-For, then the peer that connected (our
	// nearest proxy).
	chain := append(forwardedFor(r.Header.Values("X-Forwarded-For")), peer)
	i := len(chain) - 1 - h.opts.TrustProxyHops
	if i < 0 {
		// Fewer entries than trusted hops, so a client sent fewer than
		// expected: the leftmost is as far back as the chain reaches.
		i = 0
	}
	return chain[i]
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
