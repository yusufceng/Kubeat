package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/auth"
	"github.com/yusufceng/Kubeat/backend/internal/ldapauth"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// sendClient gives up on an answer that never comes, so that a request
// held where it should not be fails its test instead of hanging it.
var sendClient = &http.Client{Timeout: time.Minute}

// send is doAs for other goroutines than the test's own, which may not
// stop the test: it returns the status and the error text of the answer,
// or 0 when the request could not be made or got no answer.
func send(token, method, url, body string) (int, string) {
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		return 0, err.Error()
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := sendClient.Do(req)
	if err != nil {
		return 0, err.Error()
	}
	defer resp.Body.Close()
	var answer errorBody
	raw, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(raw, &answer)
	return resp.StatusCode, answer.Error
}

// burst sends n requests at once and counts the answers by status.
func burst(n int, request func(i int) (int, string)) map[int]int {
	var (
		mu     sync.Mutex
		counts = map[int]int{}
		wg     sync.WaitGroup
	)
	for i := range n {
		wg.Go(func() {
			code, _ := request(i)
			mu.Lock()
			counts[code]++
			mu.Unlock()
		})
	}
	wg.Wait()
	return counts
}

// Setups sent at the same moment under different names: one makes the
// administrator, the others are told that an account exists. Each one
// hashes its password before creating the account, so all of them get
// past the first count.
func TestSetupRace(t *testing.T) {
	srv, db := newTestAPI(t)
	const racers = 4
	counts := burst(racers, func(i int) (int, string) {
		return send("", "POST", srv.URL+"/api/v1/auth/setup", fmt.Sprintf(`{"username":"admin-%d","password":"parola-123"}`, i))
	})
	if counts[http.StatusOK] != 1 || counts[http.StatusConflict] != racers-1 {
		t.Errorf("answers = %v, want one 200 and %d 409", counts, racers-1)
	}
	if n, _ := db.CountUsers(context.Background()); n != 1 {
		t.Errorf("%d accounts were created, want 1", n)
	}
}

// Every way of setting a password holds it to the same rules.
func TestPasswordRules(t *testing.T) {
	srv, _ := newTestAPI(t)
	base := srv.URL
	const (
		isName   = "the password must not be the username"
		isCommon = "the password is one of the first that are guessed; choose another"
	)
	for password, want := range map[string]string{
		"YusufCoskun": isName,
		"changeme":    isCommon,
		strings.Repeat("a", auth.MaxPasswordBytes+1): "the password must be at most 72 bytes",
	} {
		body := `{"username":"yusufcoskun","password":"` + password + `"}`
		if code, msg := send("", "POST", base+"/api/v1/auth/setup", body); code != http.StatusBadRequest || msg != want {
			t.Errorf("setup with password %q: %d %q, want 400 %q", password, code, msg, want)
		}
	}
	var first sessionResponse
	if code := doAs(t, "", "POST", base+"/api/v1/auth/setup", `{"username":"yusufcoskun","password":"parola-123"}`, &first); code != http.StatusOK {
		t.Fatalf("setup: status = %d", code)
	}
	for body, want := range map[string]string{
		`{"username":"ayseyilmaz","password":"AyseYilmaz","role":"admin"}`: isName,
		`{"username":"ayse","password":"administrator","role":"admin"}`:    isCommon,
	} {
		if code, msg := send(testToken, "POST", base+"/api/v1/users", body); code != http.StatusBadRequest || msg != want {
			t.Errorf("new user %s: %d %q, want 400 %q", body, code, msg, want)
		}
	}
	ayse := addUser(t, base, "ayse", store.RoleViewer, "shop")
	if code, msg := send(testToken, "PATCH", base+"/api/v1/users/"+itoa(ayse.ID), `{"password":"12345678"}`); code != http.StatusBadRequest || msg != isCommon {
		t.Errorf("a reset to a common password: %d %q", code, msg)
	}
	if code, msg := send(first.Token, "POST", base+"/api/v1/auth/password", `{"current_password":"parola-123","new_password":"YUSUFCOSKUN"}`); code != http.StatusBadRequest || msg != isName {
		t.Errorf("changing one's password to one's name: %d %q", code, msg)
	}
}

