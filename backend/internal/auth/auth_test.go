package auth

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

func TestPermissions(t *testing.T) {
	admin := FromUser(store.User{ID: 1, Username: "admin", Role: store.RoleAdmin})
	editorAll := FromUser(store.User{ID: 2, Role: store.RoleEditor, Namespaces: []string{store.AllNamespaces}})
	editorShop := FromUser(store.User{ID: 3, Role: store.RoleEditor, Namespaces: []string{"shop", "billing"}})
	viewerShop := FromUser(store.User{ID: 4, Role: store.RoleViewer, Namespaces: []string{"shop"}})
	viewerAll := FromUser(store.User{ID: 5, Role: store.RoleViewer, Namespaces: []string{store.AllNamespaces}})

	type want struct{ readShop, readOther, readNone, editShop, editOther, editAll, seesAll, admin bool }
	for name, tc := range map[string]struct {
		p Principal
		want
	}{
		"service":     {ServicePrincipal, want{true, true, true, true, true, true, true, true}},
		"admin":       {admin, want{true, true, true, true, true, true, true, true}},
		"editor all":  {editorAll, want{true, true, true, true, true, true, true, false}},
		"editor shop": {editorShop, want{true, false, false, true, false, false, false, false}},
		"viewer shop": {viewerShop, want{true, false, false, false, false, false, false, false}},
		"viewer all":  {viewerAll, want{true, true, true, false, false, false, true, false}},
	} {
		got := want{
			tc.p.CanRead("shop"), tc.p.CanRead("other"), tc.p.CanRead(""),
			tc.p.CanEdit("shop"), tc.p.CanEdit("other"), tc.p.CanEditAll(), tc.p.SeesAll(), tc.p.IsAdmin(),
		}
		if got != tc.want {
			t.Errorf("%s: %+v, want %+v", name, got, tc.want)
		}
	}
	if !editorShop.CanEditEach([]string{"shop", "billing"}) || editorShop.CanEditEach([]string{"shop", "other"}) || editorShop.CanEditEach(nil) || !editorAll.CanEditEach(nil) {
		t.Error("CanEditEach")
	}

	ctx := WithPrincipal(context.Background(), viewerShop)
	if p, ok := FromContext(ctx); !ok || p.UserID != 4 {
		t.Errorf("FromContext = %+v, %v", p, ok)
	}
	if _, ok := FromContext(context.Background()); ok {
		t.Error("a principal came from an empty context")
	}
}

func TestPasswordsAndTokens(t *testing.T) {
	hash, err := HashPassword("gizli parola")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(hash, "gizli parola") || CheckPassword(hash, "gizli parolA") || CheckPassword("", "") || CheckPassword("not-a-hash", "x") {
		t.Error("CheckPassword")
	}
	token, stored, err := NewToken()
	if err != nil || len(token) < 40 || stored != HashToken(token) || len(stored) != 64 {
		t.Errorf("NewToken = %q, %q, %v", token, stored, err)
	}
	if other, _, _ := NewToken(); other == token {
		t.Error("two tokens are the same")
	}
}

func TestValidatePassword(t *testing.T) {
	for _, tc := range []struct {
		username, password string
		minLen             int
		ok                 bool
	}{
		{"ayse", "parola-123", 8, true},
		{"ayse", "kisa", 8, false},
		{"ayse", "12345678", 8, false}, // long enough, but the first guess
		{"ayse", "PassWord", 8, false}, // whatever the case
		{"admin", "admin", 4, false},
		{"admin", "Administrator", 4, false},
		{"ayse", "changeme", 4, false},
		{"ayse", "kubeat", 4, false},
		{"Ayse.Yilmaz", "ayse.yilmaz", 8, false}, // the username
		{"ayse", "ayse.yilmaz", 8, true},
		// Characters, not bytes, are counted toward the minimum; bcrypt's
		// limit is in bytes.
		{"ayse", "ğüşiöçİı", 8, true},
		{"ayse", "ğüşiöç", 8, false},
		{"ayse", strings.Repeat("a", MaxPasswordBytes), 8, true},
		{"ayse", strings.Repeat("a", MaxPasswordBytes+1), 8, false},
		{"ayse", strings.Repeat("ş", MaxPasswordBytes/2+1), 8, false},
	} {
		err := ValidatePassword(tc.username, tc.password, tc.minLen)
		if (err == nil) != tc.ok {
			t.Errorf("ValidatePassword(%q, %q, %d) = %v", tc.username, tc.password, tc.minLen, err)
		}
	}
	// What a too long password would hash to: it cannot be stored.
	if _, err := HashPassword(strings.Repeat("a", MaxPasswordBytes+1)); err == nil {
		t.Error("bcrypt took a password longer than MaxPasswordBytes")
	}
}

