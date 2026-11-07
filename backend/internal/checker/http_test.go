package checker

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/alert"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// certificate is a self-signed certificate for host that expires in left.
func certificate(t *testing.T, host string, left time.Duration) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: host}, DNSNames: []string{host},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(left)}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}
}

// annotated is an HTTPS monitor of the scope tests' world as an annotation
// sets one up: held to its object's namespace.
func annotated(target string) store.Monitor {
	m := httpMonitor(target)
	m.HTTP.SkipTLSVerify = true // the certificates here are self-signed
	m.Namespace, m.Source, m.Managed, m.Scope = "shop", "ingress/web", true, store.ObjectScope("shop")
	return m
}

// nobody is a sender of notifications that sends them nowhere.
type nobody struct{}

func (nobody) Send(context.Context, store.Channel, alert.Notification) error { return nil }

// A certificate the server renews shows in the very next check, which opens
// a connection of its own, and the certificate incident the expiring one
// opened closes on it: for a monitor held to a scope, an annotation
// monitor's, and for one held to none alike.
func TestHTTPCheckSeesRenewedCertificate(t *testing.T) {
	const host = "public.example.test"
	expiring := certificate(t, host, 5*24*time.Hour)
	renewed := certificate(t, host, 90*24*time.Hour)
	var served atomic.Pointer[tls.Certificate]
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	// Asked for by name (SNI), the server presents the certificate of the day.
	srv.TLS = &tls.Config{GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) { return served.Load(), nil }}
	srv.StartTLS()
	defer srv.Close()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	target := "https://" + net.JoinHostPort(host, port) + "/"
	c := scopedChecker(t, scopeDNS(t), true, &refusals{}, Options{})

	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, filepath.Join(t.TempDir(), "test.db"), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	engine := alert.New(db, nobody{}, alert.Options{RecoveryThreshold: 1, CertWarning: 14 * 24 * time.Hour, QueueSize: 10,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	certIncident := func(t *testing.T, id int64) (store.Incident, bool) {
		t.Helper()
		list, err := db.ListIncidents(ctx, 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		for _, i := range list {
			if i.MonitorID == id && i.Kind == store.IncidentCert {
				return i, true
			}
		}
		return store.Incident{}, false
	}

	unscoped := httpMonitor(target)
	unscoped.HTTP.SkipTLSVerify = true
	unscoped.Scope = &store.TargetScope{Namespaces: []string{store.AllNamespaces}}
	for _, tc := range []struct {
		name string
		m    store.Monitor
		held bool
	}{
		{"held to a scope", annotated(target), true},
		{"held to none", unscoped, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stored, err := db.CreateMonitor(ctx, store.Monitor{Name: tc.name, Type: store.MonitorHTTP, Target: target,
				Interval: 30 * time.Second, Timeout: 5 * time.Second, FailureThreshold: 1, Enabled: true})
			if err != nil {
				t.Fatal(err)
			}
			m := tc.m
			m.ID = stored.ID
			checked := func() store.CheckResult {
				t.Helper()
				got := check(c, m)
				if !got.Up {
					t.Fatalf("check: %+v", got)
				}
				if err := db.InsertCheckResults(ctx, []store.CheckResult{got}); err != nil {
					t.Fatal(err)
				}
				engine.Evaluate(ctx, []store.CheckResult{got})
				return got
			}

			served.Store(&expiring)
			if got := checked(); !got.CertExpiresAt.Equal(expiring.Leaf.NotAfter) {
				t.Fatalf("certificate expires at %s, want %s", got.CertExpiresAt, expiring.Leaf.NotAfter)
			}
			if i, ok := certIncident(t, m.ID); !ok || !i.Open() {
				t.Fatalf("no open certificate incident for a certificate with 5 days left: %+v", i)
			}
			c.mu.Lock()
			_, kept := c.held[m.ID]
			c.mu.Unlock()
			if kept != tc.held {
				t.Fatalf("transport of its own kept = %v, want %v", kept, tc.held)
			}

			served.Store(&renewed)
			got := checked()
			if !got.CertExpiresAt.Equal(renewed.Leaf.NotAfter) {
				t.Errorf("after the renewal the next check reported a certificate expiring at %s, want the renewed one's %s",
					got.CertExpiresAt, renewed.Leaf.NotAfter)
			}
			if i, ok := certIncident(t, m.ID); !ok || i.Open() || !i.ResolvedAt.Equal(got.CheckedAt) {
				t.Errorf("certificate incident after the renewal = %+v, want it resolved by the check at %s", i, got.CheckedAt)
			}
		})
	}
}

// conns are the client sides of the connections the checks got, as the
// checks' own trace reports them, and the most of them open at once.
type conns struct {
	t    *testing.T
	mu   sync.Mutex
	seen []net.Conn
	peak int
}

// trace returns ctx with a trace that tells cs of every connection a
// request of the check gets.
func (cs *conns) trace(ctx context.Context) context.Context {
	return httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) {
		cs.mu.Lock()
		defer cs.mu.Unlock()
		if !slices.Contains(cs.seen, info.Conn) {
			cs.seen = append(cs.seen, info.Conn)
		}
		cs.peak = max(cs.peak, cs.openLocked())
	}})
}