// A user update whose new password the rules refuse saves nothing of what
// came with it: not a promotion, not a new name, not re-enabling a
// disabled account. The old password goes on working, and the audit log
// has nothing to tell.
func TestRefusedPasswordSavesNoneOfTheUpdate(t *testing.T) {
	srv, _ := newTestAPIWith(t, func(o *Options) { o.MaxResults = 100 })
	base := srv.URL
	doAs(t, "", "POST", base+"/api/v1/auth/setup", `{"username":"admin","password":"parola-123"}`, nil)
	viewer := addUser(t, base, "viewer1", store.RoleViewer, "shop")
	editor := addUser(t, base, "editor1", store.RoleEditor, "shop")
	do(t, "PATCH", base+"/api/v1/users/"+itoa(editor.ID), `{"enabled":false}`, nil)
	updates := len(audited(t, base, "update"))

	for id, body := range map[int64]string{
		viewer.ID: `{"display_name":"New Name","enabled":true,"role":"admin","namespaces":[],"password":"changeme"}`,
		editor.ID: `{"enabled":true,"password":"changeme"}`,
	} {
		if code, msg := send(testToken, "PATCH", base+"/api/v1/users/"+itoa(id), body); code != http.StatusBadRequest || !strings.Contains(msg, "guessed") {
			t.Errorf("PATCH %s: %d %q, want 400 for the password", body, code, msg)
		}
	}
	var users []userResponse
	do(t, "GET", base+"/api/v1/users", "", &users)
	for _, u := range users {
		switch {
		case u.ID == viewer.ID && (u.Role != store.RoleViewer || u.DisplayName == "New Name"):
			t.Errorf("the viewer after a refused update = %+v", u)
		case u.ID == editor.ID && u.Enabled:
			t.Errorf("the disabled editor was enabled by a refused update")
		}
	}
	if _, code := signIn(t, base, "viewer1", "parola-123"); code != http.StatusOK {
		t.Errorf("the viewer's old password: status = %d, want 200", code)
	}
	if _, code := signIn(t, base, "editor1", "parola-123"); code != http.StatusForbidden {
		t.Errorf("the disabled editor's old password: status = %d, want 403", code)
	}
	if got := audited(t, base, "update"); len(got) != updates {
		t.Errorf("update entries after refused updates = %q, want the %d from before", got, updates)
	}
}

// Parallel wrong passwords for one name: as many are checked as the limit
// allows, however many arrive at once, and the right password is refused
// afterwards like any other attempt.
func TestLoginThrottleHoldsUnderParallelAttempts(t *testing.T) {
	srv, _ := newTestAPI(t) // three failures lock a name
	base := srv.URL
	doAs(t, "", "POST", base+"/api/v1/auth/setup", `{"username":"yusuf","password":"parola-123"}`, nil)
	counts := burst(12, func(i int) (int, string) {
		return send("", "POST", base+"/api/v1/auth/login", fmt.Sprintf(`{"username":"yusuf","password":"guess-%d"}`, i))
	})
	if counts[http.StatusUnauthorized] != 3 || counts[http.StatusTooManyRequests] != 9 {
		t.Errorf("answers to 12 parallel guesses = %v, want three 401 and nine 429", counts)
	}
	if _, code := signIn(t, base, "yusuf", "parola-123"); code != http.StatusTooManyRequests {
		t.Errorf("the right password after the burst: status = %d, want 429", code)
	}
}

