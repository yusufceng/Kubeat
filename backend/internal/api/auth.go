package api

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/auth"
	"github.com/yusufceng/Kubeat/backend/internal/ldapauth"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// LDAP checks a username and password against a directory.
type LDAP interface {
	URL() string
	Authenticate(ctx context.Context, username, password string) (ldapauth.Identity, error)
}

const (
	maxUsernameLength = 64
	maxNameLength     = 100
	// touchEvery bounds how often a session's last-seen time is written.
	touchEvery = time.Minute
)

// A username is what people type and directories hold: letters, digits and
// a few separators, no spaces.
var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@-]*$`)

// openPath reports whether a request needs no credentials: health and
// metrics for the cluster, push reports and public status pages, which
// carry their own secrets or are public by design, and signing in itself.
func openPath(r *http.Request) bool {
	path := r.URL.Path
	if token, isPush := strings.CutPrefix(path, pushPath); isPush {
		// A push path is the prefix and a token, nothing below or beside it.
		return !strings.Contains(token, "/")
	}
	switch {
	case path == "/api/v1/health", path == "/metrics",
		path == "/api/v1/auth/login", path == "/api/v1/auth/setup", path == "/api/v1/auth/providers",
		strings.HasPrefix(path, "/api/v1/badges/"):
		return true
	case strings.HasPrefix(path, "/api/v1/status-pages/by-slug/") && strings.HasSuffix(path, "/view"):
		return true
	}
	return false
}

func unauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="kubeat"`)
	writeError(w, http.StatusUnauthorized, msg)
}

func forbidden(w http.ResponseWriter) {
	writeError(w, http.StatusForbidden, "you may not do this")
}

// authenticate decides who a request comes from: the service token, a
// user's session, or nobody, and lets it through to open paths only then.
func (h *handler) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if openPath(r) {
			next.ServeHTTP(w, r)
			return
		}
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || token == "" {
			unauthorized(w, "sign in or present the API token")
			return
		}
		if h.opts.Token != "" && subtle.ConstantTimeCompare([]byte(token), []byte(h.opts.Token)) == 1 {
			next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), auth.ServicePrincipal)))
			return
		}

		now := time.Now().UTC()
		sess, err := h.opts.Store.GetSession(r.Context(), auth.HashToken(token), now)
		if errors.Is(err, store.ErrNotFound) {
			unauthorized(w, "the session is not valid any more; sign in again")
			return
		}
		if err != nil {
			h.internalError(w, r, err)
			return
		}
		user, err := h.opts.Store.GetUser(r.Context(), sess.UserID)
		if err != nil || !user.Enabled {
			unauthorized(w, "the account is disabled or gone")
			return
		}
		if now.Sub(sess.LastSeenAt) > touchEvery {
			_ = h.opts.Store.TouchSession(r.Context(), sess.TokenHash, now)
		}
		next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), auth.FromUser(user))))
	})
}

// principal returns who makes the request. The middleware put it there for
// every path that is not open; handlers of open paths never ask.
func principal(r *http.Request) auth.Principal {
	p, _ := auth.FromContext(r.Context())
	return p
}

// audit records what the request's principal did; a failure to record is
// logged, not surfaced: the deed is done.
func (h *handler) audit(r *http.Request, action, objectType string, objectID int64, summary string) {
	p := principal(r)
	err := h.opts.Store.AppendAudit(context.WithoutCancel(r.Context()), store.AuditEntry{
		At: time.Now().UTC(), UserID: p.UserID, Username: p.Username, Action: action, ObjectType: objectType, ObjectID: objectID, Summary: summary,
	})
	if err != nil {
		h.opts.Logger.Error("audit entry lost", "action", action, "object", objectType, "error", err)
	}
}

// requireAdmin answers 403 and returns false unless the principal may do
// everything.
func requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if !principal(r).IsAdmin() {
		forbidden(w)
		return false
	}
	return true
}

