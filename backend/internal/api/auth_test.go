package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/ldapauth"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// doAs sends a request with the given bearer token ("" for none).
func doAs(t *testing.T, token, method, url, body string, out any) int {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil && resp.StatusCode < 300 {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("%s %s: decode: %v", method, url, err)
		}
	}
	return resp.StatusCode
}

// signIn returns the session token of a user, or "" when refused.
func signIn(t *testing.T, base, username, password string) (string, int) {
	t.Helper()
	var sess sessionResponse
	code := doAs(t, "", "POST", base+"/api/v1/auth/login", `{"username":"`+username+`","password":"`+password+`"}`, &sess)
	return sess.Token, code
}

// addUser creates a local user with the service token.
func addUser(t *testing.T, base, username string, role store.Role, namespaces ...string) userResponse {
	t.Helper()
	ns, _ := json.Marshal(namespaces)
	var u userResponse
	body := `{"username":"` + username + `","password":"parola-123","role":"` + string(role) + `","namespaces":` + string(ns) + `}`
	if code := do(t, "POST", base+"/api/v1/users", body, &u); code != http.StatusCreated {
		t.Fatalf("create user %s: status = %d", username, code)
	}
	return u
}

func TestSetupAndSessions(t *testing.T) {
	srv, _ := newTestAPI(t)
	base := srv.URL

	var providers providersResponse
	if code := doAs(t, "", "GET", base+"/api/v1/auth/providers", "", &providers); code != http.StatusOK || !providers.SetupNeeded || providers.LDAP {
		t.Fatalf("providers = %+v, %d", providers, code)
	}
	for name, body := range map[string]string{
		"short password": `{"username":"admin","password":"kisa"}`,
		"bad username":   `{"username":"ad min","password":"parola-123"}`,
	} {
		if code := doAs(t, "", "POST", base+"/api/v1/auth/setup", body, nil); code != http.StatusBadRequest {
			t.Errorf("setup with %s: status = %d", name, code)
		}
	}
	var first sessionResponse
	if code := doAs(t, "", "POST", base+"/api/v1/auth/setup", `{"username":"yusuf","display_name":"Yusuf","password":"parola-123"}`, &first); code != http.StatusOK {
		t.Fatalf("setup: status = %d", code)
	}
	if first.Token == "" || first.User.Role != store.RoleAdmin || first.User.Username != "yusuf" || first.ExpiresAt.Before(time.Now()) {
		t.Errorf("first session = %+v", first)
	}
	// Only the first account comes from setup.
	if code := doAs(t, "", "POST", base+"/api/v1/auth/setup", `{"username":"other","password":"parola-123"}`, nil); code != http.StatusConflict {
		t.Errorf("second setup: status = %d, want 409", code)
	}
	doAs(t, "", "GET", base+"/api/v1/auth/providers", "", &providers)
	if providers.SetupNeeded {
		t.Error("setup still needed after the first account")
	}

	// The session works, says who it is, and ends with logout.
	var me meResponse
	if code := doAs(t, first.Token, "GET", base+"/api/v1/auth/me", "", &me); code != http.StatusOK || me.Username != "yusuf" || !me.Admin || !me.CanEdit || me.Service {
		t.Errorf("me = %+v, %d", me, code)
	}
	if code := doAs(t, first.Token, "GET", base+"/api/v1/monitors", "", nil); code != http.StatusOK {
		t.Errorf("list with a session: status = %d", code)
	}
	if code := doAs(t, first.Token, "POST", base+"/api/v1/auth/logout", "", nil); code != http.StatusNoContent {
		t.Errorf("logout: status = %d", code)
	}
	if code := doAs(t, first.Token, "GET", base+"/api/v1/monitors", "", nil); code != http.StatusUnauthorized {
		t.Errorf("after logout: status = %d, want 401", code)
	}
	// The service token says so about itself.
	doAs(t, testToken, "GET", base+"/api/v1/auth/me", "", &me)
	if !me.Service || !me.Admin {
		t.Errorf("service me = %+v", me)
	}

	// Signing in: right, wrong, and too often wrong.
	if token, code := signIn(t, base, "Yusuf", "parola-123"); code != http.StatusOK || token == "" {
		t.Errorf("sign in (case of the name differs): %d", code)
	}
	if _, code := signIn(t, base, "yusuf", "yanlis-parola"); code != http.StatusUnauthorized {
		t.Errorf("wrong password: status = %d", code)
	}
	if _, code := signIn(t, base, "nobody", "parola-123"); code != http.StatusUnauthorized {
		t.Errorf("unknown user: status = %d", code)
	}
	for i := 0; i < 2; i++ {
		signIn(t, base, "yusuf", "yanlis")
	}
	if _, code := signIn(t, base, "yusuf", "parola-123"); code != http.StatusTooManyRequests {
		t.Errorf("after three failures even the right password: status = %d, want 429", code)
	}
	if _, code := signIn(t, base, "nobody-else", "parola-123"); code != http.StatusUnauthorized {
		t.Errorf("another name is not locked: status = %d", code)
	}

	// Changing the password ends the other sessions, not this one.
	token, _ := signIn(t, base, "YUSUF", "parola-123") // locked: fails
	_ = token
	srv2, _ := newTestAPI(t)
	doAs(t, "", "POST", srv2.URL+"/api/v1/auth/setup", `{"username":"yusuf","password":"parola-123"}`, &first)
	other, _ := signIn(t, srv2.URL, "yusuf", "parola-123")
	if code := doAs(t, first.Token, "POST", srv2.URL+"/api/v1/auth/password", `{"current_password":"yanlis","new_password":"yeni-parola-1"}`, nil); code != http.StatusBadRequest {
		t.Errorf("password change with a wrong current password: status = %d", code)
	}
	if code := doAs(t, first.Token, "POST", srv2.URL+"/api/v1/auth/password", `{"current_password":"parola-123","new_password":"yeni-parola-1"}`, nil); code != http.StatusNoContent {
		t.Errorf("password change: status = %d", code)
	}
	if code := doAs(t, first.Token, "GET", srv2.URL+"/api/v1/auth/me", "", nil); code != http.StatusOK {
		t.Errorf("the session that changed the password: status = %d", code)
	}
	if code := doAs(t, other, "GET", srv2.URL+"/api/v1/auth/me", "", nil); code != http.StatusUnauthorized {
		t.Errorf("another session after the change: status = %d, want 401", code)
	}
	if _, code := signIn(t, srv2.URL, "yusuf", "yeni-parola-1"); code != http.StatusOK {
		t.Errorf("sign in with the new password: status = %d", code)
	}
}

