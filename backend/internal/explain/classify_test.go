package explain

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/yusufceng/Kubeat/backend/internal/netguard"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// openShiftPage is the OpenShift router's 503 page, whose heading comes
// after a style sheet of about 1.5 KiB.
var openShiftPage = `<html>
  <head>
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <style type="text/css">` + strings.Repeat("\n      body { font-family: \"Helvetica Neue\", Helvetica, Arial, sans-serif; }", 20) + `
    </style>
  </head>
  <body>
    <div>
      <h1>Application is not available</h1>
      <p>The application is currently not serving requests at this endpoint. It may not have been started or is still starting.</p>
    </div>
  </body>
</html>`

func check(t *testing.T, what string, got *store.Explanation, code string, params map[string]string) {
	t.Helper()
	switch {
	case code == "" && got != nil:
		t.Errorf("%s: explained as %+v, want nothing", what, *got)
	case code == "":
	case got == nil:
		t.Errorf("%s: not explained, want %s", what, code)
	case got.Code != code || !maps.Equal(got.Params, params):
		t.Errorf("%s: %s %v, want %s %v", what, got.Code, got.Params, code, params)
	}
}

func TestResponsePages(t *testing.T) {
	if len(openShiftPage) < 1500 || len(openShiftPage) > PeekBytes {
		t.Fatalf("the sample page is %d bytes", len(openShiftPage))
	}
	envoy := map[string]string{"proxy": "Envoy"}
	for _, c := range []struct {
		what   string
		status int
		body   string
		code   string
		params map[string]string
	}{
		{"OpenShift router", 503, openShiftPage, RouteNoEndpoints, nil},
		{"default-http-backend", 404, "default backend - 404", IngressDefaultBackend, nil},
		{"Envoy without endpoints", 503, "no healthy upstream", ProxyNoHealthyUpstream, envoy},
		{"Envoy, TLS", 503, "upstream connect error or disconnect/reset before headers. reset reason: connection failure, transport failure reason: TLS error: 268435703:SSL routines:OPENSSL_internal:WRONG_VERSION_NUMBER", ProxyUpstreamTLS, envoy},
		{"Envoy, refused", 503, "upstream connect error or disconnect/reset before headers. retried and the latest reset reason: remote connection failure, transport failure reason: delayed connect error: Connection refused", ProxyUpstreamConnect, envoy},
		{"Istio authorization", 403, "RBAC: access denied", MeshAccessDenied, nil},
		{"HAProxy", 503, "<html><body><h1>503 Service Unavailable</h1>\nNo server is available to handle this request.\n</body></html>", ProxyNoHealthyUpstream, map[string]string{"proxy": "HAProxy"}},
		{"Traefik", 503, "no available server", ProxyNoHealthyUpstream, map[string]string{"proxy": "Traefik"}},
		{"ingress-nginx without endpoints", 503, "<html>\r\n<head><title>503 Service Temporarily Unavailable</title></head>\r\n<body>\r\n<center><h1>503 Service Temporarily Unavailable</h1></center>\r\n<hr><center>nginx</center>\r\n</body>\r\n</html>", IngressNoEndpoints, nil},
		{"Go's TLS server asked in plain HTTP", 400, "Client sent an HTTP request to an HTTPS server.\n", HTTPToTLSPort, nil},
		{"nginx's TLS port asked in plain HTTP", 400, "<center>The plain HTTP request was sent to HTTPS port</center>", HTTPToTLSPort, nil},

		// The pages count only with the status they come with.
		{"OpenShift's page with 200", 200, openShiftPage, "", nil},
		{"nginx's 404 page", 404, "<center><h1>404 Not Found</h1></center>\r\n<hr><center>nginx</center>", Status404, nil},

		{"401", 401, "", Status401, nil},
		{"403", 403, "forbidden", Status403, nil},
		{"404", 404, "", Status404, nil},
		{"429", 429, "", Status429, nil},
		{"500", 500, "stack trace", Status500, nil},
		{"502", 502, "", Status502, nil},
		{"503 of the application", 503, `{"status":"down"}`, Status503, nil},
		{"504", 504, "upstream request timeout", Status504, nil},
		{"418", 418, "", "", nil},
		{"200", 200, "", "", nil},
		{"301", 301, "", "", nil},

		// Only the beginning of a body is looked at.
		{"a phrase past the peek", 503, strings.Repeat("x", PeekBytes) + "no healthy upstream", Status503, nil},
	} {
		check(t, c.what, Response(c.status, []byte(c.body)), c.code, c.params)
	}
}