// openLocked counts the connections seen whose client side is still open.
// A closed socket refuses even a look at its descriptor, so this is known
// the moment the connection was closed, without waiting for anything.
func (cs *conns) openLocked() int {
	open := 0
	for _, conn := range cs.seen {
		if secured, ok := conn.(*tls.Conn); ok {
			conn = secured.NetConn()
		}
		sc, ok := conn.(syscall.Conn)
		if !ok {
			cs.t.Errorf("a connection of type %T", conn)
			continue
		}
		raw, err := sc.SyscallConn()
		if err != nil {
			cs.t.Errorf("connection: %v", err)
			continue
		}
		if raw.Control(func(uintptr) {}) == nil {
			open++
		}
	}
	return open
}

func (cs *conns) counts() (seen, open, peak int) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return len(cs.seen), cs.openLocked(), cs.peak
}

// The connections of a check close when it ends, so that after the checks
// of many monitors held to a scope (annotation monitors here, each with a
// transport of its own) the connections still open and the goroutines that
// serve them are bounded by the checks under way, the workers, not by the
// monitors: none once they are done. Over HTTP/1.1 and HTTP/2 alike.
func TestHTTPCheckClosesItsConnections(t *testing.T) {
	const monitors, workers = 100, 4
	for _, h2 := range []bool{false, true} {
		name := "HTTP/1.1"
		if h2 {
			name = "HTTP/2"
		}
		t.Run(name, func(t *testing.T) {
			proto := 1
			if h2 {
				proto = 2
			}
			srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.ProtoMajor != proto {
					w.WriteHeader(http.StatusHTTPVersionNotSupported)
				}
			}))
			srv.EnableHTTP2 = h2
			srv.TLS = &tls.Config{Certificates: []tls.Certificate{certificate(t, "public.example.test", 90*24*time.Hour)}}
			srv.StartTLS()
			defer srv.Close()
			_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
			c := scopedChecker(t, scopeDNS(t), true, &refusals{}, Options{})
			before := runtime.NumGoroutine()

			cs := &conns{t: t}
			jobs := make(chan store.Monitor)
			var wg sync.WaitGroup
			for range workers {
				wg.Go(func() {
					for m := range jobs {
						ctx, cancel := context.WithTimeout(cs.trace(context.Background()), 5*time.Second)
						got := c.Check(ctx, m)
						cancel()
						if !got.Up {
							t.Errorf("monitor %d: %+v", m.ID, got)
						}
					}
				})
			}
			for i := range monitors {
				m := annotated("https://public.example.test:" + port + "/")
				m.ID = int64(i + 1)
				jobs <- m
			}
			close(jobs)
			wg.Wait()

			c.mu.Lock()
			transports := len(c.held)
			c.mu.Unlock()
			if transports != monitors {
				t.Fatalf("%d transports kept, want one for each of the %d monitors held to a scope", transports, monitors)
			}
			seen, open, peak := cs.counts()
			if seen != monitors {
				t.Errorf("%d checks got %d connections, want one each", monitors, seen)
			}
			if open != 0 {
				t.Errorf("%d connections left open after the checks", open)
			}
			if peak > workers {
				t.Errorf("%d connections open at once, more than the %d checks under way", peak, workers)
			}
			if t.Failed() {
				return // connections left open keep their goroutines
			}

			// The goroutines of a connection exit once it is closed, which
			// the checks did before they returned; waited for without a
			// bound of its own (a leak would show as the test's timeout,
			// with the goroutines left in its dump).
			for runtime.NumGoroutine() > before+workers {
				runtime.Gosched()
				time.Sleep(time.Millisecond)
			}
		})
	}
}