func TestUserManagement(t *testing.T) {
	srv, db := newTestAPIWith(t, func(o *Options) { o.MaxResults = 100 })
	base := srv.URL
	ctx := context.Background()
	doAs(t, "", "POST", base+"/api/v1/auth/setup", `{"username":"admin","password":"parola-123"}`, nil)
	adminTok, _ := signIn(t, base, "admin", "parola-123")

	editor := addUser(t, base, "ayse", store.RoleEditor, "shop", "billing")
	viewer := addUser(t, base, "mehmet", store.RoleViewer, "*")
	if strings.Join(editor.Namespaces, ",") != "billing,shop" || viewer.Namespaces[0] != "*" || editor.Source != store.UserLocal {
		t.Errorf("created = %+v, %+v", editor, viewer)
	}
	for name, body := range map[string]string{
		"taken name":     `{"username":"AYSE","password":"parola-123","role":"viewer","namespaces":["x"]}`,
		"no namespaces":  `{"username":"x","password":"parola-123","role":"viewer"}`,
		"bad role":       `{"username":"x","password":"parola-123","role":"owner"}`,
		"short password": `{"username":"x","password":"kisa","role":"admin"}`,
		"bad namespace":  `{"username":"x","password":"parola-123","role":"viewer","namespaces":["a/b"]}`,
	} {
		code := do(t, "POST", base+"/api/v1/users", body, nil)
		if code != http.StatusBadRequest && code != http.StatusConflict {
			t.Errorf("%s: status = %d", name, code)
		}
	}
	var list []userResponse
	if do(t, "GET", base+"/api/v1/users", "", &list); len(list) != 3 {
		t.Errorf("users = %+v", list)
	}

	// Only administrators manage accounts; a viewer sees nothing of them.
	editorTok, _ := signIn(t, base, "ayse", "parola-123")
	for _, target := range []string{"GET /api/v1/users", "POST /api/v1/users", "GET /api/v1/audit", "GET /api/v1/ldap/groups"} {
		method, path, _ := strings.Cut(target, " ")
		if code := doAs(t, editorTok, method, base+path, `{}`, nil); code != http.StatusForbidden {
			t.Errorf("editor %s: status = %d, want 403", target, code)
		}
	}

	// Changing a role, disabling, resetting a password.
	var updated userResponse
	url := base + "/api/v1/users/" + itoa(editor.ID)
	if code := doAs(t, adminTok, "PATCH", url, `{"role":"viewer","namespaces":["shop"],"display_name":"Ayşe"}`, &updated); code != http.StatusOK ||
		updated.Role != store.RoleViewer || strings.Join(updated.Namespaces, ",") != "shop" || updated.DisplayName != "Ayşe" {
		t.Errorf("update: status = %d, user = %+v", code, updated)
	}
	if code := doAs(t, adminTok, "PATCH", url, `{"enabled":false}`, nil); code != http.StatusOK {
		t.Errorf("disable: status = %d", code)
	}
	if code := doAs(t, editorTok, "GET", base+"/api/v1/auth/me", "", nil); code != http.StatusUnauthorized {
		t.Errorf("a disabled user's session: status = %d, want 401", code)
	}
	if _, code := signIn(t, base, "ayse", "parola-123"); code != http.StatusForbidden {
		t.Errorf("a disabled user signing in: status = %d, want 403", code)
	}
	doAs(t, adminTok, "PATCH", url, `{"enabled":true,"password":"sifirlandi-1"}`, nil)
	if _, code := signIn(t, base, "ayse", "sifirlandi-1"); code != http.StatusOK {
		t.Errorf("sign in after a reset: status = %d", code)
	}
	// An administrator can neither remove nor demote themselves.
	var me meResponse
	doAs(t, adminTok, "GET", base+"/api/v1/auth/me", "", &me)
	self := base + "/api/v1/users/" + itoa(me.ID)
	if code := doAs(t, adminTok, "PATCH", self, `{"role":"viewer","namespaces":["*"]}`, nil); code != http.StatusBadRequest {
		t.Errorf("demoting oneself: status = %d", code)
	}
	if code := doAs(t, adminTok, "DELETE", self, "", nil); code != http.StatusBadRequest {
		t.Errorf("deleting oneself: status = %d", code)
	}
	if code := doAs(t, adminTok, "DELETE", base+"/api/v1/users/"+itoa(viewer.ID), "", nil); code != http.StatusNoContent {
		t.Errorf("delete: status = %d", code)
	}
	if _, err := db.GetUser(ctx, viewer.ID); !errors.Is(err, store.ErrNotFound) {
		t.Error("the deleted user is still there")
	}

	// Everything was written down, failed sign-ins too.
	signIn(t, base, "mehmet", "yanlis")
	var audit []auditResponse
	doAs(t, adminTok, "GET", base+"/api/v1/audit?limit=50", "", &audit)
	actions := map[string]bool{}
	for _, e := range audit {
		actions[e.Action+" "+e.ObjectType] = true
	}
	for _, want := range []string{"setup user", "login user", "create user", "update user", "delete user", "login_failed "} {
		if !actions[want] {
			t.Errorf("audit lacks %q; has %v", want, actions)
		}
	}
}

