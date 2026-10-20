package explain

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"os"
	"regexp"
	"strings"
	"syscall"

	"github.com/yusufceng/Kubeat/backend/internal/netguard"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// PeekBytes is how much of an answer's body is looked at to recognize the
// pages below; each of them says what it is within its first few hundred
// bytes, OpenShift's after a style sheet of about 1.5 KiB. It is a fixed
// bound rather than a setting: only this much of what a target answers is
// ever looked at for an explanation, and none of it is kept.
const PeekBytes = 4 << 10

// maxParam bounds, in bytes, a value an explanation repeats, such as a name.
const maxParam = 100

// page recognizes an answer that a router, ingress controller or proxy gives
// on its own, by phrases at the beginning of its body.
type page struct {
	code   string
	status int      // the status the page comes with; 0 for any
	all    []string // phrases that must all be in the body
	params map[string]string
}

// pages are tried in order; the first that matches explains the answer.
var pages = []page{
	// OpenShift's router (HAProxy) has this page for a host no Route
	// matches, a path none of them has, and a Route whose pods are all down.
	{code: RouteNoEndpoints, status: 503, all: []string{"Application is not available"}},
	// The default-http-backend image that ingress-nginx sends unmatched
	// requests to.
	{code: IngressDefaultBackend, all: []string{"default backend - 404"}},
	// Envoy: Istio, OpenShift Service Mesh, Contour, Envoy Gateway.
	{code: ProxyNoHealthyUpstream, status: 503, all: []string{"no healthy upstream"}, params: envoy},
	// A connection to the upstream that failed on TLS, before any other
	// failed one: its page carries the generic phrase too.
	{code: ProxyUpstreamTLS, status: 503, all: []string{"upstream connect error or disconnect/reset before headers", "TLS error"}, params: envoy},
	{code: ProxyUpstreamConnect, status: 503, all: []string{"upstream connect error or disconnect/reset before headers"}, params: envoy},
	{code: MeshAccessDenied, status: 403, all: []string{"RBAC: access denied"}},
	// HAProxy's own page when every server of a backend is down, and
	// Traefik's when a service has no server.
	{code: ProxyNoHealthyUpstream, status: 503, all: []string{"No server is available to handle this request"}, params: map[string]string{"proxy": "HAProxy"}},
	{code: ProxyNoHealthyUpstream, status: 503, all: []string{"no available server"}, params: map[string]string{"proxy": "Traefik"}},
	// nginx's stock 503 page, which ingress-nginx answers with when the
	// Service of the Ingress has no ready endpoint.
	{code: IngressNoEndpoints, status: 503, all: []string{"<center>nginx"}},
	// A TLS port asked in plain HTTP: Go's servers and nginx say so.
	{code: HTTPToTLSPort, status: 400, all: []string{"Client sent an HTTP request to an HTTPS server"}},
	{code: HTTPToTLSPort, status: 400, all: []string{"The plain HTTP request was sent to HTTPS port"}},
}

var envoy = map[string]string{"proxy": "Envoy"}

// statuses explain an error status that no page explains.
var statuses = map[int]string{
	401: Status401, 403: Status403, 404: Status404, 429: Status429,
	500: Status500, 502: Status502, 503: Status503, 504: Status504,
}

// Response explains an answer whose status the monitor did not expect, by
// the page it is and otherwise by its status; nil for a status none of the
// rules knows. Of body, at most the first PeekBytes are looked at.
func Response(status int, body []byte) *store.Explanation {
	if len(body) > PeekBytes {
		body = body[:PeekBytes]
	}
	for _, p := range pages {
		if p.status != 0 && p.status != status {
			continue
		}
		if containsAll(body, p.all) {
			return explanation(p.code, p.params)
		}
	}
	if code, ok := statuses[status]; ok {
		return explanation(code, nil)
	}
	return nil
}

func containsAll(body []byte, phrases []string) bool {
	for _, phrase := range phrases {
		if !bytes.Contains(body, []byte(phrase)) {
			return false
		}
	}
	return true
}

// An errorMatcher recognizes a failure in an error's chain, a textMatcher in
// an error's text; both return the values its wording needs.
type (
	errorMatcher func(err error) (map[string]string, bool)
	textMatcher  func(msg string) (map[string]string, bool)
)

// failure recognizes a failure to get an answer: in an error's chain, as
// most engines report it, or in the text of one that arrived as text only,
// as gRPC's do.
type failure struct {
	code  string
	chain errorMatcher // nil when only the text tells
	text  textMatcher  // nil when only the chain tells
}

