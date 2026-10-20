package checker

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"google.golang.org/grpc"

	"github.com/yusufceng/Kubeat/backend/internal/explain"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// explanation is the code a result's diagnostics explain it with; empty
// when they do not.
func explanation(r store.CheckResult) string {
	if r.Diagnostics == nil || r.Diagnostics.Explanation == nil {
		return ""
	}
	return r.Diagnostics.Explanation.Code
}

// routerPage is OpenShift's router page, carrying things that must not
// reach a result.
const routerPage = `<html><head><style>body { margin: 0 }</style></head><body>
<h1>Application is not available</h1>
<p>The application is currently not serving requests at this endpoint.</p>
<!-- TOPSECRET-BODY: internal-token=abc123 -->
</body></html>`

// A failed check keeps where its time went, who answered and how, and what
// that means; but none of the body, and of the headers only the few kept,
// cut down where they could carry a credential.
func TestHTTPFailureKeepsDiagnosticsButNotTheAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		h := w.Header()
		h.Set("Content-Type", "text/html")
		h.Set("Server", "haproxy")
		h.Set("Set-Cookie", "session=s3cr3t-cookie; HttpOnly")
		h.Set("X-Internal-Secret", "do-not-keep")
		h.Set("Location", "https://probe:hunter2@login.example/next?token=one-time#frag")
		h.Add("Www-Authenticate", `Bearer realm="corp", error="invalid_token", error_description="token of alice@corp expired"`)
		h.Add("Www-Authenticate", `Basic realm="a, b"`)
		h.Set("X-Request-Id", "7b1f-req")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(routerPage))
	}))
	defer srv.Close()

	got := New(Options{}).Check(context.Background(), httpMonitor(srv.URL+"/healthz"))
	d := got.Diagnostics
	if got.Up || got.StatusCode != 503 || d == nil {
		t.Fatalf("result = %+v", got)
	}
	if d.Explanation == nil || d.Explanation.Code != explain.RouteNoEndpoints {
		t.Errorf("explanation = %+v", d.Explanation)
	}
	if d.StatusLine != "HTTP/1.1 503 Service Unavailable" || d.RemoteAddr != srv.Listener.Addr().String() || d.Redirects != 0 || d.TLS != nil {
		t.Errorf("diagnostics = %+v", d)
	}
	want := map[string]string{
		"content-type":     "text/html",
		"content-length":   d.Headers["content-length"],
		"server":           "haproxy",
		"location":         "https://login.example/next?…",
		"www-authenticate": "Bearer, Basic",
		"x-request-id":     "7b1f-req",
	}
	if !maps.Equal(d.Headers, want) || d.Headers["content-length"] == "" {
		t.Errorf("headers = %v\nwant      %v", d.Headers, want)
	}
	tm := d.Timing
	if tm == nil || tm.Total <= 0 || tm.Connect == nil || tm.FirstByte == nil || tm.DNS != nil || tm.TLS != nil || *tm.FirstByte > tm.Total {
		t.Errorf("timing = %+v", tm)
	}

	encoded, _ := json.Marshal(d)
	for _, secret := range []string{"TOPSECRET", "internal-token", "Application is not available", "s3cr3t", "do-not-keep", "hunter2", "probe", "one-time", "frag", "alice", "realm"} {
		if strings.Contains(string(encoded), secret) {
			t.Errorf("the diagnostics keep %q: %s", secret, encoded)
		}
	}
}