func TestPermissions(t *testing.T) {
	srv, db := newTestAPI(t)
	base := srv.URL
	ctx := context.Background()
	doAs(t, "", "POST", base+"/api/v1/auth/setup", `{"username":"admin","password":"parola-123"}`, nil)
	addUser(t, base, "editor", store.RoleEditor, "shop")
	addUser(t, base, "viewer", store.RoleViewer, "billing")
	addUser(t, base, "seer", store.RoleViewer, "*")
	editor, _ := signIn(t, base, "editor", "parola-123")
	viewer, _ := signIn(t, base, "viewer", "parola-123")
	seer, _ := signIn(t, base, "seer", "parola-123")

	// Monitors in three places, made with the service token.
	mk := func(name, ns string) int64 {
		var m monitorResponse
		if code := do(t, "POST", base+"/api/v1/monitors", monitorJSON(name, "tcp", "x:1", `"namespace":"`+ns+`"`), &m); code != http.StatusCreated {
			t.Fatalf("create %s: status = %d", name, code)
		}
		return m.ID
	}
	shop, billing, loose := mk("shop-db", "shop"), mk("billing-db", "billing"), mk("loose", "")
	db.OpenIncident(ctx, store.Incident{MonitorID: shop, StartedAt: time.Now(), Cause: "x"})
	db.OpenIncident(ctx, store.Incident{MonitorID: billing, StartedAt: time.Now(), Cause: "x"})

	names := func(token string) string {
		var list []monitorResponse
		doAs(t, token, "GET", base+"/api/v1/monitors", "", &list)
		var out []string
		for _, m := range list {
			out = append(out, m.Name)
		}
		return strings.Join(out, ",")
	}
	if got := names(editor); got != "shop-db" {
		t.Errorf("editor sees %q", got)
	}
	if got := names(viewer); got != "billing-db" {
		t.Errorf("viewer sees %q", got)
	}
	if got := names(seer); got != "shop-db,billing-db,loose" {
		t.Errorf("seer sees %q", got)
	}
	// What one may not see does not exist.
	for name, tc := range map[string]struct {
		token string
		id    int64
		want  int
	}{
		"editor, own":      {editor, shop, http.StatusOK},
		"editor, other":    {editor, billing, http.StatusNotFound},
		"editor, no group": {editor, loose, http.StatusNotFound},
		"viewer, own":      {viewer, billing, http.StatusOK},
		"viewer, other":    {viewer, shop, http.StatusNotFound},
		"seer, no group":   {seer, loose, http.StatusOK},
	} {
		for _, path := range []string{"", "/results?limit=1", "/uptime?windows=1h", "/workload"} {
			if code := doAs(t, tc.token, "GET", base+"/api/v1/monitors/"+itoa(tc.id)+path, "", nil); code != tc.want {
				t.Errorf("%s GET monitor%s: status = %d, want %d", name, path, code, tc.want)
			}
		}
	}
	var incidents []incidentResponse
	doAs(t, viewer, "GET", base+"/api/v1/incidents", "", &incidents)
	if len(incidents) != 1 || incidents[0].MonitorID != billing {
		t.Errorf("viewer's incidents = %+v", incidents)
	}

	// Changing: viewers never, editors within their namespaces.
	type attempt struct {
		token, method, path, body string
		want                      int
	}
	attempts := []attempt{
		{viewer, "POST", "/monitors", monitorJSON("v", "tcp", "x:1", `"namespace":"billing"`), http.StatusForbidden},
		{viewer, "PATCH", "/monitors/" + itoa(billing), `{"enabled":false}`, http.StatusForbidden},
		{viewer, "DELETE", "/monitors/" + itoa(billing), "", http.StatusForbidden},
		{seer, "PATCH", "/monitors/" + itoa(loose), `{"enabled":false}`, http.StatusForbidden},
		{editor, "POST", "/monitors", monitorJSON("e1", "tcp", "x:1", `"namespace":"shop"`), http.StatusCreated},
		{editor, "POST", "/monitors", monitorJSON("e2", "tcp", "x:1", `"namespace":"billing"`), http.StatusForbidden},
		{editor, "POST", "/monitors", monitorJSON("e3", "tcp", "x:1", ""), http.StatusForbidden},
		{editor, "PATCH", "/monitors/" + itoa(shop), `{"description":"ok"}`, http.StatusOK},
		{editor, "PATCH", "/monitors/" + itoa(shop), `{"namespace":"billing"}`, http.StatusForbidden},
		{editor, "PATCH", "/monitors/" + itoa(billing), `{"description":"x"}`, http.StatusNotFound},
		{editor, "DELETE", "/monitors/" + itoa(billing), "", http.StatusNotFound},
		{editor, "POST", "/monitors/" + itoa(shop) + "/clone", `{"name":"shop-db-2"}`, http.StatusCreated},
		{editor, "POST", "/monitors/" + itoa(billing) + "/clone", `{"name":"x"}`, http.StatusNotFound},
		// Channels: only within the editor's namespaces, never without a scope.
		{editor, "POST", "/channels", `{"name":"shop-hook","type":"webhook","target":"https://h/1","namespaces":["shop"]}`, http.StatusCreated},
		{editor, "POST", "/channels", `{"name":"all-hook","type":"webhook","target":"https://h/2"}`, http.StatusForbidden},
		{editor, "POST", "/channels", `{"name":"billing-hook","type":"webhook","target":"https://h/3","namespaces":["billing"]}`, http.StatusForbidden},
		{viewer, "POST", "/channels", `{"name":"v","type":"webhook","target":"https://h/4","namespaces":["billing"]}`, http.StatusForbidden},
		// Maintenance: the scope decides.
		{editor, "POST", "/maintenance", `{"name":"shop","duration_seconds":60,"namespace":"shop"}`, http.StatusCreated},
		{editor, "POST", "/maintenance", `{"name":"all","duration_seconds":60}`, http.StatusForbidden},
		{editor, "POST", "/maintenance", `{"name":"b","duration_seconds":60,"monitor_id":` + itoa(billing) + `}`, http.StatusForbidden},
		{viewer, "POST", "/maintenance", `{"name":"v","duration_seconds":60,"namespace":"billing"}`, http.StatusForbidden},
		// Status pages: only monitors one may change.
		{editor, "POST", "/status-pages", `{"slug":"shop","title":"Shop","monitors":[{"monitor_id":` + itoa(shop) + `}]}`, http.StatusCreated},
		{editor, "POST", "/status-pages", `{"slug":"mixed","title":"x","monitors":[{"monitor_id":` + itoa(billing) + `}]}`, http.StatusBadRequest},
		{viewer, "POST", "/status-pages", `{"slug":"v","title":"x"}`, http.StatusForbidden},
	}
	for _, a := range attempts {
		if code := doAs(t, a.token, a.method, base+"/api/v1"+a.path, a.body, nil); code != a.want {
			t.Errorf("%s %s by %s: status = %d, want %d", a.method, a.path, a.token[:4], code, a.want)
		}
	}

	// Lists show each their own; the global channel of the admin stays hidden.
	do(t, "POST", base+"/api/v1/channels", `{"name":"global","type":"webhook","target":"https://h/9"}`, nil)
	var channels []channelResponse
	doAs(t, editor, "GET", base+"/api/v1/channels", "", &channels)
	if len(channels) != 1 || channels[0].Name != "shop-hook" {
		t.Errorf("editor's channels = %+v", channels)
	}
	doAs(t, viewer, "GET", base+"/api/v1/channels", "", &channels)
	if len(channels) != 0 {
		t.Errorf("viewer's channels = %+v", channels)
	}
	var pages []statusPageResponse
	doAs(t, editor, "GET", base+"/api/v1/status-pages", "", &pages)
	if len(pages) != 1 || pages[0].Slug != "shop" {
		t.Errorf("editor's pages = %+v", pages)
	}
	var windows []maintenanceResponse
	doAs(t, viewer, "GET", base+"/api/v1/maintenance", "", &windows)
	if len(windows) != 0 {
		t.Errorf("viewer's windows = %+v (the shop window is not theirs)", windows)
	}
	do(t, "POST", base+"/api/v1/maintenance", `{"name":"everything","duration_seconds":60}`, nil)
	doAs(t, viewer, "GET", base+"/api/v1/maintenance", "", &windows)
	if len(windows) != 1 || windows[0].Name != "everything" {
		t.Errorf("viewer's windows after a global one = %+v", windows)
	}
}

