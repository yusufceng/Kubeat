package checker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/textproto"
	"net/url"
	"strings"

	"github.com/yusufceng/Kubeat/backend/internal/explain"
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
	return c.checkHTTPAt(ctx, m, "")
}

// checkHTTPAt sends the request to addr ("ip:port") instead of where the
// URL leads, when addr is given; the request itself is unchanged.
//
// A failed check also says why, as far as the request tells: the phases of
// the request and where its time went, the address and certificate that
// answered, the status line and a few headers of the answer, and what the
// failure most likely means. Of the body, only the beginning of an answer
// with an unexpected status is looked at, to recognize the page it is.
func (c *Checker) checkHTTPAt(ctx context.Context, m store.Monitor, addr string) outcome {
	o := m.HTTP
	expected, err := ParseStatusSet(o.ExpectedStatus)
	if err != nil {
		return failedf("expected status: %w", err)
	}

	rec := newRecorder()

	var body io.Reader
	if o.Body != "" {
		body = strings.NewReader(o.Body)
	}
	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, rec.trace()), o.Method, m.Target, body)
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
		if http.CanonicalHeaderKey(name) != "Host" {
			req.Header.Set(name, value)
		}
	}
	// net/http takes the Host header from here; trimmed, as the monitor's
	// destination counts it, since it would send a padded one empty.
	site := o.HostHeader()
	if site != "" {
		req.Host = site
	}

	transport := c.verifying
	if o.SkipTLSVerify {
		transport = c.insecure
	}
	if addr != "" {
		// A transport of its own, so that the connection pool does not hand
		// this pod's connection to a check of another.
		t := transport.Clone()
		t.DialContext = c.dialTo(addr)
		defer t.CloseIdleConnections()
		transport = t
	}
	// The monitor's headers, credentials and body are for the server it
	// checks: the one its URL names, or that one on TLS (store.SameServer),
	// and the site its Host header names there. A redirect that stays on it
	// goes to that site, though net/http keeps the Host header only on a
	// relative one. A redirect to another server goes without them, with
	// its own name for the host, and is not followed when net/http would
	// send the body along (307, 308). Left to itself, net/http would keep
	// custom headers on every redirect, credentials on one to the same host
	// name on another port, and the Host header on one to "//elsewhere".
	server := store.Endpoint(m.Target)
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(next *http.Request, via []*http.Request) error {
			if o.MaxRedirects == 0 {
				return http.ErrUseLastResponse // judge the redirect itself
			}
			if len(via) > o.MaxRedirects {
				return redirectLimit(o.MaxRedirects)
			}
			if store.SameServer(server, store.Endpoint(next.URL.String())) {
				if site != "" {
					next.Host = site
				}
				return nil
			}
			if next.Body != nil && next.Body != http.NoBody {
				return errBodyToAnotherServer
			}
			withoutOwnHeaders(next, o)
			return nil
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		// Stopped by the redirect limit, the last redirect comes along,
		// its body closed: where it pointed tells a loop.
		return rec.finish(m, failed(shortened(c.notHTTP(m, err))), resp, rec.explain(err))
	}
	defer resp.Body.Close()

	// The leaf certificate of the connection that answered (after redirects).
	out := outcome{statusCode: resp.StatusCode, certExpiry: leafExpiry(resp.TLS)}

	if !expected.Contains(resp.StatusCode) {
		// The beginning of the body tells a router's or a proxy's own page,
		// which explains more than the status; it goes no further than that.
		e := explain.Response(resp.StatusCode, peek(resp.Body))
		out.err = fmt.Errorf("unexpected status %d", resp.StatusCode)
		return rec.finish(m, out, resp, e)
	}
	if o.Keyword == "" && o.JSONPath == "" {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, drainBytes))
		return rec.finish(m, out, resp, nil)
	}

	content, err := io.ReadAll(io.LimitReader(resp.Body, c.opts.BodyLimit))
	if err != nil {
		out.err = fmt.Errorf("read body: %w", err)
		return rec.finish(m, out, resp, rec.explain(err))
	}
	if o.Keyword != "" {
		found := bytes.Contains(content, []byte(o.Keyword))
		switch {
		case !found && !o.InvertKeyword:
			out.err = fmt.Errorf("keyword %q not found in the first %d bytes of the response", o.Keyword, len(content))
			return rec.finish(m, out, resp, nil)
		case found && o.InvertKeyword:
			out.err = fmt.Errorf("keyword %q is in the response, and must not be", o.Keyword)
			return rec.finish(m, out, resp, nil)
		}
	}
	if o.JSONPath != "" {
		out.detail, out.err = matchJSON(content, o.JSONPath, o.JSONOperator, o.JSONExpected)
	}
	return rec.finish(m, out, resp, nil)
}

// maxErrorURL bounds the address that a failed request's error repeats:
// with the whole of a long target, a PromQL query say, the reason after it
// would not fit in what a result keeps (maxDetail).
const maxErrorURL = 120

// shortened returns err with the address it repeats cut to maxErrorURL.
func shortened(err error) error {
	var failure *url.Error
	if !errors.As(err, &failure) || err != failure || len(failure.URL) <= maxErrorURL {
		return err
	}
	return &url.Error{Op: failure.Op, URL: truncate(failure.URL, maxErrorURL), Err: failure.Err}
}

// errBodyToAnotherServer fails a check whose redirect would send its
// request body, which often carries a password or a token, to a server
// other than the monitor's.
var errBodyToAnotherServer = errors.New("the redirect would send the request body to another server")

// notHTTP returns err, unless it says the server's answer was not HTTP:
// net/http quotes in it the line it could not read, the banner of whatever
// listens there, and the check stores that it was not HTTP instead.
func (c *Checker) notHTTP(m store.Monitor, err error) error {
	words := err
	var failure *url.Error
	if errors.As(err, &failure) {
		words = failure.Err
	}
	if !strings.Contains(words.Error(), "malformed HTTP ") && !errors.As(words, new(textproto.ProtocolError)) {
		return err
	}
	kind := c.targetSaid(m, "the server did not answer with HTTP", words.Error())
	if failure != nil {
		return &url.Error{Op: failure.Op, URL: failure.URL, Err: kind}
	}
	return kind
}

// withoutOwnHeaders takes the monitor's headers and credentials off a
// request to another server than the monitor's, and leaves the host to the
// request's URL.
func withoutOwnHeaders(r *http.Request, o store.HTTPOptions) {
	for name := range o.Headers {
		r.Header.Del(name)
	}
	if o.Auth != store.HTTPAuthNone {
		r.Header.Del("Authorization")
	}
	r.Host = ""
}