// A flood of made-up names fills the throttle, and each new name makes
// room instead of being refused: the made-up names push each other out,
// and nobody is locked out. Not the administrator's right password, not a
// password change, and not an administrator made with the API token as
// the way back in.
func TestMadeUpNamesFloodLocksNobodyOut(t *testing.T) {
	srv, _ := newTestAPIWith(t, func(o *Options) {
		o.Throttle = &auth.Throttle{MaxFailures: 3, Lockout: time.Minute, MaxKeys: 4}
	})
	base := srv.URL
	var admin sessionResponse
	doAs(t, "", "POST", base+"/api/v1/auth/setup", `{"username":"admin","password":"parola-123"}`, &admin)

	counts := burst(12, func(i int) (int, string) {
		return send("", "POST", base+"/api/v1/auth/login", fmt.Sprintf(`{"username":"made-up-%d","password":"guess"}`, i))
	})
	if counts[http.StatusUnauthorized] != 12 {
		t.Errorf("answers to 12 made-up names with room for 4 = %v, want twelve 401: each one is let in and refused as a wrong password", counts)
	}
	if _, code := signIn(t, base, "admin", "parola-123"); code != http.StatusOK {
		t.Errorf("the administrator's right password after the flood: status = %d, want 200", code)
	}
	if code := doAs(t, admin.Token, "POST", base+"/api/v1/auth/password", `{"current_password":"parola-123","new_password":"yeni-parola-1"}`, nil); code != http.StatusNoContent {
		t.Errorf("a password change after the flood: status = %d, want 204", code)
	}
	addUser(t, base, "breakglass", store.RoleAdmin)
	if _, code := signIn(t, base, "breakglass", "parola-123"); code != http.StatusOK {
		t.Errorf("an administrator made with the API token: status = %d, want 200", code)
	}
}

// Answers that are not a wrong password do not count toward the lock: the
// directory being down, or the right password of a disabled account.
func TestLoginAttemptsThatAreNotGuesses(t *testing.T) {
	directory := &fakeLDAP{users: map[string]struct {
		password string
		groups   []string
	}{"ayse": {"s3cret", []string{"cn=admins,dc=example,dc=com"}}}}
	srv, _ := newTestAPIWith(t, func(o *Options) {
		o.LDAP = directory
		o.LDAPDefaultRole = store.RoleAdmin
	})
	base := srv.URL
	doAs(t, "", "POST", base+"/api/v1/auth/setup", `{"username":"admin","password":"parola-123"}`, nil)

	directory.down = true
	for i := range 5 {
		if _, code := signIn(t, base, "ayse", "s3cret"); code != http.StatusBadGateway {
			t.Fatalf("directory down, attempt %d: status = %d, want 502", i+1, code)
		}
	}
	directory.down = false
	if _, code := signIn(t, base, "ayse", "s3cret"); code != http.StatusOK {
		t.Errorf("once the directory is back: status = %d, want 200", code)
	}

	mehmet := addUser(t, base, "mehmet", store.RoleViewer, "shop")
	do(t, "PATCH", base+"/api/v1/users/"+itoa(mehmet.ID), `{"enabled":false}`, nil)
	for i := range 5 {
		if _, code := signIn(t, base, "mehmet", "parola-123"); code != http.StatusForbidden {
			t.Fatalf("disabled account, attempt %d: status = %d, want 403", i+1, code)
		}
	}
	do(t, "PATCH", base+"/api/v1/users/"+itoa(mehmet.ID), `{"enabled":true}`, nil)
	if _, code := signIn(t, base, "mehmet", "parola-123"); code != http.StatusOK {
		t.Errorf("once enabled again: status = %d, want 200", code)
	}
}

// blockingLDAP keeps every directory sign-in waiting until released, as
// a directory that never answers would.
type blockingLDAP struct {
	entered, release chan struct{}
}

func (b *blockingLDAP) URL() string { return "ldap://blocking" }

func (b *blockingLDAP) Authenticate(context.Context, string, string) (ldapauth.Identity, error) {
	select {
	case b.entered <- struct{}{}:
	case <-b.release:
	}
	<-b.release
	return ldapauth.Identity{}, ldapauth.ErrInvalidCredentials
}

// newBlockedAPI serves the handler with a directory that never answers
// until the returned function releases it.
func newBlockedAPI(t *testing.T, customize func(*Options)) (string, *blockingLDAP, func()) {
	t.Helper()
	directory := &blockingLDAP{entered: make(chan struct{}), release: make(chan struct{})}
	srv, _ := newTestAPIWith(t, func(o *Options) {
		o.LDAP = directory
		customize(o)
	})
	// Released before the server is closed, which waits for held requests.
	release := sync.OnceFunc(func() { close(directory.release) })
	t.Cleanup(release)
	doAs(t, "", "POST", srv.URL+"/api/v1/auth/setup", `{"username":"admin","password":"parola-123"}`, nil)
	return srv.URL, directory, release
}