type userResponse struct {
	ID          int64      `json:"id"`
	Username    string     `json:"username"`
	DisplayName string     `json:"display_name"`
	Email       string     `json:"email"`
	Source      string     `json:"source"`
	Role        store.Role `json:"role"`
	// Namespaces is empty for an admin; ["*"] means every namespace.
	Namespaces  []string   `json:"namespaces"`
	Enabled     bool       `json:"enabled"`
	CreatedAt   time.Time  `json:"created_at"`
	LastLoginAt *time.Time `json:"last_login_at"`
}

func toUserResponse(u store.User) userResponse {
	resp := userResponse{
		ID: u.ID, Username: u.Username, DisplayName: u.DisplayName, Email: u.Email, Source: u.Source,
		Role: u.Role, Namespaces: u.Namespaces, Enabled: u.Enabled, CreatedAt: u.CreatedAt,
	}
	if resp.Namespaces == nil {
		resp.Namespaces = []string{}
	}
	if !u.LastLoginAt.IsZero() {
		resp.LastLoginAt = &u.LastLoginAt
	}
	return resp
}

// validUsername, validRole and scopeNamespaces validate what a user or a
// group mapping is given.
func validUsername(name string) bool {
	return name != "" && len(name) <= maxUsernameLength && usernamePattern.MatchString(name)
}

func validRole(role store.Role) bool { return slices.Contains(store.Roles, role) }

// scopeNamespaces validates the namespaces of an editor or viewer: names,
// or "*" alone for all of them.
func scopeNamespaces(role store.Role, namespaces []string) ([]string, error) {
	if role == store.RoleAdmin {
		return nil, nil
	}
	if len(namespaces) == 1 && strings.TrimSpace(namespaces[0]) == store.AllNamespaces {
		return []string{store.AllNamespaces}, nil
	}
	out, err := channelNamespaces(namespaces)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, errors.New("namespaces: an editor or viewer needs at least one namespace, or \"*\" for all")
	}
	return out, nil
}

func (h *handler) validPassword(password string) error {
	if len(password) < h.opts.PasswordMinLength {
		return fmt.Errorf("the password must be at least %d characters", h.opts.PasswordMinLength)
	}
	if len(password) > maxHeaderValue {
		return errors.New("the password is too long")
	}
	return nil
}

// newSession signs the user in and returns the token for their browser.
func (h *handler) newSession(ctx context.Context, u store.User) (token string, expires time.Time, err error) {
	token, hash, err := auth.NewToken()
	if err != nil {
		return "", time.Time{}, err
	}
	now := time.Now().UTC()
	expires = now.Add(h.opts.SessionTTL)
	if err := h.opts.Store.CreateSession(ctx, store.Session{TokenHash: hash, UserID: u.ID, CreatedAt: now, ExpiresAt: expires, LastSeenAt: now}); err != nil {
		return "", time.Time{}, err
	}
	u.LastLoginAt = now
	if _, err := h.opts.Store.UpdateUser(ctx, u); err != nil {
		h.opts.Logger.Warn("last login time not recorded", "user", u.Username, "error", err)
	}
	return token, expires, nil
}

type sessionResponse struct {
	Token     string       `json:"token"`
	ExpiresAt time.Time    `json:"expires_at"`
	User      userResponse `json:"user"`
}

type providersResponse struct {
	// SetupNeeded is true while there is no account at all: the first
	// visitor creates the administrator.
	SetupNeeded bool `json:"setup_needed"`
	// LDAP says whether directory accounts can sign in.
	LDAP bool `json:"ldap"`
}

// authProviders tells the sign-in page what it can offer.
func (h *handler) authProviders(w http.ResponseWriter, r *http.Request) {
	n, err := h.opts.Store.CountUsers(r.Context())
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, providersResponse{SetupNeeded: n == 0, LDAP: h.opts.LDAP != nil})
}

type setupRequest struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
}

