// Package checker performs a single uptime check of a monitor.
package checker

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// drainBytes bounds how much of a response body is read just to release the
// connection, when the body itself is not inspected.
const drainBytes = 64 << 10

// Options configures a Checker.
type Options struct {
	// InsecureSkipVerify disables TLS certificate verification for every
	// HTTP check; monitors can also opt out individually.
	InsecureSkipVerify bool
	UserAgent          string
	// KeywordBodyLimit is how much of a response body is searched for a
	// monitor's keyword.
	KeywordBodyLimit int64
}

// Checker runs HTTP and TCP checks. It is safe for concurrent use.
type Checker struct {
	verifying *http.Client
	insecure  *http.Client
	dialer    *net.Dialer
	opts      Options
}

// New returns a Checker. Timeouts are not set here; every check is bounded
// by the context passed to Check.
func New(opts Options) *Checker {
	client := func(skipVerify bool) *http.Client {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: skipVerify}
		return &http.Client{Transport: transport}
	}
	return &Checker{
		verifying: client(opts.InsecureSkipVerify),
		insecure:  client(true),
		dialer:    &net.Dialer{},
		opts:      opts,
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
		result.StatusCode, err = c.checkHTTP(ctx, m)
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

// checkHTTP reports the response status code, and an error unless the
// response satisfies the monitor's expectations.
func (c *Checker) checkHTTP(ctx context.Context, m store.Monitor) (int, error) {
	expected, err := ParseStatusSet(m.HTTP.ExpectedStatus)
	if err != nil {
		return 0, fmt.Errorf("expected status: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, m.HTTP.Method, m.Target, nil)
	if err != nil {
		return 0, err
	}
	if c.opts.UserAgent != "" {
		req.Header.Set("User-Agent", c.opts.UserAgent)
	}
	for name, value := range m.HTTP.Headers {
		if http.CanonicalHeaderKey(name) == "Host" {
			req.Host = value // net/http takes the Host header from here
			continue
		}
		req.Header.Set(name, value)
	}

	client := c.verifying
	if m.HTTP.SkipTLSVerify {
		client = c.insecure
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if !expected.Contains(resp.StatusCode) {
		// Drain a bounded amount so the connection can be reused.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, drainBytes))
		return resp.StatusCode, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	if m.HTTP.Keyword == "" {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, drainBytes))
		return resp.StatusCode, nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, c.opts.KeywordBodyLimit))
	if err != nil {
		return resp.StatusCode, fmt.Errorf("read body: %w", err)
	}
	if !bytes.Contains(body, []byte(m.HTTP.Keyword)) {
		return resp.StatusCode, fmt.Errorf("keyword %q not found in the first %d bytes of the response", m.HTTP.Keyword, len(body))
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
