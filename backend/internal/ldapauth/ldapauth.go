// Package ldapauth checks usernames and passwords against an LDAP
// directory, such as Active Directory or OpenLDAP, and reads the groups a
// user is in.
package ldapauth

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"
)

// Config says where the directory is and how users are found in it.
type Config struct {
	// URL is ldap://host:389 or ldaps://host:636; empty disables LDAP.
	URL string
	// StartTLS upgrades an ldap:// connection to TLS before anything is sent.
	StartTLS           bool
	CAFile             string
	InsecureSkipVerify bool
	// BindDN and BindPassword are the account that searches for users;
	// empty searches anonymously, which few directories allow.
	BindDN       string
	BindPassword string
	// BaseDN is where users are searched for, with UserFilter, in which
	// {username} stands for what was typed: "(uid={username})" for most
	// directories, "(sAMAccountName={username})" for Active Directory.
	BaseDN     string
	UserFilter string
	// The attributes of a user's name, mail and groups.
	DisplayNameAttr string
	EmailAttr       string
	GroupAttr       string
	// GroupFilter, if set, finds a user's groups by searching GroupBaseDN
	// (BaseDN when empty) for entries matching it, with {dn} standing for
	// the user's DN: "(member={dn})". For directories without memberOf.
	GroupBaseDN string
	GroupFilter string
	Timeout     time.Duration
}

// Defaults of the configuration.
const (
	DefaultUserFilter      = "(uid={username})"
	DefaultDisplayNameAttr = "cn"
	DefaultEmailAttr       = "mail"
	DefaultGroupAttr       = "memberOf"
)

var (
	// ErrInvalidCredentials means the username or the password is wrong.
	ErrInvalidCredentials = errors.New("ldap: invalid username or password")
	// ErrUnavailable means the directory could not be asked.
	ErrUnavailable = errors.New("ldap: the directory is not reachable")
)

// Identity is what the directory says about a user who signed in.
type Identity struct {
	DN          string
	Username    string
	DisplayName string
	Email       string
	// Groups are the DNs of the groups the user is in.
	Groups []string
}

// Client talks to one directory.
type Client struct {
	cfg Config
	tls *tls.Config
}

// New returns a Client, or nil when cfg.URL is empty.
func New(cfg Config) (*Client, error) {
	if cfg.URL == "" {
		return nil, nil
	}
	u, err := url.Parse(cfg.URL)
	if err != nil || (u.Scheme != "ldap" && u.Scheme != "ldaps") || u.Host == "" {
		return nil, errors.New("ldap: the URL must be ldap://host:port or ldaps://host:port")
	}
	if cfg.BaseDN == "" {
		return nil, errors.New("ldap: a base DN is required")
	}
	if cfg.UserFilter == "" {
		cfg.UserFilter = DefaultUserFilter
	}
	if !strings.Contains(cfg.UserFilter, "{username}") {
		return nil, errors.New("ldap: the user filter must contain {username}")
	}
	if cfg.GroupFilter != "" && !strings.Contains(cfg.GroupFilter, "{dn}") {
		return nil, errors.New("ldap: the group filter must contain {dn}")
	}
	if cfg.DisplayNameAttr == "" {
		cfg.DisplayNameAttr = DefaultDisplayNameAttr
	}
	if cfg.EmailAttr == "" {
		cfg.EmailAttr = DefaultEmailAttr
	}
	if cfg.GroupAttr == "" {
		cfg.GroupAttr = DefaultGroupAttr
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 10 * time.Second
	}
	tlsConfig := &tls.Config{ServerName: u.Hostname(), InsecureSkipVerify: cfg.InsecureSkipVerify}
	if cfg.CAFile != "" {
		pem, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("ldap: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("ldap: %s contains no certificates", cfg.CAFile)
		}
		tlsConfig.RootCAs = pool
	}
	return &Client{cfg: cfg, tls: tlsConfig}, nil
}

// URL returns where the directory is, for display.
func (c *Client) URL() string { return c.cfg.URL }

