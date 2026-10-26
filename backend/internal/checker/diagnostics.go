package checker

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/explain"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// The bounds of what a failed check keeps about why it failed. They are
// part of what keeps a result from carrying what a target answered, so
// they are fixed rather than settings.
const (
	// maxDiagnostics bounds, in bytes, the diagnostics of one result as
	// they are stored. The caps below keep almost every one well under it;
	// one that is not loses its least telling parts first (shrinkSteps).
	maxDiagnostics = 1024
	// maxHeaderValue bounds a kept header's value, maxName a name such as a
	// certificate's subject or a pod's, maxStatusLine the status line.
	maxHeaderValue = 100
	maxName        = 100
	maxStatusLine  = 64
	// maxSANs is how many of the names a certificate is valid for are kept;
	// the rest are only counted.
	maxSANs = 5
)

// keptHeaders are the headers of an answer that a failed check keeps: who
// answered (Server, Via), what the answer was (Content-Type and -Length),
// where it pointed (Location), when to try again (Retry-After), how long
// the proxy's upstream took and the id to find the request in its logs,
// and which authentication was asked for. No other header is kept: cookies,
// tokens and whatever else a service sends stay out of the results. Of
// these, the values that could carry a credential are cut down: a location
// to its scheme, host and path, an authentication challenge to its scheme.
var keptHeaders = []string{
	"Content-Type", "Content-Length", "Server", "Via", "Location", "Retry-After",
	"X-Envoy-Upstream-Service-Time", "X-Request-Id", "Www-Authenticate",
}

// redirectLimit is the error that stops a request after the monitor's
// number of redirects, so that the explanation can tell a loop.
type redirectLimit int

func (n redirectLimit) Error() string { return "stopped after " + strconv.Itoa(int(n)) + " redirects" }

// phases are the moments of one HTTP request; zero until they happen.
type phases struct {
	request, dnsStart, dnsDone, connectStart, connectDone, tlsStart, tlsDone, gotConn, firstByte time.Time
	// connected says a connection attempt succeeded; addr is the address
	// connected to, or the last one tried.
	connected bool
	addr      string
	tlsState  *tls.ConnectionState
	tlsErr    error
}

// recorder follows the requests of an HTTP check through their phases, for
// the diagnostics of a check that fails, and keeps a hop per request (the
// host it went to, the address that answered) for the scheduler's
// ceilings. A redirect goes through the phases again, and what is kept of
// them is the last request's: the one whose answer is judged. The
// transport reports from goroutines of its own, and a dial it gave up on
// may still report after the request returned; hence the lock.
type recorder struct {
	mu    sync.Mutex
	start time.Time
	// next is the host the request about to be sent is addressed to: the
	// target's, then each redirect's (redirect). GetConn's own host:port
	// would be the proxy's where one is used.
	next string
	hops []store.Hop // one per request sent: one, and one more per redirect followed
	last phases
}

// newRecorder records the requests of a check addressed to host.
func newRecorder(host string) *recorder { return &recorder{start: time.Now(), next: host} }

// redirect notes that the next request goes to host.
func (r *recorder) redirect(host string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.next = host
}

// setAddr notes the address the current request connected to.
func (p *phases) setAddr(hops []store.Hop, addr string) {
	p.addr = addr
	if n := len(hops); n > 0 {
		hops[n-1].Addr = hostOf(addr)
	}
}