// Refusing a name without an account costs what refusing a wrong password
// costs: a comparison with a hash of the same cost.
func TestCheckPasswordWithoutHashTakesAComparison(t *testing.T) {
	PrepareDummy()
	if cost, err := bcrypt.Cost(dummyHash()); err != nil || cost != bcryptCost {
		t.Fatalf("the dummy hash has cost %d (%v), want %d", cost, err, bcryptCost)
	}
	start := time.Now()
	if CheckPassword("", "parola-123") {
		t.Error("a password matched no hash")
	}
	// A cost-12 comparison takes well over 10ms on any machine; an answer
	// without one takes microseconds.
	if took := time.Since(start); took < 10*time.Millisecond {
		t.Errorf("refusing without a hash took %s: no comparison was made", took)
	}
}

func TestThrottle(t *testing.T) {
	th := &Throttle{MaxFailures: 3, Lockout: 10 * time.Minute}
	t0 := time.Unix(1_700_000_000, 0)
	for i, at := range []time.Time{t0, t0.Add(time.Minute), t0.Add(2 * time.Minute)} {
		if n, _, ok := th.Begin("ayse", at); !ok || n != i+1 {
			t.Fatalf("attempt %d: number %d, ok %v", i+1, n, ok)
		}
	}
	// Three attempts began and none succeeded: the fourth is refused until
	// the first ages out.
	if _, locked, ok := th.Begin("ayse", t0.Add(3*time.Minute)); ok || locked != 7*time.Minute {
		t.Errorf("fourth attempt: locked for %s, ok %v; want 7m, false", locked, ok)
	}
	if n, _, ok := th.Begin("mehmet", t0.Add(3*time.Minute)); !ok || n != 1 {
		t.Error("another user is locked")
	}
	if n, _, ok := th.Begin("ayse", t0.Add(10*time.Minute)); !ok || n != 3 {
		t.Errorf("after the first attempt aged out: number %d, ok %v; want 3, true", n, ok)
	}

	// An attempt taken back does not count; a success forgets them all.
	at := t0.Add(4 * time.Minute)
	th.Begin("can", at)
	th.Begin("can", at.Add(time.Second))
	th.Undo("can", at.Add(time.Second))
	if n, _, _ := th.Begin("can", at.Add(2*time.Second)); n != 2 {
		t.Errorf("after an undo: attempt number %d, want 2", n)
	}
	th.Reset("can")
	if n, _, _ := th.Begin("can", at.Add(3*time.Second)); n != 1 {
		t.Errorf("after a reset: attempt number %d, want 1", n)
	}
	th.Undo("can", at.Add(3*time.Second))
	th.Undo("nobody", at) // nothing to take back
	if got := th.Sweep(at.Add(3 * time.Second)); got != 2 {
		t.Errorf("after undoing the only attempt of a key, %d keys are tracked, want 2 (ayse, mehmet)", got)
	}
}

// Names tried once and never again are forgotten by the sweep, not kept
// for as long as the process runs.
func TestThrottleSweep(t *testing.T) {
	th := &Throttle{MaxFailures: 3, Lockout: 10 * time.Minute}
	t0 := time.Unix(1_700_000_000, 0)
	for i := range 1000 {
		th.Begin(fmt.Sprintf("made-up-%d", i), t0)
	}
	th.Begin("ayse", t0)
	th.Begin("ayse", t0.Add(5*time.Minute))
	if got := th.Sweep(t0.Add(9 * time.Minute)); got != 1001 {
		t.Errorf("before the lockout ended %d keys are tracked, want 1001", got)
	}
	// The newest attempt decides: ayse tried again five minutes later.
	if got := th.Sweep(t0.Add(10 * time.Minute)); got != 1 {
		t.Errorf("after the lockout %d keys are tracked, want 1", got)
	}
	if n, _, _ := th.Begin("ayse", t0.Add(11*time.Minute)); n != 2 {
		t.Errorf("ayse's recent attempt was forgotten: number %d, want 2", n)
	}
	if got := th.Sweep(t0.Add(30 * time.Minute)); got != 0 {
		t.Errorf("%d keys are tracked long after the last attempt", got)
	}
}

