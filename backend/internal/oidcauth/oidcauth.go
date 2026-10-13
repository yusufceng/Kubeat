// Package oidcauth signs users in through an OpenID Connect provider
// (Keycloak, Azure AD, Okta, Dex, Authentik...) with the authorization code
// flow and PKCE. Kubeat is the OIDC client: the browser is sent to the
// provider and comes back with a code that this package exchanges and
// verifies; the frontend only relays.
package oidcauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// Config says where the provider is and how to read its claims.
type Config struct {
	// Issuer is the provider's issuer URL, which serves
	// /.well-known/openid-configuration.
	Issuer       string
	ClientID     string
	ClientSecret string // empty for a public client; PKCE is always used
	// RedirectURL is where the provider sends the browser back to: the
	// frontend's /auth/callback, as registered with the provider. Left
	// empty, each sign-in uses the address the frontend says it is served
	// at; the provider still only accepts registered ones.
	RedirectURL string
	// Scopes are requested in addition to "openid".
	Scopes []string
	// The claims the account is made from. Groups may come from the ID
	// token or from the userinfo endpoint; both are read.
	UsernameClaim    string
	DisplayNameClaim string
	EmailClaim       string
	GroupsClaim      string
	// CAFile verifies the provider's certificate when it is not signed by
	// a public authority; InsecureSkipVerify skips verification.
	CAFile             string
	InsecureSkipVerify bool
	// Timeout bounds each request to the provider.
	Timeout time.Duration
	// StateTTL is how long a sign-in may take between leaving for the
	// provider and coming back.
	StateTTL time.Duration
}

// Identity is what the provider said about a user.
type Identity struct {
	Subject     string
	Username    string
	DisplayName string
	Email       string
	Groups      []string
}

var (
	// ErrInvalidState means the callback did not match a sign-in that was
	// started here, or came too late.
	ErrInvalidState = errors.New("unknown or expired sign-in state")
	// ErrUnavailable means the provider could not be reached or answered
	// badly; the user's credentials were not judged.
	ErrUnavailable = errors.New("the identity provider could not be reached")
)

// pending is a sign-in that left for the provider and may come back.
type pending struct {
	nonce, verifier, redirect string
	expires                   time.Time
}

// Client is an OIDC relying party for one provider.
type Client struct {
	cfg    Config
	client *http.Client

	mu       sync.Mutex
	provider *oidc.Provider // nil until discovery succeeded
	pending  map[string]pending
	now      func() time.Time
}

