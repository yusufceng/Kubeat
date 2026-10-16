package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/auth"
	"github.com/yusufceng/Kubeat/backend/internal/ldapauth"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// send is doAs for other goroutines than the test's own, which may not
// stop the test: it returns the status and the error text of the answer,
// or 0 when the request could not be made.
func send(token, method, url, body string) (int, string) {
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		return 0, err.Error()
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
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

// blockingLDAP keeps every directory sign-in waiting until released.
type blockingLDAP struct {
	entered, release chan struct{}
}

func (b *blockingLDAP) URL() string { return "ldap://blocking" }

func (b *blockingLDAP) Authenticate(context.Context, string, string) (ldapauth.Identity, error) {
	b.entered <- struct{}{}
	<-b.release
	return ldapauth.Identity{}, ldapauth.ErrInvalidCredentials
}

// While every slot is busy, a sign-in waits its turn for a while and is
// then refused with 503, without counting as a guess.
func TestLoginWaitsForAFreeSlot(t *testing.T) {
	directory := &blockingLDAP{entered: make(chan struct{}), release: make(chan struct{})}
	srv, _ := newTestAPIWith(t, func(o *Options) {
		o.LDAP = directory
		o.LoginConcurrency, o.LoginQueueWait = 1, 50*time.Millisecond
	})
	// Released before the server is closed, which waits for the held request.
	release := sync.OnceFunc(func() { close(directory.release) })
	t.Cleanup(release)
	base := srv.URL
	doAs(t, "", "POST", base+"/api/v1/auth/setup", `{"username":"admin","password":"parola-123"}`, nil)

	held := make(chan int)
	go func() {
		code, _ := send("", "POST", base+"/api/v1/auth/login", `{"username":"dirk","password":"anything"}`)
		held <- code
	}()
	<-directory.entered // the only slot is taken
	for i := range 4 {  // more than the three failures that lock a name
		start := time.Now()
		code, msg := send("", "POST", base+"/api/v1/auth/login", `{"username":"admin","password":"parola-123"}`)
		if code != http.StatusServiceUnavailable || !strings.HasPrefix(msg, "too many sign-ins") {
			t.Errorf("attempt %d while busy: %d %q, want 503", i+1, code, msg)
		}
		if waited := time.Since(start); waited < 50*time.Millisecond {
			t.Errorf("attempt %d was refused after %s, before the queue wait", i+1, waited)
		}
	}
	release()
	if code := <-held; code != http.StatusUnauthorized {
		t.Errorf("the directory sign-in that held the slot: status = %d, want 401", code)
	}
	if _, code := signIn(t, base, "admin", "parola-123"); code != http.StatusOK {
		t.Errorf("after the slot came free: status = %d, want 200 (the refusals were counted)", code)
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

// The audit log gets a name's first failure and the one that locks it,
// not every guess, and only so many failures a minute in all.
func TestFailedSignInsInTheAuditLog(t *testing.T) {
	srv, _ := newTestAPI(t) // three failures lock
	base := srv.URL
	doAs(t, "", "POST", base+"/api/v1/auth/setup", `{"username":"yusuf","password":"parola-123"}`, nil)
	for range 3 {
		signIn(t, base, "yusuf", "yanlis")
	}
	failed := func(base string) []string {
		var entries []auditResponse
		do(t, "GET", base+"/api/v1/audit?limit=100", "", &entries)
		var out []string
		for _, e := range entries {
			if e.Action == "login_failed" {
				out = append(out, e.Username+": "+e.Summary)
			}
		}
		return out
	}
	want := []string{"yusuf: wrong password; sign-in locked after 3 failed attempts", "yusuf: wrong password"}
	if got := failed(base); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("failed sign-ins in the audit log = %q, want %q", got, want)
	}

	// Made-up names fail for the first time each: the cap holds them.
	capped, _ := newTestAPIWith(t, func(o *Options) { o.LoginAuditPerMinute = 1 })
	for i := range 5 {
		signIn(t, capped.URL, fmt.Sprintf("made-up-%d", i), "parola-123")
	}
	// One a minute; two if the sign-ins happened to span two minutes.
	if got := failed(capped.URL); len(got) < 1 || len(got) > 2 {
		t.Errorf("five failed sign-ins with a cap of one a minute left %d entries: %q", len(got), got)
	}

	tr := &handler{opts: Options{Language: "tr"}}
	if got := fmt.Sprintf(tr.auditText("%s; sign-in locked after %d failed attempts"), tr.auditText("wrong password"), 3); got != "parola yanlış; 3 hatalı denemeden sonra giriş kilitlendi" {
		t.Errorf("in Turkish: %q", got)
	}
}