// authSetup creates the first administrator. It works only while there is
// no account: afterwards accounts are made by administrators.
func (h *handler) authSetup(w http.ResponseWriter, r *http.Request) {
	var req setupRequest
	if !decodeBody(w, r, &req) {
		return
	}
	n, err := h.opts.Store.CountUsers(r.Context())
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	if n > 0 {
		writeError(w, http.StatusConflict, "an account exists already; sign in instead")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if !validUsername(req.Username) {
		writeError(w, http.StatusBadRequest, "username must be letters, digits and . _ @ -, without spaces")
		return
	}
	if err := h.validPassword(req.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	u, err := h.opts.Store.CreateUser(r.Context(), store.User{
		Username: req.Username, DisplayName: strings.TrimSpace(req.DisplayName), Source: store.UserLocal, PasswordHash: hash, Role: store.RoleAdmin, Enabled: true,
	})
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "an account exists already; sign in instead")
		return
	}
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	h.signIn(w, r, u, "setup")
}

// signIn answers a successful sign-in with a session.
func (h *handler) signIn(w http.ResponseWriter, r *http.Request, u store.User, action string) {
	token, expires, err := h.newSession(r.Context(), u)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	r = r.WithContext(auth.WithPrincipal(r.Context(), auth.FromUser(u)))
	h.audit(r, action, "user", u.ID, u.Username)
	writeJSON(w, http.StatusOK, sessionResponse{Token: token, ExpiresAt: expires, User: toUserResponse(u)})
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// authLogin signs a user in with a password: a local account's, or, for a
// name no local account has, the directory's.
func (h *handler) authLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !decodeBody(w, r, &req) {
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if !validUsername(req.Username) || req.Password == "" {
		unauthorized(w, "invalid username or password")
		return
	}
	key := strings.ToLower(req.Username)
	now := time.Now().UTC()
	if locked := h.opts.Throttle.LockedFor(key, now); locked > 0 {
		writeError(w, http.StatusTooManyRequests, fmt.Sprintf("too many failed attempts; try again in %d minutes", int(locked.Minutes())+1))
		return
	}

	user, err := h.opts.Store.GetUserByUsername(r.Context(), req.Username)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		h.internalError(w, r, err)
		return
	}
	known := err == nil
	switch {
	case known && user.Source == store.UserLocal:
		if !auth.CheckPassword(user.PasswordHash, req.Password) {
			h.loginFailed(w, r, key, req.Username, "wrong password")
			return
		}
	case h.opts.LDAP != nil:
		identity, err := h.opts.LDAP.Authenticate(r.Context(), req.Username, req.Password)
		if errors.Is(err, ldapauth.ErrInvalidCredentials) {
			h.loginFailed(w, r, key, req.Username, "rejected by the directory")
			return
		}
		if err != nil {
			h.opts.Logger.Error("ldap sign-in failed", "user", req.Username, "error", err)
			writeError(w, http.StatusBadGateway, "the directory could not be asked; try again later")
			return
		}
		if user, err = h.ldapUser(r.Context(), user, known, identity); err != nil {
			var denied noRoleError
			if errors.As(err, &denied) {
				h.audit(r, "login_denied", "user", user.ID, req.Username+": "+err.Error())
				writeError(w, http.StatusForbidden, err.Error())
				return
			}
			h.internalError(w, r, err)
			return
		}
	default:
		h.loginFailed(w, r, key, req.Username, "unknown user")
		return
	}
	if !user.Enabled {
		writeError(w, http.StatusForbidden, "the account is disabled")
		return
	}
	h.opts.Throttle.Reset(key)
	h.signIn(w, r, user, "login")
}

// loginFailed counts the failure and answers as for any wrong password:
// nothing in the answer tells a username apart from a password.
func (h *handler) loginFailed(w http.ResponseWriter, r *http.Request, key, username, why string) {
	h.opts.Throttle.Fail(key, time.Now().UTC())
	h.opts.Logger.Info("sign-in refused", "user", username, "reason", why)
	_ = h.opts.Store.AppendAudit(r.Context(), store.AuditEntry{At: time.Now().UTC(), Username: username, Action: "login_failed", Summary: why})
	unauthorized(w, "invalid username or password")
}

