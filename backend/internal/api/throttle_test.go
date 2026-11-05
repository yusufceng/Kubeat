package api

import (
	"net/http"
	"testing"
)

// clientIP reads the source the throttle keys on: the peer without a
// trusted proxy, and the right entry of X-Forwarded-For with one, ignoring
// whatever a client put to the left of the hops it trusts.
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
		{name: "fewer entries than hops clamps to the leftmost", hops: 3, peer: "10.0.0.1:5000", xff: []string{"203.0.113.7"}, want: "203.0.113.7"},
		{name: "a hop but no header falls back to the peer", hops: 1, peer: "10.0.0.1:5000", want: "10.0.0.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &handler{opts: Options{TrustProxyHops: tc.hops}}
			r := &http.Request{RemoteAddr: tc.peer, Header: http.Header{}}
			for _, v := range tc.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			if got := h.clientIP(r); got != tc.want {
				t.Errorf("clientIP(hops=%d, peer=%q, xff=%v) = %q, want %q", tc.hops, tc.peer, tc.xff, got, tc.want)
			}
		})
	}
}