// A successful check keeps nothing more; a failure on the content keeps how
// the request went, with nothing to explain it beyond its error.
func TestOnlyFailuresKeepDiagnostics(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"status":"degraded"}`))
	}))
	defer srv.Close()
	c := New(Options{BodyLimit: 1 << 10})

	if got := c.Check(context.Background(), httpMonitor(srv.URL)); !got.Up || got.Diagnostics != nil {
		t.Errorf("success: %+v", got)
	}
	m := httpMonitor(srv.URL)
	m.HTTP.Keyword = `"status":"ok"`
	got := c.Check(context.Background(), m)
	if got.Up || got.Diagnostics == nil || got.Diagnostics.Explanation != nil || got.Diagnostics.StatusLine != "HTTP/1.1 200 OK" {
		t.Errorf("keyword failure: %+v", got.Diagnostics)
	}

	// Inverted: a target that answers fails the monitor, and the details
	// say what answered; one that does not answer passes it, with none.
	m = httpMonitor(srv.URL)
	m.Inverted = true
	if got := c.Check(context.Background(), m); got.Up || got.Diagnostics == nil || got.Diagnostics.Explanation != nil || got.Diagnostics.StatusLine != "HTTP/1.1 200 OK" {
		t.Errorf("inverted, answering: %+v", got)
	}
	m.Target = closedAddr(t, "http://")
	if got := c.Check(context.Background(), m); !got.Up || got.Diagnostics != nil {
		t.Errorf("inverted, not answering: %+v", got)
	}
}

// closedAddr is an address nothing listens at, with prefix in front.
func closedAddr(t *testing.T, prefix string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return prefix + addr
}

func TestHTTPFailuresAreExplained(t *testing.T) {
	loop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/again?n=1", http.StatusFound)
	}))
	defer loop.Close()
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(5 * time.Second):
		case <-r.Context().Done():
		}
	}))
	defer slow.Close()
	secure := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer secure.Close()
	plain := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer plain.Close()
	port := secure.Listener.Addr().(*net.TCPAddr).Port

	c := New(Options{})
	check := func(m store.Monitor) store.CheckResult {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		return c.Check(ctx, m)
	}

	refused := closedAddr(t, "")
	got := check(httpMonitor("http://" + refused + "/"))
	if explanation(got) != explain.ConnectionRefused || got.Diagnostics.RemoteAddr != refused || got.Diagnostics.StatusLine != "" || got.Diagnostics.Timing.FirstByte != nil {
		t.Errorf("refused: %+v", got.Diagnostics)
	}

	got = check(httpMonitor(slow.URL))
	if explanation(got) != explain.ResponseTimeout || got.Diagnostics.Timing.Connect == nil {
		t.Errorf("slow: %+v", got.Diagnostics)
	}

	m := httpMonitor(loop.URL)
	m.HTTP.MaxRedirects = 2
	got = check(m)
	if d := got.Diagnostics; explanation(got) != explain.TooManyRedirects || d.Redirects != 2 || d.StatusLine != "HTTP/1.1 302 Found" || d.Headers["location"] != "/again?…" {
		t.Errorf("redirect loop: %+v", got.Diagnostics)
	}

	// The certificate verification refused says why it was refused.
	got = check(httpMonitor(secure.URL))
	cert := secure.Certificate()
	if d := got.Diagnostics; explanation(got) != explain.TLSUnknownAuthority || d.TLS == nil || !d.TLS.NotAfter.Equal(cert.NotAfter) ||
		!slices.Contains(d.TLS.SANs, "example.com") || !slices.Contains(d.TLS.SANs, "127.0.0.1") || d.Timing.TLS == nil {
		t.Errorf("unknown authority: %+v, TLS %+v", got.Diagnostics, got.Diagnostics.TLS)
	}
	// With the certificate's issuer trusted, a name it was not issued for.
	trusting := New(Options{})
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	trusting.verifying.TLSClientConfig = &tls.Config{RootCAs: roots}
	got = trusting.Check(context.Background(), httpMonitor("https://localhost:"+strconv.Itoa(port)+"/"))
	if d := got.Diagnostics; explanation(got) != explain.TLSHostnameMismatch || d.Explanation.Params["host"] != "localhost" || d.TLS == nil || len(d.TLS.SANs) == 0 {
		t.Errorf("hostname: %q %+v", got.Error, got.Diagnostics)
	}
	got = check(httpMonitor("http://" + secure.Listener.Addr().String() + "/"))
	if explanation(got) != explain.HTTPToTLSPort || got.StatusCode != 400 {
		t.Errorf("plain HTTP to a TLS port: %+v %+v", got, got.Diagnostics)
	}
	got = check(httpMonitor("https://" + plain.Listener.Addr().String() + "/"))
	if explanation(got) != explain.TLSToPlainPort {
		t.Errorf("TLS to a plain port: %+v %+v", got, got.Diagnostics)
	}
	// Verified, the certificate is described too.
	m = httpMonitor(secure.URL)
	m.HTTP.ExpectedStatus = "204"
	m.HTTP.SkipTLSVerify = true
	got = check(m)
	if d := got.Diagnostics; d == nil || d.TLS == nil || d.TLS.Version != "TLS 1.3" || !d.TLS.NotAfter.Equal(cert.NotAfter) || explanation(got) != "" {
		t.Errorf("verified TLS: %+v", got.Diagnostics)
	}
}

// The phases tell a request that ran out of time where it was.
func TestTimeoutPhase(t *testing.T) {
	now := time.Now()
	for want, p := range map[string]phases{
		explain.DNSFailed:       {request: now, dnsStart: now},
		explain.ConnectTimeout:  {request: now, dnsStart: now, dnsDone: now, connectStart: now},
		explain.ResponseTimeout: {request: now, connectStart: now, connectDone: now, connected: true, tlsStart: now},
		explain.Timeout:         {request: now},
	} {
		r := &recorder{last: p}
		if got := r.timeout(); got != want {
			t.Errorf("%+v: %s, want %s", p, got, want)
		}
	}
	// A refused attempt followed by none that worked is still connecting.
	r := &recorder{last: phases{connectStart: now, connectDone: now}}
	if got := r.timeout(); got != explain.ConnectTimeout {
		t.Errorf("after a failed attempt: %s", got)
	}
	r = &recorder{last: phases{gotConn: now}}
	if got := r.timeout(); got != explain.ResponseTimeout {
		t.Errorf("with a connection: %s", got)
	}
}

func TestKeptHeaders(t *testing.T) {
	h := http.Header{}
	h.Add("Via", "1.1 router")
	h.Add("Via", "1.1 envoy")
	h.Set("Retry-After", "120")
	h.Set("X-Envoy-Upstream-Service-Time", "12")
	h.Set("Server", strings.Repeat("x", 300))
	h.Set("Location", "/login?next=%2Fadmin")
	h.Set("Authorization", "Bearer abc")
	h.Set("Proxy-Authenticate", "Basic")
	h.Set("Cookie", "a=b")
	got := headers(h)
	want := map[string]string{
		"via": "1.1 router, 1.1 envoy", "retry-after": "120", "x-envoy-upstream-service-time": "12",
		"server": strings.Repeat("x", maxHeaderValue) + "…", "location": "/login?…",
	}
	if !maps.Equal(got, want) {
		t.Errorf("headers = %v", got)
	}
	if headers(http.Header{"Set-Cookie": {"a=b"}}) != nil {
		t.Error("an answer without a kept header keeps something")
	}

	// What comes from the network is kept printable and short.
	if got := clean(" ok\x00\x1b[31m\x7fş\xff ", 10); got != "ok[31mş" {
		t.Errorf("clean = %q", got)
	}
	if got := clean(strings.Repeat("ş", 10), 5); got != "şş…" {
		t.Errorf("clean of a long value = %q", got)
	}

	for in, want := range map[string]string{
		"https://u:p@host.example:8443/a/b?c=d#e": "https://host.example:8443/a/b?…",
		"https://host.example/a":                  "https://host.example/a",
		"/a?":                                     "/a?…",
		"%zz":                                     "",
	} {
		if got := withoutCredentials(in); got != want {
			t.Errorf("withoutCredentials(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{
		`Negotiate YIIGhgYJKoZIhvcSAQICAQ==, NTLM`:                       "Negotiate, NTLM",
		`Bearer realm="a, Basic b", error="invalid_token"`:               "Bearer",
		`Digest realm="x", nonce="y", algorithm=MD5, qop="auth", Bearer`: "Digest, Bearer",
		`realm="only parameters"`:                                        "",
	} {
		if got := authSchemes([]string{in}); got != want {
			t.Errorf("authSchemes(%q) = %q, want %q", in, got, want)
		}
	}
}

// Diagnostics too large to keep lose their least telling parts, and keep
// their explanation.
func TestDiagnosticsAreBounded(t *testing.T) {
	long := strings.Repeat("a", maxName)
	d := &store.Diagnostics{
		Explanation: &store.Explanation{Code: explain.DNSServiceNotFound, Params: map[string]string{"service": long, "namespace": long}},
		Timing:      &store.Timing{Total: 12345.678},
		RemoteAddr:  "[fd00:1234:5678:9abc:def0:1234:5678:9abc]:8443",
		StatusLine:  "HTTP/1.1 503 " + strings.Repeat("r", maxStatusLine),
		TLS:         &store.TLSDetails{Version: "TLS 1.3", Subject: long, Issuer: long, SANs: []string{long, long, long, long, long}, MoreSANs: 20, NotAfter: time.Now()},
		Headers:     map[string]string{},
		Pod:         long,
	}
	for _, name := range keptHeaders {
		d.Headers[strings.ToLower(name)] = strings.Repeat("h", maxHeaderValue)
	}
	if encodedSize(d) <= maxDiagnostics {
		t.Fatalf("the sample is only %d bytes", encodedSize(d))
	}
	bound(d)
	if size := encodedSize(d); size > maxDiagnostics || d.Explanation == nil || d.Timing == nil || d.StatusLine == "" {
		t.Errorf("bounded to %d bytes: %+v", size, d)
	}
	// What fits stays as it is.
	small := &store.Diagnostics{Explanation: &store.Explanation{Code: explain.Status503}, Headers: map[string]string{"server": "envoy"}}
	bound(small)
	if small.Headers["server"] != "envoy" {
		t.Errorf("a small one lost a header: %+v", small)
	}
}

func TestWebSocketFailureIsExplained(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Server", "envoy")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("no healthy upstream"))
	}))
	defer srv.Close()
	got := New(Options{}).Check(context.Background(), store.Monitor{Type: store.MonitorWebSocket, Target: "ws" + strings.TrimPrefix(srv.URL, "http")})
	d := got.Diagnostics
	if got.Up || explanation(got) != explain.ProxyNoHealthyUpstream || d.Explanation.Params["proxy"] != "Envoy" ||
		d.Headers["server"] != "envoy" || d.StatusLine != "HTTP/1.1 503 Service Unavailable" || d.Timing == nil {
		t.Errorf("websocket: %+v %+v", got, d)
	}
}

// Every engine that reaches out over the network explains its transport
// errors; only HTTP and WebSocket checks time their phases.
func TestEnginesExplainTheirErrors(t *testing.T) {
	addr := closedAddr(t, "")
	dns := dnsServer(t, func(dnsmessage.Question) []dnsmessage.ResourceBody { return nil })
	c := New(Options{Pushes: fakePushes{err: store.ErrNotFound}, Workloads: fakeWorkloads{desired: 2, ready: 1}})
	for _, tc := range []struct {
		m    store.Monitor
		want string
	}{
		{store.Monitor{Type: store.MonitorTCP, Target: addr}, explain.ConnectionRefused},
		{store.Monitor{Type: store.MonitorGRPC, Target: addr}, explain.ConnectionRefused},
		{store.Monitor{Type: store.MonitorWebSocket, Target: "ws://" + addr}, explain.ConnectionRefused},
		{store.Monitor{Type: store.MonitorRedis, Target: addr}, explain.ConnectionRefused},
		{store.Monitor{Type: store.MonitorPostgres, Target: addr, Database: store.DatabaseOptions{Username: "u", Query: "SELECT 1"}}, explain.ConnectionRefused},
		{store.Monitor{Type: store.MonitorMySQL, Target: addr, Database: store.DatabaseOptions{Username: "u", Query: "SELECT 1"}}, explain.ConnectionRefused},
		{store.Monitor{Type: store.MonitorDNS, Target: "api.shop.svc.cluster.local", DNS: store.DNSOptions{RecordType: "A", Resolver: dns}}, explain.DNSServiceNotFound},
		{store.Monitor{Type: store.MonitorDNS, Target: "gone.shop.example", DNS: store.DNSOptions{RecordType: "A", Resolver: dns}}, explain.DNSNotFound},
		// A push monitor's failure is its source's report, and a workload
		// monitor's the cluster's; neither is explained.
		{store.Monitor{Type: store.MonitorPush, Interval: time.Minute}, ""},
		{store.Monitor{Type: store.MonitorWorkload, Target: "shop/deployment/web"}, ""},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		got := c.Check(ctx, tc.m)
		cancel()
		if got.Up || explanation(got) != tc.want {
			t.Errorf("%s %s: %q, %+v", tc.m.Type, tc.m.Target, got.Error, got.Diagnostics)
			continue
		}
		if timed := got.Diagnostics != nil && got.Diagnostics.Timing != nil; timed != (tc.m.Type == store.MonitorWebSocket) {
			t.Errorf("%s: timing %+v", tc.m.Type, got.Diagnostics)
		}
	}

	// A service name says which Service and namespace it meant.
	got := c.Check(context.Background(), store.Monitor{Type: store.MonitorDNS, Target: "api.shop.svc.cluster.local", DNS: store.DNSOptions{RecordType: "A", Resolver: dns}})
	if p := got.Diagnostics.Explanation.Params; p["service"] != "api" || p["namespace"] != "shop" {
		t.Errorf("service name: %v", p)
	}
}

// A plaintext gRPC server asked for TLS: the status says so in words only,
// which explain it.
func TestGRPCTLSToAPlainServer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer()
	go srv.Serve(ln)
	defer srv.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	got := New(Options{}).Check(ctx, store.Monitor{Type: store.MonitorGRPC, Target: ln.Addr().String(), GRPC: store.GRPCOptions{TLS: true, SkipTLSVerify: true}})
	if got.Up || explanation(got) != explain.TLSToPlainPort {
		t.Errorf("tls against a plaintext server: %q, %+v", got.Error, got.Diagnostics)
	}
}

// The details of a monitor that checks each pod are those of the first pod
// that failed, named.
func TestEachPodNamesThePodOfTheDetails(t *testing.T) {
	good := podServer(t, func(http.ResponseWriter, *http.Request) {})
	bad := podServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("no healthy upstream"))
	})
	m := httpMonitor("http://api.shop.svc:8080/healthz")
	m.Namespace, m.Source, m.EachPod = "shop", "service/api:8080", true

	got := New(Options{Endpoints: fakeEndpoints{endpoints: []Endpoint{good, bad}}}).Check(context.Background(), m)
	d := got.Diagnostics
	if got.Up || d == nil || d.Pod != bad.Pod || d.RemoteAddr != bad.Address || explanation(got) != explain.ProxyNoHealthyUpstream {
		t.Errorf("each pod: %+v %+v", got, d)
	}

	tcp := store.Monitor{Type: store.MonitorTCP, Target: "api.shop.svc:8080", Namespace: "shop", Source: "service/api:8080", EachPod: true}
	gone := Endpoint{Pod: "api-gone", Address: closedAddr(t, "")}
	got = New(Options{Endpoints: fakeEndpoints{endpoints: []Endpoint{good, gone}}}).Check(context.Background(), tcp)
	if got.Up || got.Diagnostics == nil || got.Diagnostics.Pod != "api-gone" || explanation(got) != explain.ConnectionRefused {
		t.Errorf("each pod, tcp: %+v %+v", got, got.Diagnostics)
	}
}
