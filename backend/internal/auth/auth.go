// Package auth says who is making a request and what they may do.
package auth

import (
	"container/list"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// Principal is who a request comes from: a signed-in user, or the service
// token, which may do everything.
type Principal struct {
	UserID      int64
	Username    string
	DisplayName string
	Role        store.Role
	// Namespaces are what an editor or viewer may see; see store.User.
	Namespaces []string
	// Service is true for the API token.
	Service bool
}

// IsAdmin reports whether the principal may do everything.
func (p Principal) IsAdmin() bool {
	return p.Service || p.Role == store.RoleAdmin
}

// SeesAll reports whether every namespace is in the principal's scope.
func (p Principal) SeesAll() bool {
	return p.IsAdmin() || slices.Contains(p.Namespaces, store.AllNamespaces)
}

// CanRead reports whether the principal may see the monitors of a
// namespace or group. Monitors without one ("") are seen only by those who
// see all.
func (p Principal) CanRead(namespace string) bool {
	return p.SeesAll() || (namespace != "" && slices.Contains(p.Namespaces, namespace))
}

// CanEdit reports whether the principal may change the monitors of a
// namespace or group.
func (p Principal) CanEdit(namespace string) bool {
	return p.IsAdmin() || (p.Role == store.RoleEditor && p.CanRead(namespace))
}

// CanEditAll reports whether the principal may change monitors anywhere,
// which is what a thing that covers every namespace needs: a channel or a
// maintenance window without a scope.
func (p Principal) CanEditAll() bool {
	return p.IsAdmin() || (p.Role == store.RoleEditor && p.SeesAll())
}

// CanEditEach reports whether the principal may change monitors in every
// one of the given namespaces; an empty list means every namespace.
func (p Principal) CanEditEach(namespaces []string) bool {
	if len(namespaces) == 0 {
		return p.CanEditAll()
	}
	for _, ns := range namespaces {
		if !p.CanEdit(ns) {
			return false
		}
	}
	return true
}

// FromUser returns the principal a signed-in user acts as.
func FromUser(u store.User) Principal {
	return Principal{UserID: u.ID, Username: u.Username, DisplayName: u.DisplayName, Role: u.Role, Namespaces: u.Namespaces}
}

// ServicePrincipal is who the API token acts as.
var ServicePrincipal = Principal{Username: "api-token", Role: store.RoleAdmin, Service: true}

type contextKey struct{}

// WithPrincipal returns a context carrying p.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, contextKey{}, p)
}

// FromContext returns the principal of a request; ok is false for an
// unauthenticated one.
func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(contextKey{}).(Principal)
	return p, ok
}

// bcryptCost makes a hash take tens of milliseconds, which slows guessing
// without slowing sign-in noticeably.
const bcryptCost = 12

// MaxPasswordBytes is the longest password bcrypt takes: it would ignore
// what follows, so a longer one is refused rather than cut short.
const MaxPasswordBytes = 72

// commonPasswords are refused whatever the minimum length: they are the
// first anyone tries against a known username such as "admin".
var commonPasswords = []string{"admin", "administrator", "password", "changeme", "kubeat", "12345678"}

// ValidatePassword says what is wrong with password as the password of
// username, or returns nil. A password has at least minLen characters and
// at most MaxPasswordBytes bytes, and is neither the username nor one of
// the common passwords, whatever the case of its letters. Every password
// is held to it: the first account's, a new user's, a changed one, and
// the administrator's from UPTIME_ADMIN_PASSWORD.
func ValidatePassword(username, password string, minLen int) error {
	minLen = max(minLen, 1) // never empty
	switch {
	case utf8.RuneCountInString(password) < minLen:
		return fmt.Errorf("the password must be at least %d characters", minLen)
	case len(password) > MaxPasswordBytes:
		return fmt.Errorf("the password must be at most %d bytes", MaxPasswordBytes)
	case strings.EqualFold(password, username):
		return errors.New("the password must not be the username")
	case slices.ContainsFunc(commonPasswords, func(common string) bool { return strings.EqualFold(common, password) }):
		return errors.New("the password is one of the first that are guessed; choose another")
	}
	return nil
}

// HashPassword returns the stored form of a password.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	return string(hash), err
}

// dummyHash is what a password is compared with when there is nothing to
// compare it with: a hash of random bytes, made with the cost of the real
// ones, so that refusing a name without an account takes as long as
// refusing a wrong password, and the time of the answer tells neither.
var dummyHash = sync.OnceValue(func() []byte {
	secret := make([]byte, 32)
	_, _ = rand.Read(secret)
	hash, _ := bcrypt.GenerateFromPassword(secret, bcryptCost)
	return hash
})

// PrepareDummy makes the dummy hash now, at startup, instead of during the
// first sign-in of an unknown name, which would take twice as long.
func PrepareDummy() { dummyHash() }