// With all its room taken, the throttle lets a new name in and makes room
// for it rather than refusing it: the name with the fewest attempts goes,
// of those the one tried longest ago, however long ago the others were
// tried.
func TestThrottleMaxKeys(t *testing.T) {
	th := &Throttle{MaxFailures: 5, Lockout: 10 * time.Minute, MaxKeys: 3}
	t0 := time.Unix(1_700_000_000, 0)
	at := func(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }
	th.Begin("ayse", at(0))
	th.Begin("ayse", at(1))
	th.Begin("made-up-1", at(2))
	th.Begin("made-up-2", at(3))
	if n, _, ok := th.Begin("made-up-3", at(4)); !ok || n != 1 {
		t.Fatalf("a new name while full: number %d, ok %v; want 1, true", n, ok)
	}
	if got := th.Sweep(at(4)); got != 3 {
		t.Errorf("%d keys are tracked, want the bound, 3", got)
	}
	// made-up-1 made room; ayse, tried twice, kept her failures.
	if n, _, _ := th.Begin("ayse", at(5)); n != 3 {
		t.Errorf("ayse's attempt number after the flood = %d, want 3", n)
	}
	if n, _, _ := th.Begin("made-up-1", at(6)); n != 1 {
		t.Errorf("made-up-1's attempt number = %d, want 1: it was forgotten", n)
	}

	// Every name was tried more than once now, ayse longest ago, but she
	// has the most failures: of the names tried twice, the one tried
	// longest ago goes, made-up-3.
	th.Begin("made-up-3", at(7))
	th.Begin("made-up-1", at(8))
	th.Begin("made-up-4", at(9))
	if n, _, _ := th.Begin("ayse", at(10)); n != 4 {
		t.Errorf("ayse's attempt number = %d, want 4: the name with the most failures was pushed out", n)
	}
	if n, _, _ := th.Begin("made-up-3", at(11)); n != 1 {
		t.Errorf("made-up-3's attempt number = %d, want 1: it was forgotten", n)
	}
}

// A locked name is never pushed out to make room, and takes none: names
// locked on purpose do not leave a name being guessed at, one failure
// short of the lock, the only one to push out.
func TestThrottleKeepsLockedNames(t *testing.T) {
	var logs bytes.Buffer
	th := &Throttle{MaxFailures: 3, Lockout: 10 * time.Minute, MaxKeys: 3, Logger: slog.New(slog.NewTextHandler(&logs, nil))}
	t0 := time.Unix(1_700_000_000, 0)
	at := func(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }
	th.Begin("ayse", at(0))
	th.Begin("ayse", at(1))
	for i, name := range []string{"locked-1", "locked-2"} {
		for j := range 3 {
			th.Begin(name, at(2+3*i+j))
		}
	}
	// Full, and every name but ayse is locked: new names are let in beyond
	// the bound, which is for the names that are not locked...
	for i := range 2 {
		if n, _, ok := th.Begin(fmt.Sprintf("made-up-%d", i), at(10+i)); !ok || n != 1 {
			t.Fatalf("a new name while the locked names fill the bound: number %d, ok %v; want 1, true", n, ok)
		}
	}
	if got := th.Sweep(at(12)); got != 5 {
		t.Errorf("%d keys are tracked, want 5: the locked names take no room", got)
	}
	if !strings.Contains(logs.String(), "admitted=1 locked=2 max=3") {
		t.Errorf("going beyond the bound was not reported: %q", logs.String())
	}
	// ...until those fill it: then the fewest attempts go, made-up-0.
	th.Begin("made-up-2", at(13))
	if n, _, ok := th.Begin("ayse", at(14)); !ok || n != 3 {
		t.Errorf("ayse's attempt: number %d, ok %v; want 3, true: her failures were kept", n, ok)
	}
	if _, _, ok := th.Begin("ayse", at(15)); ok {
		t.Error("ayse was not locked after her third failure")
	}
	for _, name := range []string{"locked-1", "locked-2"} {
		if _, locked, ok := th.Begin(name, at(16)); ok || locked <= 0 {
			t.Errorf("%s was forgotten while locked", name)
		}
	}
	if n, _, _ := th.Begin("made-up-0", at(17)); n != 1 {
		t.Errorf("made-up-0's attempt number = %d, want 1: it was forgotten", n)
	}
}

