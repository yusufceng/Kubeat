package k8s

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// FailureReason says what kind of failure err, from a request of this
// client to the cluster API, was, in words anyone may read: forbidden,
// unauthorized, not found, no answer in time, an untrusted certificate, no
// connection. The error itself names Kubeat's service account, the API
// server's address and what is wrong with its certificate, which only an
// administrator needs, from the log. Kubeat's own words about the object
// asked for (ErrNoWorkload, ErrOutOfScope) name nothing else and are
// returned as they are.
func FailureReason(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrNoWorkload), errors.Is(err, ErrOutOfScope):
		return err.Error()
	case apierrors.IsForbidden(err):
		return "the cluster API refused Kubeat's request (forbidden); ask an administrator"
	case apierrors.IsUnauthorized(err):
		return "the cluster API did not accept Kubeat's credentials (unauthorized); ask an administrator"
	case apierrors.IsNotFound(err):
		return "the cluster API does not have what was asked for (not found)"
	case apierrors.IsTimeout(err), apierrors.IsServerTimeout(err), timedOut(err):
		return "the cluster API did not answer in time"
	case untrustedCertificate(err):
		return "the cluster API's certificate is not trusted; ask an administrator"
	case unreachable(err):
		return "the cluster API could not be reached"
	default:
		return "the request to the cluster API failed; an administrator can see the details in the log"
	}
}

// timedOut reports whether err is the request running out of time, on the
// way to the server or waiting for it.
func timedOut(err error) bool {
	var netErr net.Error
	return errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout())
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
// all: no route to it, nothing listening, no DNS answer. The HTTP client
// reports these as *url.Error, which is a net.Error.
func unreachable(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr)
}
