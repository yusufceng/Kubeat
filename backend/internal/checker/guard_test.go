package checker

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/explain"
	"github.com/yusufceng/Kubeat/backend/internal/netguard"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

func guard(t *testing.T, allow ...string) *netguard.Policy {
	t.Helper()
	p, err := netguard.New(nil, allow)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// Every engine connects through the guarded dialer, or asks the guard
// itself: none of them reaches the backend's own loopback.
func TestGuardHoldsEveryEngine(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	addr := srv.Listener.Addr().String()
	ws := "ws" + strings.TrimPrefix(srv.URL, "http")

	pod := httpMonitor("http://api.shop.svc:8080/healthz")
	pod.Namespace, pod.Source, pod.EachPod = "shop", "service/api:8080", true
	tcpPod := store.Monitor{Type: store.MonitorTCP, Target: "api.shop.svc:8080", Namespace: "shop", Source: "service/api:8080", EachPod: true}

	monitors := map[string]store.Monitor{
		"http":       httpMonitor(srv.URL),
		"http-name":  httpMonitor(strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)),
		"tcp":        {Type: store.MonitorTCP, Target: addr},
		"tcp-tls":    {Type: store.MonitorTCP, Target: addr, TCP: store.TCPOptions{TLS: true}},
		"ping":       {Type: store.MonitorPing, Target: "127.0.0.1"},
		"dns":        {Type: store.MonitorDNS, Target: "api.shop.example", DNS: store.DNSOptions{RecordType: "A", Resolver: "127.0.0.1:53"}},
		"grpc":       {Type: store.MonitorGRPC, Target: addr},
		"websocket":  {Type: store.MonitorWebSocket, Target: ws},
		"postgres":   {Type: store.MonitorPostgres, Target: "127.0.0.1:5432", Database: store.DatabaseOptions{Username: "u", Query: "SELECT 1"}},
		"mysql":      {Type: store.MonitorMySQL, Target: "127.0.0.1:3306", Database: store.DatabaseOptions{Username: "u", Query: "SELECT 1"}},
		"redis":      {Type: store.MonitorRedis, Target: "127.0.0.1:6379"},
		"each-pod":   pod,
		"each-pod-t": tcpPod,
	}
	c := New(Options{Guard: guard(t), Endpoints: fakeEndpoints{endpoints: []Endpoint{{Pod: "api-1", Address: addr}}}})
	for name, m := range monitors {
		// A refusal is not the failure an inverted monitor expects either:
		// the connection never left, so the policy's error stands.
		for _, inverted := range []bool{false, true} {
			m.Inverted = inverted
			t.Run(fmt.Sprintf("%s/inverted=%v", name, inverted), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				got := c.Check(ctx, m)
				if got.Up || !strings.Contains(got.Error, "is blocked by the target policy") || !strings.Contains(got.Error, "UPTIME_TARGET_ALLOW_CIDRS") {
					t.Errorf("result = %+v", got)
				}
				// And the explanation says so, not that nothing answered.
				if code := explanation(got); code != explain.BlockedByPolicy {
					t.Errorf("explained as %q: %+v", code, got.Diagnostics)
				}
			})
		}
	}
}

// An inverted monitor still takes a real failure of its target for the one
// it expects, and an answer for a failure of its own.
func TestInvertedMonitorBehindTheGuard(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	closed := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	closedAddr := closed.Listener.Addr().String()
	closed.Close()
	c := New(Options{Guard: guard(t, "127.0.0.0/8")})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	m := store.Monitor{Type: store.MonitorTCP, Target: closedAddr, Inverted: true}
	if got := c.Check(ctx, m); !got.Up || !strings.HasPrefix(got.Detail, "failed as expected: ") {
		t.Errorf("an allowed target that refuses: %+v", got)
	}
	m.Target = srv.Listener.Addr().String()
	if got := c.Check(ctx, m); got.Up || !strings.HasPrefix(got.Error, "the target responded") {
		t.Errorf("an allowed target that answers: %+v", got)
	}
}

// An exception lets the same targets through.
func TestGuardException(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	c := New(Options{Guard: guard(t, "127.0.0.0/8", "::1/128")})
	ctx := context.Background()

	for _, m := range []store.Monitor{
		httpMonitor(srv.URL),
		{Type: store.MonitorTCP, Target: srv.Listener.Addr().String()},
		{Type: store.MonitorRedis, Target: fakeRedis(t, "", "", "+PONG")},
	} {
		if got := c.Check(ctx, m); !got.Up {
			t.Errorf("%s %s: %+v", m.Type, m.Target, got)
		}
	}
}

