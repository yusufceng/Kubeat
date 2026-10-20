// Names are resolved as in the image, built with CGO_ENABLED=0: the system
// resolver of a development machine (macOS) reads a host such as
// 2852039166 as an address, and would hide what the proxy tests are about.

//go:debug netdns=go

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
	"sync"
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
		"169.254.169.254", "169.254.20.10", "fe80::1", "fe80::1%eth0",
		"fd00:ec2::254", "fd20:ce::254", "168.63.129.16", "100.100.100.200", // AWS, Google Cloud, Azure, Alibaba
		"224.0.0.1", "239.255.255.250", "ff02::1", "255.255.255.255",
		"64:ff9b::a9fe:a9fe", "64:ff9b::7f00:1"} // NAT64 to metadata and loopback
	// What a cluster monitor must still reach: ClusterIPs (Kubernetes and
	// OpenShift), pod IPs, private and carrier-grade NAT networks, ULA
	// IPv6, public addresses, and a public address through NAT64.
	allowed := []string{"10.0.0.1", "10.96.0.10", "10.128.2.15", "172.30.0.10", "172.16.0.1", "192.168.1.1",
		"100.64.0.5", "fd01::1", "fd02::10", "fd00:ec2::253", "fd20:ce::253", "168.63.129.17", "8.8.8.8", "2001:db8::1",
		"64:ff9b::808:808"}
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
	// A NAT64 prefix of the network's own is not read: where it puts the
	// IPv4 address is its choice. The mapped forms of the denied ranges,
	// denied as extra ranges, close it (the README's example).
	own := "64:ff9b:1:1::a9fe:a9fe" // 169.254.169.254 under 64:ff9b:1:1::/96
	if err := mustNew(t, nil, nil).Check(netip.MustParseAddr(own)); err != nil {
		t.Errorf("a prefix of the network's own is decoded after all: %v", err)
	}
	mapped := mustNew(t, []string{"64:ff9b:1:1::a9fe:0/112", "64:ff9b:1:1::7f00:0/104"}, nil)
	for _, s := range []string{own, "64:ff9b:1:1::7f00:1"} {
		if err := mapped.Check(netip.MustParseAddr(s)); !errors.Is(err, ErrBlocked) {
			t.Errorf("%s with its mapped ranges denied: %v", s, err)
		}
	}
	if err := mapped.Check(netip.MustParseAddr("64:ff9b:1:1::808:808")); err != nil {
		t.Errorf("a public address under the network's own prefix: %v", err)
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

// A plain http:// request names the address the proxy fetches with its
// Host header; an https:// one with its URL, the header staying in the
// tunnel.
func TestProxyChecksTheHostHeader(t *testing.T) {
	p := mustNew(t, nil, nil)
	proxy, _ := url.Parse("http://proxy.example:3128")
	viaProxy := p.Proxy(func(*http.Request) (*url.URL, error) { return proxy, nil })
	for _, c := range []struct {
		target, host string
		blocked      bool
	}{
		{"http://10.0.0.5/", "169.254.169.254", true},
		{"http://10.0.0.5/", "127.0.0.1:8080", true},
		{"http://10.0.0.5/", "[::1]:8080", true},
		{"http://10.0.0.5/", "2852039166", true},
		{"http://10.0.0.5/", "localhost", true},
		{"http://10.0.0.5/", "10.0.0.6:8080", false},
		{"http://10.0.0.5/", "api.shop.example", false}, // a name that does not resolve here
		{"https://10.0.0.5/", "169.254.169.254", false},
	} {
		req := httptest.NewRequest(http.MethodGet, c.target, nil)
		req.Host = c.host
		got, err := viaProxy(req)
		if c.blocked && (!errors.Is(err, ErrBlocked) || got != nil) {
			t.Errorf("%s with Host %s: want blocked, got %v, %v", c.target, c.host, got, err)
		}
		if !c.blocked && (err != nil || got != proxy) {
			t.Errorf("%s with Host %s: want the proxy, got %v, %v", c.target, c.host, got, err)
		}
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
		// Numbers that resolve to nothing here, and that a proxy reading
		// them as inet_aton does takes for 169.254.169.254 or 127.0.0.1.
		"http://2852039166/latest/meta-data/": true, // one decimal number
		"http://0xa9fea9fe/":                  true, // one hexadecimal number
		"http://0xa9.0xfe.0xa9.0xfe/":         true,
		"http://0XA9.0XFE.0XA9.0XFE/":         true,
		"http://0251.0376.0251.0376/":         true, // octal
		"http://0xa9.254.0251.0xfe/":          true, // mixed
		"http://169.254.43518/":               true, // a.b.c, c of 16 bits
		"http://169.16689662/":                true, // a.b, b of 24 bits
		"http://2130706433:8080/":             true,
		"http://127.1/":                       true,
		"http://0x7f.1/":                      true,
		"http://017700000001/":                true,
		"http://0/":                           true,
		"http://127.0.0.1./":                  true, // a trailing dot
		// More of them, which a proxy drops all the same.
		"http://2852039166../latest/meta-data/": true,
		"http://2852039166.../":                 true,
		"http://169.254.169.254../":             true,
		"http://169.254.169.254.../":            true,
		"http://0xa9.0xfe.0xa9.0xfe../":         true,
		"http://127.1.../":                      true,
		"http://localhost.:8080/":               true, // not in the hosts file to Go's own resolver
		"http://localhost..:8080/":              true,
		"http://localhost...:8080/":             true,
		"http://10.0.0.5../":                    false,
		// Spelled as numbers but not a valid address: refused, since
		// implementations of inet_aton disagree on them.
		"http://08.0.0.1/":    true,
		"http://0x.1.2.3/":    true,
		"http://4294967296/":  true,
		"http://10.0.0.256/":  true,
		"http://10.16777216/": true,
		"http://0x1ffffffff/": true,
		"http://167772165/":   false, // 10.0.0.5
		"http://0x0a.0.0.5/":  false,
		"http://10.5/":        false, // 10.0.0.5
		"http://1.2.3.4.5/":   false, // five parts: a name, which does not resolve
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

// A trusted address is reached whatever it resolves to, decided on the
// address as asked for: the rest of its range stays denied.
func TestTrustedAddress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer other.Close()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	strict := mustNew(t, nil, nil)
	sidecar := strict.Trust("127.0.0.1:"+port, "", "not an address")
	d := sidecar.Dialer(&net.Dialer{})
	conn, err := d.DialContext(ctx, "tcp", "127.0.0.1:"+port)
	if err != nil {
		t.Fatalf("the trusted address: %v", err)
	}
	conn.Close()
	for _, addr := range []string{other.Listener.Addr().String(), "localhost:" + port, "[::ffff:127.0.0.1]:" + port} {
		if conn, err := d.DialContext(ctx, "tcp", addr); !errors.Is(err, ErrBlocked) {
			if conn != nil {
				conn.Close()
			}
			t.Errorf("%s, not asked for as trusted: err = %v", addr, err)
		}
	}
	// The policy trusted from is left as it was.
	if conn, err := strict.Dialer(&net.Dialer{}).DialContext(ctx, "tcp", "127.0.0.1:"+port); !errors.Is(err, ErrBlocked) {
		if conn != nil {
			conn.Close()
		}
		t.Errorf("Trust changed the policy it was called on: %v", err)
	}
	// A name is compared without regard to case.
	byName := strict.Trust("LocalHost:" + port).Dialer(&net.Dialer{})
	if conn, err := byName.DialContext(ctx, "tcp", "localhost:"+port); err != nil {
		t.Errorf("a trusted name: %v", err)
	} else {
		conn.Close()
	}

	var none *Policy
	if none.Trust("127.0.0.1:"+port) != nil {
		t.Error("trusting from a nil policy made one")
	}
}

// A proxy the policy trusts is reached on loopback without an exception,
// and the requests handed to it still have their targets checked.
func TestTrustedProxy(t *testing.T) {
	var (
		mu   sync.Mutex
		seen []string
	)
	proxy := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, r.Host)
	}))
	defer proxy.Close()
	proxyURL, _ := url.Parse(proxy.URL)
	client := func(p *Policy) *http.Client {
		return &http.Client{Transport: &http.Transport{
			DialContext: p.Dialer(&net.Dialer{}).DialContext, Proxy: p.Proxy(http.ProxyURL(proxyURL)),
		}}
	}

	if _, err := client(mustNew(t, nil, nil)).Get("http://192.0.2.10/healthz"); !errors.Is(err, ErrBlocked) {
		t.Errorf("an untrusted proxy on loopback: err = %v", err)
	}
	trusted := mustNew(t, nil, nil).Trust(HostPort(proxy.URL))
	resp, err := client(trusted).Get("http://192.0.2.10/healthz")
	if err != nil {
		t.Fatalf("through the trusted proxy: %v", err)
	}
	resp.Body.Close()
	if _, err := client(trusted).Get("http://169.254.169.254/latest/meta-data/"); !errors.Is(err, ErrBlocked) {
		t.Errorf("a denied target through the trusted proxy: err = %v", err)
	}
	mu.Lock()
	if strings.Join(seen, ",") != "192.0.2.10" {
		t.Errorf("the proxy was handed %v", seen)
	}
	mu.Unlock()

	// A target the policy trusts is handed over unchecked.
	viaProxy := mustNew(t, nil, nil).Trust("169.254.169.254:80").Proxy(http.ProxyURL(proxyURL))
	if got, err := viaProxy(httptest.NewRequest(http.MethodGet, "http://169.254.169.254/", nil)); err != nil || got == nil {
		t.Errorf("a trusted target: %v, %v", got, err)
	}
}