// CheckPassword reports whether password is the one hash was made from.
// Without a hash (a name with no account, or an account whose password is
// kept elsewhere) it compares with the dummy hash all the same and reports
// false: answering sooner would tell such names apart.
func CheckPassword(hash, password string) bool {
	if hash == "" {
		_ = bcrypt.CompareHashAndPassword(dummyHash(), []byte(password))
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// NewToken returns a session token for a browser and the hash under which
// it is stored.
func NewToken() (token, hash string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, HashToken(token), nil
}

// HashToken returns the stored form of a token.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Throttle slows down password guessing: after too many failures for a key
// (a username) within the lockout period, attempts for it are refused.
//
// An attempt counts as a failure from the moment it begins, before its
// password is checked: parallel attempts cannot all be tried before the
// first failure is recorded. A success forgets the key (Reset), and an
// attempt that turns out not to be a guess is taken back (Undo).
type Throttle struct {
	MaxFailures int
	Lockout     time.Duration
	// MaxKeys bounds how many keys that are not locked are tracked at
	// once; zero means no bound. A new key that finds them all taken is
	// not refused: it makes room. The keys whose attempts have all aged
	// out go first, then the key with the fewest attempts, of those the
	// one tried longest ago. A locked key is never pushed out, and takes
	// no room either: were it counted, names locked on purpose, each for
	// MaxFailures checked passwords, could fill the bound and leave a name
	// being guessed at the only one to push out, for a single attempt
	// each time. A flood of made-up names, each tried once, thus pushes
	// out its own names: it neither grows the memory without end nor
	// locks anyone out, and pushing out a name with k failures takes about
	// k attempts at each of MaxKeys other names.
	MaxKeys int
	// Logger, if set, is told about refused attempts, about keys forgotten
	// to make room and about keys tracked beyond MaxKeys, at most once a
	// minute.
	Logger *slog.Logger

	mu   sync.Mutex
	keys map[string]*tracked
	// byCount holds the keys by their number of attempts, each list from
	// the key tried longest ago to the one tried last; the locked keys are
	// those with MaxFailures. An empty list is dropped, so that finding the
	// fewest attempts looks at few lists.
	byCount map[int]*list.List
	// What the next report tells, and when the last one was made.
	refused, forgotten, forgottenRepeated, overBound int
	refusedName                                      string
	reported                                         time.Time
}

// tracked is a key's attempts within the lockout period, oldest first, the
// labels First was told about, and its place in the lists: under how many
// attempts it is listed, and where.
type tracked struct {
	key      string
	attempts []time.Time
	labels   []string
	listed   int
	at       *list.Element
}

// reportEvery bounds how often the throttle logs: a flood of attempts must
// not become a flood of log lines.
const reportEvery = time.Minute

// Begin reserves an attempt for key at now. It returns the attempt's
// number within the lockout period, 1 for the first, and true; or, when
// the key has no attempt left, how long it is still refused, and false.
func (t *Throttle) Begin(key string, now time.Time) (attempt int, locked time.Duration, ok bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	k := t.prune(key, now)
	if k != nil && len(k.attempts) >= t.MaxFailures {
		if t.refused == 0 {
			t.refusedName = key
		}
		t.refused++
		t.report(now, false)
		return 0, k.attempts[0].Add(t.Lockout).Sub(now), false
	}
	if k == nil {
		if t.MaxKeys > 0 && len(t.keys) >= t.MaxKeys {
			t.makeRoom(now)
		}
		if t.keys == nil {
			t.keys = make(map[string]*tracked)
		}
		k = &tracked{key: key}
		t.keys[key] = k
	}
	k.attempts = append(k.attempts, now)
	t.touch(k)
	return len(k.attempts), 0, true
}

// Undo takes back the attempt for key that began at at: one that was not
// a guess, because the password could not be checked, or was right for an
// account that is disabled.
func (t *Throttle) Undo(key string, at time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	k := t.keys[key]
	if k == nil {
		return
	}
	if i := slices.IndexFunc(k.attempts, at.Equal); i >= 0 {
		k.attempts = slices.Delete(k.attempts, i, i+1)
		t.relist(k)
	}
}

// Reset forgets the attempts of key, after a successful one.
func (t *Throttle) Reset(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if k := t.keys[key]; k != nil {
		t.forget(k)
	}
}

// First reports whether label is new to key within its lockout period,
// and remembers it from now on, until the key is forgotten: when its
// attempts have aged out, one succeeded, or it made room for another. The
// audit log asks it about the first refusal of each kind a name gets, so
// as not to write one a guess. A key that is not tracked remembers
// nothing, and every label is new to it.
func (t *Throttle) First(key, label string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	k := t.keys[key]
	if k == nil {
		return true
	}
	if slices.Contains(k.labels, label) {
		return false
	}
	k.labels = append(k.labels, label)
	return true
}

// Sweep forgets the keys whose attempts have all aged out. Begin does that
// only for the key it is asked about, and for the oldest keys while the
// throttle is full, so a name tried once and never again would otherwise
// be kept until the process ends. Sweep also logs what is left to report,
// and returns how many keys are still tracked.
func (t *Throttle) Sweep(now time.Time) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, k := range t.keys {
		if now.Sub(latest(k)) >= t.Lockout {
			t.forget(k)
		}
	}
	t.report(now, true)
	return len(t.keys)
}

// prune drops the attempts of key older than the lockout, and the key once
// none is left; it returns the key, or nil if it is not tracked. Call with
// mu held.
func (t *Throttle) prune(key string, now time.Time) *tracked {
	k := t.keys[key]
	if k == nil {
		return nil
	}
	aged := 0
	for aged < len(k.attempts) && now.Sub(k.attempts[aged]) >= t.Lockout {
		aged++
	}
	if aged == 0 {
		return k
	}
	k.attempts = k.attempts[aged:]
	t.relist(k)
	if len(k.attempts) == 0 {
		return nil
	}
	return k
}

// makeRoom makes room for one more key: it forgets the keys whose attempts
// have all aged out, which are at the front of the lists, and then, while
// MaxKeys keys that are not locked are left, the one with the fewest
// attempts that was tried longest ago. Locked keys stay: when they fill
// the bound, the new key is tracked beyond it. Call with mu held.
func (t *Throttle) makeRoom(now time.Time) {
	for _, l := range t.byCount {
		for e := l.Front(); e != nil && now.Sub(latest(e.Value.(*tracked))) >= t.Lockout; e = l.Front() {
			t.forget(e.Value.(*tracked))
		}
	}
	for len(t.keys)-t.lockedKeys() >= t.MaxKeys {
		k := t.fewest()
		if k == nil {
			break
		}
		if len(k.attempts) > 1 {
			t.forgottenRepeated++
		}
		t.forget(k)
		t.forgotten++
	}
	if len(t.keys) >= t.MaxKeys {
		t.overBound++
	}
	t.report(now, false)
}

// lockedKeys returns how many keys are locked: those with MaxFailures
// attempts, which no key has more of. Call with mu held.
func (t *Throttle) lockedKeys() int {
	if l := t.byCount[t.MaxFailures]; l != nil {
		return l.Len()
	}
	return 0
}

// fewest returns the key with the fewest attempts that is not locked, of
// those the one tried longest ago, or nil if every key is locked. Call
// with mu held.
func (t *Throttle) fewest() *tracked {
	least := 0
	for n := range t.byCount {
		if n < t.MaxFailures && (least == 0 || n < least) {
			least = n
		}
	}
	if least == 0 {
		return nil
	}
	return t.byCount[least].Front().Value.(*tracked)
}

// touch puts k at the end of the list for its number of attempts, as the
// key tried last. Call with mu held.
func (t *Throttle) touch(k *tracked) {
	t.unlist(k)
	n := len(k.attempts)
	l := t.byCount[n]
	if l == nil {
		if t.byCount == nil {
			t.byCount = make(map[int]*list.List)
		}
		l = list.New()
		t.byCount[n] = l
	}
	k.listed, k.at = n, l.PushBack(k)
}

// relist follows k's attempts after some were dropped: it forgets k when
// none is left, and otherwise moves it to the list for their number. It
// goes to the end of that list, though its last attempt is no newer, which
// keeps it a little longer than its due. Call with mu held.
func (t *Throttle) relist(k *tracked) {
	if len(k.attempts) == 0 {
		t.forget(k)
		return
	}
	t.touch(k)
}

// unlist takes k out of its list, and drops the list once it is empty.
// Call with mu held.
func (t *Throttle) unlist(k *tracked) {
	if k.at == nil {
		return
	}
	l := t.byCount[k.listed]
	l.Remove(k.at)
	if l.Len() == 0 {
		delete(t.byCount, k.listed)
	}
	k.listed, k.at = 0, nil
}

// forget stops tracking k. Call with mu held.
func (t *Throttle) forget(k *tracked) {
	t.unlist(k)
	delete(t.keys, k.key)
}

// report logs the attempts refused, the keys forgotten to make room and
// those tracked beyond MaxKeys since the last report, if the last report
// was a minute ago or more, or in any case with flush. Call with mu held.
func (t *Throttle) report(now time.Time, flush bool) {
	if (t.refused == 0 && t.forgotten == 0 && t.overBound == 0) || (!flush && now.Sub(t.reported) < reportEvery) {
		return
	}
	if t.Logger != nil && t.refused > 0 {
		t.Logger.Warn("sign-in attempts refused: the name is locked after too many failed attempts",
			"refused", t.refused, "name", t.refusedName)
	}
	if t.Logger != nil && t.forgotten > 0 {
		t.Logger.Warn("the sign-in throttle is full, perhaps from a flood of made-up usernames: the names with the fewest recent attempts are forgotten to make room",
			"forgotten", t.forgotten, "tried_more_than_once", t.forgottenRepeated, "max", t.MaxKeys)
	}
	if t.Logger != nil && t.overBound > 0 {
		t.Logger.Warn("locked names fill the sign-in throttle: they are kept, and new names are tracked beyond its bound until the locks expire",
			"admitted", t.overBound, "locked", t.lockedKeys(), "max", t.MaxKeys)
	}
	t.refused, t.forgotten, t.forgottenRepeated, t.overBound, t.refusedName = 0, 0, 0, 0, ""
	t.reported = now
}

// latest returns when k was last tried.
func latest(k *tracked) time.Time {
	return slices.MaxFunc(k.attempts, time.Time.Compare)
}
