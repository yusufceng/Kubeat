package checker

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

func TestHTTPRequestOptions(t *testing.T) {
	var gotMethod, gotBody, gotType, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotMethod, gotBody, gotType, gotAuth = r.Method, string(body), r.Header.Get("Content-Type"), r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/hop1":
			http.Redirect(w, r, "/hop2", http.StatusFound)
		case "/hop2":
			http.Redirect(w, r, "/", http.StatusFound)
		default:
			w.Write([]byte(`{"status":"ok","error":null}`))
		}
	}))
	defer srv.Close()
	c := New(Options{BodyLimit: 1 << 20})
	check := func(path string, edit func(*store.HTTPOptions)) store.CheckResult {
		m := httpMonitor(srv.URL + path)
		edit(&m.HTTP)
		return c.Check(context.Background(), m)
	}

	got := check("/", func(o *store.HTTPOptions) {
		o.Method, o.Body, o.ContentType = "POST", `{"ping":1}`, "application/json"
		o.Auth, o.AuthUser, o.AuthSecret = store.HTTPAuthBasic, "kubeat", "s3cret"
	})
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("kubeat:s3cret"))
	if !got.Up || gotMethod != "POST" || gotBody != `{"ping":1}` || gotType != "application/json" || gotAuth != wantAuth {
		t.Errorf("post: up=%v method=%q body=%q type=%q auth=%q (%s)", got.Up, gotMethod, gotBody, gotType, gotAuth, got.Error)
	}

	check("/", func(o *store.HTTPOptions) { o.Auth, o.AuthSecret = store.HTTPAuthBearer, "t0ken" })
	if gotAuth != "Bearer t0ken" {
		t.Errorf("bearer: auth = %q", gotAuth)
	}
	// A header given explicitly wins over the auth option.
	check("/", func(o *store.HTTPOptions) {
		o.Auth, o.AuthSecret = store.HTTPAuthBearer, "t0ken"
		o.Headers = map[string]string{"Authorization": "Custom x"}
	})
	if gotAuth != "Custom x" {
		t.Errorf("explicit header: auth = %q", gotAuth)
	}

	// Without following redirects the redirect is what is judged.
	if got := check("/hop1", func(o *store.HTTPOptions) { o.MaxRedirects = 0 }); !got.Up || got.StatusCode != 302 {
		t.Errorf("no redirects: %+v", got)
	}
	if got := check("/hop1", func(o *store.HTTPOptions) { o.MaxRedirects, o.ExpectedStatus = 0, "200" }); got.Up || got.StatusCode != 302 {
		t.Errorf("no redirects, 200 expected: %+v", got)
	}
	if got := check("/hop1", func(o *store.HTTPOptions) { o.MaxRedirects = 2 }); !got.Up || got.StatusCode != 200 {
		t.Errorf("two redirects allowed: %+v", got)
	}
	if got := check("/hop1", func(o *store.HTTPOptions) { o.MaxRedirects = 1 }); got.Up || !strings.Contains(got.Error, "redirects") {
		t.Errorf("one redirect allowed, two needed: %+v", got)
	}

	// A keyword that must not be there.
	if got := check("/", func(o *store.HTTPOptions) { o.Keyword, o.InvertKeyword = "error", true }); got.Up || !strings.Contains(got.Error, "must not") {
		t.Errorf("forbidden keyword present: %+v", got)
	}
	if got := check("/", func(o *store.HTTPOptions) { o.Keyword, o.InvertKeyword = "exception", true }); !got.Up {
		t.Errorf("forbidden keyword absent: %+v", got)
	}

	// A value in the JSON body.
	if got := check("/", func(o *store.HTTPOptions) { o.JSONPath, o.JSONExpected = "status", "ok" }); !got.Up || got.Detail != "status = ok" {
		t.Errorf("json match: %+v", got)
	}
	if got := check("/", func(o *store.HTTPOptions) { o.JSONPath, o.JSONExpected = "status", "ready" }); got.Up || !strings.Contains(got.Error, "is ok, expected == ready") {
		t.Errorf("json mismatch: %+v", got)
	}
}

