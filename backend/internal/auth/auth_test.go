package auth

import (
	"context"
	"testing"
	"time"

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

func TestThrottle(t *testing.T) {
	th := &Throttle{MaxFailures: 3, Lockout: 10 * time.Minute}
	t0 := time.Unix(1_700_000_000, 0)
	if th.LockedFor("ayse", t0) != 0 {
		t.Error("locked before any failure")
	}
	th.Fail("ayse", t0)
	th.Fail("ayse", t0.Add(time.Minute))
	if th.LockedFor("ayse", t0.Add(2*time.Minute)) != 0 {
		t.Error("locked after two failures")
	}
	th.Fail("ayse", t0.Add(2*time.Minute))
	if got := th.LockedFor("ayse", t0.Add(3*time.Minute)); got != 7*time.Minute {
		t.Errorf("locked for %s, want 7m", got)
	}
	// Another user is not affected; the lock ends when the first failure ages out.
	if th.LockedFor("mehmet", t0.Add(3*time.Minute)) != 0 {
		t.Error("another user is locked")
	}
	if th.LockedFor("ayse", t0.Add(10*time.Minute)) != 0 {
		t.Error("still locked after the lockout")
	}
	th.Fail("ayse", t0.Add(11*time.Minute))
	th.Fail("ayse", t0.Add(11*time.Minute))
	th.Fail("ayse", t0.Add(11*time.Minute))
	th.Reset("ayse")
	if th.LockedFor("ayse", t0.Add(11*time.Minute)) != 0 {
		t.Error("locked after a reset")
	}
}
