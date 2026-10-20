package k8s

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// What anyone may be told about a failed request to the cluster API: its
// kind, never Kubeat's service account or the API server's address.
func TestFailureReason(t *testing.T) {
	apiURL := "https://172.30.0.1:443/apis/apps/v1/namespaces/shop/deployments/api"
	refusal := apierrors.NewForbidden(schema.GroupResource{Group: "apps", Resource: "deployments"}, "api",
		errors.New(`User "system:serviceaccount:kubeat:kubeat" cannot get resource "deployments" in API group "apps" in the namespace "shop"`))
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"forbidden", fmt.Errorf("get deployment: %w", refusal), "the cluster API refused Kubeat's request (forbidden); ask an administrator"},
		{"unauthorized", apierrors.NewUnauthorized("Unauthorized"),
			"the cluster API did not accept Kubeat's credentials (unauthorized); ask an administrator"},
		{"not found", fmt.Errorf("list endpointslices: %w", apierrors.NewNotFound(schema.GroupResource{Group: "discovery.k8s.io", Resource: "endpointslices"}, "")),
			"the cluster API does not have what was asked for (not found)"},
		{"no answer in time", fmt.Errorf("get deployment: %w", &url.Error{Op: "Get", URL: apiURL, Err: context.DeadlineExceeded}),
			"the cluster API did not answer in time"},
		{"the server's timeout", apierrors.NewTimeoutError("request did not complete", 1), "the cluster API did not answer in time"},
		{"untrusted certificate", &url.Error{Op: "Get", URL: apiURL, Err: &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}},
			"the cluster API's certificate is not trusted; ask an administrator"},
		{"nothing listening", fmt.Errorf("get service: %w", &url.Error{Op: "Get", URL: apiURL,
			Err: &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connect: connection refused")}}),
			"the cluster API could not be reached"},
		{"Kubeat's own words", fmt.Errorf("%w: deployment shop/api does not exist", ErrNoWorkload),
			"k8s: no workload behind this source: deployment shop/api does not exist"},
		{"anything else", apierrors.NewInternalError(errors.New("etcd at 10.0.0.7 is down")),
			"the request to the cluster API failed; an administrator can see the details in the log"},
	} {
		got := FailureReason(tc.err)
		if got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
		for _, secret := range []string{"system:serviceaccount", "172.30.0.1", "10.0.0.7"} {
			if strings.Contains(got, secret) {
				t.Errorf("%s: %q names %s", tc.name, got, secret)
			}
		}
	}
	if got := FailureReason(nil); got != "" {
		t.Errorf("no error: %q", got)
	}
}