func TestMatchJSON(t *testing.T) {
	const doc = `{"status":"UP","count":12,"ratio":0.5,"ok":true,"none":null,
		"checks":[{"name":"db","state":"up"},{"name":"cache","state":"degraded"}],"grid":[[1,2],[3,4]],
		"big":12345678901234567890}`
	pass := []struct{ path, op, want string }{
		{"status", "==", "UP"},
		{"status", "", "UP"}, // the default operator
		{"status", "!=", "DOWN"},
		{"status", "contains", "U"},
		{"count", "==", "12"},
		{"count", "==", "12.0"}, // numbers compare by value
		{"count", ">", "5"},
		{"count", ">=", "12"},
		{"ratio", "<", "1"},
		{"ratio", "<=", "0.5"},
		{"ok", "==", "true"},
		{"none", "==", "null"},
		{"checks[1].state", "==", "degraded"},
		{"checks[0].name", "==", "db"},
		{"grid[1][0]", "==", "3"},
		{"big", "==", "12345678901234567890"},
		{"checks[0]", "contains", `"state":"up"`},
	}
	for _, tt := range pass {
		if _, err := matchJSON([]byte(doc), tt.path, tt.op, tt.want); err != nil {
			t.Errorf("%s %s %s: %v", tt.path, tt.op, tt.want, err)
		}
	}
	fail := []struct{ path, op, want string }{
		{"status", "==", "up"}, // text is case-sensitive
		{"count", "<", "12"},
		{"status", ">", "1"},  // ordering needs numbers
		{"missing", "==", ""}, // no such key
		{"checks[2].state", "==", "up"},
		{"status.inner", "==", "x"},
		{"checks.name", "==", "db"}, // an array needs an index
		{"status[0]", "==", "U"},
		{"status", "~", "UP"},
	}
	for _, tt := range fail {
		if detail, err := matchJSON([]byte(doc), tt.path, tt.op, tt.want); err == nil {
			t.Errorf("%s %s %s: passed with %q", tt.path, tt.op, tt.want, detail)
		}
	}

	// A document that is an array itself.
	if _, err := matchJSON([]byte(`[{"id":7}]`), "[0].id", "==", "7"); err != nil {
		t.Errorf("root array: %v", err)
	}
	if _, err := matchJSON([]byte(`<html>`), "status", "==", "UP"); err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("not JSON: err = %v", err)
	}
	for _, bad := range []string{"", ".a", "a.", "a..b", "a[x]", "a[-1]", "a[0", "a[0]b", "a.[0]"} {
		if _, err := ParseJSONPath(bad); err == nil {
			t.Errorf("ParseJSONPath(%q) was accepted", bad)
		}
	}
}

func TestTCPWithTLS(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	addr := srv.Listener.Addr().String()
	c := New(Options{})
	ctx := context.Background()

	m := store.Monitor{Type: store.MonitorTCP, Target: addr, TCP: store.TCPOptions{TLS: true}}
	if got := c.Check(ctx, m); got.Up || !strings.Contains(got.Error, "tls handshake") {
		t.Errorf("self-signed certificate: %+v", got)
	}
	m.TCP.SkipTLSVerify = true
	got := c.Check(ctx, m)
	if !got.Up || !got.CertExpiresAt.Equal(srv.Certificate().NotAfter) || !strings.HasPrefix(got.Detail, "TLS") {
		t.Errorf("tls check: %+v", got)
	}
	// Without the option the port is merely open.
	if got := c.Check(ctx, store.Monitor{Type: store.MonitorTCP, Target: addr}); !got.Up || !got.CertExpiresAt.IsZero() {
		t.Errorf("plain check of a TLS port: %+v", got)
	}

	// A port that does not speak TLS fails the handshake.
	plain := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer plain.Close()
	m.Target = plain.Listener.Addr().String()
	if got := c.Check(ctx, m); got.Up {
		t.Errorf("handshake with a plain port succeeded: %+v", got)
	}
}

// dnsServer answers queries from a fixed table, over UDP.
func dnsServer(t *testing.T, answer func(q dnsmessage.Question) []dnsmessage.ResourceBody) string {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })

	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			var query dnsmessage.Message
			if err := query.Unpack(buf[:n]); err != nil || len(query.Questions) == 0 {
				continue
			}
			q := query.Questions[0]
			reply := dnsmessage.Message{
				Header:    dnsmessage.Header{ID: query.ID, Response: true, RecursionAvailable: true},
				Questions: query.Questions,
			}
			bodies := answer(q)
			if bodies == nil {
				reply.RCode = dnsmessage.RCodeNameError
			}
			for _, body := range bodies {
				reply.Answers = append(reply.Answers, dnsmessage.Resource{
					Header: dnsmessage.ResourceHeader{Name: q.Name, Type: q.Type, Class: dnsmessage.ClassINET, TTL: 60},
					Body:   body,
				})
			}
			if packed, err := reply.Pack(); err == nil {
				_, _ = conn.WriteTo(packed, from)
			}
		}
	}()
	return conn.LocalAddr().String()
}

