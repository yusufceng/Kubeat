package api

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"regexp"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/yusufceng/Kubeat/backend/internal/auth"
	"github.com/yusufceng/Kubeat/backend/internal/k8s"
)

// What a user is told when a request Kubeat makes for them, to the cluster
// API or to the metrics server, fails. The error itself names Kubeat's
// service account, the API server's address, the metrics server's URL and
// how their certificates fail to verify: an administrator needs that to
// fix the setup and gets it, and it always goes to the log. Everyone else
// is told what kind of failure it was, which is enough to know whom to ask.

// logDetails ends what everyone but an administrator is told about a
// failure that has no kind of its own.
const logDetails = "an administrator can see the details in the log"

// forbiddenRequest picks the verb, the resource, its API group and the
// namespace out of the API server's refusal, which reads
// `User "…" cannot list resource "services" in API group "" in the
// namespace "shop"`. The user, Kubeat's service account, is left out.
var forbiddenRequest = regexp.MustCompile(`cannot ([a-z]+) resource "([^"]+)" in API group "([^"]*)"(?: in the namespace "([^"]+)")?`)

// upstreamReason is what the principal may be told about err, a failed
// request to the cluster API or the metrics server: the error itself for
// an administrator, otherwise the kind of failure.
func upstreamReason(p auth.Principal, err error) string {
	switch {
	case err == nil:
		return logDetails
	case p.IsAdmin(),
		// Kubeat's own words about the object it was asked for, which name
		// nothing else.
		errors.Is(err, k8s.ErrNoWorkload), errors.Is(err, k8s.ErrOutOfScope):
		return err.Error()
	case apierrors.IsForbidden(err):
		return forbiddenReason(p, err)
	case apierrors.IsUnauthorized(err):
		return "the server did not accept Kubeat's credentials; ask an administrator"
	case untrustedCertificate(err):
		return "the server's certificate is not trusted; ask an administrator"
	case unreachable(err):
		return "the server could not be reached"
	default:
		return logDetails
	}
}

// forbiddenReason says what the API server would not let Kubeat's service
// account do: the verb and the resource, and the namespace when the
// principal may read it. The Status details carry the resource; the verb
// and the namespace are only in the message.
func forbiddenReason(p auth.Principal, err error) string {
	var status apierrors.APIStatus
	if !errors.As(err, &status) {
		return logDetails
	}
	s := status.Status()
	verb, resource, namespace := "access", "", ""
	if d := s.Details; d != nil && d.Kind != "" {
		resource = qualifiedResource(d.Kind, d.Group)
	}
	if m := forbiddenRequest.FindStringSubmatch(s.Message); m != nil {
		verb, namespace = m[1], m[4]
		if resource == "" {
			resource = qualifiedResource(m[2], m[3])
		}
	}
	if resource == "" {
		return "Kubeat's service account was refused; ask an administrator"
	}
	text := "Kubeat's service account may not " + verb + " " + resource
	if namespace != "" && p.CanRead(namespace) {
		text += " in " + namespace
	}
	return text + "; ask an administrator"
}

// qualifiedResource writes a resource the way kubectl does, with its API
// group unless that is the core one: services, routes.route.openshift.io.
func qualifiedResource(resource, group string) string {
	if group == "" {
		return resource
	}
	return resource + "." + group
}

// untrustedCertificate reports whether err is the server's certificate
// failing verification: signed by an unknown authority, expired, or for
// another host.
func untrustedCertificate(err error) bool {
	var (
		verification *tls.CertificateVerificationError
		authority    x509.UnknownAuthorityError
		invalid      x509.CertificateInvalidError
		hostname     x509.HostnameError
	)
	return errors.As(err, &verification) || errors.As(err, &authority) || errors.As(err, &invalid) || errors.As(err, &hostname)
}

// unreachable reports whether err is a failure to talk to the server at
// all: no route to it, nothing listening, no DNS answer, no answer in time.
// The HTTP clients report these as *url.Error, which is a net.Error.
func unreachable(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) || errors.Is(err, context.DeadlineExceeded)
}

// clusterDetail is how the backend reaches the cluster, or why it does not
// (Options.DiscoveryStatus), as far as the principal may know: it names the
// kubeconfig context or carries the error of connecting, which only an
// administrator needs and can act on. Others get "".
func (h *handler) clusterDetail(p auth.Principal) string {
	if p.IsAdmin() {
		return h.opts.DiscoveryStatus
	}
	return ""
}

// withDetail appends detail to text after a colon, when there is one.
func withDetail(text, detail string) string {
	if detail == "" {
		return text
	}
	return text + ": " + detail
}