// hostOf is the host of a "host:port", or addr itself when it has no port.
func hostOf(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

// trace is the recorder's httptrace hooks.
func (r *recorder) trace() *httptrace.ClientTrace {
	at := func(set func(p *phases, now time.Time)) {
		now := time.Now()
		r.mu.Lock()
		defer r.mu.Unlock()
		set(&r.last, now)
	}
	return &httptrace.ClientTrace{
		GetConn: func(string) {
			now := time.Now()
			r.mu.Lock()
			defer r.mu.Unlock()
			r.hops = append(r.hops, store.Hop{Host: r.next})
			r.last = phases{request: now}
		},
		DNSStart: func(httptrace.DNSStartInfo) { at(func(p *phases, now time.Time) { p.dnsStart = now }) },
		DNSDone:  func(httptrace.DNSDoneInfo) { at(func(p *phases, now time.Time) { p.dnsDone = now }) },
		ConnectStart: func(_, addr string) {
			at(func(p *phases, now time.Time) {
				if p.connectStart.IsZero() {
					p.connectStart = now
				}
				if !p.connected {
					p.addr = addr
				}
			})
		},
		ConnectDone: func(_, addr string, err error) {
			at(func(p *phases, now time.Time) {
				switch {
				case err == nil && !p.connected:
					p.connected, p.connectDone = true, now
					p.setAddr(r.hops, addr)
				case err != nil && !p.connected:
					p.connectDone, p.addr = now, addr
				}
			})
		},
		TLSHandshakeStart: func() { at(func(p *phases, now time.Time) { p.tlsStart = now }) },
		TLSHandshakeDone: func(state tls.ConnectionState, err error) {
			at(func(p *phases, now time.Time) { p.tlsDone, p.tlsState, p.tlsErr = now, &state, err })
		},
		GotConn: func(info httptrace.GotConnInfo) {
			at(func(p *phases, now time.Time) {
				p.gotConn = now
				if info.Conn != nil && info.Conn.RemoteAddr() != nil {
					p.setAddr(r.hops, info.Conn.RemoteAddr().String())
				}
			})
		},
		GotFirstResponseByte: func() { at(func(p *phases, now time.Time) { p.firstByte = now }) },
	}
}

// finish adds what the recorder saw, with the explanation e, to an outcome
// that failed, or that fails m by succeeding when m is inverted; answer is
// the answer that was judged, nil when none came.
func (r *recorder) finish(m store.Monitor, out outcome, answer *http.Response, e *store.Explanation) outcome {
	if out.err != nil || m.Inverted {
		out.diag = r.diagnostics(answer, e)
	}
	r.mu.Lock()
	out.hops = slices.Clone(r.hops)
	r.mu.Unlock()
	return out
}

// explain explains the error of a request that got no answer to judge.
func (r *recorder) explain(err error) *store.Explanation {
	if _, ok := errors.AsType[redirectLimit](err); ok {
		return &store.Explanation{Code: explain.TooManyRedirects}
	}
	e := explain.Error(err)
	if e != nil && (e.Code == explain.Timeout || e.Code == explain.ConnectTimeout) {
		// The error only says that time ran out; the phases say where.
		return &store.Explanation{Code: r.timeout()}
	}
	r.mu.Lock()
	handshake := r.last.tlsErr
	r.mu.Unlock()
	if e == nil && handshake != nil {
		// net/http words some handshake failures in its own text, such as
		// a plain HTTP answer to a TLS request; the handshake's own error
		// still tells.
		e = explain.Error(handshake)
	}
	return e
}

// timeout is the code of a request that ran out of time, by the phase it
// was in: looking up the name, connecting, or waiting for the answer once
// connected, the TLS handshake included.
func (r *recorder) timeout() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.last
	switch {
	case !p.dnsStart.IsZero() && p.dnsDone.IsZero():
		return explain.DNSFailed
	case p.connected || !p.gotConn.IsZero():
		return explain.ResponseTimeout
	case !p.connectStart.IsZero():
		return explain.ConnectTimeout
	default:
		return explain.Timeout
	}
}

// diagnostics is what the recorder saw of the requests, with what the
// answer says about itself when one came, and the explanation.
func (r *recorder) diagnostics(answer *http.Response, e *store.Explanation) *store.Diagnostics {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.last
	d := &store.Diagnostics{
		Explanation: e,
		RemoteAddr:  clean(p.addr, maxName),
		Redirects:   max(len(r.hops)-1, 0),
		Timing:      &store.Timing{Total: millis(time.Since(r.start))},
	}
	t := d.Timing
	if !p.dnsStart.IsZero() && !p.dnsDone.IsZero() {
		t.DNS = span(p.dnsStart, p.dnsDone)
	}
	if !p.connectStart.IsZero() && !p.connectDone.IsZero() {
		t.Connect = span(p.connectStart, p.connectDone)
	}
	if !p.tlsStart.IsZero() && !p.tlsDone.IsZero() {
		t.TLS = span(p.tlsStart, p.tlsDone)
	}
	if !p.firstByte.IsZero() {
		t.FirstByte = span(p.request, p.firstByte)
	}
	switch {
	case answer != nil && answer.TLS != nil: // also a reused connection's
		d.TLS = tlsDetails(answer.TLS, nil)
	case p.tlsState != nil:
		d.TLS = tlsDetails(p.tlsState, p.tlsErr)
	}
	if answer != nil {
		d.StatusLine = clean(answer.Proto+" "+answer.Status, maxStatusLine)
		d.Headers = headers(answer.Header)
	}
	return d
}

