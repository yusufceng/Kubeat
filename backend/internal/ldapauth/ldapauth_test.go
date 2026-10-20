package ldapauth

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestConfig(t *testing.T) {
	if c, err := New(Config{}); c != nil || err != nil {
		t.Errorf("without a URL: %v, %v", c, err)
	}
	for name, cfg := range map[string]Config{
		"scheme":       {URL: "http://ldap.example.com", BaseDN: "dc=example,dc=com"},
		"no base":      {URL: "ldap://ldap.example.com"},
		"user filter":  {URL: "ldap://ldap.example.com", BaseDN: "dc=x", UserFilter: "(uid=ayse)"},
		"group filter": {URL: "ldap://ldap.example.com", BaseDN: "dc=x", GroupFilter: "(member=x)"},
		"ca file":      {URL: "ldap://ldap.example.com", BaseDN: "dc=x", CAFile: "/nonexistent"},
	} {
		if _, err := New(cfg); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if !SameDN("CN=Shop-Team, OU=Groups,DC=Example,DC=com", "cn=shop-team,ou=groups,dc=example,dc=com") || SameDN("cn=a,dc=x", "cn=b,dc=x") {
		t.Error("SameDN")
	}
}

// silentDirectory accepts connections and never answers them, but with
// first, if given, to the first request on each; it returns its address.
func silentDirectory(t *testing.T, first []byte) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var (
		mu    sync.Mutex
		conns []net.Conn
	)
	t.Cleanup(func() {
		l.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			c.Close()
		}
	})
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
			go func() {
				if first != nil {
					if _, err := c.Read(make([]byte, 512)); err != nil {
						return
					}
					c.Write(first)
				}
				io.Copy(io.Discard, c)
			}()
		}
	}()
	return l.Addr().String()
}

// A sign-in stops as soon as its request ends, whatever the directory is
// doing, rather than when the directory's timeout runs out: a request
// whose client went away must give its slot back.
func TestAuthenticateStopsWithItsRequest(t *testing.T) {
	for _, scheme := range []string{"ldap", "ldaps"} {
		c, err := New(Config{
			URL: scheme + "://" + silentDirectory(t, nil), BaseDN: "dc=example,dc=com",
			BindDN: "uid=svc-kubeat,dc=example,dc=com", BindPassword: "s3cret", Timeout: 5 * time.Second,
		})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(100*time.Millisecond, cancel)
		start := time.Now()
		_, err = c.Authenticate(ctx, "ayse", "s3cret")
		if !errors.Is(err, ErrUnavailable) {
			t.Errorf("%s: err = %v, want ErrUnavailable", scheme, err)
		}
		if took := time.Since(start); took > 2*time.Second {
			t.Errorf("%s: returned %s after the request began, not when it ended", scheme, took)
		}
	}
}

// A directory that agrees to StartTLS and then says nothing does not hold
// the sign-in beyond the timeout.
func TestStartTLSHasATimeout(t *testing.T) {
	// The answer to the first request, StartTLS: message 1, success.
	agreed := []byte{0x30, 0x0c, 0x02, 0x01, 0x01, 0x78, 0x07, 0x0a, 0x01, 0x00, 0x04, 0x00, 0x04, 0x00}
	c, err := New(Config{URL: "ldap://" + silentDirectory(t, agreed), StartTLS: true, BaseDN: "dc=example,dc=com", Timeout: 200 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := c.Authenticate(context.Background(), "ayse", "s3cret")
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "StartTLS") {
			t.Errorf("err = %v, want ErrUnavailable from StartTLS", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the TLS handshake after StartTLS is still waiting")
	}
}

// Runs against a directory with the users of scratchpad/ldap/glauth.cfg:
//
//	KUBEAT_TEST_LDAP=ldap://127.0.0.1:3893 go test ./internal/ldapauth/
func TestAgainstADirectory(t *testing.T) {
	url := os.Getenv("KUBEAT_TEST_LDAP")
	if url == "" {
		t.Skip("KUBEAT_TEST_LDAP is not set")
	}
	c, err := New(Config{
		URL: url, BaseDN: "dc=example,dc=com",
		BindDN: "uid=svc-kubeat,ou=svcaccts,dc=example,dc=com", BindPassword: "s3cret",
		UserFilter: "(uid={username})", DisplayNameAttr: "givenName",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	id, err := c.Authenticate(ctx, "ayse", "s3cret")
	if err != nil {
		t.Fatalf("sign in: %v", err)
	}
	if id.Username != "ayse" || !strings.Contains(id.DN, "ayse") || id.Email != "ayse@example.com" || id.DisplayName == "" {
		t.Errorf("identity = %+v", id)
	}
	if len(id.Groups) == 0 || !strings.Contains(strings.ToLower(strings.Join(id.Groups, " ")), "platform-admins") {
		t.Errorf("groups = %v, want the platform-admins group", id.Groups)
	}
	mehmet, err := c.Authenticate(ctx, "mehmet", "s3cret")
	if err != nil || len(mehmet.Groups) < 2 {
		t.Errorf("mehmet = %+v, %v (want two groups)", mehmet, err)
	}

	for name, attempt := range map[string][2]string{
		"wrong password": {"ayse", "wrong"},
		"unknown user":   {"nobody", "s3cret"},
		"empty password": {"ayse", ""},
	} {
		if _, err := c.Authenticate(ctx, attempt[0], attempt[1]); !errors.Is(err, ErrInvalidCredentials) {
			t.Errorf("%s: err = %v, want ErrInvalidCredentials", name, err)
		}
	}
	// The filter is escaped, so this is a search for a strange name: a real
	// directory finds nobody, glauth refuses the escaped filter. Either way
	// nobody signs in.
	if _, err := c.Authenticate(ctx, "ayse)(uid=*", "s3cret"); err == nil {
		t.Error("a filter injection signed in")
	}
	// A search account that cannot bind, and a directory that is not there.
	bad, _ := New(Config{URL: url, BaseDN: "dc=example,dc=com", BindDN: "uid=svc-kubeat,ou=svcaccts,dc=example,dc=com", BindPassword: "wrong"})
	if _, err := bad.Authenticate(ctx, "ayse", "s3cret"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("bad search account: err = %v, want ErrUnavailable", err)
	}
	gone, _ := New(Config{URL: "ldap://127.0.0.1:1", BaseDN: "dc=example,dc=com"})
	if _, err := gone.Authenticate(ctx, "ayse", "s3cret"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("unreachable directory: err = %v, want ErrUnavailable", err)
	}
}
