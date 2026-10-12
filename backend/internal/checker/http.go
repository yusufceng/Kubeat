package checker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// drainBytes bounds how much of a response body is read just to release the
// connection, when the body itself is not inspected.
const drainBytes = 64 << 10

// HTTPMethods are the request methods an HTTP monitor may use.
var HTTPMethods = []string{
	http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions,
}

// checkHTTP sends the monitor's request. The outcome carries the response
// status and, for HTTPS, when the server's certificate expires; its error
// is nil only if the response satisfies the monitor's expectations.
func (c *Checker) checkHTTP(ctx context.Context, m store.Monitor) outcome {
	o := m.HTTP
	expected, err := ParseStatusSet(o.ExpectedStatus)
	if err != nil {
		return failedf("expected status: %w", err)
	}

	var body io.Reader
	if o.Body != "" {
		body = strings.NewReader(o.Body)
	}
	req, err := http.NewRequestWithContext(ctx, o.Method, m.Target, body)
	if err != nil {
		return failed(err)
	}
	if c.opts.UserAgent != "" {
		req.Header.Set("User-Agent", c.opts.UserAgent)
	}
	if o.Body != "" && o.ContentType != "" {
		req.Header.Set("Content-Type", o.ContentType)
	}
	switch o.Auth {
	case store.HTTPAuthBasic:
		req.SetBasicAuth(o.AuthUser, o.AuthSecret)
	case store.HTTPAuthBearer:
		req.Header.Set("Authorization", "Bearer "+o.AuthSecret)
	}
	// Explicit headers win over what the options above set.
	for name, value := range o.Headers {
		if http.CanonicalHeaderKey(name) == "Host" {
			req.Host = value // net/http takes the Host header from here
			continue
		}
		req.Header.Set(name, value)
	}

	transport := c.verifying
	if o.SkipTLSVerify {
		transport = c.insecure
	}
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if o.MaxRedirects == 0 {
				return http.ErrUseLastResponse // judge the redirect itself
			}
			if len(via) > o.MaxRedirects {
				return fmt.Errorf("stopped after %d redirects", o.MaxRedirects)
			}
			return nil
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return failed(err)
	}
	defer resp.Body.Close()

	// The leaf certificate of the connection that answered (after redirects).
	out := outcome{statusCode: resp.StatusCode, certExpiry: leafExpiry(resp.TLS)}

	if !expected.Contains(resp.StatusCode) {
		// Drain a bounded amount so the connection can be reused.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, drainBytes))
		out.err = fmt.Errorf("unexpected status %d", resp.StatusCode)
		return out
	}
	if o.Keyword == "" && o.JSONPath == "" {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, drainBytes))
		return out
	}

	content, err := io.ReadAll(io.LimitReader(resp.Body, c.opts.BodyLimit))
	if err != nil {
		out.err = fmt.Errorf("read body: %w", err)
		return out
	}
	if o.Keyword != "" {
		found := bytes.Contains(content, []byte(o.Keyword))
		switch {
		case !found && !o.InvertKeyword:
			out.err = fmt.Errorf("keyword %q not found in the first %d bytes of the response", o.Keyword, len(content))
			return out
		case found && o.InvertKeyword:
			out.err = fmt.Errorf("keyword %q is in the response, and must not be", o.Keyword)
			return out
		}
	}
	if o.JSONPath != "" {
		out.detail, out.err = matchJSON(content, o.JSONPath, o.JSONOperator, o.JSONExpected)
	}
	return out
}
