package oidcauth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// fakeProvider is an OpenID provider: discovery, JWKS, token and userinfo
// endpoints, with one user.
type fakeProvider struct {
	srv  *httptest.Server
	key  *rsa.PrivateKey
	code string // the code the token endpoint accepts
	// what the last token request carried
	gotRedirect, gotVerifier, gotCode string
	nonce                             string // put in the ID token; set from the auth request
	groupsInUserInfo                  bool
	audience                          string
}

func newFakeProvider(t *testing.T) *fakeProvider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeProvider{key: key, code: "good-code", audience: "kubeat"}
	mux := http.NewServeMux()
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	issuer := f.srv.URL

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": issuer, "authorization_endpoint": issuer + "/auth", "token_endpoint": issuer + "/token",
			"jwks_uri": issuer + "/keys", "userinfo_endpoint": issuer + "/userinfo",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": "k1", "use": "sig", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = r.ParseForm()
		f.gotCode, f.gotRedirect, f.gotVerifier = r.Form.Get("code"), r.Form.Get("redirect_uri"), r.Form.Get("code_verifier")
		if f.gotCode != f.code {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
			return
		}
		claims := map[string]any{
			"iss": issuer, "aud": f.audience, "sub": "u-1", "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
			"nonce": f.nonce, "preferred_username": "ayse", "name": "Ayşe Yönetici", "email": "ayse@example.com",
		}
		if !f.groupsInUserInfo {
			claims["groups"] = []string{"/platform-admins", "/shop-team"}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "at-1", "token_type": "Bearer", "expires_in": 3600, "id_token": f.sign(claims),
		})
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer at-1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		info := map[string]any{"sub": "u-1", "email": "ayse@example.com"}
		if f.groupsInUserInfo {
			info["groups"] = []string{"/platform-admins"}
		}
		_ = json.NewEncoder(w).Encode(info)
	})
	return f
}