// failures are tried in order; the first that matches explains the error.
// The order matters where one failure looks like another: a refused
// connection to a denied address is the policy's doing, a lookup that timed
// out is the DNS server's, and what a proxy reports in a gRPC status
// mentions the connection errors it had.
var failures = []failure{
	{BlockedByPolicy, blocked, anyOf(netguard.ErrBlocked.Error())},
	{ProxyNoHealthyUpstream, nil, with(envoy, anyOf("no healthy upstream"))},
	{ProxyUpstreamTLS, nil, with(envoy, allOf("upstream connect error or disconnect/reset before headers", "TLS error"))},
	{ProxyUpstreamConnect, nil, with(envoy, anyOf("upstream connect error or disconnect/reset before headers"))},
	{MeshAccessDenied, nil, anyOf("RBAC: access denied")},
	{DNSServiceNotFound, dnsNotFound(serviceParams), lookupNotFound(serviceParams)},
	{DNSNotFound, dnsNotFound(hostParams), lookupNotFound(hostParams)},
	{DNSFailed, dnsFailed, anyOf("server misbehaving")},
	{ConnectionRefused, is(syscall.ECONNREFUSED), anyOf("connection refused")},
	{ConnectionReset, is(syscall.ECONNRESET), anyOf("connection reset by peer")},
	{HostUnreachable, is(syscall.EHOSTUNREACH, syscall.ENETUNREACH), anyOf("no route to host", "network is unreachable")},
	{TLSHostnameMismatch, hostnameMismatch, certificateNames},
	{TLSExpired, certificateExpired, anyOf("x509: certificate has expired or is not yet valid")},
	{TLSUnknownAuthority, as[x509.UnknownAuthorityError], anyOf("x509: certificate signed by unknown authority")},
	{TLSInvalidCertificate, certificateRejected, anyOf("x509: ", "tls: failed to verify certificate")},
	{TLSToPlainPort, as[tls.RecordHeaderError], anyOf("first record does not look like a TLS handshake", "server gave HTTP response to HTTPS client")},
	{TLSHandshakeFailed, remoteAlert, anyOf("remote error: tls: ")},
	{ConnectTimeout, dialTimeout, nil},
	{Timeout, timedOut, anyOf("i/o timeout", "context deadline exceeded")},
	{ConnectionClosed, is(io.EOF, io.ErrUnexpectedEOF), anyOf("error reading from server: EOF", "connection closed before server preface received")},
}

// Error explains a failure to get an answer from its error's chain; nil
// when no rule recognizes it.
func Error(err error) *store.Explanation {
	if err == nil {
		return nil
	}
	for _, f := range failures {
		if f.chain == nil {
			continue
		}
		if params, ok := f.chain(err); ok {
			return explanation(f.code, params)
		}
	}
	return nil
}

// Message explains a failure that arrived as text only, as a gRPC status
// does with the transport error behind it; nil when no rule recognizes it.
// The text is only matched against known phrases; none of it is kept beyond
// the names some explanations repeat.
func Message(msg string) *store.Explanation {
	for _, f := range failures {
		if f.text == nil {
			continue
		}
		if params, ok := f.text(msg); ok {
			return explanation(f.code, params)
		}
	}
	return nil
}

// explanation builds an explanation with the values its wording needs, each
// cut to maxParam bytes. The values are copied, so that a rule's own stay
// as they are.
func explanation(code string, params map[string]string) *store.Explanation {
	e := &store.Explanation{Code: code}
	if len(params) > 0 {
		e.Params = make(map[string]string, len(params))
		for name, value := range params {
			e.Params[name] = short(value)
		}
	}
	return e
}

// short cuts s to maxParam bytes without splitting a character.
func short(s string) string {
	if len(s) <= maxParam {
		return s
	}
	cut := maxParam
	for cut > 0 && s[cut]&0xC0 == 0x80 { // a continuation byte of UTF-8
		cut--
	}
	return s[:cut] + "…"
}

// blocked recognizes the target policy's refusal. A DNS lookup keeps only
// the text of the error that stopped it, so the refusal to reach a
// monitor's resolver is recognized by its words there.
func blocked(err error) (map[string]string, bool) {
	if errors.Is(err, netguard.ErrBlocked) {
		return nil, true
	}
	dnsErr, ok := errors.AsType[*net.DNSError](err)
	return nil, ok && strings.Contains(dnsErr.Err, netguard.ErrBlocked.Error())
}

func is(targets ...error) errorMatcher {
	return func(err error) (map[string]string, bool) {
		for _, target := range targets {
			if errors.Is(err, target) {
				return nil, true
			}
		}
		return nil, false
	}
}

func as[T error](err error) (map[string]string, bool) {
	_, ok := errors.AsType[T](err)
	return nil, ok
}