func (c *Client) connect(ctx context.Context) (*ldap.Conn, error) {
	dialCtx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()
	conn, err := ldap.DialURL(c.cfg.URL, ldap.DialWithTLSConfig(c.tls), ldap.DialWithDialer(dialerFor(dialCtx)))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	conn.SetTimeout(c.cfg.Timeout)
	if c.cfg.StartTLS && !strings.HasPrefix(c.cfg.URL, "ldaps://") {
		if err := conn.StartTLS(c.tls); err != nil {
			conn.Close()
			return nil, fmt.Errorf("%w: StartTLS: %v", ErrUnavailable, err)
		}
	}
	return conn, nil
}

// Authenticate checks the password of a user and returns what the
// directory knows about them.
func (c *Client) Authenticate(ctx context.Context, username, password string) (Identity, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return Identity{}, ErrInvalidCredentials // an empty password would be an anonymous bind
	}
	conn, err := c.connect(ctx)
	if err != nil {
		return Identity{}, err
	}
	defer conn.Close()

	// Find the user with the search account, then bind as the user.
	if c.cfg.BindDN != "" {
		if err := conn.Bind(c.cfg.BindDN, c.cfg.BindPassword); err != nil {
			return Identity{}, fmt.Errorf("%w: the search account cannot bind: %v", ErrUnavailable, err)
		}
	}
	filter := strings.ReplaceAll(c.cfg.UserFilter, "{username}", ldap.EscapeFilter(username))
	attrs := []string{c.cfg.DisplayNameAttr, c.cfg.EmailAttr, c.cfg.GroupAttr}
	result, err := conn.Search(ldap.NewSearchRequest(c.cfg.BaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 2, 0, false, filter, attrs, nil))
	if err != nil {
		return Identity{}, fmt.Errorf("%w: search: %v", ErrUnavailable, err)
	}
	if len(result.Entries) != 1 {
		return Identity{}, ErrInvalidCredentials
	}
	entry := result.Entries[0]
	if err := conn.Bind(entry.DN, password); err != nil {
		if ldap.IsErrorWithCode(err, ldap.LDAPResultInvalidCredentials) {
			return Identity{}, ErrInvalidCredentials
		}
		return Identity{}, fmt.Errorf("%w: bind: %v", ErrUnavailable, err)
	}

	id := Identity{
		DN: entry.DN, Username: username,
		DisplayName: entry.GetAttributeValue(c.cfg.DisplayNameAttr),
		Email:       entry.GetAttributeValue(c.cfg.EmailAttr),
		Groups:      entry.GetAttributeValues(c.cfg.GroupAttr),
	}
	if c.cfg.GroupFilter != "" {
		groups, err := c.searchGroups(conn, entry.DN)
		if err != nil {
			return Identity{}, err
		}
		id.Groups = append(id.Groups, groups...)
	}
	return id, nil
}

func (c *Client) searchGroups(conn *ldap.Conn, userDN string) ([]string, error) {
	base := c.cfg.GroupBaseDN
	if base == "" {
		base = c.cfg.BaseDN
	}
	filter := strings.ReplaceAll(c.cfg.GroupFilter, "{dn}", ldap.EscapeFilter(userDN))
	result, err := conn.Search(ldap.NewSearchRequest(base, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false, filter, []string{"dn"}, nil))
	if err != nil {
		return nil, fmt.Errorf("%w: group search: %v", ErrUnavailable, err)
	}
	groups := make([]string, 0, len(result.Entries))
	for _, e := range result.Entries {
		groups = append(groups, e.DN)
	}
	return groups, nil
}

// SameDN reports whether two distinguished names name the same entry,
// ignoring case and the spaces around their parts.
func SameDN(a, b string) bool {
	parse := func(s string) string {
		dn, err := ldap.ParseDN(s)
		if err != nil {
			return strings.ToLower(strings.TrimSpace(s))
		}
		return strings.ToLower(dn.String())
	}
	return parse(a) == parse(b)
}