// fakeTimeout is a net.Error that timed out, as a dialer's is.
type fakeTimeout struct{}

func (fakeTimeout) Error() string   { return "i/o timeout" }
func (fakeTimeout) Timeout() bool   { return true }
func (fakeTimeout) Temporary() bool { return true }

func dial(err error) error {
	return &url.Error{Op: "Get", URL: "http://api.shop.svc:8080/healthz",
		Err: &net.OpError{Op: "dial", Net: "tcp", Err: err}}
}

func verification(err error) error {
	return &url.Error{Op: "Get", URL: "https://api.shop.svc/", Err: &tls.CertificateVerificationError{Err: err}}
}

func TestErrorChains(t *testing.T) {
	policy, err := netguard.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	refusal := policy.Check(netip.MustParseAddr("169.254.169.254"))
	leaf := &x509.Certificate{DNSNames: []string{"api.example.com"}}

	for _, c := range []struct {
		what   string
		err    error
		code   string
		params map[string]string
	}{
		{"policy", dial(refusal), BlockedByPolicy, nil},
		// A DNS lookup keeps only the text of what stopped it.
		{"policy at the resolver", &net.DNSError{Err: "dial udp 127.0.0.1:53: " + refusal.Error(), Name: "api.shop.example"}, BlockedByPolicy, nil},
		{"refused", dial(&os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED}), ConnectionRefused, nil},
		{"reset", &net.OpError{Op: "read", Err: &os.SyscallError{Syscall: "read", Err: syscall.ECONNRESET}}, ConnectionReset, nil},
		{"no route", dial(&os.SyscallError{Syscall: "connect", Err: syscall.EHOSTUNREACH}), HostUnreachable, nil},
		{"no network", dial(&os.SyscallError{Syscall: "connect", Err: syscall.ENETUNREACH}), HostUnreachable, nil},
		{"Service", dial(&net.DNSError{Err: "no such host", Name: "api.shop.svc.cluster.local", IsNotFound: true}), DNSServiceNotFound, map[string]string{"service": "api", "namespace": "shop"}},
		{"headless Service", &net.DNSError{Err: "no such host", Name: "web-0.nginx.default.svc", IsNotFound: true}, DNSServiceNotFound, map[string]string{"service": "nginx", "namespace": "default"}},
		{"other name", &net.DNSError{Err: "no such host", Name: "api.example.com.", IsNotFound: true}, DNSNotFound, map[string]string{"host": "api.example.com"}},
		{"not a Service: svc too early", &net.DNSError{Err: "no such host", Name: "x.svc", IsNotFound: true}, DNSNotFound, map[string]string{"host": "x.svc"}},
		{"DNS timeout", &net.DNSError{Err: "i/o timeout", Name: "api.shop.svc", IsTimeout: true}, DNSFailed, nil},
		{"DNS server failure", &net.DNSError{Err: "server misbehaving", Name: "api.shop.svc", IsTemporary: true}, DNSFailed, nil},
		{"hostname", verification(x509.HostnameError{Certificate: leaf, Host: "api.shop.svc"}), TLSHostnameMismatch, map[string]string{"host": "api.shop.svc"}},
		{"expired", verification(x509.CertificateInvalidError{Cert: leaf, Reason: x509.Expired}), TLSExpired, nil},
		{"unknown authority", verification(x509.UnknownAuthorityError{}), TLSUnknownAuthority, nil},
		{"other certificate problem", verification(x509.CertificateInvalidError{Cert: leaf, Reason: x509.NotAuthorizedToSign}), TLSInvalidCertificate, nil},
		{"plain port", fmt.Errorf("tls handshake: %w", tls.RecordHeaderError{Msg: "first record does not look like a TLS handshake"}), TLSToPlainPort, nil},
		{"alert", &net.OpError{Op: "remote error", Err: errors.New("tls: certificate required")}, TLSHandshakeFailed, nil},
		{"dial timeout", dial(fakeTimeout{}), ConnectTimeout, nil},
		{"deadline", &url.Error{Op: "Get", URL: "http://x", Err: context.DeadlineExceeded}, Timeout, nil},
		{"read deadline", fmt.Errorf("read: %w", os.ErrDeadlineExceeded), Timeout, nil},
		{"closed", &url.Error{Op: "Get", URL: "http://x", Err: io.EOF}, ConnectionClosed, nil},
		{"unexpected EOF", io.ErrUnexpectedEOF, ConnectionClosed, nil},
		{"anything else", errors.New("password authentication failed for user \"kubeat\""), "", nil},
		// What a server says is not taken for the policy's words.
		{"a server's own words", fmt.Errorf("login: %w", errors.New("ERR 10.0.0.1 is blocked by the target policy")), "", nil},
	} {
		check(t, c.what, Error(c.err), c.code, c.params)
	}
	check(t, "nil", Error(nil), "", nil)
}