// A proxy the operator set is reached at its exact address, a sidecar's on
// loopback too, without an exception; the rest of loopback stays closed.
func TestGuardTrustsTheProxy(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer proxy.Close()
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer other.Close()
	proxyURL, _ := url.Parse(proxy.URL)
	trusting := guard(t).Trust(netguard.HostPort(proxy.URL))
	c := New(Options{Guard: trusting})
	// What the transport would take from HTTP_PROXY.
	c.verifying.Proxy = trusting.Proxy(http.ProxyURL(proxyURL))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if got := c.Check(ctx, httpMonitor("http://192.0.2.10/healthz")); !got.Up || got.StatusCode != http.StatusOK {
		t.Errorf("through the trusted proxy: %+v", got)
	}
	for _, m := range []store.Monitor{
		httpMonitor(other.URL), // handed to the proxy, with its target checked first
		{Type: store.MonitorTCP, Target: other.Listener.Addr().String()},
	} {
		if got := c.Check(ctx, m); got.Up || !strings.Contains(got.Error, "is blocked by the target policy") {
			t.Errorf("%s %s: %+v", m.Type, m.Target, got)
		}
	}
}

// A redirect is another connection, so the guard checks every hop: a hop
// to a denied address fails with the guard's error, and succeeds once the
// address is allowed.
func TestGuardChecksRedirectHops(t *testing.T) {
	ln, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skip("no IPv6 loopback here: " + err.Error())
	}
	target := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	target.Listener.Close()
	target.Listener = ln
	target.Start()
	defer target.Close()
	// The first hop is on 127.0.0.1, the second on ::1.
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/next", http.StatusFound)
	}))
	defer front.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	m := httpMonitor(front.URL)

	out := New(Options{Guard: guard(t, "127.0.0.1/32")}).checkHTTP(ctx, m)
	if !errors.Is(out.err, netguard.ErrBlocked) || !strings.Contains(out.err.Error(), "::1 is blocked by the target policy") {
		t.Errorf("redirect to a denied address: %v", out.err)
	}

	got := New(Options{Guard: guard(t, "127.0.0.1/32", "::1/128")}).Check(ctx, m)
	if !got.Up || got.StatusCode != http.StatusOK {
		t.Errorf("redirect to an allowed address: %+v", got)
	}
}

// A JSON check repeats only the beginning of the value it found, so that a
// path selecting a whole object does not copy the response into results.
func TestJSONValueIsShortenedInResults(t *testing.T) {
	long := `{"data":{"items":["` + strings.Repeat("x", 5000) + `"]},"name":"` + strings.Repeat("ş", 150) + `","n":"` + strings.Repeat("7", 300) + `"}`

	_, err := matchJSON([]byte(long), "data", "==", "nothing")
	if err == nil || len(err.Error()) > 200 || !strings.Contains(err.Error(), `is {"items":["xxx`) || !strings.Contains(err.Error(), "…, expected == nothing") {
		t.Errorf("mismatch: %d bytes: %v", len(err.Error()), err)
	}
	detail, err := matchJSON([]byte(long), "data", "contains", "items")
	if err != nil || detail != "data = "+shownValue(`{"items":["`+strings.Repeat("x", 5000)+`"]}`) || !strings.HasSuffix(detail, "…") {
		t.Errorf("match: %q, %v", detail, err)
	}
	// Characters are counted, not bytes: a cut never splits one.
	if got := shownValue(strings.Repeat("ş", 150)); got != strings.Repeat("ş", maxShownValue)+"…" {
		t.Errorf("shownValue = %q", got)
	}
	if got := shownValue(strings.Repeat("a", maxShownValue)); got != strings.Repeat("a", maxShownValue) {
		t.Errorf("a value of exactly the limit was cut: %q", got)
	}
	// An ordering that needs numbers repeats the value shortened too.
	_, err = matchJSON([]byte(long), "name", ">", "1")
	if err == nil || strings.Count(err.Error(), "ş") != maxShownValue {
		t.Errorf("ordering error: %v", err)
	}
	if _, err := matchJSON([]byte(long), "n", "==", "7"); err == nil || strings.Count(err.Error(), "7") != maxShownValue+1 {
		t.Errorf("numeric mismatch: %v", err)
	}
}
