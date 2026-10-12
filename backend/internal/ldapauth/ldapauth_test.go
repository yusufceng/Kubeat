package ldapauth

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
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
