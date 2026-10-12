// Package checker performs a single uptime check of a monitor.
package checker

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// maxBodyBytes bounds how much of a response body is read before the
// connection is released; the body itself is not inspected.
const maxBodyBytes = 64 << 10

// Options configures a Checker.
type Options struct {
	// InsecureSkipVerify disables TLS certificate verification for HTTP
	// checks, for clusters whose routes use a private CA.
	InsecureSkipVerify bool
	UserAgent          string
}

// Checker runs HTTP and TCP checks. It is safe for concurrent use.
type Checker struct {
	client    *http.Client
	dialer    *net.Dialer
	userAgent string
}

// New returns a Checker. Timeouts are not set here; every check is bounded
// by the context passed to Check.
func New(opts Options) *Checker {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: opts.InsecureSkipVerify}
	return &Checker{
		client:    &http.Client{Transport: transport},
		dialer:    &net.Dialer{},
		userAgent: opts.UserAgent,
	}
}

// Check runs one check of m and reports the outcome. A failed check is not
// an error: it is a result with Up == false and Error set.
func (c *Checker) Check(ctx context.Context, m store.Monitor) store.CheckResult {
	start := time.Now()
	result := store.CheckResult{MonitorID: m.ID, CheckedAt: start.UTC().Truncate(time.Millisecond)}

	var err error
	switch m.Type {
	case store.MonitorHTTP:
		result.StatusCode, err = c.checkHTTP(ctx, m.Target)
	case store.MonitorTCP:
		err = c.checkTCP(ctx, m.Target)
	default:
		err = fmt.Errorf("unsupported monitor type %q", m.Type)
	}

	result.Latency = time.Since(start)
	if err != nil {
		result.Error = err.Error()
	} else {
		result.Up = true
	}
	return result
}

// checkHTTP reports the response status code, and an error unless it is 2xx
// or 3xx.
func (c *Checker) checkHTTP(ctx context.Context, target string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return 0, err
	}
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	// Drain a bounded amount so the connection can be reused.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBodyBytes))

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return resp.StatusCode, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return resp.StatusCode, nil
}

func (c *Checker) checkTCP(ctx context.Context, target string) error {
	conn, err := c.dialer.DialContext(ctx, "tcp", target)
	if err != nil {
		return err
	}
	return conn.Close()
}
