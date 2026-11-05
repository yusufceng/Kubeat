package api

import (
	"bytes"
	"log/slog"
	"net/http"
	"strings"
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
