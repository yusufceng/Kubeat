package netguard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"
)

func mustNew(t *testing.T, extraDeny, allow []string) *Policy {
	t.Helper()
	p, err := New(extraDeny, allow)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDefaultPolicy(t *testing.T) {
	p := mustNew(t, nil, nil)
	denied := []string{"127.0.0.1", "127.9.9.9", "::1", "::ffff:127.0.0.1", "0.0.0.0", "0.1.2.3", "::",
		"169.254.169.254", "169.254.20.10", "fe80::1", "fe80::1%eth0", "fd00:ec2::254", "100.100.100.200",
		"224.0.0.1", "239.255.255.250", "ff02::1", "255.255.255.255",
		"64:ff9b::a9fe:a9fe", "64:ff9b::7f00:1"} // NAT64 to metadata and loopback
	// What a cluster monitor must still reach: ClusterIPs (Kubernetes and
	// OpenShift), pod IPs, private and carrier-grade NAT networks, ULA
	// IPv6, public addresses, and a public address through NAT64.
	allowed := []string{"10.0.0.1", "10.96.0.10", "10.128.2.15", "172.30.0.10", "172.16.0.1", "192.168.1.1",
		"100.64.0.5", "fd01::1", "fd02::10", "fd00:ec2::253", "8.8.8.8", "2001:db8::1", "64:ff9b::808:808"}
	for _, s := range denied {
		if err := p.Check(netip.MustParseAddr(s)); !errors.Is(err, ErrBlocked) {
			t.Errorf("%s: want blocked, got %v", s, err)
		}
	}
	for _, s := range allowed {
		if err := p.Check(netip.MustParseAddr(s)); err != nil {
			t.Errorf("%s: want allowed, got %v", s, err)
		}
	}
	if err := p.Check(netip.Addr{}); !errors.Is(err, ErrBlocked) {
		t.Errorf("the zero address: want blocked, got %v", err)
	}
}

// The error says what was refused, why, and what would allow it.
func TestBlockedErrorNamesTheAddressAndTheSetting(t *testing.T) {
	p := mustNew(t, nil, nil)
	err := p.Check(netip.MustParseAddr("169.254.169.254"))
	want := "169.254.169.254 is blocked by the target policy (169.254.0.0/16); UPTIME_TARGET_ALLOW_CIDRS can allow it"
	if err == nil || err.Error() != want {
		t.Errorf("err = %v\nwant  %s", err, want)
	}
	// A NAT64 address names the IPv4 address it was found as.
	err = p.Check(netip.MustParseAddr("64:ff9b::a9fe:a9fe"))
	if err == nil || !strings.HasPrefix(err.Error(), "64:ff9b::a9fe:a9fe (169.254.169.254) is blocked") {
		t.Errorf("NAT64: err = %v", err)
	}
}

func TestExceptionsWinAndExtraDenials(t *testing.T) {
	p := mustNew(t, []string{"10.0.0.0/24", " 192.168.0.0/16 "}, []string{"169.254.20.10/32", "::ffff:127.0.0.1/128"})
	if err := p.Check(netip.MustParseAddr("169.254.20.10")); err != nil {
		t.Errorf("an exception within a built-in range: %v", err)
	}
	// An IPv4 exception written in IPv6 form is the IPv4 one.
	if err := p.Check(netip.MustParseAddr("127.0.0.1")); err != nil {
		t.Errorf("an exception in IPv6 form: %v", err)
	}
	if err := p.Check(netip.MustParseAddr("169.254.169.254")); err == nil {
		t.Error("the rest of the range is no longer denied")
	}
	for _, s := range []string{"10.0.0.7", "192.168.10.1"} {
		if err := p.Check(netip.MustParseAddr(s)); !errors.Is(err, ErrBlocked) {
			t.Errorf("%s: an extra denial was ignored: %v", s, err)
		}
	}
	if err := p.Check(netip.MustParseAddr("10.0.1.7")); err != nil {
		t.Errorf("outside the extra range: %v", err)
	}
	// A range denied in NAT64 form stays denied, whatever it carries.
	nat := mustNew(t, []string{"64:ff9b::/96"}, nil)
	if err := nat.Check(netip.MustParseAddr("64:ff9b::808:808")); !errors.Is(err, ErrBlocked) {
		t.Errorf("a denied NAT64 range: %v", err)
	}

	for _, bad := range []string{"not-a-cidr", "10.0.0.1", "10.0.0.0/33", "fe80::1%eth0/64"} {
		if _, err := New([]string{bad}, nil); err == nil {
			t.Errorf("extra denial %q was accepted", bad)
		}
		if _, err := New(nil, []string{bad}); err == nil {
			t.Errorf("exception %q was accepted", bad)
		}
	}
}

// The check runs on the resolved address at connect time: a name that
// resolves to loopback is refused although the name itself looks harmless.
func TestDialerChecksTheResolvedAddress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	d := mustNew(t, nil, nil).Dialer(&net.Dialer{})
	for _, addr := range []string{"localhost:" + port, "127.0.0.1:" + port, "[::ffff:127.0.0.1]:" + port} {
		conn, err := d.DialContext(ctx, "tcp", addr)
		if !errors.Is(err, ErrBlocked) {
			if conn != nil {
				conn.Close()
			}
			t.Errorf("%s: want blocked, got %v", addr, err)
		}
	}
	if _, err := (&http.Client{Transport: &http.Transport{DialContext: d.DialContext}}).Get(srv.URL); !errors.Is(err, ErrBlocked) {
		t.Errorf("HTTP request to loopback: err = %v", err)
	}

	// An exception lets it through, which is what local development uses.
	open := mustNew(t, nil, []string{"127.0.0.0/8", "::1/128"})
	resp, err := (&http.Client{Transport: open.Transport()}).Get(srv.URL)
	if err != nil {
		t.Fatalf("allowed loopback: %v", err)
	}
	resp.Body.Close()

	// A nil policy changes nothing, which is what tests with local servers use.
	var none *Policy
	conn, err := none.Dialer(&net.Dialer{}).DialContext(ctx, "tcp", "127.0.0.1:"+port)
	if err != nil {
		t.Fatalf("nil policy: %v", err)
	}
	conn.Close()
	if resp, err := (&http.Client{Transport: none.Transport()}).Get(srv.URL); err != nil {
		t.Errorf("nil policy transport: %v", err)
	} else {
		resp.Body.Close()
	}
}