func TestDNSCheck(t *testing.T) {
	server := dnsServer(t, func(q dnsmessage.Question) []dnsmessage.ResourceBody {
		if q.Name.String() != "api.shop.example." {
			return nil
		}
		switch q.Type {
		case dnsmessage.TypeA:
			return []dnsmessage.ResourceBody{&dnsmessage.AResource{A: [4]byte{10, 0, 0, 2}}, &dnsmessage.AResource{A: [4]byte{10, 0, 0, 1}}}
		case dnsmessage.TypeTXT:
			return []dnsmessage.ResourceBody{&dnsmessage.TXTResource{TXT: []string{"v=spf1 -all"}}}
		case dnsmessage.TypeMX:
			return []dnsmessage.ResourceBody{&dnsmessage.MXResource{Pref: 10, MX: dnsmessage.MustNewName("mail.shop.example.")}}
		default:
			return []dnsmessage.ResourceBody{} // the name exists, without records of this type
		}
	})
	c := New(Options{})
	check := func(name string, o store.DNSOptions) store.CheckResult {
		o.Resolver = server
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return c.Check(ctx, store.Monitor{Type: store.MonitorDNS, Target: name, DNS: o})
	}

	if got := check("api.shop.example", store.DNSOptions{RecordType: "A"}); !got.Up || got.Detail != "10.0.0.1, 10.0.0.2" {
		t.Errorf("A: %+v", got)
	}
	if got := check("api.shop.example", store.DNSOptions{RecordType: "TXT", Expected: "SPF1"}); !got.Up || got.Detail != "v=spf1 -all" {
		t.Errorf("TXT with an expected value: %+v", got)
	}
	if got := check("api.shop.example", store.DNSOptions{RecordType: "MX"}); !got.Up || got.Detail != "10 mail.shop.example" {
		t.Errorf("MX: %+v", got)
	}
	if got := check("api.shop.example", store.DNSOptions{RecordType: "A", Expected: "10.0.0.9"}); got.Up || !strings.Contains(got.Error, `contains "10.0.0.9"`) {
		t.Errorf("unexpected address: %+v", got)
	}
	if got := check("api.shop.example", store.DNSOptions{RecordType: "AAAA"}); got.Up {
		t.Errorf("no AAAA records: %+v", got)
	}
	if got := check("gone.shop.example", store.DNSOptions{RecordType: "A"}); got.Up {
		t.Errorf("unknown name: %+v", got)
	}
	if got := check("api.shop.example", store.DNSOptions{RecordType: "LOC"}); got.Up || !strings.Contains(got.Error, "unsupported") {
		t.Errorf("unsupported type: %+v", got)
	}
}

func TestPingCheck(t *testing.T) {
	c := New(Options{})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	got := c.Check(ctx, store.Monitor{Type: store.MonitorPing, Target: "127.0.0.1"})
	if strings.Contains(got.Error, "not permitted") {
		t.Skip("this machine does not allow unprivileged ICMP: " + got.Error)
	}
	if !got.Up || got.Detail != "reply from 127.0.0.1" || got.Latency <= 0 || got.Latency > time.Second {
		t.Errorf("ping of the loopback address: %+v", got)
	}

	// An address nothing answers from; TEST-NET-1 is reserved for documentation.
	ctx, cancel = context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if got := c.Check(ctx, store.Monitor{Type: store.MonitorPing, Target: "192.0.2.1"}); got.Up {
		t.Errorf("ping of an unused address: %+v", got)
	}
	if got := c.Check(context.Background(), store.Monitor{Type: store.MonitorPing, Target: "no-such-host.invalid"}); got.Up {
		t.Errorf("ping of an unknown name: %+v", got)
	}
}

type fakePushes struct {
	report store.PushReport
	err    error
}

func (f fakePushes) LastPush(context.Context, int64) (store.PushReport, error) {
	return f.report, f.err
}