// millis is d in milliseconds, to the microsecond.
func millis(d time.Duration) float64 {
	return math.Round(float64(d)/float64(time.Microsecond)) / 1000
}

func span(from, to time.Time) *float64 {
	ms := millis(max(to.Sub(from), 0))
	return &ms
}

// peek reads the beginning of a body, only to recognize the page it is, and
// drains a bounded amount of the rest so that the connection can be reused.
// What it returns must not be kept.
func peek(body io.Reader) []byte {
	head, _ := io.ReadAll(io.LimitReader(body, explain.PeekBytes))
	_, _ = io.Copy(io.Discard, io.LimitReader(body, drainBytes))
	return head
}

// headers are the kept headers of an answer, by lower-case name.
func headers(h http.Header) map[string]string {
	var kept map[string]string
	for _, name := range keptHeaders {
		values := h.Values(name)
		if len(values) == 0 {
			continue
		}
		var value string
		switch name {
		case "Location":
			value = withoutCredentials(values[0])
		case "Www-Authenticate":
			value = authSchemes(values)
		default:
			value = strings.Join(values, ", ")
		}
		if value = clean(value, maxHeaderValue); value == "" {
			continue
		}
		if kept == nil {
			kept = make(map[string]string)
		}
		kept[strings.ToLower(name)] = value
	}
	return kept
}

// withoutCredentials is where a redirect points, without what could be a
// credential: the user and password, the query (signed and one-time links
// carry their token there) and the fragment. A "?…" says a query was left
// out.
func withoutCredentials(location string) string {
	u, err := url.Parse(strings.TrimSpace(location))
	if err != nil {
		return ""
	}
	query := u.RawQuery != "" || u.ForceQuery
	u.User, u.RawQuery, u.ForceQuery, u.Fragment, u.RawFragment = nil, "", false, "", ""
	if query {
		return u.String() + "?…"
	}
	return u.String()
}

// authSchemes are the schemes of the authentication challenges in a
// WWW-Authenticate header, such as "Basic, Bearer"; their parameters (a
// realm, an error, a description that may name accounts) are left out.
func authSchemes(values []string) string {
	var schemes []string
	for _, value := range values {
		for _, part := range splitUnquoted(value, ',') {
			scheme, _, _ := strings.Cut(strings.TrimSpace(part), " ")
			// A part that is a parameter of the challenge before it has an "=".
			if !isToken(scheme) || slices.Contains(schemes, scheme) {
				continue
			}
			schemes = append(schemes, scheme)
		}
	}
	return strings.Join(schemes, ", ")
}

// splitUnquoted splits s at each sep that is not inside double quotes.
func splitUnquoted(s string, sep rune) []string {
	var (
		parts           []string
		quoted, escaped bool
		begin           int
	)
	for i, c := range s {
		switch {
		case escaped:
			escaped = false
		case c == '\\' && quoted:
			escaped = true
		case c == '"':
			quoted = !quoted
		case c == sep && !quoted:
			parts = append(parts, s[begin:i])
			begin = i + 1
		}
	}
	return append(parts, s[begin:])
}

// isToken reports whether s is an HTTP token, as an authentication scheme
// is (RFC 9110).
func isToken(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", c)) {
			return false
		}
	}
	return true
}

