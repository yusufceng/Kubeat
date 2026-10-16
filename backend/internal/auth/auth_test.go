package auth

import (
	"context"
	"fmt"
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

// With all its room taken, the throttle refuses new names instead of
// growing, and keeps counting for the names it holds.
func TestThrottleMaxKeys(t *testing.T) {
	th := &Throttle{MaxFailures: 3, Lockout: 10 * time.Minute, MaxKeys: 2}
	t0 := time.Unix(1_700_000_000, 0)
	th.Begin("ayse", t0)
	th.Begin("made-up-1", t0)
	if _, locked, ok := th.Begin("made-up-2", t0.Add(time.Second)); ok || locked != th.Lockout {
		t.Errorf("a new name while full: locked for %s, ok %v; want the lockout, false", locked, ok)
	}
	if n, _, ok := th.Begin("ayse", t0.Add(time.Second)); !ok || n != 2 {
		t.Errorf("a tracked name while full: number %d, ok %v", n, ok)
	}
	th.Reset("ayse")
	if _, _, ok := th.Begin("made-up-2", t0.Add(2*time.Second)); !ok {
		t.Error("a new name is refused after a key was freed")
	}
	th.Sweep(t0.Add(time.Hour))
	if _, _, ok := th.Begin("made-up-3", t0.Add(time.Hour)); !ok {
		t.Error("a new name is refused after the sweep made room")
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