func TestPushCheck(t *testing.T) {
	m := store.Monitor{ID: 3, Type: store.MonitorPush, Interval: time.Minute, Timeout: 10 * time.Second}
	check := func(p Pushes) store.CheckResult {
		return New(Options{Pushes: p}).Check(context.Background(), m)
	}
	ago := func(d time.Duration) time.Time { return time.Now().Add(-d) }

	got := check(fakePushes{report: store.PushReport{ReceivedAt: ago(30 * time.Second), Up: true, Message: "yedek alındı", Latency: 4200 * time.Millisecond}})
	if !got.Up || got.Detail != "yedek alındı" || got.Latency != 4200*time.Millisecond {
		t.Errorf("recent report: %+v", got)
	}
	// Late, but within the grace period.
	if got := check(fakePushes{report: store.PushReport{ReceivedAt: ago(65 * time.Second), Up: true}}); !got.Up || got.Latency != 0 {
		t.Errorf("report within the grace period: %+v", got)
	}
	if got := check(fakePushes{report: store.PushReport{ReceivedAt: ago(75 * time.Second), Up: true}}); got.Up || !strings.Contains(got.Error, "no push received for") {
		t.Errorf("missed report: %+v", got)
	}
	if got := check(fakePushes{report: store.PushReport{ReceivedAt: ago(time.Second), Up: false, Message: "disk dolu"}}); got.Up || !strings.Contains(got.Error, "disk dolu") {
		t.Errorf("reported failure: %+v", got)
	}
	if got := check(fakePushes{err: store.ErrNotFound}); got.Up || got.Error != "no push received yet" {
		t.Errorf("never reported: %+v", got)
	}
	if got := check(fakePushes{err: errors.New("database is locked")}); got.Up || !strings.Contains(got.Error, "locked") {
		t.Errorf("store failure: %+v", got)
	}
	if got := check(nil); got.Up {
		t.Errorf("without a push source: %+v", got)
	}
}

func TestGRPCCheck(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer()
	state := health.NewServer()
	healthpb.RegisterHealthServer(srv, state)
	state.SetServingStatus("shop.Cart", healthpb.HealthCheckResponse_NOT_SERVING)
	go srv.Serve(ln)
	defer srv.Stop()

	c := New(Options{UserAgent: "kubeat/test"})
	check := func(target string, o store.GRPCOptions) store.CheckResult {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return c.Check(ctx, store.Monitor{Type: store.MonitorGRPC, Target: target, GRPC: o})
	}
	addr := ln.Addr().String()

	if got := check(addr, store.GRPCOptions{}); !got.Up || got.Detail != "SERVING" {
		t.Errorf("server as a whole: %+v", got)
	}
	if got := check(addr, store.GRPCOptions{Service: "shop.Cart"}); got.Up || !strings.Contains(got.Error, "NOT_SERVING") {
		t.Errorf("service that is not serving: %+v", got)
	}
	if got := check(addr, store.GRPCOptions{Service: "shop.Unknown"}); got.Up || !strings.Contains(got.Error, "does not know the service") {
		t.Errorf("unknown service: %+v", got)
	}
	// A plaintext server does not complete a TLS handshake.
	if got := check(addr, store.GRPCOptions{TLS: true, SkipTLSVerify: true}); got.Up {
		t.Errorf("tls against a plaintext server: %+v", got)
	}

	// A gRPC server without the health service.
	bare, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	plain := grpc.NewServer()
	go plain.Serve(bare)
	defer plain.Stop()
	if got := check(bare.Addr().String(), store.GRPCOptions{}); got.Up || !strings.Contains(got.Error, "does not implement") {
		t.Errorf("server without health checking: %+v", got)
	}

	closed := bare.Addr().String()
	plain.Stop()
	if got := check(closed, store.GRPCOptions{}); got.Up {
		t.Errorf("closed port: %+v", got)
	}
}