// holdDirectory starts n directory sign-ins and returns once all of them
// wait on the directory; their answers come on the channel.
func holdDirectory(t *testing.T, base string, directory *blockingLDAP, n int) <-chan int {
	t.Helper()
	held := make(chan int, n)
	for i := range n {
		go func() {
			code, _ := send("", "POST", base+"/api/v1/auth/login", fmt.Sprintf(`{"username":"held-%d","password":"anything"}`, i))
			held <- code
		}()
	}
	timeout := time.After(30 * time.Second)
	for i := range n {
		select {
		case <-directory.entered:
		case <-timeout:
			t.Fatalf("only %d of %d directory sign-ins reached the directory", i, n)
		}
	}
	return held
}

// A directory that does not answer holds only the directory's slots: with
// a single password-check slot, local sign-in, the way in while the
// directory is down, goes on.
func TestHungDirectoryLeavesLocalSignInAlone(t *testing.T) {
	base, directory, release := newBlockedAPI(t, func(o *Options) {
		// Long enough for any comparison to finish, however slow the
		// machine: what must not be waited for is the directory.
		o.LoginConcurrency, o.LoginQueueWait = 1, 20*time.Second
	})
	held := holdDirectory(t, base, directory, 2)
	if _, code := signIn(t, base, "admin", "parola-123"); code != http.StatusOK {
		t.Errorf("the administrator while the directory hangs: status = %d, want 200", code)
	}
	release()
	for range 2 {
		if code := <-held; code != http.StatusUnauthorized {
			t.Errorf("a directory sign-in once released: status = %d, want 401", code)
		}
	}
}

// While every directory slot is taken, a directory sign-in waits its turn
// for a while and is then refused with 503, without counting as a guess.
func TestLoginWaitsForAFreeSlot(t *testing.T) {
	base, directory, release := newBlockedAPI(t, func(o *Options) {
		// A password-check slot for each sign-in's dummy comparison: what
		// is waited for is a directory slot.
		o.LoginConcurrency, o.LoginQueueWait = directoryConcurrency+1, 100*time.Millisecond
	})
	held := holdDirectory(t, base, directory, directoryConcurrency)
	for i := range 4 { // more than the three failures that lock a name
		start := time.Now()
		code, msg := send("", "POST", base+"/api/v1/auth/login", `{"username":"dirk","password":"anything"}`)
		if code != http.StatusServiceUnavailable || !strings.HasPrefix(msg, "too many sign-ins") {
			t.Errorf("attempt %d while busy: %d %q, want 503", i+1, code, msg)
		}
		if waited := time.Since(start); waited < 100*time.Millisecond {
			t.Errorf("attempt %d was refused after %s, before the queue wait", i+1, waited)
		}
	}
	release()
	for range directoryConcurrency {
		if code := <-held; code != http.StatusUnauthorized {
			t.Errorf("a directory sign-in that held a slot: status = %d, want 401", code)
		}
	}
	if code, _ := send("", "POST", base+"/api/v1/auth/login", `{"username":"dirk","password":"anything"}`); code != http.StatusUnauthorized {
		t.Errorf("after the slots came free: status = %d, want 401 (the refusals were counted)", code)
	}
}

// With a directory, a name without a local account is refused no sooner
// than a local account's wrong password: the time of the answer does not
// tell which names have one.
func TestDirectorySignInTakesAComparison(t *testing.T) {
	srv, _ := newTestAPIWith(t, func(o *Options) {
		o.LDAP = &fakeLDAP{} // answers at once: nobody is in it
		o.Throttle = &auth.Throttle{MaxFailures: 10, Lockout: time.Minute}
	})
	base := srv.URL
	doAs(t, "", "POST", base+"/api/v1/auth/setup", `{"username":"admin","password":"parola-123"}`, nil)
	took := func(username string) time.Duration {
		start := time.Now()
		if _, code := signIn(t, base, username, "yanlis-parola"); code != http.StatusUnauthorized {
			t.Fatalf("%s: status = %d, want 401", username, code)
		}
		return time.Since(start)
	}
	took("admin") // the first comparison may be slower
	local, unknown := took("admin"), took("nobody")
	if unknown < local/2 {
		t.Errorf("a name without a local account was refused in %s, a local account in %s", unknown, local)
	}
}