// sign makes an RS256 JWT of the claims.
func (f *fakeProvider) sign(claims map[string]any) string {
	enc := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	signing := enc(map[string]string{"alg": "RS256", "kid": "k1", "typ": "JWT"}) + "." + enc(claims)
	sum := sha256.Sum256([]byte(signing))
	sig, _ := rsa.SignPKCS1v15(rand.Reader, f.key, crypto.SHA256, sum[:])
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func newClient(t *testing.T, f *fakeProvider) *Client {
	t.Helper()
	c, err := New(Config{Issuer: f.srv.URL, ClientID: "kubeat", ClientSecret: "s", RedirectURL: "https://kubeat.example/auth/callback",
		Scopes: []string{"profile", "email", "groups"}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// begin starts a sign-in and returns the state and the nonce the provider
// will see, as the browser would carry them.
func begin(t *testing.T, c *Client, f *fakeProvider) string {
	t.Helper()
	authURL, err := c.Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if !strings.HasPrefix(authURL, f.srv.URL+"/auth?") || q.Get("client_id") != "kubeat" || q.Get("code_challenge_method") != "S256" ||
		q.Get("redirect_uri") != "https://kubeat.example/auth/callback" || !strings.Contains(q.Get("scope"), "openid") {
		t.Fatalf("auth URL = %s", authURL)
	}
	f.nonce = q.Get("nonce")
	return q.Get("state")
}

func TestSignInThroughTheProvider(t *testing.T) {
	f := newFakeProvider(t)
	c := newClient(t, f)
	state := begin(t, c, f)

	id, err := c.Finish(context.Background(), state, "good-code")
	if err != nil {
		t.Fatal(err)
	}
	if id.Username != "ayse" || id.DisplayName != "Ayşe Yönetici" || id.Email != "ayse@example.com" || id.Subject != "u-1" ||
		len(id.Groups) != 2 || id.Groups[0] != "/platform-admins" {
		t.Errorf("identity = %+v", id)
	}
	if f.gotRedirect != "https://kubeat.example/auth/callback" || f.gotVerifier == "" {
		t.Errorf("token request: redirect=%q verifier=%q", f.gotRedirect, f.gotVerifier)
	}
	// A state is good once.
	if _, err := c.Finish(context.Background(), state, "good-code"); !errors.Is(err, ErrInvalidState) {
		t.Errorf("second use of the state: err = %v", err)
	}
}

func TestGroupsFromUserInfo(t *testing.T) {
	f := newFakeProvider(t)
	f.groupsInUserInfo = true
	c := newClient(t, f)
	state := begin(t, c, f)
	id, err := c.Finish(context.Background(), state, "good-code")
	if err != nil {
		t.Fatal(err)
	}
	if len(id.Groups) != 1 || id.Groups[0] != "/platform-admins" {
		t.Errorf("groups = %v, want the userinfo groups", id.Groups)
	}
}

func TestRefusals(t *testing.T) {
	f := newFakeProvider(t)
	c := newClient(t, f)

	if _, err := c.Finish(context.Background(), "never-issued", "good-code"); !errors.Is(err, ErrInvalidState) {
		t.Errorf("unknown state: err = %v", err)
	}
	state := begin(t, c, f)
	if _, err := c.Finish(context.Background(), state, "wrong-code"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("wrong code: err = %v", err)
	}
	// The ID token must be for us and carry our nonce.
	state = begin(t, c, f)
	f.nonce = "someone-else's"
	if _, err := c.Finish(context.Background(), state, "good-code"); err == nil || !strings.Contains(err.Error(), "nonce") {
		t.Errorf("nonce mismatch: err = %v", err)
	}
	state = begin(t, c, f)
	f.audience = "another-client"
	if _, err := c.Finish(context.Background(), state, "good-code"); err == nil || !strings.Contains(err.Error(), "invalid ID token") {
		t.Errorf("wrong audience: err = %v", err)
	}
	// An expired state is forgotten.
	f.audience = "kubeat"
	state = begin(t, c, f)
	c.now = func() time.Time { return time.Now().Add(time.Hour) }
	if _, err := c.Finish(context.Background(), state, "good-code"); !errors.Is(err, ErrInvalidState) {
		t.Errorf("expired state: err = %v", err)
	}
}

func TestProviderAwayAtStart(t *testing.T) {
	c, err := New(Config{Issuer: "http://127.0.0.1:1", ClientID: "kubeat", RedirectURL: "https://x/auth/callback", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Start(context.Background(), ""); !errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v, want ErrUnavailable", err)
	}
	if c, err := New(Config{}); c != nil || err != nil {
		t.Errorf("no issuer: %v %v, want nil client", c, err)
	}
	if _, err := New(Config{Issuer: "http://x"}); err == nil {
		t.Error("issuer without client id was accepted")
	}
}

func TestSameGroup(t *testing.T) {
	for mapped, actual := range map[string]string{"/platform-admins": "platform-admins", "Platform-Admins": "/platform-admins", "abc-123": " abc-123 "} {
		if !SameGroup(mapped, actual) {
			t.Errorf("SameGroup(%q, %q) = false", mapped, actual)
		}
	}
	if SameGroup("platform-admins", "platform-admins-2") {
		t.Error("different groups matched")
	}
}

func TestRedirectFromTheCaller(t *testing.T) {
	f := newFakeProvider(t)
	c, err := New(Config{Issuer: f.srv.URL, ClientID: "kubeat", Scopes: []string{"profile"}})
	if err != nil {
		t.Fatal(err)
	}
	// Without a configured redirect, the caller's /auth/callback is used
	// when it is on this machine: the frontend run with next dev.
	authURL, err := c.Start(context.Background(), "http://localhost:3000/auth/callback")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(authURL)
	if got := u.Query().Get("redirect_uri"); got != "http://localhost:3000/auth/callback" {
		t.Errorf("redirect_uri = %q", got)
	}
	// ...and the exchange sends the same one back.
	f.nonce = u.Query().Get("nonce")
	if _, err := c.Finish(context.Background(), u.Query().Get("state"), "good-code"); err != nil {
		t.Fatal(err)
	}
	if f.gotRedirect != "http://localhost:3000/auth/callback" {
		t.Errorf("token request redirect = %q", f.gotRedirect)
	}
	for _, local := range []string{"http://127.0.0.1:3000/auth/callback", "http://[::1]:3000/auth/callback", "https://LOCALHOST/auth/callback"} {
		if _, err := c.Start(context.Background(), local); err != nil {
			t.Errorf("caller redirect %q: %v", local, err)
		}
	}
	// Anywhere else the code would go wherever an unauthenticated request
	// said: to another host, or to the pod's own address behind a Route.
	for _, bad := range []string{
		"", "https://attacker.apps.example/auth/callback", "https://kubeat.apps.example/auth/callback",
		"https://0.0.0.0:3000/auth/callback", "https://10.128.0.12:3000/auth/callback", "http://localhost.attacker.example/auth/callback",
		"http://localhost:3000/elsewhere", "http://localhost:3000/auth/callback/../admin", "ftp://localhost/auth/callback", "//localhost/auth/callback",
	} {
		_, err := c.Start(context.Background(), bad)
		if err == nil || !strings.Contains(err.Error(), "set UPTIME_OIDC_REDIRECT_URL or UPTIME_PUBLIC_URL") {
			t.Errorf("caller redirect %q: err = %v, want a refusal that says what to set", bad, err)
		}
	}
	// A configured redirect wins over whatever the caller says.
	fixed := newClient(t, f)
	authURL, err = fixed.Start(context.Background(), "https://evil.example/auth/callback")
	if err != nil {
		t.Fatal(err)
	}
	u, _ = url.Parse(authURL)
	if got := u.Query().Get("redirect_uri"); got != "https://kubeat.example/auth/callback" {
		t.Errorf("configured redirect was not used: %q", got)
	}
}