// tlsDetails describes a TLS connection by the leaf certificate it was
// presented: the peer's own once verified, or else the one verification
// refused, which says best why it was.
func tlsDetails(state *tls.ConnectionState, err error) *store.TLSDetails {
	var leaf *x509.Certificate
	if len(state.PeerCertificates) > 0 {
		leaf = state.PeerCertificates[0]
	} else if refused, ok := errors.AsType[*tls.CertificateVerificationError](err); ok && len(refused.UnverifiedCertificates) > 0 {
		leaf = refused.UnverifiedCertificates[0]
	}
	if state.Version == 0 && leaf == nil {
		return nil // the handshake failed before anything was learned
	}
	d := &store.TLSDetails{}
	if state.Version != 0 {
		d.Version = tls.VersionName(state.Version)
	}
	if leaf == nil {
		return d
	}
	d.Subject = clean(leaf.Subject.CommonName, maxName)
	d.Issuer = clean(leaf.Issuer.CommonName, maxName)
	d.NotAfter = leaf.NotAfter.UTC()
	names := slices.Clone(leaf.DNSNames)
	for _, ip := range leaf.IPAddresses {
		names = append(names, ip.String())
	}
	for i, name := range names {
		if i == maxSANs {
			d.MoreSANs = len(names) - maxSANs
			break
		}
		d.SANs = append(d.SANs, clean(name, maxName))
	}
	return d
}

// clean makes a value that came from the network fit to keep: valid UTF-8
// without control characters, cut to max bytes.
func clean(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.ToValidUTF8(s, ""))
	return truncate(strings.TrimSpace(s), max)
}

// shrinkSteps make diagnostics smaller, the least telling part first, for
// the rare ones beyond maxDiagnostics. What is left after the last step
// (the explanation, timing, address, status line and pod) is bounded well
// below it by the caps above.
var shrinkSteps = []func(d *store.Diagnostics){
	func(d *store.Diagnostics) {
		if d.TLS != nil && len(d.TLS.SANs) > 1 {
			d.TLS.MoreSANs += len(d.TLS.SANs) - 1
			d.TLS.SANs = d.TLS.SANs[:1]
		}
	},
	func(d *store.Diagnostics) {
		for _, name := range []string{"via", "x-request-id", "x-envoy-upstream-service-time", "content-length", "retry-after"} {
			delete(d.Headers, name)
		}
	},
	func(d *store.Diagnostics) { d.Headers = nil },
	func(d *store.Diagnostics) { d.TLS = nil },
}

// bound keeps the diagnostics within maxDiagnostics.
func bound(d *store.Diagnostics) {
	for _, shrink := range shrinkSteps {
		if encodedSize(d) <= maxDiagnostics {
			return
		}
		shrink(d)
	}
}

func encodedSize(d *store.Diagnostics) int {
	b, _ := json.Marshal(d)
	return len(b)
}

// explainable says whether a failure of the type is the network's, which
// its error can explain. A push monitor's failure is what its source
// reported, or that it did not report; a workload monitor's comes from the
// cluster's API about the pods, not from reaching them.
func explainable(t store.MonitorType) bool {
	return t != store.MonitorPush && t != store.MonitorWorkload
}

// diagnose is what a failed check keeps about why it failed: what its
// engine saw, explained by its error where the engine left that to it,
// within maxDiagnostics; nil when there is nothing to say.
func diagnose(t store.MonitorType, out outcome) *store.Diagnostics {
	d := out.diag
	if out.err != nil && (d == nil || d.Explanation == nil) && explainable(t) {
		if e := explain.Error(out.err); e != nil {
			if d == nil {
				d = &store.Diagnostics{}
			}
			d.Explanation = e
		}
	}
	if d != nil {
		bound(d)
	}
	return d
}

// afterConnect explains a failure that came once a connection was made: a
// timeout then means that the server did not answer, not that nobody took
// the connection.
func afterConnect(out outcome) outcome {
	if out.err == nil || out.diag != nil {
		return out
	}
	if e := explain.Error(out.err); e != nil && e.Code == explain.Timeout {
		out.diag = &store.Diagnostics{Explanation: &store.Explanation{Code: explain.ResponseTimeout}}
	}
	return out
}