// The current password, asked for to change it, is throttled like a
// sign-in, under a key of its own.
func TestPasswordChangeIsThrottled(t *testing.T) {
	srv, _ := newTestAPI(t) // three failures lock
	base := srv.URL
	var first sessionResponse
	doAs(t, "", "POST", base+"/api/v1/auth/setup", `{"username":"yusuf","password":"parola-123"}`, &first)
	change := func(current string) int {
		return doAs(t, first.Token, "POST", base+"/api/v1/auth/password", `{"current_password":"`+current+`","new_password":"yeni-parola-1"}`, nil)
	}
	// A success forgets the failures before it.
	change("yanlis-1")
	change("yanlis-2")
	if code := change("parola-123"); code != http.StatusNoContent {
		t.Fatalf("change: status = %d", code)
	}
	for i := range 3 {
		if code := change("yanlis"); code != http.StatusBadRequest {
			t.Errorf("wrong current password %d: status = %d, want 400", i+1, code)
		}
	}
	if code := change("yeni-parola-1"); code != http.StatusTooManyRequests {
		t.Errorf("the right current password after three wrong ones: status = %d, want 429", code)
	}
	// Signing in is a matter of its own.
	if _, code := signIn(t, base, "yusuf", "yeni-parola-1"); code != http.StatusOK {
		t.Errorf("sign in while password changes are locked: status = %d, want 200", code)
	}
}

// audited returns the audit log's entries of action, newest first, as
// "username: summary".
func audited(t *testing.T, base, action string) []string {
	t.Helper()
	var entries []auditResponse
	do(t, "GET", base+"/api/v1/audit?limit=100", "", &entries)
	var out []string
	for _, e := range entries {
		if e.Action == action {
			out = append(out, e.Username+": "+e.Summary)
		}
	}
	return out
}

// The audit log gets a name's first failure and the one that locks it,
// not every guess, and only so many failures of names without an account
// a minute in all, made up or only in the directory; a local account's
// are never left out for them.
func TestFailedSignInsInTheAuditLog(t *testing.T) {
	srv, _ := newTestAPI(t) // three failures lock
	base := srv.URL
	doAs(t, "", "POST", base+"/api/v1/auth/setup", `{"username":"yusuf","password":"parola-123"}`, nil)
	for range 3 {
		signIn(t, base, "yusuf", "yanlis")
	}
	failed := func(base string) []string { return audited(t, base, "login_failed") }
	want := []string{"yusuf: wrong password; sign-in locked after 3 failed attempts", "yusuf: wrong password"}
	if got := failed(base); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("failed sign-ins in the audit log = %q, want %q", got, want)
	}

	// Made-up names fail for the first time each: the cap holds them, with
	// a directory or without.
	capped, _ := newTestAPIWith(t, func(o *Options) { o.LoginAuditPerMinute, o.MaxResults = 1, 100 })
	cappedDirectory, _ := newTestAPIWith(t, func(o *Options) { o.LoginAuditPerMinute, o.MaxResults, o.LDAP = 1, 100, &fakeLDAP{} })
	for _, base := range []string{capped.URL, cappedDirectory.URL} {
		for i := range 5 {
			signIn(t, base, fmt.Sprintf("made-up-%d", i), "parola-123")
		}
		// One a minute; two if the sign-ins happened to span two minutes.
		if got := failed(base); len(got) < 1 || len(got) > 2 {
			t.Errorf("five failed sign-ins with a cap of one a minute left %d entries: %q", len(got), got)
		}
	}

	// The cap is spent, but the guesses at a local account that follow are
	// written all the same: a flood of made-up names must not hide them.
	doAs(t, "", "POST", capped.URL+"/api/v1/auth/setup", `{"username":"admin","password":"parola-123"}`, nil)
	for range 3 {
		signIn(t, capped.URL, "admin", "yanlis")
	}
	var admin []string
	for _, entry := range failed(capped.URL) {
		if strings.HasPrefix(entry, "admin: ") {
			admin = append(admin, entry)
		}
	}
	if want := []string{"admin: wrong password; sign-in locked after 3 failed attempts", "admin: wrong password"}; strings.Join(admin, "|") != strings.Join(want, "|") {
		t.Errorf("the administrator's failures after the cap was spent = %q, want %q", admin, want)
	}

	tr := &handler{opts: Options{Language: "tr"}}
	if got := fmt.Sprintf(tr.auditText("%s; sign-in locked after %d failed attempts"), tr.auditText("wrong password"), 3); got != "parola yanlış; 3 hatalı denemeden sonra giriş kilitlendi" {
		t.Errorf("in Turkish: %q", got)
	}
}