// A label is new to a key once within its lockout period, and again only
// once the key is forgotten.
func TestThrottleFirst(t *testing.T) {
	th := &Throttle{MaxFailures: 5, Lockout: 10 * time.Minute}
	t0 := time.Unix(1_700_000_000, 0)
	th.Begin("ayse", t0)
	if !th.First("ayse", "login_failed") || th.First("ayse", "login_failed") {
		t.Error("login_failed was not new exactly once")
	}
	if !th.First("ayse", "login_denied") {
		t.Error("another label was not new")
	}
	th.Begin("ayse", t0.Add(time.Minute))
	if th.First("ayse", "login_failed") || th.First("ayse", "login_denied") {
		t.Error("a label was new again within the lockout period")
	}
	th.Reset("ayse")
	th.Begin("ayse", t0.Add(2*time.Minute))
	if !th.First("ayse", "login_failed") {
		t.Error("a label was not new after a success")
	}
	th.Begin("ayse", t0.Add(13*time.Minute)) // the attempt before has aged out
	if !th.First("ayse", "login_failed") {
		t.Error("a label was not new once the attempts had aged out")
	}
	if !th.First("nobody", "login_failed") || !th.First("nobody", "login_failed") {
		t.Error("a key that is not tracked remembered a label")
	}
}

// Names whose attempts have all aged out make room before any other, also
// before a name tried only once.
func TestThrottleFullForgetsAgedNamesFirst(t *testing.T) {
	th := &Throttle{MaxFailures: 5, Lockout: 10 * time.Minute, MaxKeys: 2}
	t0 := time.Unix(1_700_000_000, 0)
	th.Begin("old", t0)
	th.Begin("old", t0.Add(time.Second))
	th.Begin("ayse", t0.Add(5*time.Minute))
	later := t0.Add(11 * time.Minute)
	if _, _, ok := th.Begin("mehmet", later); !ok {
		t.Fatal("a new name was refused")
	}
	if n, _, _ := th.Begin("ayse", later.Add(time.Second)); n != 2 {
		t.Errorf("ayse's attempt number = %d, want 2: she was pushed out though an aged name could go", n)
	}
}

// What the throttle refuses, forgets and tracks beyond its bound is logged,
// at most once a minute; the sweep logs what was left over.
func TestThrottleReports(t *testing.T) {
	var logs bytes.Buffer
	th := &Throttle{MaxFailures: 2, Lockout: 10 * time.Minute, MaxKeys: 1, Logger: slog.New(slog.NewTextHandler(&logs, nil))}
	t0 := time.Unix(1_700_000_000, 0)
	at := func(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }
	for i := range 5 { // each new name pushes out the one before
		th.Begin(fmt.Sprintf("made-up-%d", i), at(i))
	}
	th.Begin("made-up-4", at(5)) // locked by this one
	th.Begin("made-up-4", at(6)) // refused
	th.Begin("made-up-5", at(7)) // tracked beyond the bound: the locked name stays
	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], "forgotten=1 ") {
		t.Fatalf("logged within the first minute: %q, want one line with forgotten=1", logs.String())
	}
	logs.Reset()
	th.Sweep(at(8))
	if got := logs.String(); !strings.Contains(got, "forgotten=3 ") || !strings.Contains(got, "refused=1 name=made-up-4") ||
		!strings.Contains(got, "admitted=1 locked=1 max=1") {
		t.Errorf("the sweep's report = %q, want the three names forgotten, the refusal and the name beyond the bound since the first line", got)
	}
	logs.Reset()
	th.Sweep(at(9))
	if logs.Len() != 0 {
		t.Errorf("an empty report was logged: %q", logs.String())
	}
}

// Parallel attempts for one name: exactly MaxFailures begin, however many
// are sent at once.
func TestThrottleBeginIsAtomic(t *testing.T) {
	th := &Throttle{MaxFailures: 10, Lockout: time.Minute}
	now := time.Unix(1_700_000_000, 0)
	var began atomic.Int32
	var wg sync.WaitGroup
	for range 200 {
		wg.Go(func() {
			if _, _, ok := th.Begin("ayse", now); ok {
				began.Add(1)
			}
		})
	}
	wg.Wait()
	if began.Load() != 10 {
		t.Errorf("%d attempts began, want 10", began.Load())
	}
}