// gRPC hands its transport errors over as text, as do proxies in a status.
func TestMessages(t *testing.T) {
	envoy := map[string]string{"proxy": "Envoy"}
	for _, c := range []struct {
		msg    string
		code   string
		params map[string]string
	}{
		{`connection error: desc = "transport: Error while dialing: dial tcp 127.0.0.1:1: connect: connection refused"`, ConnectionRefused, nil},
		{`connection error: desc = "transport: Error while dialing: dial tcp: lookup api.shop.svc on 10.96.0.10:53: no such host"`, DNSServiceNotFound, map[string]string{"service": "api", "namespace": "shop"}},
		{`connection error: desc = "transport: Error while dialing: dial tcp: lookup grpc.example.com: no such host"`, DNSNotFound, map[string]string{"host": "grpc.example.com"}},
		{"no healthy upstream", ProxyNoHealthyUpstream, envoy},
		{"upstream connect error or disconnect/reset before headers. reset reason: connection failure, transport failure reason: TLS error: 268435703:SSL routines", ProxyUpstreamTLS, envoy},
		// Envoy's words for what happened upstream are its explanation, not ours.
		{"upstream connect error or disconnect/reset before headers. reset reason: connection failure, transport failure reason: delayed connect error: connection refused", ProxyUpstreamConnect, envoy},
		{"RBAC: access denied", MeshAccessDenied, nil},
		{`authentication handshake failed: tls: failed to verify certificate: x509: certificate is valid for a.example, *.a.example, not b.example`, TLSHostnameMismatch, map[string]string{"host": "b.example"}},
		{`x509: certificate is not valid for any names, but wanted to match api.shop.svc`, TLSHostnameMismatch, map[string]string{"host": "api.shop.svc"}},
		{`x509: cannot validate certificate for 10.0.0.5 because it doesn't contain any IP SANs`, TLSHostnameMismatch, map[string]string{"host": "10.0.0.5"}},
		// As gRPC quotes it, the name ends where the quote does.
		{`connection error: desc = "transport: authentication handshake failed: tls: failed to verify certificate: x509: certificate is valid for a.example, not b.example"`, TLSHostnameMismatch, map[string]string{"host": "b.example"}},
		{`tls: failed to verify certificate: x509: certificate signed by unknown authority`, TLSUnknownAuthority, nil},
		{`tls: failed to verify certificate: x509: certificate has expired or is not yet valid: current time …`, TLSExpired, nil},
		{`tls: first record does not look like a TLS handshake`, TLSToPlainPort, nil},
		{`remote error: tls: handshake failure`, TLSHandshakeFailed, nil},
		{`dial tcp 127.0.0.1:1: 127.0.0.1 is blocked by the target policy (127.0.0.0/8); UPTIME_TARGET_ALLOW_CIDRS can allow it`, BlockedByPolicy, nil},
		{`connection error: desc = "transport: Error while dialing: dial tcp 10.0.0.9:50051: i/o timeout"`, Timeout, nil},
		{"error reading from server: EOF", ConnectionClosed, nil},
		{"the server does not implement the gRPC health checking protocol", "", nil},
	} {
		check(t, c.msg, Message(c.msg), c.code, c.params)
	}
}

// A name an explanation repeats is cut short, whatever its length.
func TestParamsAreShort(t *testing.T) {
	long := strings.Repeat("ş", 200) + ".example"
	got := Error(&net.DNSError{Err: "no such host", Name: long, IsNotFound: true})
	if got == nil || len(got.Params["host"]) > maxParam+len("…") || !strings.HasSuffix(got.Params["host"], "…") || !strings.HasPrefix(long, strings.TrimSuffix(got.Params["host"], "…")) {
		t.Errorf("explanation = %+v", got)
	}
	// A rule's own values stay as they are.
	if e := Response(503, []byte("no healthy upstream")); e == nil {
		t.Fatal("not explained")
	} else {
		e.Params["proxy"] = "changed"
	}
	if got := Response(503, []byte("no healthy upstream")); got.Params["proxy"] != "Envoy" {
		t.Errorf("a rule's value changed: %+v", got)
	}
}
