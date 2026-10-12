package checker

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// websocketGUID is the fixed value RFC 6455 has the server append to the
// client's key before hashing it.
const websocketGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// checkWebSocket performs the opening handshake of a WebSocket connection
// and closes it. It succeeds when the server switches protocols and proves,
// through the accept key, that it understood the request as a WebSocket one.
func (c *Checker) checkWebSocket(ctx context.Context, m store.Monitor) outcome {
	u, err := url.Parse(m.Target)
	if err != nil {
		return failed(err)
	}
	switch u.Scheme {
	case "ws":
		u.Scheme = "http"
	case "wss":
		u.Scheme = "https"
	default:
		return failedf("target must be a ws:// or wss:// URL")
	}

	key := make([]byte, 16)
	_, _ = rand.Read(key)
	clientKey := base64.StdEncoding.EncodeToString(key)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return failed(err)
	}
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", clientKey)
	if m.WebSocket.Subprotocol != "" {
		req.Header.Set("Sec-WebSocket-Protocol", m.WebSocket.Subprotocol)
	}
	if c.opts.UserAgent != "" {
		req.Header.Set("User-Agent", c.opts.UserAgent)
	}

	// The handshake is HTTP/1.1; a connection of its own, since after the
	// upgrade it is no longer an HTTP connection that could be reused.
	transport := &http.Transport{
		Proxy:             http.ProxyFromEnvironment,
		DialContext:       c.dialer.DialContext,
		TLSClientConfig:   c.tlsConfig("", m.WebSocket.SkipTLSVerify),
		TLSNextProto:      map[string]func(string, *tls.Conn) http.RoundTripper{}, // no HTTP/2
		DisableKeepAlives: true,
	}
	defer transport.CloseIdleConnections()
	resp, err := transport.RoundTrip(req)
	if err != nil {
		return failed(err)
	}
	defer resp.Body.Close()

	out := outcome{statusCode: resp.StatusCode, certExpiry: leafExpiry(resp.TLS)}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		out.err = fmt.Errorf("the server answered %d instead of switching to WebSocket", resp.StatusCode)
		return out
	}
	digest := sha1.Sum([]byte(clientKey + websocketGUID))
	if resp.Header.Get("Sec-WebSocket-Accept") != base64.StdEncoding.EncodeToString(digest[:]) {
		out.err = fmt.Errorf("the server's Sec-WebSocket-Accept does not match the request")
		return out
	}
	if want := m.WebSocket.Subprotocol; want != "" {
		if got := resp.Header.Get("Sec-WebSocket-Protocol"); !strings.EqualFold(got, want) {
			out.err = fmt.Errorf("the server did not accept the subprotocol %q", want)
			return out
		}
		out.detail = want
	}
	return out
}