// fakeLDAP stands in for a directory: a password and groups per user.
type fakeLDAP struct {
	users map[string]struct {
		password string
		groups   []string
	}
	down bool
}

func (f fakeLDAP) URL() string { return "ldap://fake" }

func (f fakeLDAP) Authenticate(_ context.Context, username, password string) (ldapauth.Identity, error) {
	if f.down {
		return ldapauth.Identity{}, ldapauth.ErrUnavailable
	}
	u, ok := f.users[username]
	if !ok || u.password != password {
		return ldapauth.Identity{}, ldapauth.ErrInvalidCredentials
	}
	return ldapauth.Identity{DN: "uid=" + username + ",dc=example,dc=com", Username: username, DisplayName: strings.ToUpper(username[:1]) + username[1:], Email: username + "@example.com", Groups: u.groups}, nil
}

func TestLDAPSignIn(t *testing.T) {
	directory := &fakeLDAP{users: map[string]struct {
		password string
		groups   []string
	}{
		"ayse":   {"s3cret", []string{"cn=platform-admins,ou=groups,dc=example,dc=com"}},
		"mehmet": {"s3cret", []string{"CN=Shop-Team,OU=groups,DC=example,DC=com", "cn=billing-team,ou=groups,dc=example,dc=com"}},
		"nobody": {"s3cret", []string{"cn=other,ou=groups,dc=example,dc=com"}},
		"admin":  {"ldap-pass", []string{"cn=platform-admins,ou=groups,dc=example,dc=com"}},
	}}
	srv, _ := newTestAPIWith(t, func(o *Options) { o.LDAP = directory })
	base := srv.URL
	doAs(t, "", "POST", base+"/api/v1/auth/setup", `{"username":"admin","password":"parola-123"}`, nil)

	var providers providersResponse
	doAs(t, "", "GET", base+"/api/v1/auth/providers", "", &providers)
	if !providers.LDAP {
		t.Error("the sign-in page is not told about LDAP")
	}
	// Mappings: admins, and two teams with their namespaces.
	for _, body := range []string{
		`{"group_dn":"cn=platform-admins,ou=groups,dc=example,dc=com","role":"admin"}`,
		`{"group_dn":"cn=shop-team,ou=groups,dc=example,dc=com","role":"editor","namespaces":["shop"]}`,
		`{"group_dn":"cn=billing-team,ou=groups,dc=example,dc=com","role":"editor","namespaces":["billing"]}`,
	} {
		if code := do(t, "POST", base+"/api/v1/ldap/groups", body, nil); code != http.StatusCreated {
			t.Fatalf("mapping: status = %d", code)
		}
	}
	if code := do(t, "POST", base+"/api/v1/ldap/groups", `{"group_dn":"CN=Shop-Team,OU=groups,DC=example,DC=com","role":"viewer","namespaces":["shop"]}`, nil); code != http.StatusConflict {
		t.Errorf("a mapping of the same group in another case: status = %d, want 409", code)
	}

	// A directory user gets the role of their groups, and a local record.
	var sess sessionResponse
	if code := doAs(t, "", "POST", base+"/api/v1/auth/login", `{"username":"ayse","password":"s3cret"}`, &sess); code != http.StatusOK {
		t.Fatalf("ayse: status = %d", code)
	}
	if sess.User.Role != store.RoleAdmin || sess.User.Source != store.UserLDAP || sess.User.DisplayName != "Ayse" || sess.User.Email != "ayse@example.com" {
		t.Errorf("ayse = %+v", sess.User)
	}
	// Groups add up: two editor groups, both namespaces.
	doAs(t, "", "POST", base+"/api/v1/auth/login", `{"username":"mehmet","password":"s3cret"}`, &sess)
	if sess.User.Role != store.RoleEditor || strings.Join(sess.User.Namespaces, ",") != "billing,shop" {
		t.Errorf("mehmet = %+v", sess.User)
	}
	mehmet := sess.Token
	if code := doAs(t, mehmet, "POST", base+"/api/v1/monitors", monitorJSON("m", "tcp", "x:1", `"namespace":"shop"`), nil); code != http.StatusCreated {
		t.Errorf("mehmet creating in shop: status = %d", code)
	}
	// No mapped group, no access; a wrong password is a wrong password.
	if code := doAs(t, "", "POST", base+"/api/v1/auth/login", `{"username":"nobody","password":"s3cret"}`, nil); code != http.StatusForbidden {
		t.Errorf("unmapped directory user: status = %d, want 403", code)
	}
	if code := doAs(t, "", "POST", base+"/api/v1/auth/login", `{"username":"ayse","password":"wrong"}`, nil); code != http.StatusUnauthorized {
		t.Errorf("wrong directory password: status = %d", code)
	}
	// A local account of the same name is checked locally, never in the directory.
	if code := doAs(t, "", "POST", base+"/api/v1/auth/login", `{"username":"admin","password":"ldap-pass"}`, nil); code != http.StatusUnauthorized {
		t.Errorf("the directory password of a local user: status = %d, want 401", code)
	}

	// A directory user's role is the directory's to give.
	var users []userResponse
	do(t, "GET", base+"/api/v1/users", "", &users)
	var mehmetID int64
	for _, u := range users {
		if u.Username == "mehmet" {
			mehmetID = u.ID
		}
	}
	if code := do(t, "PATCH", base+"/api/v1/users/"+itoa(mehmetID), `{"role":"admin"}`, nil); code != http.StatusBadRequest {
		t.Errorf("changing a directory user's role here: status = %d, want 400", code)
	}
	if code := doAs(t, mehmet, "POST", base+"/api/v1/auth/password", `{"current_password":"s3cret","new_password":"parola-123"}`, nil); code != http.StatusBadRequest {
		t.Errorf("a directory user changing their password here: status = %d, want 400", code)
	}
	// The group mapping changes: the next sign-in brings the new role.
	var groups []groupRoleResponse
	do(t, "GET", base+"/api/v1/ldap/groups", "", &groups)
	for _, g := range groups {
		if strings.Contains(g.GroupDN, "shop-team") {
			do(t, "DELETE", base+"/api/v1/ldap/groups/"+itoa(g.ID), "", nil)
		}
	}
	doAs(t, "", "POST", base+"/api/v1/auth/login", `{"username":"mehmet","password":"s3cret"}`, &sess)
	if strings.Join(sess.User.Namespaces, ",") != "billing" {
		t.Errorf("mehmet after the mapping was removed = %+v", sess.User)
	}
	// The directory being down is not a wrong password.
	directory.down = true
	if code := doAs(t, "", "POST", base+"/api/v1/auth/login", `{"username":"ayse","password":"s3cret"}`, nil); code != http.StatusBadGateway {
		t.Errorf("directory down: status = %d, want 502", code)
	}
	directory.down = false

	// A default role lets everyone in the directory see something.
	srv2, _ := newTestAPIWith(t, func(o *Options) {
		o.LDAP = directory
		o.LDAPDefaultRole, o.LDAPDefaultNamespaces = store.RoleViewer, []string{"public"}
	})
	doAs(t, "", "POST", srv2.URL+"/api/v1/auth/setup", `{"username":"admin","password":"parola-123"}`, nil)
	if code := doAs(t, "", "POST", srv2.URL+"/api/v1/auth/login", `{"username":"nobody","password":"s3cret"}`, &sess); code != http.StatusOK ||
		sess.User.Role != store.RoleViewer || strings.Join(sess.User.Namespaces, ",") != "public" {
		t.Errorf("default role: status = %d, user = %+v", code, sess.User)
	}
}