// The failures of a name with an account here are written however many
// made-up names spent the cap before them: those of a directory user who
// signed in before, and of an SSO account, as much as a local account's.
func TestKnownNamesPassTheAuditCap(t *testing.T) {
	directory := &fakeLDAP{users: map[string]struct {
		password string
		groups   []string
	}{"alice": {"s3cret", nil}}}
	withDirectory, _ := newTestAPIWith(t, func(o *Options) {
		o.LDAP, o.LDAPDefaultRole, o.LoginAuditPerMinute, o.MaxResults = directory, store.RoleAdmin, 1, 100
	})
	withoutDirectory, db := newTestAPIWith(t, func(o *Options) { o.LoginAuditPerMinute, o.MaxResults = 1, 100 })
	for _, base := range []string{withDirectory.URL, withoutDirectory.URL} {
		doAs(t, "", "POST", base+"/api/v1/auth/setup", `{"username":"admin","password":"parola-123"}`, nil)
	}
	// alice signs in once, and has an account from the directory since.
	if _, code := signIn(t, withDirectory.URL, "alice", "s3cret"); code != http.StatusOK {
		t.Fatalf("alice's first sign-in: status = %d", code)
	}
	// bob signs in through SSO, and has no password: there is no directory.
	if _, err := db.CreateUser(context.Background(), store.User{
		Username: "bob", Source: store.UserOIDC, Role: store.RoleViewer, Namespaces: []string{"shop"}, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		base, name, why string
	}{
		{withDirectory.URL, "alice", "rejected by the directory"},
		{withoutDirectory.URL, "bob", "the account has no password here"},
	} {
		for i := range 3 {
			signIn(t, tc.base, fmt.Sprintf("made-up-%d", i), "parola-123")
		}
		for range 3 {
			signIn(t, tc.base, tc.name, "yanlis")
		}
		var got []string
		for _, entry := range audited(t, tc.base, "login_failed") {
			if strings.HasPrefix(entry, tc.name+": ") {
				got = append(got, entry)
			}
		}
		want := []string{tc.name + ": " + tc.why + "; sign-in locked after 3 failed attempts", tc.name + ": " + tc.why}
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("%s's failures after the cap was spent = %q, want %q", tc.name, got, want)
		}
	}
}