// noRoleError means the directory knows the user but no group of theirs
// gives them a role here.
type noRoleError struct{ groups int }

func (e noRoleError) Error() string {
	return fmt.Sprintf("none of your %d directory groups has a role here; ask an administrator", e.groups)
}

// ldapUser creates or updates the local record of a directory user from
// what the directory said. The role comes from the groups on every sign-in,
// so a change in the directory takes effect the next time.
func (h *handler) ldapUser(ctx context.Context, current store.User, known bool, id ldapauth.Identity) (store.User, error) {
	mappings, err := h.opts.Store.ListLDAPGroupRoles(ctx)
	if err != nil {
		return store.User{}, err
	}
	role, namespaces, found := h.opts.LDAPDefaultRole, h.opts.LDAPDefaultNamespaces, h.opts.LDAPDefaultRole != ""
	for _, m := range mappings {
		for _, g := range id.Groups {
			if !ldapauth.SameDN(m.GroupDN, g) {
				continue
			}
			// The strongest role wins; namespaces add up.
			switch {
			case !found || rank(m.Role) > rank(role):
				role, namespaces, found = m.Role, m.Namespaces, true
			case m.Role == role:
				namespaces = mergeNamespaces(namespaces, m.Namespaces)
			}
		}
	}
	if !found {
		return current, noRoleError{len(id.Groups)}
	}
	if role != store.RoleAdmin && len(namespaces) == 0 {
		// An editor or viewer without namespaces could see nothing.
		return current, noRoleError{len(id.Groups)}
	}

	u := current
	u.Username, u.Source, u.Role, u.Namespaces = id.Username, store.UserLDAP, role, namespaces
	if id.DisplayName != "" {
		u.DisplayName = id.DisplayName
	}
	if id.Email != "" {
		u.Email = id.Email
	}
	if !known {
		u.Enabled = true
		return h.opts.Store.CreateUser(ctx, u)
	}
	return h.opts.Store.UpdateUser(ctx, u)
}

func rank(role store.Role) int {
	switch role {
	case store.RoleAdmin:
		return 3
	case store.RoleEditor:
		return 2
	default:
		return 1
	}
}

// mergeNamespaces joins two scopes; "*" swallows the rest.
func mergeNamespaces(a, b []string) []string {
	if slices.Contains(a, store.AllNamespaces) || slices.Contains(b, store.AllNamespaces) {
		return []string{store.AllNamespaces}
	}
	out := slices.Clone(a)
	for _, ns := range b {
		if !slices.Contains(out, ns) {
			out = append(out, ns)
		}
	}
	slices.Sort(out)
	return out
}

