package api

import (
	"net/http"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/license"
)

// License says which Kubeat Enterprise features are on (see the license
// package); nil is the community edition.
type License interface {
	Allows(license.Feature) bool
	Status() license.Status
}

// licensed reports whether the license turns f on.
func (h *handler) licensed(f license.Feature) bool {
	return h.opts.License != nil && h.opts.License.Allows(f)
}

// licenseErrorBody is the answer to what needs a feature the license does
// not turn on: 402, with the feature, so the UI can say so instead of
// showing a permission error.
type licenseErrorBody struct {
	Error   string          `json:"error"`
	Feature license.Feature `json:"feature"`
}

// requireLicense answers 402 and returns false unless the license turns f on.
func (h *handler) requireLicense(w http.ResponseWriter, f license.Feature, what string) bool {
	if h.licensed(f) {
		return true
	}
	writeJSON(w, http.StatusPaymentRequired, licenseErrorBody{
		Error:   what + " needs a Kubeat Enterprise license with the " + string(f) + " feature",
		Feature: f,
	})
	return false
}

// ldap is the directory, while the license allows signing in through it.
func (h *handler) ldap() LDAP {
	if !h.licensed(license.SSO) {
		return nil
	}
	return h.opts.LDAP
}

// oidc is the OpenID Connect provider, while the license allows it.
func (h *handler) oidc() OIDC {
	if !h.licensed(license.SSO) {
		return nil
	}
	return h.opts.OIDC
}

type licenseResponse struct {
	State    license.State `json:"state"`
	Problem  string        `json:"problem,omitempty"`
	ID       string        `json:"id,omitempty"`
	Customer string        `json:"customer,omitempty"`
	// Features are those on now: none once the license ended.
	Features  []license.Feature `json:"features"`
	IssuedAt  *time.Time        `json:"issued_at,omitempty"`
	ExpiresAt *time.Time        `json:"expires_at,omitempty"`
	// CommunityStatusPages is how many status pages may be made without
	// the status_pages feature.
	CommunityStatusPages int `json:"community_status_pages"`
}

// getLicense tells any signed-in user which features are on, so that the
// UI can mark what needs a license; who the license is for and its id only
// administrators see.
func (h *handler) getLicense(w http.ResponseWriter, r *http.Request) {
	s := license.Status{State: license.Community}
	if h.opts.License != nil {
		s = h.opts.License.Status()
	}
	resp := licenseResponse{State: s.State, Features: []license.Feature{}, CommunityStatusPages: license.CommunityStatusPages}
	for _, f := range license.Features {
		if h.licensed(f) {
			resp.Features = append(resp.Features, f)
		}
	}
	if !s.ExpiresAt.IsZero() {
		resp.ExpiresAt = &s.ExpiresAt
	}
	if principal(r).IsAdmin() {
		resp.Problem, resp.ID, resp.Customer = s.Problem, s.ID, s.Customer
		if !s.IssuedAt.IsZero() {
			resp.IssuedAt = &s.IssuedAt
		}
	}
	writeJSON(w, http.StatusOK, resp)
}
