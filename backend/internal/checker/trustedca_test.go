package checker

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/explain"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// trustedCATargetEnv carries, to the child process of
// TestTrustedCABundle, the address it checks; set, the test is the child.
const trustedCATargetEnv = "KUBEAT_TEST_TRUSTED_CA_TARGET"

// The internal CA recipe (README, "OpenShift", "Kurum CA'sı") mounts a
// ConfigMap holding the CA and points SSL_CERT_DIR at it. It rests on how Go
// loads the roots that the TLS checks verify against, as they give none of
// their own, on Linux, where Kubeat runs: SSL_CERT_DIR adds the
// certificates in its directories to the system bundle file, read through
// the links a ConfigMap volume is made of, while SSL_CERT_FILE replaces
// that file. (SSL_CERT_DIR also replaces the default directories, which in
// Kubeat's image hold the same bundle as the file; the README says what
// that leaves of each variable there.) The recipe's volume is optional: a
// ConfigMap not there leaves the directory empty, which keeps the system
// bundle, so that only the internally signed targets fail and the pod
// still starts. An HTTP check, over the shared
// transports, and a TCP check with TLS, over the configuration the gRPC,
// WebSocket and database checks build alike, see the same roots. They are
// read once in a process, so each case checks its target in a child
// process of its own, started with the case's environment.
func TestTrustedCABundle(t *testing.T) {
	if target := os.Getenv(trustedCATargetEnv); target != "" {
		trustedCAChild(target)
		return
	}
	if runtime.GOOS != "linux" {
		t.Skipf("Go reads SSL_CERT_DIR and SSL_CERT_FILE on Linux, where Kubeat runs; on %s the platform verifier decides", runtime.GOOS)
	}
	internal := newTestCA(t, "Kubeat test internal CA")
	public := newTestCA(t, "Kubeat test public CA")
	internalTarget := internal.server(t).Listener.Addr().String()
	publicTarget := public.server(t).Listener.Addr().String()

	work := t.TempDir()
	mounted := configMapVolume(t, filepath.Join(work, "trusted-ca"), map[string][]byte{"ca-bundle.crt": internal.pem})
	// What the kubelet mounts for an optional ConfigMap that is not there,
	// and for the empty one OpenShift has yet to fill.
	empty := configMapVolume(t, filepath.Join(work, "missing-ca"), nil)
	// The system bundle file, standing for the image's: a CA of the test's
	// own, so that what SSL_CERT_DIR keeps of it shows.
	system := filepath.Join(work, "system-bundle.crt")
	internalOnly := filepath.Join(work, "internal-only.crt")
	for path, data := range map[string][]byte{system: public.pem, internalOnly: internal.pem} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	for _, c := range []struct {
		name   string
		env    []string
		target string
		want   string
	}{
		{"neither variable: the internal CA is not trusted", nil, internalTarget, explain.TLSUnknownAuthority},
		{"SSL_CERT_DIR: the internal CA is trusted", []string{"SSL_CERT_DIR=" + mounted}, internalTarget, "up"},
		{"SSL_CERT_DIR adds to the bundle file: the internal CA", []string{"SSL_CERT_FILE=" + system, "SSL_CERT_DIR=" + mounted}, internalTarget, "up"},
		{"SSL_CERT_DIR adds to the bundle file: the bundle's CA", []string{"SSL_CERT_FILE=" + system, "SSL_CERT_DIR=" + mounted}, publicTarget, "up"},
		{"the bundle file alone: its CA", []string{"SSL_CERT_FILE=" + system}, publicTarget, "up"},
		{"SSL_CERT_FILE replaces the bundle file: the internal CA", []string{"SSL_CERT_FILE=" + internalOnly}, internalTarget, "up"},
		{"SSL_CERT_FILE replaces the bundle file: the bundle's CA is lost", []string{"SSL_CERT_FILE=" + internalOnly}, publicTarget, explain.TLSUnknownAuthority},
		{"the ConfigMap missing, an empty volume: the bundle's CA is kept", []string{"SSL_CERT_FILE=" + system, "SSL_CERT_DIR=" + empty}, publicTarget, "up"},
		{"the ConfigMap missing, an empty volume: the internal CA is not trusted", []string{"SSL_CERT_FILE=" + system, "SSL_CERT_DIR=" + empty}, internalTarget, explain.TLSUnknownAuthority},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := runTrustedCAChild(t, c.target, c.env)
			for _, check := range []string{"http", "tcp"} {
				if got[check] != c.want {
					t.Errorf("%s check: got %q, want %s", check, got[check], c.want)
				}
			}
		})
	}
}