// authLogout ends the session the request was made with.
func (h *handler) authLogout(w http.ResponseWriter, r *http.Request) {
	token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if p := principal(r); !p.Service {
		if err := h.opts.Store.DeleteSession(r.Context(), auth.HashToken(token)); err != nil {
			h.internalError(w, r, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

type meResponse struct {
	userResponse
	// Service is true for the API token, which has no account.
	Service bool `json:"service"`
	// Permissions, so that the UI shows only what the user may do.
	Admin   bool `json:"admin"`
	SeesAll bool `json:"sees_all"`
	CanEdit bool `json:"can_edit"`
}

func (h *handler) authMe(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	resp := meResponse{Service: p.Service, Admin: p.IsAdmin(), SeesAll: p.SeesAll(), CanEdit: p.IsAdmin() || p.Role == store.RoleEditor}
	if p.Service {
		resp.userResponse = userResponse{Username: p.Username, Role: p.Role, Namespaces: []string{}}
	} else {
		u, err := h.opts.Store.GetUser(r.Context(), p.UserID)
		if err != nil {
			h.internalError(w, r, err)
			return
		}
		resp.userResponse = toUserResponse(u)
	}
	writeJSON(w, http.StatusOK, resp)
}

type passwordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// authPassword changes the password of the signed-in local user.
func (h *handler) authPassword(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if p.Service {
		forbidden(w)
		return
	}
	var req passwordRequest
	if !decodeBody(w, r, &req) {
		return
	}
	u, err := h.opts.Store.GetUser(r.Context(), p.UserID)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	if u.Source != store.UserLocal {
		writeError(w, http.StatusBadRequest, "the password of a directory account is changed in the directory")
		return
	}
	if !auth.CheckPassword(u.PasswordHash, req.CurrentPassword) {
		writeError(w, http.StatusBadRequest, "the current password is wrong")
		return
	}
	if err := h.setPassword(r, u, req.NewPassword); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.audit(r, "password_change", "user", u.ID, u.Username)
	w.WriteHeader(http.StatusNoContent)
}

// setPassword stores a new password and signs the user out everywhere but
// here, since whoever knew the old password may be signed in somewhere.
func (h *handler) setPassword(r *http.Request, u store.User, password string) error {
	if err := h.validPassword(password); err != nil {
		return err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	if err := h.opts.Store.SetPassword(r.Context(), u.ID, hash); err != nil {
		return err
	}
	keep := ""
	if p := principal(r); p.UserID == u.ID {
		token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		keep = auth.HashToken(token)
	}
	if err := h.opts.Store.DeleteUserSessions(r.Context(), u.ID); err != nil {
		return err
	}
	if keep != "" {
		now := time.Now().UTC()
		return h.opts.Store.CreateSession(r.Context(), store.Session{TokenHash: keep, UserID: u.ID, CreatedAt: now, ExpiresAt: now.Add(h.opts.SessionTTL), LastSeenAt: now})
	}
	return nil
}

func (h *handler) listUsers(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	users, err := h.opts.Store.ListUsers(r.Context())
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	resp := make([]userResponse, 0, len(users))
	for _, u := range users {
		resp = append(resp, toUserResponse(u))
	}
	writeJSON(w, http.StatusOK, resp)
}

type createUserRequest struct {
	Username    string     `json:"username"`
	DisplayName string     `json:"display_name"`
	Email       string     `json:"email"`
	Password    string     `json:"password"`
	Role        store.Role `json:"role"`
	Namespaces  []string   `json:"namespaces"`
}

func (h *handler) createUser(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	var req createUserRequest
	if !decodeBody(w, r, &req) {
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	switch {
	case !validUsername(req.Username):
		writeError(w, http.StatusBadRequest, "username must be letters, digits and . _ @ -, without spaces")
		return
	case !validRole(req.Role):
		writeError(w, http.StatusBadRequest, "role must be admin, editor or viewer")
		return
	case len(req.DisplayName) > maxNameLength || len(req.Email) > maxNameLength:
		writeError(w, http.StatusBadRequest, "display_name and email are at most 100 characters")
		return
	}
	namespaces, err := scopeNamespaces(req.Role, req.Namespaces)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.validPassword(req.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	u, err := h.opts.Store.CreateUser(r.Context(), store.User{
		Username: req.Username, DisplayName: strings.TrimSpace(req.DisplayName), Email: strings.TrimSpace(req.Email),
		Source: store.UserLocal, PasswordHash: hash, Role: req.Role, Namespaces: namespaces, Enabled: true,
	})
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "a user with this name already exists")
		return
	}
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	h.audit(r, "create", "user", u.ID, fmt.Sprintf("%s (%s)", u.Username, u.Role))
	writeJSON(w, http.StatusCreated, toUserResponse(u))
}

type updateUserRequest struct {
	DisplayName *string     `json:"display_name"`
	Email       *string     `json:"email"`
	Role        *store.Role `json:"role"`
	Namespaces  *[]string   `json:"namespaces"`
	Enabled     *bool       `json:"enabled"`
	// Password sets a new one for a local user.
	Password *string `json:"password"`
}

func (h *handler) updateUser(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	var req updateUserRequest
	if !decodeBody(w, r, &req) {
		return
	}
	u, err := h.opts.Store.GetUser(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		h.internalError(w, r, err)
		return
	}

	if u.Source == store.UserLDAP && (req.Role != nil || req.Namespaces != nil || req.Password != nil) {
		writeError(w, http.StatusBadRequest, "the role and password of a directory account come from the directory and its group mappings")
		return
	}
	if req.DisplayName != nil {
		u.DisplayName = strings.TrimSpace(*req.DisplayName)
	}
	if req.Email != nil {
		u.Email = strings.TrimSpace(*req.Email)
	}
	if len(u.DisplayName) > maxNameLength || len(u.Email) > maxNameLength {
		writeError(w, http.StatusBadRequest, "display_name and email are at most 100 characters")
		return
	}
	if req.Role != nil {
		if !validRole(*req.Role) {
			writeError(w, http.StatusBadRequest, "role must be admin, editor or viewer")
			return
		}
		u.Role = *req.Role
	}
	if req.Role != nil || req.Namespaces != nil {
		given := u.Namespaces
		if req.Namespaces != nil {
			given = *req.Namespaces
		}
		if u.Namespaces, err = scopeNamespaces(u.Role, given); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if req.Enabled != nil {
		u.Enabled = *req.Enabled
	}
	// The last administrator can neither step down nor be switched off.
	if p := principal(r); p.UserID == u.ID && (u.Role != store.RoleAdmin || !u.Enabled) {
		writeError(w, http.StatusBadRequest, "you cannot take your own administrator role or disable yourself")
		return
	}

	updated, err := h.opts.Store.UpdateUser(r.Context(), u)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	if req.Password != nil {
		if err := h.setPassword(r, updated, *req.Password); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if req.Enabled != nil && !*req.Enabled {
		_ = h.opts.Store.DeleteUserSessions(r.Context(), u.ID) // signed out at once
	}
	h.audit(r, "update", "user", u.ID, u.Username)
	writeJSON(w, http.StatusOK, toUserResponse(updated))
}

func (h *handler) deleteUser(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if principal(r).UserID == id {
		writeError(w, http.StatusBadRequest, "you cannot delete yourself")
		return
	}
	u, err := h.opts.Store.GetUser(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	if err := h.opts.Store.DeleteUser(r.Context(), id); err != nil {
		h.internalError(w, r, err)
		return
	}
	h.audit(r, "delete", "user", id, u.Username)
	w.WriteHeader(http.StatusNoContent)
}

type ldapStatusResponse struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url,omitempty"`
	// DefaultRole is what a directory user gets without a mapped group;
	// empty means they cannot sign in.
	DefaultRole store.Role `json:"default_role"`
}

func (h *handler) ldapStatus(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	resp := ldapStatusResponse{Enabled: h.opts.LDAP != nil, DefaultRole: h.opts.LDAPDefaultRole}
	if h.opts.LDAP != nil {
		resp.URL = h.opts.LDAP.URL()
	}
	writeJSON(w, http.StatusOK, resp)
}

type groupRoleResponse struct {
	ID         int64      `json:"id"`
	GroupDN    string     `json:"group_dn"`
	Role       store.Role `json:"role"`
	Namespaces []string   `json:"namespaces"`
	CreatedAt  time.Time  `json:"created_at"`
}

func toGroupRoleResponse(g store.LDAPGroupRole) groupRoleResponse {
	resp := groupRoleResponse{ID: g.ID, GroupDN: g.GroupDN, Role: g.Role, Namespaces: g.Namespaces, CreatedAt: g.CreatedAt}
	if resp.Namespaces == nil {
		resp.Namespaces = []string{}
	}
	return resp
}

type groupRoleRequest struct {
	GroupDN    *string     `json:"group_dn"`
	Role       *store.Role `json:"role"`
	Namespaces *[]string   `json:"namespaces"`
}

// applyGroupRole validates the request and sets its fields on g.
func applyGroupRole(req groupRoleRequest, g *store.LDAPGroupRole) error {
	if req.GroupDN != nil {
		g.GroupDN = strings.TrimSpace(*req.GroupDN)
	}
	if req.Role != nil {
		g.Role = *req.Role
	}
	if g.GroupDN == "" || len(g.GroupDN) > maxHeaderValue || strings.ContainsAny(g.GroupDN, "\r\n") {
		return errors.New("group_dn is required: the distinguished name of the group")
	}
	if !validRole(g.Role) {
		return errors.New("role must be admin, editor or viewer")
	}
	given := g.Namespaces
	if req.Namespaces != nil {
		given = *req.Namespaces
	}
	var err error
	g.Namespaces, err = scopeNamespaces(g.Role, given)
	return err
}

func (h *handler) listGroupRoles(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	groups, err := h.opts.Store.ListLDAPGroupRoles(r.Context())
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	resp := make([]groupRoleResponse, 0, len(groups))
	for _, g := range groups {
		resp = append(resp, toGroupRoleResponse(g))
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *handler) createGroupRole(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	var req groupRoleRequest
	if !decodeBody(w, r, &req) {
		return
	}
	var g store.LDAPGroupRole
	if err := applyGroupRole(req, &g); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	created, err := h.opts.Store.CreateLDAPGroupRole(r.Context(), g)
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "this group is mapped already")
		return
	}
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	h.audit(r, "create", "ldap_group", created.ID, fmt.Sprintf("%s -> %s", created.GroupDN, created.Role))
	writeJSON(w, http.StatusCreated, toGroupRoleResponse(created))
}

func (h *handler) updateGroupRole(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "group mapping not found")
		return
	}
	var req groupRoleRequest
	if !decodeBody(w, r, &req) {
		return
	}
	groups, err := h.opts.Store.ListLDAPGroupRoles(r.Context())
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	i := slices.IndexFunc(groups, func(g store.LDAPGroupRole) bool { return g.ID == id })
	if i < 0 {
		writeError(w, http.StatusNotFound, "group mapping not found")
		return
	}
	g := groups[i]
	if err := applyGroupRole(req, &g); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := h.opts.Store.UpdateLDAPGroupRole(r.Context(), g)
	switch {
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "this group is mapped already")
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "group mapping not found")
	case err != nil:
		h.internalError(w, r, err)
	default:
		h.audit(r, "update", "ldap_group", updated.ID, fmt.Sprintf("%s -> %s", updated.GroupDN, updated.Role))
		writeJSON(w, http.StatusOK, toGroupRoleResponse(updated))
	}
}

func (h *handler) deleteGroupRole(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "group mapping not found")
		return
	}
	err := h.opts.Store.DeleteLDAPGroupRole(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "group mapping not found")
		return
	}
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	h.audit(r, "delete", "ldap_group", id, "")
	w.WriteHeader(http.StatusNoContent)
}

type auditResponse struct {
	ID         int64     `json:"id"`
	At         time.Time `json:"at"`
	UserID     int64     `json:"user_id"`
	Username   string    `json:"username"`
	Action     string    `json:"action"`
	ObjectType string    `json:"object_type"`
	ObjectID   int64     `json:"object_id"`
	Summary    string    `json:"summary"`
}

func (h *handler) listAudit(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	limit, err := h.limit(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	entries, err := h.opts.Store.ListAudit(r.Context(), limit)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	resp := make([]auditResponse, 0, len(entries))
	for _, e := range entries {
		resp = append(resp, auditResponse{ID: e.ID, At: e.At, UserID: e.UserID, Username: e.Username, Action: e.Action, ObjectType: e.ObjectType, ObjectID: e.ObjectID, Summary: e.Summary})
	}
	writeJSON(w, http.StatusOK, resp)
}