func TestHostPort(t *testing.T) {
	for raw, want := range map[string]string{
		"https://api.telegram.org":                "api.telegram.org:443",
		"https://events.pagerduty.com/v2/enqueue": "events.pagerduty.com:443",
		"HTTP://Proxy.Local":                      "proxy.local:80",
		"http://127.0.0.1:3128":                   "127.0.0.1:3128",
		"http://[::1]:3128/":                      "[::1]:3128",
		"socks5://socks.local":                    "socks.local:1080",
		"ftp://files.local":                       "",
		"api.telegram.org":                        "",
		"":                                        "",
	} {
		if got := HostPort(raw); got != want {
			t.Errorf("HostPort(%q) = %q, want %q", raw, got, want)
		}
	}
}

// The proxies are read as net/http reads them, whatever NO_PROXY says.
func TestEnvironmentProxies(t *testing.T) {
	for _, name := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "NO_PROXY", "no_proxy", "REQUEST_METHOD"} {
		t.Setenv(name, "")
	}
	if got := EnvironmentProxies(); len(got) != 0 {
		t.Errorf("without proxies: %v", got)
	}
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:3128")
	t.Setenv("https_proxy", "Proxy.Local:8443") // without a scheme, as http://
	t.Setenv("NO_PROXY", "*")
	if got := EnvironmentProxies(); strings.Join(got, ",") != "127.0.0.1:3128,proxy.local:8443" {
		t.Errorf("proxies = %v", got)
	}
	t.Setenv("https_proxy", "http://127.0.0.1:3128")
	if got := EnvironmentProxies(); strings.Join(got, ",") != "127.0.0.1:3128" {
		t.Errorf("one proxy for both: %v", got)
	}
}

