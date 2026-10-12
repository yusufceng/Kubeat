// Package auth says who is making a request and what they may do.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"slices"
	"sync"
	"time"

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

// HashPassword returns the stored form of a password.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	return string(hash), err
}

// CheckPassword reports whether password is the one hash was made from.
func CheckPassword(hash, password string) bool {
	return hash != "" && bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
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
type Throttle struct {
	MaxFailures int
	Lockout     time.Duration

	mu       sync.Mutex
	failures map[string][]time.Time
}

// LockedFor returns how long attempts for key are still refused; 0 if
// they are not.
func (t *Throttle) LockedFor(key string, now time.Time) time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	recent := t.prune(key, now)
	if len(recent) < t.MaxFailures {
		return 0
	}
	return recent[0].Add(t.Lockout).Sub(now)
}

// Fail records a failed attempt for key.
func (t *Throttle) Fail(key string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.failures == nil {
		t.failures = make(map[string][]time.Time)
	}
	t.failures[key] = append(t.prune(key, now), now)
}

// Reset forgets the failures of key, after a successful attempt.
func (t *Throttle) Reset(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.failures, key)
}

// prune drops the failures of key older than the lockout; call with mu held.
func (t *Throttle) prune(key string, now time.Time) []time.Time {
	recent := t.failures[key]
	for len(recent) > 0 && now.Sub(recent[0]) >= t.Lockout {
		recent = recent[1:]
	}
	if t.failures != nil {
		if len(recent) == 0 {
			delete(t.failures, key)
		} else {
			t.failures[key] = recent
		}
	}
	return recent
}
