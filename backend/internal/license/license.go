// Package license reads the Kubeat Enterprise license: a file signed with
// Kubeat's offline key that names the customer, the features it turns on
// and when it ends. Without one, Kubeat is the community edition.
//
// A license only ever turns features on. Monitoring, notifications and
// status pages work with or without one, and a license that ends or is
// removed never widens anyone's access: what it turned on is refused from
// then on, what it restricted stays restricted. Nothing is sent anywhere
// to check it.
package license

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
)

// Feature is something a license turns on.
type Feature string

const (
	// SSO is signing in through LDAP or an OpenID Connect provider.
	SSO Feature = "sso"
	// ScopedAccess is limiting a user to some namespaces.
	ScopedAccess Feature = "scoped_access"
	// Audit is reading the audit log. It is written either way.
	Audit Feature = "audit"
	// StatusPages is having more than CommunityStatusPages status pages.
	StatusPages Feature = "status_pages"
)

// Features are all the features, in the order the UI lists them.
var Features = []Feature{SSO, ScopedAccess, Audit, StatusPages}

// CommunityStatusPages is how many status pages the community edition may
// create. Pages beyond it, made under a license that has since ended, stay
// up and can be edited.
const CommunityStatusPages = 1

// WarnBefore is how long before its end a license is reported as ending.
const WarnBefore = 30 * 24 * time.Hour

// prefix starts every license and is part of what is signed.
const prefix = "kubeat-license-v1."

// Claims are what a license says.
type Claims struct {
	ID        string    `json:"id"`
	Customer  string    `json:"customer"`
	Features  []Feature `json:"features"`
	IssuedAt  time.Time `json:"issued_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Parse checks the signature of a license against key and returns what it
// says. It does not look at the dates.
func Parse(data []byte, key ed25519.PublicKey) (Claims, error) {
	text := strings.TrimSpace(string(data))
	rest, ok := strings.CutPrefix(text, prefix)
	if !ok {
		return Claims{}, errors.New("not a Kubeat license")
	}
	body, sig, ok := strings.Cut(rest, ".")
	if !ok {
		return Claims{}, errors.New("the license has no signature")
	}
	signature, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !ed25519.Verify(key, []byte(prefix+body), signature) {
		return Claims{}, errors.New("the license signature is not valid")
	}
	payload, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return Claims{}, errors.New("the license is damaged")
	}
	var c Claims
	if err := json.Unmarshal(payload, &c); err != nil {
		return Claims{}, errors.New("the license is damaged")
	}
	if c.ExpiresAt.IsZero() {
		return Claims{}, errors.New("the license has no end date")
	}
	return c, nil
}

// State is where a license stands.
type State string

const (
	// Community: no license is configured.
	Community State = "community"
	// Active: the license is valid.
	Active State = "active"
	// Expiring: valid, ending within WarnBefore.
	Expiring State = "expiring"
	// Expired: the license ended; its features are off.
	Expired State = "expired"
	// Invalid: a license is configured but cannot be used; Problem says why.
	Invalid State = "invalid"
)

// Status is what the admin UI shows.
type Status struct {
	State     State
	Problem   string
	ID        string
	Customer  string
	Features  []Feature
	IssuedAt  time.Time
	ExpiresAt time.Time
}

// Manager holds the license read from a file, and reads it again when the
// file changes: a renewed Secret takes effect without a restart.
type Manager struct {
	path string
	key  ed25519.PublicKey
	now  func() time.Time

	mu      sync.RWMutex
	claims  *Claims
	problem string
	stamp   string // size and time of the file last read
}

// NewManager reads the license at path; an empty path is the community
// edition. A file that cannot be used is reported by Status, not here:
// Kubeat runs either way.
func NewManager(path string, key ed25519.PublicKey) *Manager {
	m := &Manager{path: path, key: key, now: time.Now}
	m.Reload()
	return m
}

// Reload reads the file again if it changed, and reports whether it did.
func (m *Manager) Reload() bool {
	if m.path == "" {
		return false
	}
	stamp := "unreadable"
	if fi, err := os.Stat(m.path); err == nil {
		stamp = fmt.Sprint(fi.Size(), fi.ModTime().UnixNano())
	}
	m.mu.RLock()
	same := stamp == m.stamp
	m.mu.RUnlock()
	if same {
		return false
	}

	var claims *Claims
	problem := ""
	data, err := os.ReadFile(m.path)
	if err != nil {
		problem = "the license file cannot be read: " + err.Error()
	} else if c, err := Parse(data, m.key); err != nil {
		problem = err.Error()
	} else {
		claims = &c
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stamp, m.claims, m.problem = stamp, claims, problem
	return true
}

// Status says where the license stands now.
func (m *Manager) Status() Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	switch {
	case m.path == "":
		return Status{State: Community}
	case m.claims == nil:
		return Status{State: Invalid, Problem: m.problem}
	}
	c := m.claims
	s := Status{State: Active, ID: c.ID, Customer: c.Customer, Features: slices.Clone(c.Features), IssuedAt: c.IssuedAt, ExpiresAt: c.ExpiresAt}
	now := m.now()
	switch {
	case !now.Before(c.ExpiresAt):
		s.State = Expired
	case c.ExpiresAt.Sub(now) <= WarnBefore:
		s.State = Expiring
	}
	return s
}

// Allows reports whether the license turns f on now.
func (m *Manager) Allows(f Feature) bool {
	if m == nil {
		return false
	}
	s := m.Status()
	return (s.State == Active || s.State == Expiring) && slices.Contains(s.Features, f)
}