// A host made of numbers is read as inet_aton reads it, or refused.
func TestNumericHosts(t *testing.T) {
	for host, want := range map[string]string{
		"2852039166": "169.254.169.254", "0xa9fea9fe": "169.254.169.254", "0xa9.0xfe.0xa9.0xfe": "169.254.169.254",
		"0251.0376.0251.0376": "169.254.169.254", "169.254.43518": "169.254.169.254", "169.16689662": "169.254.169.254",
		"2130706433": "127.0.0.1", "127.1": "127.0.0.1", "0x7f.1": "127.0.0.1", "017700000001": "127.0.0.1",
		"0": "0.0.0.0", "00": "0.0.0.0", "0x0": "0.0.0.0", "4294967295": "255.255.255.255", "10.0.0.5": "10.0.0.5",
	} {
		addr, ok := parseNumeric(host)
		if !numeric(host) || !ok || addr.String() != want {
			t.Errorf("%s: numeric %v, read as %v (%v), want %s", host, numeric(host), addr, ok, want)
		}
	}
	// Numbers, but none that makes an address.
	for _, host := range []string{"08", "0x", "0x.1.2.3", "4294967296", "1.2.3.256", "1.2.65536", "1.16777216", "0x1ffffffff", "0400.0.0.1"} {
		if !numeric(host) {
			t.Errorf("%s: not taken for a number", host)
		}
		if addr, ok := parseNumeric(host); ok {
			t.Errorf("%s: read as %v", host, addr)
		}
	}
	for _, host := range []string{"", "1..2", "1.2.3.4.5", "example.com", "0xcafe.local", "1e3", "+1", "1.2.3.4a", "0xg"} {
		if numeric(host) {
			t.Errorf("%q: taken for a number", host)
		}
	}
	// The error names the host as it was written, and what it reads as.
	err := mustNew(t, nil, nil).CheckHost(context.Background(), "2852039166")
	if err == nil || !strings.HasPrefix(err.Error(), "2852039166: 169.254.169.254 is blocked by the target policy") {
		t.Errorf("err = %v", err)
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
	logger.Info("target policy", "policy", mustNew(t, []string{"10.0.0.0/8"}, nil).Trust("Proxy.Local:3128"))
	var entry struct {
		Policy struct {
			ExtraDeny []string `json:"extra_deny"`
			Allow     []string `json:"allow"`
			Trusted   []string `json:"trusted"`
		} `json:"policy"`
	}
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("%v: %s", err, buf.Bytes())
	}
	if strings.Join(entry.Policy.ExtraDeny, ",") != "10.0.0.0/8" || entry.Policy.Allow == nil || len(entry.Policy.Allow) != 0 ||
		strings.Join(entry.Policy.Trusted, ",") != "proxy.local:3128" {
		t.Errorf("logged %s", buf.Bytes())
	}
}