func anyOf(phrases ...string) textMatcher {
	return func(msg string) (map[string]string, bool) {
		for _, phrase := range phrases {
			if strings.Contains(msg, phrase) {
				return nil, true
			}
		}
		return nil, false
	}
}

func allOf(phrases ...string) textMatcher {
	return func(msg string) (map[string]string, bool) {
		for _, phrase := range phrases {
			if !strings.Contains(msg, phrase) {
				return nil, false
			}
		}
		return nil, true
	}
}

// with gives what match recognizes fixed values.
func with(params map[string]string, match textMatcher) textMatcher {
	return func(msg string) (map[string]string, bool) {
		if _, ok := match(msg); !ok {
			return nil, false
		}
		return params, true
	}
}

// nameParams are the values of an explanation about a name that does not
// resolve, or false when they do not fit it.
type nameParams func(name string) (map[string]string, bool)

// serviceParams fit a cluster DNS name: service.namespace.svc, with the
// cluster's domain after it or not, and for a headless Service a pod's
// name in front of it.
func serviceParams(name string) (map[string]string, bool) {
	labels := strings.Split(strings.TrimSuffix(strings.ToLower(name), "."), ".")
	for i := 2; i < len(labels); i++ {
		if labels[i] == "svc" && labels[i-2] != "" && labels[i-1] != "" {
			return map[string]string{"service": labels[i-2], "namespace": labels[i-1]}, true
		}
	}
	return nil, false
}

// hostParams fit any name.
func hostParams(name string) (map[string]string, bool) {
	return map[string]string{"host": strings.TrimSuffix(name, ".")}, name != ""
}

func dnsNotFound(params nameParams) errorMatcher {
	return func(err error) (map[string]string, bool) {
		dnsErr, ok := errors.AsType[*net.DNSError](err)
		if !ok || !dnsErr.IsNotFound {
			return nil, false
		}
		return params(dnsErr.Name)
	}
}

// lookupFailure is how Go words a lookup of a name that does not exist,
// within the text of an error: "lookup NAME on SERVER: no such host".
var lookupFailure = regexp.MustCompile(`lookup (\S+?)(?: on \S+)?: no such host`)

func lookupNotFound(params nameParams) textMatcher {
	return func(msg string) (map[string]string, bool) {
		m := lookupFailure.FindStringSubmatch(msg)
		if m == nil {
			return nil, false
		}
		return params(m[1])
	}
}

// dnsFailed recognizes a lookup that failed for another reason than the
// name not existing: the server did not answer, or answered with an error.
func dnsFailed(err error) (map[string]string, bool) {
	dnsErr, ok := errors.AsType[*net.DNSError](err)
	return nil, ok && !dnsErr.IsNotFound
}

func hostnameMismatch(err error) (map[string]string, bool) {
	hostErr, ok := errors.AsType[x509.HostnameError](err)
	if !ok {
		return nil, false
	}
	return map[string]string{"host": hostErr.Host}, true
}

// certificateHost finds the name a certificate was not valid for in the
// text of an error, in the three ways crypto/x509 words it; the name ends
// where the text quoting the error does.
var certificateHost = regexp.MustCompile(`x509: (?:certificate is valid for .*?, not|certificate is not valid for any names, but wanted to match|cannot validate certificate for) ([^\s"\\]+)`)

func certificateNames(msg string) (map[string]string, bool) {
	m := certificateHost.FindStringSubmatch(msg)
	if m == nil {
		return nil, false
	}
	return map[string]string{"host": m[1]}, true
}

func certificateExpired(err error) (map[string]string, bool) {
	invalid, ok := errors.AsType[x509.CertificateInvalidError](err)
	return nil, ok && invalid.Reason == x509.Expired
}

// certificateRejected recognizes any other reason a certificate failed
// verification.
func certificateRejected(err error) (map[string]string, bool) {
	if _, ok := errors.AsType[*tls.CertificateVerificationError](err); ok {
		return nil, true
	}
	_, ok := errors.AsType[x509.CertificateInvalidError](err)
	return nil, ok
}

// remoteAlert recognizes a TLS alert the server sent, which is how it
// refuses a handshake.
func remoteAlert(err error) (map[string]string, bool) {
	opErr, ok := errors.AsType[*net.OpError](err)
	return nil, ok && opErr.Op == "remote error"
}

// dialTimeout recognizes a connection attempt that nobody answered.
func dialTimeout(err error) (map[string]string, bool) {
	opErr, ok := errors.AsType[*net.OpError](err)
	return nil, ok && opErr.Op == "dial" && opErr.Timeout()
}

func timedOut(err error) (map[string]string, bool) {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return nil, true
	}
	netErr, ok := errors.AsType[net.Error](err)
	return nil, ok && netErr.Timeout()
}
