// Package explain tells what a failed check most likely means: the pages
// that a cluster's routers, ingress controllers and proxies answer with when
// nothing is behind them, the error statuses, and the transport errors of
// every kind of check. A failure it recognizes becomes an explanation: a
// stable code with the few values its wording needs. The API returns the
// code, which the frontend words in each user's language, with the English
// text as a fallback; notifications carry the text in the install's
// language. Both texts are kept here (texts.go), the rules that recognize
// failures in classify.go.
//
// An answer's body is looked at only to recognize the known pages, and only
// its first PeekBytes. What is kept is the code, never the body: a target is
// set by whoever may edit a monitor, and what an internal service answers
// must not become readable through its results.
package explain

import (
	"sort"
	"strings"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// The codes of the explanations. They are part of the API: a code keeps its
// meaning once released, and the frontend translates by it.
const (
	// Pages that the cluster's own routing answers with.
	RouteNoEndpoints       = "route_no_endpoints"        // OpenShift router: no Route matches, or no ready pods behind it
	IngressDefaultBackend  = "ingress_default_backend"   // ingress-nginx's default backend: no Ingress rule matches
	IngressNoEndpoints     = "ingress_no_endpoints"      // nginx's own 503 page: the Ingress's Service has no ready pods
	ProxyNoHealthyUpstream = "proxy_no_healthy_upstream" // {proxy} has no healthy backend
	ProxyUpstreamConnect   = "proxy_upstream_connect_error"
	ProxyUpstreamTLS       = "proxy_upstream_tls_error"
	MeshAccessDenied       = "mesh_access_denied" // a service mesh's authorization policy
	HTTPToTLSPort          = "http_to_tls_port"   // plain HTTP sent to a port that expects TLS

	// Error statuses, for an answer that no page above explains.
	Status401 = "http_401"
	Status403 = "http_403"
	Status404 = "http_404"
	Status429 = "http_429"
	Status500 = "http_500"
	Status502 = "http_502"
	Status503 = "http_503"
	Status504 = "http_504"

	// Failures to get an answer at all.
	BlockedByPolicy       = "blocked_by_policy"
	DNSServiceNotFound    = "dns_service_not_found" // {service} {namespace}
	DNSNotFound           = "dns_not_found"         // {host}
	DNSFailed             = "dns_failed"
	ConnectionRefused     = "connection_refused"
	ConnectionReset       = "connection_reset"
	ConnectionClosed      = "connection_closed"
	HostUnreachable       = "host_unreachable"
	ConnectTimeout        = "connect_timeout"
	ResponseTimeout       = "response_timeout"
	Timeout               = "timeout"               // when it is not known which of the two above
	TLSHostnameMismatch   = "tls_hostname_mismatch" // {host}
	TLSExpired            = "tls_expired"
	TLSUnknownAuthority   = "tls_unknown_authority"
	TLSInvalidCertificate = "tls_certificate"
	TLSToPlainPort        = "tls_to_plain_port"
	TLSHandshakeFailed    = "tls_handshake_failed"
	TooManyRedirects      = "too_many_redirects"
)

// Languages the texts are written in; the same as the notifications'.
const (
	English = "en"
	Turkish = "tr"
)

// Text words an explanation in a language, English unless it is one of the
// known ones. It is empty for a code this version does not know, such as
// one a later version wrote into a result.
func Text(e store.Explanation, language string) string {
	texts, ok := languages[language]
	if !ok {
		texts = languages[English]
	}
	template := texts[e.Code]
	if template == "" || len(e.Params) == 0 {
		return template
	}
	pairs := make([]string, 0, 2*len(e.Params))
	for name, value := range e.Params {
		pairs = append(pairs, "{"+name+"}", value)
	}
	return strings.NewReplacer(pairs...).Replace(template)
}

// Codes lists every code, sorted.
func Codes() []string {
	codes := make([]string, 0, len(languages[English]))
	for code := range languages[English] {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}