func TestWebSocketCheck(t *testing.T) {
	handshake := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/plain-http":
			w.Write([]byte("hello"))
			return
		case "/wrong-accept":
			w.Header().Set("Upgrade", "websocket")
			w.Header().Set("Connection", "Upgrade")
			w.Header().Set("Sec-WebSocket-Accept", "bm9wZQ==")
			w.WriteHeader(http.StatusSwitchingProtocols)
			return
		}
		if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") || r.Header.Get("Sec-WebSocket-Version") != "13" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		digest := sha1.Sum([]byte(r.Header.Get("Sec-WebSocket-Key") + websocketGUID))
		w.Header().Set("Upgrade", "websocket")
		w.Header().Set("Connection", "Upgrade")
		w.Header().Set("Sec-WebSocket-Accept", base64.StdEncoding.EncodeToString(digest[:]))
		if r.Header.Get("Sec-WebSocket-Protocol") == "graphql-ws" {
			w.Header().Set("Sec-WebSocket-Protocol", "graphql-ws")
		}
		w.WriteHeader(http.StatusSwitchingProtocols)
	}
	srv := httptest.NewServer(http.HandlerFunc(handshake))
	defer srv.Close()
	secure := httptest.NewTLSServer(http.HandlerFunc(handshake))
	defer secure.Close()

	c := New(Options{})
	check := func(target string, o store.WebSocketOptions) store.CheckResult {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return c.Check(ctx, store.Monitor{Type: store.MonitorWebSocket, Target: target, WebSocket: o})
	}
	ws := "ws" + strings.TrimPrefix(srv.URL, "http")
	wss := "wss" + strings.TrimPrefix(secure.URL, "https")

	if got := check(ws+"/socket", store.WebSocketOptions{}); !got.Up || got.StatusCode != 101 {
		t.Errorf("handshake: %+v", got)
	}
	if got := check(ws+"/socket", store.WebSocketOptions{Subprotocol: "graphql-ws"}); !got.Up || got.Detail != "graphql-ws" {
		t.Errorf("accepted subprotocol: %+v", got)
	}
	if got := check(ws+"/socket", store.WebSocketOptions{Subprotocol: "mqtt"}); got.Up || !strings.Contains(got.Error, "subprotocol") {
		t.Errorf("refused subprotocol: %+v", got)
	}
	if got := check(ws+"/plain-http", store.WebSocketOptions{}); got.Up || got.StatusCode != 200 {
		t.Errorf("endpoint that does not upgrade: %+v", got)
	}
	if got := check(ws+"/wrong-accept", store.WebSocketOptions{}); got.Up || !strings.Contains(got.Error, "Sec-WebSocket-Accept") {
		t.Errorf("wrong accept key: %+v", got)
	}
	if got := check(wss+"/socket", store.WebSocketOptions{}); got.Up {
		t.Errorf("self-signed certificate accepted: %+v", got)
	}
	if got := check(wss+"/socket", store.WebSocketOptions{SkipTLSVerify: true}); !got.Up || got.CertExpiresAt.IsZero() {
		t.Errorf("wss handshake: %+v", got)
	}
	if got := check(srv.URL, store.WebSocketOptions{}); got.Up || !strings.Contains(got.Error, "ws://") {
		t.Errorf("http:// target: %+v", got)
	}
}

