package api

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/yusufceng/Kubeat/backend/internal/oidcauth"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

type fakeOIDC struct {
	identity oidcauth.Identity
	err      error
}

func (f *fakeOIDC) Issuer() string { return "https://sso.example.com/realms/corp" }

func (f *fakeOIDC) Start(context.Context) (string, error) {
	return "https://sso.example.com/realms/corp/auth?state=s1", f.err
}

func (f *fakeOIDC) Finish(_ context.Context, state, code string) (oidcauth.Identity, error) {
	if f.err != nil {
		return oidcauth.Identity{}, f.err
	}
	if state != "s1" || code != "c1" {
		return oidcauth.Identity{}, oidcauth.ErrInvalidState
	}
	return f.identity, nil
}

func TestOIDCSignIn(t *testing.T) {
	idp := &fakeOIDC{identity: oidcauth.Identity{Subject: "u-1", Username: "ayse", DisplayName: "Ayşe", Email: "ayse@example.com", Groups: []string{"/platform-admins"}}}
	srv, db := newTestAPIWith(t, func(o *Options) { o.OIDC, o.OIDCName = idp, "Corp SSO" })
	ctx := context.Background()

	var providers providersResponse
	if code := doAs(t, "", http.MethodGet, srv.URL+"/api/v1/auth/providers", "", &providers); code != http.StatusOK || providers.OIDC == nil || providers.OIDC.Name != "Corp SSO" {
		t.Fatalf("providers = %d %+v", code, providers)
	}
	var start map[string]string
	if code := doAs(t, "", http.MethodGet, srv.URL+"/api/v1/auth/oidc/start", "", &start); code != http.StatusOK || start["url"] == "" {
		t.Fatalf("start = %d %v", code, start)
	}

	// No group mapping yet: the provider knows her, Kubeat has no role for her.
	if code := doAs(t, "", http.MethodPost, srv.URL+"/api/v1/auth/oidc/callback", `{"state":"s1","code":"c1"}`, nil); code != http.StatusForbidden {
		t.Errorf("without a mapped group = %d, want 403", code)
	}
	if _, err := db.CreateLDAPGroupRole(ctx, store.LDAPGroupRole{GroupDN: "platform-admins", Role: store.RoleAdmin}); err != nil {
		t.Fatal(err)
	}
	var sess sessionResponse
	if code := doAs(t, "", http.MethodPost, srv.URL+"/api/v1/auth/oidc/callback", `{"state":"s1","code":"c1"}`, &sess); code != http.StatusOK || sess.Token == "" {
		t.Fatalf("callback = %d %+v", code, sess)
	}
	if sess.User.Username != "ayse" || sess.User.Role != store.RoleAdmin || sess.User.Source != store.UserOIDC || sess.User.DisplayName != "Ayşe" {
		t.Errorf("user = %+v", sess.User)
	}
	var me meResponse
	if code := doAs(t, sess.Token, http.MethodGet, srv.URL+"/api/v1/auth/me", "", &me); code != http.StatusOK || !me.Admin {
		t.Errorf("me = %d %+v", code, me)
	}
	// The administrator may disable her but not change what the provider decides.
	var problem errorBody
	if code := do(t, http.MethodPatch, srv.URL+"/api/v1/users/"+itoa(sess.User.ID), `{"role":"viewer"}`, &problem); code != http.StatusBadRequest {
		t.Errorf("role change on an OIDC user = %d %s, want 400", code, problem.Error)
	}

	// A bad state or a provider that is away.
	if code := doAs(t, "", http.MethodPost, srv.URL+"/api/v1/auth/oidc/callback", `{"state":"stale","code":"c1"}`, &problem); code != http.StatusUnauthorized {
		t.Errorf("stale state = %d, want 401", code)
	}
	idp.err = oidcauth.ErrUnavailable
	if code := doAs(t, "", http.MethodPost, srv.URL+"/api/v1/auth/oidc/callback", `{"state":"s1","code":"c1"}`, &problem); code != http.StatusBadGateway {
		t.Errorf("provider away = %d, want 502", code)
	}
	idp.err = nil

	// A local account of the same name is not the provider's to take.
	addUser(t, srv.URL, "mehmet", store.RoleViewer, "shop")
	idp.identity = oidcauth.Identity{Subject: "u-2", Username: "mehmet", Groups: []string{"platform-admins"}}
	if code := doAs(t, "", http.MethodPost, srv.URL+"/api/v1/auth/oidc/callback", `{"state":"s1","code":"c1"}`, &problem); code != http.StatusForbidden {
		t.Errorf("local name through OIDC = %d, want 403", code)
	}

	// Without a provider the endpoints do not exist.
	plain, _ := newTestAPI(t)
	if code := doAs(t, "", http.MethodGet, plain.URL+"/api/v1/auth/oidc/start", "", nil); code != http.StatusNotFound {
		t.Errorf("start without OIDC = %d, want 404", code)
	}
	_ = errors.New
}