// An account made by SSO signs in through SSO only: with LDAP on, a
// directory user of the same name, who may be someone else, does not get
// it with the directory's password.
func TestTheDirectoryDoesNotTakeAnSSOAccount(t *testing.T) {
	directory := &fakeLDAP{users: map[string]struct {
		password string
		groups   []string
	}{"carol": {"s3cret", nil}}}
	srv, db := newTestAPIWith(t, func(o *Options) { o.LDAP, o.LDAPDefaultRole = directory, store.RoleAdmin })
	doAs(t, "", "POST", srv.URL+"/api/v1/auth/setup", `{"username":"admin","password":"parola-123"}`, nil)
	if _, err := db.CreateUser(context.Background(), store.User{
		Username: "carol", Source: store.UserOIDC, Role: store.RoleViewer, Namespaces: []string{"shop"}, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, code := signIn(t, srv.URL, "carol", "s3cret"); code != http.StatusUnauthorized {
		t.Errorf("the directory's carol signing in to the SSO account: status = %d, want 401", code)
	}
	user, err := db.GetUserByUsername(context.Background(), "carol")
	if err != nil || user.Source != store.UserOIDC || user.Role != store.RoleViewer {
		t.Errorf("the SSO account after the attempt = %+v, %v; want it unchanged", user, err)
	}
}

// A name's first refusal of each kind within the lockout period is
// written, not only its first refusal: a directory user without a role who
// mistypes the password first leaves the denial too, which tells the
// administrator that a group of theirs is not mapped.
func TestFirstRefusalOfEachKindInTheAuditLog(t *testing.T) {
	directory := &fakeLDAP{users: map[string]struct {
		password string
		groups   []string
	}{"nobody": {"s3cret", []string{"cn=other,dc=example,dc=com"}}}}
	srv, _ := newTestAPIWith(t, func(o *Options) {
		o.LDAP, o.MaxResults = directory, 100
		o.Throttle = &auth.Throttle{MaxFailures: 5, Lockout: time.Minute}
	})
	base := srv.URL
	var codes []int
	for _, password := range []string{"yanlis", "s3cret", "yanlis", "s3cret", "s3cret"} {
		_, code := signIn(t, base, "nobody", password)
		codes = append(codes, code)
	}
	if want := []int{401, 403, 401, 403, 403}; !slices.Equal(codes, want) {
		t.Errorf("answers = %v, want %v", codes, want)
	}
	if got, want := audited(t, base, "login_failed"), []string{"nobody: rejected by the directory"}; !slices.Equal(got, want) {
		t.Errorf("failures in the audit log = %q, want %q", got, want)
	}
	want := []string{
		": nobody: none of your 1 groups has a role here; ask an administrator; sign-in locked after 5 failed attempts",
		": nobody: none of your 1 groups has a role here; ask an administrator",
	}
	if got := audited(t, base, "login_denied"); !slices.Equal(got, want) {
		t.Errorf("denials in the audit log = %q, want %q", got, want)
	}
}

// Everyone in the directory has a password, most of them without a role
// here. The right password of one without a role is refused, and counts
// toward the lock like a wrong one; the audit log gets the first refusal
// and the one that locks the name, within the cap, not a row a request.
func TestDirectoryUserWithoutARoleIsThrottled(t *testing.T) {
	directory := &fakeLDAP{users: map[string]struct {
		password string
		groups   []string
	}{
		"nobody":  {"s3cret", []string{"cn=other,dc=example,dc=com"}},
		"nobody2": {"s3cret", nil},
		"nobody3": {"s3cret", nil},
	}}
	srv, _ := newTestAPIWith(t, func(o *Options) { o.LDAP, o.MaxResults = directory, 100 }) // three failures lock
	base := srv.URL
	var codes []int
	for range 5 {
		_, code := signIn(t, base, "nobody", "s3cret")
		codes = append(codes, code)
	}
	if want := []int{403, 403, 403, 429, 429}; !slices.Equal(codes, want) {
		t.Errorf("answers to five sign-ins without a role = %v, want %v", codes, want)
	}
	want := []string{
		": nobody: none of your 1 groups has a role here; ask an administrator; sign-in locked after 3 failed attempts",
		": nobody: none of your 1 groups has a role here; ask an administrator",
	}
	if got := audited(t, base, "login_denied"); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("refusals in the audit log = %q, want %q", got, want)
	}

	capped, _ := newTestAPIWith(t, func(o *Options) { o.LoginAuditPerMinute, o.MaxResults, o.LDAP = 1, 100, directory })
	for _, name := range []string{"nobody", "nobody2", "nobody3"} {
		signIn(t, capped.URL, name, "s3cret")
	}
	// One a minute; two if the sign-ins happened to span two minutes.
	if got := audited(t, capped.URL, "login_denied"); len(got) < 1 || len(got) > 2 {
		t.Errorf("three refusals with a cap of one a minute left %d entries: %q", len(got), got)
	}
}