// New returns a client. The provider is discovered on first use, not here,
// so that Kubeat starts even when the provider is briefly away.
func New(cfg Config) (*Client, error) {
	if cfg.Issuer == "" {
		return nil, nil
	}
	if cfg.ClientID == "" {
		return nil, errors.New("oidc: a client id is required")
	}
	if cfg.UsernameClaim == "" {
		cfg.UsernameClaim = "preferred_username"
	}
	if cfg.DisplayNameClaim == "" {
		cfg.DisplayNameClaim = "name"
	}
	if cfg.EmailClaim == "" {
		cfg.EmailClaim = "email"
	}
	if cfg.GroupsClaim == "" {
		cfg.GroupsClaim = "groups"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	if cfg.StateTTL <= 0 {
		cfg.StateTTL = 10 * time.Minute
	}
	tlsConfig := &tls.Config{InsecureSkipVerify: cfg.InsecureSkipVerify} //nolint:gosec // an explicit setting
	if cfg.CAFile != "" {
		pem, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("oidc: read CA file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("oidc: %s holds no certificate", cfg.CAFile)
		}
		tlsConfig.RootCAs = pool
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsConfig
	return &Client{
		cfg:     cfg,
		client:  &http.Client{Transport: transport, Timeout: cfg.Timeout},
		pending: make(map[string]pending),
		now:     time.Now,
	}, nil
}

// Issuer is the provider's issuer URL.
func (c *Client) Issuer() string { return c.cfg.Issuer }

// discover fetches the provider's configuration once.
func (c *Client) discover(ctx context.Context) (*oidc.Provider, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.provider != nil {
		return c.provider, nil
	}
	p, err := oidc.NewProvider(oidc.ClientContext(ctx, c.client), c.cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	c.provider = p
	return p, nil
}

func (c *Client) oauthConfig(p *oidc.Provider, redirect string) oauth2.Config {
	scopes := []string{oidc.ScopeOpenID}
	for _, s := range c.cfg.Scopes {
		if s != "" && s != oidc.ScopeOpenID {
			scopes = append(scopes, s)
		}
	}
	return oauth2.Config{
		ClientID: c.cfg.ClientID, ClientSecret: c.cfg.ClientSecret,
		Endpoint: p.Endpoint(), RedirectURL: redirect, Scopes: scopes,
	}
}

// resolveRedirect is the redirect of one sign-in: the configured one, or
// else the caller's, which must be an HTTPS /auth/callback (plain HTTP only
// on localhost). The provider rejects unregistered ones either way; this
// only keeps obvious nonsense out of the flow.
func (c *Client) resolveRedirect(callers string) (string, error) {
	if c.cfg.RedirectURL != "" {
		return c.cfg.RedirectURL, nil
	}
	u, err := url.Parse(callers)
	if err != nil || u.Path != "/auth/callback" || u.Host == "" {
		return "", fmt.Errorf("no redirect URL is configured and %q is not an /auth/callback address", callers)
	}
	host := u.Hostname()
	if u.Scheme != "https" && !(u.Scheme == "http" && (host == "localhost" || host == "127.0.0.1")) {
		return "", fmt.Errorf("no redirect URL is configured and %q is not HTTPS", callers)
	}
	return u.String(), nil
}

func random() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Start begins a sign-in: it returns the URL to send the browser to and
// remembers the state the provider will bring back. callersRedirect is
// where the frontend says it is served, used when no redirect URL is
// configured.
func (c *Client) Start(ctx context.Context, callersRedirect string) (authURL string, err error) {
	redirect, err := c.resolveRedirect(callersRedirect)
	if err != nil {
		return "", err
	}
	p, err := c.discover(ctx)
	if err != nil {
		return "", err
	}
	state, err := random()
	if err != nil {
		return "", err
	}
	nonce, err := random()
	if err != nil {
		return "", err
	}
	verifier := oauth2.GenerateVerifier()

	c.mu.Lock()
	now := c.now()
	for s, p := range c.pending { // forget sign-ins that never came back
		if !p.expires.After(now) {
			delete(c.pending, s)
		}
	}
	c.pending[state] = pending{nonce: nonce, verifier: verifier, redirect: redirect, expires: now.Add(c.cfg.StateTTL)}
	c.mu.Unlock()

	conf := c.oauthConfig(p, redirect)
	return conf.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), nil
}

// Finish completes a sign-in with what the provider sent back: it
// exchanges the code, verifies the ID token against the state's nonce and
// reads the user's claims, from the ID token and the userinfo endpoint.
func (c *Client) Finish(ctx context.Context, state, code string) (Identity, error) {
	c.mu.Lock()
	p, ok := c.pending[state]
	delete(c.pending, state) // a state is good once
	c.mu.Unlock()
	if !ok || !p.expires.After(c.now()) {
		return Identity{}, ErrInvalidState
	}
	provider, err := c.discover(ctx)
	if err != nil {
		return Identity{}, err
	}
	conf := c.oauthConfig(provider, p.redirect)
	ctx = oidc.ClientContext(ctx, c.client)
	token, err := conf.Exchange(ctx, code, oauth2.VerifierOption(p.verifier))
	if err != nil {
		return Identity{}, fmt.Errorf("%w: code exchange: %v", ErrUnavailable, err)
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok || raw == "" {
		return Identity{}, fmt.Errorf("%w: the provider sent no ID token", ErrUnavailable)
	}
	idToken, err := provider.Verifier(&oidc.Config{ClientID: c.cfg.ClientID}).Verify(ctx, raw)
	if err != nil {
		return Identity{}, fmt.Errorf("invalid ID token: %w", err)
	}
	if idToken.Nonce != p.nonce {
		return Identity{}, errors.New("invalid ID token: nonce mismatch")
	}
	claims := make(map[string]any)
	if err := idToken.Claims(&claims); err != nil {
		return Identity{}, fmt.Errorf("invalid ID token: %w", err)
	}
	// The userinfo endpoint often carries what the ID token leaves out
	// (groups, email); it adds to the ID token's claims, never replaces.
	if info, err := provider.UserInfo(ctx, oauth2.StaticTokenSource(token)); err == nil {
		extra := make(map[string]any)
		if err := info.Claims(&extra); err == nil {
			for k, v := range extra {
				if _, set := claims[k]; !set {
					claims[k] = v
				}
			}
		}
	}

	id := Identity{
		Subject:     idToken.Subject,
		Username:    stringClaim(claims, c.cfg.UsernameClaim),
		DisplayName: stringClaim(claims, c.cfg.DisplayNameClaim),
		Email:       stringClaim(claims, c.cfg.EmailClaim),
		Groups:      stringsClaim(claims, c.cfg.GroupsClaim),
	}
	if id.Username == "" {
		id.Username = id.Email
	}
	if id.Username == "" {
		return Identity{}, fmt.Errorf("the ID token has no %q claim to use as a username", c.cfg.UsernameClaim)
	}
	return id, nil
}

func stringClaim(claims map[string]any, name string) string {
	v, _ := claims[name].(string)
	return strings.TrimSpace(v)
}

// stringsClaim reads a list claim; a single string is a list of one.
func stringsClaim(claims map[string]any, name string) []string {
	switch v := claims[name].(type) {
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	case []string:
		return v
	case string:
		if v = strings.TrimSpace(v); v != "" {
			return []string{v}
		}
	}
	return nil
}

// SameGroup compares a mapped group with one the provider sent: exactly,
// ignoring case, and ignoring a leading slash (Keycloak sends group paths).
func SameGroup(mapped, actual string) bool {
	norm := func(s string) string { return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(s), "/")) }
	return norm(mapped) == norm(actual)
}

// S256 is the PKCE challenge of a verifier; exported for tests.
func S256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