// A redirect is another connection, so it is checked like the first one.
func TestRedirectHopIsChecked(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer target.Close()
	// The first server is reached through an exception for its exact
	// address; it redirects to another loopback address, which nothing
	// excepts. The policy refuses that before connecting to it.
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, port, _ := net.SplitHostPort(target.Listener.Addr().String())
		http.Redirect(w, r, "http://[::ffff:127.0.0.2]:"+port+"/", http.StatusFound)
	}))
	defer front.Close()
	p := mustNew(t, nil, []string{"127.0.0.1/32"})
	_, err := (&http.Client{Transport: p.Transport()}).Get(front.URL)
	if !errors.Is(err, ErrBlocked) || !strings.Contains(err.Error(), "127.0.0.2 is blocked") {
		t.Errorf("redirect to a denied address: err = %v", err)
	}
}

func TestProxyChecksTheTarget(t *testing.T) {
	p := mustNew(t, nil, nil)
	proxy, _ := url.Parse("http://proxy.example:3128")
	viaProxy := p.Proxy(func(*http.Request) (*url.URL, error) { return proxy, nil })
	for target, blocked := range map[string]bool{
		"http://169.254.169.254/latest/meta-data/": true,
		"http://[::1]:8080/":                       true,
		"http://localhost:8080/":                   true, // resolves to loopback
		"http://10.0.0.5/":                         false,
		"https://172.30.4.2:8443/healthz":          false,
	} {
		got, err := viaProxy(httptest.NewRequest(http.MethodGet, target, nil))
		if blocked && (!errors.Is(err, ErrBlocked) || got != nil) {
			t.Errorf("%s: want blocked, got %v, %v", target, got, err)
		}
		if !blocked && (err != nil || got != proxy) {
			t.Errorf("%s: want the proxy, got %v, %v", target, got, err)
		}
	}
	// A request the proxy is not used for is left to the dialer.
	direct := p.Proxy(func(*http.Request) (*url.URL, error) { return nil, nil })
	if got, err := direct(httptest.NewRequest(http.MethodGet, "http://169.254.169.254/", nil)); got != nil || err != nil {
		t.Errorf("without a proxy: %v, %v", got, err)
	}
	failing := p.Proxy(func(*http.Request) (*url.URL, error) { return nil, errors.New("bad proxy setting") })
	if _, err := failing(httptest.NewRequest(http.MethodGet, "http://10.0.0.5/", nil)); err == nil || err.Error() != "bad proxy setting" {
		t.Errorf("a failing proxy function: err = %v", err)
	}

	var none *Policy
	if none.Proxy(nil) != nil {
		t.Error("a nil policy wrapped a nil proxy function")
	}
	if err := none.CheckHost(context.Background(), "localhost"); err != nil {
		t.Errorf("a nil policy checked a host: %v", err)
	}
}

func TestCheckHost(t *testing.T) {
	p := mustNew(t, nil, nil)
	ctx := context.Background()
	if err := p.CheckHost(ctx, "localhost"); !errors.Is(err, ErrBlocked) {
		t.Errorf("localhost: %v", err)
	}
	if err := p.CheckHost(ctx, "fe80::1%eth0"); !errors.Is(err, ErrBlocked) {
		t.Errorf("a zoned link-local address: %v", err)
	}
	if err := p.CheckHost(ctx, "10.1.2.3"); err != nil {
		t.Errorf("a ClusterIP: %v", err)
	}
}

func TestLogValue(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	logger.Info("target policy", "policy", mustNew(t, []string{"10.0.0.0/8"}, nil))
	var entry struct {
		Policy struct {
			ExtraDeny []string `json:"extra_deny"`
			Allow     []string `json:"allow"`
		} `json:"policy"`
	}
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("%v: %s", err, buf.Bytes())
	}
	if strings.Join(entry.Policy.ExtraDeny, ",") != "10.0.0.0/8" || entry.Policy.Allow == nil || len(entry.Policy.Allow) != 0 {
		t.Errorf("logged %s", buf.Bytes())
	}
}
