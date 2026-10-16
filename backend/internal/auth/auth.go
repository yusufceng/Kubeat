// Package auth says who is making a request and what they may do.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
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
	// MaxKeys bounds how many keys are tracked at once; zero means no
	// bound. While all are taken, a key not tracked yet is refused as if
	// locked: a flood of made-up names can neither grow the memory without
	// end nor push out the failures of the names it is aimed at.
	MaxKeys int

	mu       sync.Mutex
	attempts map[string][]time.Time
}

// Begin reserves an attempt for key at now. It returns the attempt's
// number within the lockout period, 1 for the first, and true; or, when
// the key has no attempt left, how long it is still refused, and false.
func (t *Throttle) Begin(key string, now time.Time) (attempt int, locked time.Duration, ok bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	recent := t.prune(key, now)
	if len(recent) >= t.MaxFailures {
		return 0, recent[0].Add(t.Lockout).Sub(now), false
	}
	if len(recent) == 0 && t.MaxKeys > 0 && len(t.attempts) >= t.MaxKeys {
		// Room comes back as Sweep forgets keys, within a lockout period.
		return 0, t.Lockout, false
	}
	if t.attempts == nil {
		t.attempts = make(map[string][]time.Time)
	}
	t.attempts[key] = append(recent, now)
	return len(recent) + 1, 0, true
}

// Undo takes back the attempt for key that began at at: one that was not
// a guess, because the password could not be checked, or was right for an
// account that may not sign in.
func (t *Throttle) Undo(key string, at time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	recent := t.attempts[key]
	if i := slices.IndexFunc(recent, at.Equal); i >= 0 {
		recent = slices.Delete(recent, i, i+1)
	}
	if len(recent) == 0 {
		delete(t.attempts, key)
	} else {
		t.attempts[key] = recent
	}
}

// Reset forgets the attempts of key, after a successful one.
func (t *Throttle) Reset(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.attempts, key)
}

// Sweep forgets the keys whose attempts have all aged out. Begin does that
// only for the key it is asked about, so a name tried once and never again
// would otherwise be kept until the process ends. Sweep returns how many
// keys are still tracked.
func (t *Throttle) Sweep(now time.Time) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	for key, recent := range t.attempts {
		if len(recent) == 0 || now.Sub(slices.MaxFunc(recent, time.Time.Compare)) >= t.Lockout {
			delete(t.attempts, key)
		}
	}
	return len(t.attempts)
}

// prune drops the attempts of key older than the lockout; call with mu held.
func (t *Throttle) prune(key string, now time.Time) []time.Time {
	recent := t.attempts[key]
	for len(recent) > 0 && now.Sub(recent[0]) >= t.Lockout {
		recent = recent[1:]
	}
	if len(recent) == 0 {
		delete(t.attempts, key)
	} else {
		t.attempts[key] = recent
	}
	return recent
}