func TestInvertedMonitor(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	c := New(Options{})
	m := store.Monitor{Type: store.MonitorTCP, Target: ln.Addr().String(), Inverted: true}

	// Reachable, although it must not be.
	if got := c.Check(context.Background(), m); got.Up || !strings.Contains(got.Error, "expects it to fail") || got.Detail != "" {
		t.Errorf("reachable target: %+v", got)
	}
	ln.Close()
	if got := c.Check(context.Background(), m); !got.Up || got.Error != "" || !strings.HasPrefix(got.Detail, "failed as expected: ") {
		t.Errorf("unreachable target: %+v", got)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("kısa", 10); got != "kısa" {
		t.Errorf("short text changed: %q", got)
	}
	// "ş" is two bytes; the cut must not split it.
	if got := truncate("aaşb", 3); got != "aa…" {
		t.Errorf("truncate = %q", got)
	}
}

type fakeWorkloads struct {
	desired, ready   int
	rolling, stalled bool
	err              error
}

func (f fakeWorkloads) Replicas(context.Context, string) (WorkloadStatus, error) {
	return WorkloadStatus{Desired: f.desired, Ready: f.ready, Rolling: f.rolling, Stalled: f.stalled}, f.err
}

func TestWorkloadCheck(t *testing.T) {
	check := func(w Workloads, minReady int) store.CheckResult {
		m := store.Monitor{Type: store.MonitorWorkload, Target: "shop/deployment/web", Workload: store.WorkloadOptions{MinReady: minReady}}
		return New(Options{Workloads: w}).Check(context.Background(), m)
	}

	if got := check(fakeWorkloads{desired: 3, ready: 3}, 0); !got.Up || got.Detail != "3 of 3 pods are ready" {
		t.Errorf("all ready: %+v", got)
	}
	// Without a minimum, one missing pod is a failure.
	if got := check(fakeWorkloads{desired: 3, ready: 2}, 0); got.Up || got.Error != "2 of 3 pods are ready" {
		t.Errorf("one pod missing: %+v", got)
	}
	// With one, the workload may be short of pods as long as enough are ready.
	if got := check(fakeWorkloads{desired: 3, ready: 2}, 2); !got.Up || got.Detail != "2 of 3 pods are ready" {
		t.Errorf("enough ready: %+v", got)
	}
	if got := check(fakeWorkloads{desired: 3, ready: 1}, 2); got.Up || got.Error != "1 of 3 pods are ready, at least 2 are needed" {
		t.Errorf("too few ready: %+v", got)
	}
	// More is asked for than the workload is meant to run.
	if got := check(fakeWorkloads{desired: 1, ready: 1}, 2); got.Up {
		t.Errorf("minimum above the replicas: %+v", got)
	}
	if got := check(fakeWorkloads{}, 0); got.Up || got.Error != "the workload is scaled to zero" {
		t.Errorf("scaled to zero: %+v", got)
	}
	if got := check(fakeWorkloads{err: errors.New("deployment shop/web does not exist")}, 0); got.Up || !strings.Contains(got.Error, "does not exist") {
		t.Errorf("missing workload: %+v", got)
	}
	if got := check(nil, 0); got.Up || !strings.Contains(got.Error, "cluster") {
		t.Errorf("without a cluster: %+v", got)
	}
}

func TestWorkloadCheckTolaratesRollouts(t *testing.T) {
	m := store.Monitor{Type: store.MonitorWorkload, Target: "shop/deployment/web"}
	clock := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	c := New(Options{Workloads: nil, RolloutGrace: 10 * time.Minute})
	c.now = func() time.Time { return clock }
	check := func(w Workloads) store.CheckResult {
		c.opts.Workloads = w
		return c.Check(context.Background(), m)
	}

	// Missing pods during a rollout are not a failure.
	if got := check(fakeWorkloads{desired: 3, ready: 2, rolling: true}); !got.Up || !strings.HasPrefix(got.Detail, "rollout in progress for 0s") {
		t.Errorf("rollout just started: %+v", got)
	}
	clock = clock.Add(9 * time.Minute)
	if got := check(fakeWorkloads{desired: 3, ready: 1, rolling: true}); !got.Up || !strings.Contains(got.Detail, "for 9m0s") {
		t.Errorf("rollout at 9m: %+v", got)
	}
	// ...until it has gone on for too long.
	clock = clock.Add(2 * time.Minute)
	if got := check(fakeWorkloads{desired: 3, ready: 1, rolling: true}); got.Up || !strings.Contains(got.Error, "going on for 11m0s") {
		t.Errorf("rollout at 11m: %+v", got)
	}
	// ...or the cluster has given up on it.
	if got := check(fakeWorkloads{desired: 3, ready: 1, rolling: true, stalled: true}); got.Up || !strings.Contains(got.Error, "stalled") {
		t.Errorf("stalled rollout: %+v", got)
	}
	// The rollout finishing clears the clock; the next one starts fresh.
	if got := check(fakeWorkloads{desired: 3, ready: 3}); !got.Up || got.Detail != "3 of 3 pods are ready" {
		t.Errorf("after the rollout: %+v", got)
	}
	if got := check(fakeWorkloads{desired: 3, ready: 2, rolling: true}); !got.Up || !strings.HasPrefix(got.Detail, "rollout in progress for 0s") {
		t.Errorf("next rollout: %+v", got)
	}
	// All pods ready mid-rollout is simply fine.
	if got := check(fakeWorkloads{desired: 3, ready: 3, rolling: true}); !got.Up || got.Detail != "3 of 3 pods are ready, rollout in progress" {
		t.Errorf("ready during rollout: %+v", got)
	}
	// Without grace, a rollout gets no allowance.
	c.opts.RolloutGrace = 0
	if got := check(fakeWorkloads{desired: 3, ready: 2, rolling: true}); got.Up {
		t.Errorf("no grace: %+v", got)
	}
}
