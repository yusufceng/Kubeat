package api

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/license"
	"github.com/yusufceng/Kubeat/backend/internal/oidcauth"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// fullLicense turns every feature on.
type fullLicense struct{}

func (fullLicense) Allows(license.Feature) bool { return true }
func (fullLicense) Status() license.Status {
	return license.Status{State: license.Active, Customer: "Test", Features: license.Features, ExpiresAt: time.Now().AddDate(1, 0, 0)}
}

// switchLicense is a license a test can end.
type switchLicense struct {
	mu    sync.Mutex
	ended bool
}

func (l *switchLicense) end() { l.mu.Lock(); l.ended = true; l.mu.Unlock() }

func (l *switchLicense) Allows(license.Feature) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return !l.ended
}
func (l *switchLicense) Status() license.Status {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := license.Status{State: license.Active, Customer: "Acme", ID: "KL-1", Features: license.Features, ExpiresAt: time.Now().AddDate(1, 0, 0)}
	if l.ended {
		s.State = license.Expired
	}
	return s
}

// doBody is do, returning the body of any answer.
func doBody(t *testing.T, method, url, body string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func wantLicenseRefusal(t *testing.T, what string, code int, body map[string]any, feature license.Feature) {
	t.Helper()
	if code != http.StatusPaymentRequired || body["feature"] != string(feature) {
		t.Errorf("%s: status = %d, body = %v; want 402 for %s", what, code, body, feature)
	}
}

func directory() *fakeLDAP {
	return &fakeLDAP{users: map[string]struct {
		password string
		groups   []string
	}{"ayse": {"s3cret", []string{"cn=admins,dc=example,dc=com"}}}}
}

// Without a license, what it turns on is refused, and what Kubeat does
// without one goes on.
func TestCommunityEdition(t *testing.T) {
	idp := &fakeOIDC{identity: oidcauth.Identity{Subject: "u-1", Username: "ayse"}}
	srv, _ := newTestAPIWith(t, func(o *Options) {
		o.License = nil
		o.LDAP, o.LDAPDefaultRole, o.LDAPDefaultNamespaces = directory(), store.RoleViewer, []string{"*"}
		o.OIDC, o.OIDCName = idp, "SSO"
	})
	base := srv.URL + "/api/v1"

	var lic licenseResponse
	if do(t, "GET", base+"/license", "", &lic); lic.State != license.Community || len(lic.Features) != 0 || lic.CommunityStatusPages != 1 {
		t.Errorf("license = %+v", lic)
	}

	// SSO is configured but off.
	var providers providersResponse
	doAs(t, "", "GET", base+"/auth/providers", "", &providers)
	if providers.LDAP || providers.OIDC != nil {
		t.Errorf("providers = %+v", providers)
	}
	var ldap ldapStatusResponse
	do(t, "GET", base+"/ldap", "", &ldap)
	if ldap.Enabled || ldap.Problem != ssoUnlicensed || ldap.URL == "" {
		t.Errorf("ldap status = %+v", ldap)
	}
	var oidc oidcStatusResponse
	do(t, "GET", base+"/oidc", "", &oidc)
	if oidc.Enabled || oidc.Problem != ssoUnlicensed {
		t.Errorf("oidc status = %+v", oidc)
	}
	if _, code := signIn(t, srv.URL, "ayse", "s3cret"); code != http.StatusUnauthorized {
		t.Errorf("directory sign-in: status = %d, want 401", code)
	}
	if code := doAs(t, "", "GET", base+"/auth/oidc/start", "", nil); code != http.StatusNotFound {
		t.Errorf("oidc start: status = %d, want 404", code)
	}
	if code := doAs(t, "", "POST", base+"/auth/oidc/callback", `{"state":"s1","code":"c1"}`, nil); code != http.StatusNotFound {
		t.Errorf("oidc callback: status = %d, want 404", code)
	}
	code, body := doBody(t, "POST", base+"/ldap/groups", `{"group_dn":"cn=admins,dc=example,dc=com","role":"admin"}`)
	wantLicenseRefusal(t, "group mapping", code, body, license.SSO)

	// The audit log is written, but not shown.
	code, body = doBody(t, "GET", base+"/audit", "")
	wantLicenseRefusal(t, "audit log", code, body, license.Audit)

	// Users can have any role, but not be limited to namespaces.
	code, body = doBody(t, "POST", base+"/users", `{"username":"ali","password":"parola-123","role":"editor","namespaces":["shop"]}`)
	wantLicenseRefusal(t, "scoped user", code, body, license.ScopedAccess)
	addUser(t, srv.URL, "veli", store.RoleEditor, "*")
	addUser(t, srv.URL, "root", store.RoleAdmin)

	// One status page.
	if code := do(t, "POST", base+"/status-pages", `{"slug":"one","title":"One"}`, nil); code != http.StatusCreated {
		t.Fatalf("first status page: status = %d", code)
	}
	code, body = doBody(t, "POST", base+"/status-pages", `{"slug":"two","title":"Two"}`)
	wantLicenseRefusal(t, "second status page", code, body, license.StatusPages)

	// Monitoring is not touched.
	if code := do(t, "POST", base+"/monitors", monitorJSON("web", "http", "http://web/", ""), nil); code != http.StatusCreated {
		t.Errorf("monitor: status = %d", code)
	}
}

// When a license ends, what it turned on is refused from then on; what it
// restricted stays restricted, and what was made under it stays.
func TestLicenseEndKeepsRestrictions(t *testing.T) {
	lic := &switchLicense{}
	srv, _ := newTestAPIWith(t, func(o *Options) {
		o.License = lic
		o.LDAP, o.LDAPDefaultRole, o.LDAPDefaultNamespaces = directory(), store.RoleViewer, []string{"*"}
	})
	base := srv.URL + "/api/v1"
	ali := addUser(t, srv.URL, "ali", store.RoleEditor, "shop", "billing")
	if code := do(t, "POST", base+"/ldap/groups", `{"group_dn":"cn=ops,dc=example,dc=com","role":"viewer","namespaces":["ops"]}`, nil); code != http.StatusCreated {
		t.Fatalf("scoped group mapping: status = %d", code)
	}
	for _, slug := range []string{"one", "two"} {
		if code := do(t, "POST", base+"/status-pages", `{"slug":"`+slug+`","title":"`+slug+`"}`, nil); code != http.StatusCreated {
			t.Fatalf("status page %s: status = %d", slug, code)
		}
	}
	if _, code := signIn(t, srv.URL, "ayse", "s3cret"); code != http.StatusOK {
		t.Fatalf("directory sign-in under the license: status = %d", code)
	}

	lic.end()
	var status licenseResponse
	if do(t, "GET", base+"/license", "", &status); status.State != license.Expired || len(status.Features) != 0 || status.Customer != "Acme" {
		t.Errorf("ended license = %+v", status)
	}
	if _, code := signIn(t, srv.URL, "ayse", "s3cret"); code != http.StatusUnauthorized {
		t.Errorf("directory sign-in after the end: status = %d, want 401", code)
	}

	// Ali stays limited; the limit can be narrowed or lifted, not moved.
	var u userResponse
	if code := do(t, "PATCH", base+"/users/"+itoa(ali.ID), `{"display_name":"Ali"}`, &u); code != http.StatusOK || len(u.Namespaces) != 2 {
		t.Errorf("rename: status = %d, user = %+v", code, u)
	}
	if code := do(t, "PATCH", base+"/users/"+itoa(ali.ID), `{"namespaces":["shop"]}`, &u); code != http.StatusOK || !slices.Equal(u.Namespaces, []string{"shop"}) {
		t.Errorf("narrowing: status = %d, user = %+v", code, u)
	}
	if code := do(t, "PATCH", base+"/users/"+itoa(ali.ID), `{"role":"viewer"}`, &u); code != http.StatusOK || !slices.Equal(u.Namespaces, []string{"shop"}) {
		t.Errorf("new role, same namespaces: status = %d, user = %+v", code, u)
	}
	code, body := doBody(t, "PATCH", base+"/users/"+itoa(ali.ID), `{"namespaces":["billing"]}`)
	wantLicenseRefusal(t, "moving the limit", code, body, license.ScopedAccess)
	code, body = doBody(t, "PATCH", base+"/users/"+itoa(ali.ID), `{"namespaces":["shop","ops"]}`)
	wantLicenseRefusal(t, "widening the limit", code, body, license.ScopedAccess)
	if code := do(t, "PATCH", base+"/users/"+itoa(ali.ID), `{"namespaces":["*"]}`, &u); code != http.StatusOK || !slices.Equal(u.Namespaces, []string{"*"}) {
		t.Errorf("lifting the limit: status = %d, user = %+v", code, u)
	}

	// Both status pages stay and can be edited; a third cannot be made.
	var pages []statusPageResponse
	if do(t, "GET", base+"/status-pages", "", &pages); len(pages) != 2 {
		t.Fatalf("status pages = %d, want 2", len(pages))
	}
	if code := do(t, "PATCH", base+"/status-pages/"+itoa(pages[1].ID), `{"title":"Renamed"}`, nil); code != http.StatusOK {
		t.Errorf("editing a page made under the license: status = %d", code)
	}
	code, body = doBody(t, "POST", base+"/status-pages", `{"slug":"three","title":"Three"}`)
	wantLicenseRefusal(t, "third status page", code, body, license.StatusPages)

	// Group mappings can be removed, not changed.
	var groups []groupRoleResponse
	do(t, "GET", base+"/ldap/groups", "", &groups)
	code, body = doBody(t, "PATCH", base+"/ldap/groups/"+itoa(groups[0].ID), `{"role":"admin"}`)
	wantLicenseRefusal(t, "changing a group mapping", code, body, license.SSO)
	if code := do(t, "DELETE", base+"/ldap/groups/"+itoa(groups[0].ID), "", nil); code != http.StatusNoContent {
		t.Errorf("removing a group mapping: status = %d", code)
	}
}

// Only administrators see whom the license is for.
func TestLicenseDetailsAreTheAdmins(t *testing.T) {
	srv, _ := newTestAPIWith(t, func(o *Options) { o.License = &switchLicense{} })
	addUser(t, srv.URL, "viewer", store.RoleViewer, "*")
	token, _ := signIn(t, srv.URL, "viewer", "parola-123")
	var lic licenseResponse
	if code := doAs(t, token, "GET", srv.URL+"/api/v1/license", "", &lic); code != http.StatusOK ||
		lic.State != license.Active || len(lic.Features) != len(license.Features) || lic.Customer != "" || lic.ID != "" {
		t.Errorf("viewer: status = %d, license = %+v", code, lic)
	}
	if code := doAs(t, "", "GET", srv.URL+"/api/v1/license", "", nil); code != http.StatusUnauthorized {
		t.Errorf("anonymous: status = %d, want 401", code)
	}
}