// trustedCAChild checks the HTTPS server at addr with a checker of the
// defaults, so with the system roots, as an HTTP monitor and as a TCP
// monitor with TLS, and prints what came of each: "up", or the code the
// failure is explained with.
func trustedCAChild(addr string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := New(Options{})
	for check, m := range map[string]store.Monitor{
		"http": httpMonitor("https://" + addr + "/"),
		"tcp":  {Type: store.MonitorTCP, Target: addr, TCP: store.TCPOptions{TLS: true}},
	} {
		got := c.Check(ctx, m)
		switch {
		case got.Up:
			fmt.Printf("%s=up\n", check)
		case explanation(got) != "":
			fmt.Printf("%s=%s\n", check, explanation(got))
		default:
			fmt.Printf("%s=unexplained: %s\n", check, got.Error)
		}
	}
}

// runTrustedCAChild runs this test again in a child process that checks
// addr, with env added to an environment that has neither SSL_CERT_FILE
// nor SSL_CERT_DIR, and returns what the child printed for each check.
func runTrustedCAChild(t *testing.T, addr string, env []string) map[string]string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestTrustedCABundle$", "-test.count=1")
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); name != "SSL_CERT_FILE" && name != "SSL_CERT_DIR" && name != trustedCATargetEnv {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = append(append(cmd.Env, env...), trustedCATargetEnv+"="+addr)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child: %v\n%s", err, out)
	}
	results := map[string]string{}
	for sc := bufio.NewScanner(bytes.NewReader(out)); sc.Scan(); {
		for _, check := range []string{"http", "tcp"} {
			if result, ok := strings.CutPrefix(sc.Text(), check+"="); ok {
				results[check] = result
			}
		}
	}
	if len(results) != 2 {
		t.Fatalf("child printed no result for a check:\n%s", out)
	}
	return results
}

// testCA is a certificate authority of a test's own.
type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  []byte
}

func newTestCA(t *testing.T, name string) testCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: serialNumber(t), Subject: pkix.Name{CommonName: name},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return testCA{cert: cert, key: key, pem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}
}

// server starts an HTTPS server answering 200 with a certificate for
// 127.0.0.1 that the CA issued; a TCP check with TLS completes its
// handshake as well.
func (ca testCA) server(t *testing.T) *httptest.Server {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: serialNumber(t), Subject: pkix.Name{CommonName: "127.0.0.1"},
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	// A refused certificate, as some cases want, is not the server's error.
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

func serialNumber(t *testing.T) *big.Int {
	t.Helper()
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		t.Fatal(err)
	}
	return serial
}

// configMapVolume lays dir out as the kubelet mounts a ConfigMap volume
// with these keys, none for one that is empty or, optional, not there, and
// returns dir: the data in a directory of its own, a "..data" link to it,
// and for each key a link through "..data".
func configMapVolume(t *testing.T, dir string, keys map[string][]byte) string {
	t.Helper()
	const timestamped = "..2026_11_08_12_00_00.000000001"
	if err := os.MkdirAll(filepath.Join(dir, timestamped), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(timestamped, filepath.Join(dir, "..data")); err != nil {
		t.Fatal(err)
	}
	for key, data := range keys {
		if err := os.WriteFile(filepath.Join(dir, timestamped, key), data, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join("..data", key), filepath.Join(dir, key)); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
