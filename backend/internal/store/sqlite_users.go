package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

const userColumns = "id, username, display_name, email, source, password_hash, role, namespaces, enabled, created_at, last_login_at"

func scanUser(row rowScanner) (User, error) {
	var (
		u          User
		namespaces string
		createdAt  int64
		lastLogin  sql.NullInt64
		err        error
	)
	if err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Email, &u.Source, &u.PasswordHash, &u.Role, &namespaces, &u.Enabled, &createdAt, &lastLogin); err != nil {
		return User{}, err
	}
	if u.Namespaces, err = decodeNamespaces(namespaces); err != nil {
		return User{}, fmt.Errorf("user %d: %w", u.ID, err)
	}
	u.CreatedAt = time.UnixMilli(createdAt).UTC()
	u.LastLoginAt = unixMilliOrZero(lastLogin)
	return u, nil
}

// userFields validates and returns the stored forms of what every write
// of a user needs.
func userFields(u *User) (namespaces string, err error) {
	u.Username = strings.TrimSpace(u.Username)
	if u.Username == "" {
		return "", errors.New("username is required")
	}
	if !slices.Contains(Roles, u.Role) {
		return "", fmt.Errorf("unknown role %q", u.Role)
	}
	if u.Role == RoleAdmin || len(u.Namespaces) == 0 {
		u.Namespaces = nil
	}
	return encodeNamespaces(u.Namespaces)
}

func (s *SQLite) CreateUser(ctx context.Context, u User) (User, error) {
	namespaces, err := userFields(&u)
	if err != nil {
		return User{}, fmt.Errorf("create user: %w", err)
	}
	if u.Source == "" {
		u.Source = UserLocal
	}
	u.CreatedAt = time.Now().UTC().Truncate(time.Millisecond)
	res, err := s.write.ExecContext(ctx,
		`INSERT INTO users (username, display_name, email, source, password_hash, role, namespaces, enabled, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.Username, u.DisplayName, u.Email, u.Source, u.PasswordHash, u.Role, namespaces, u.Enabled, u.CreatedAt.UnixMilli())
	if isUniqueViolation(err) {
		return User{}, ErrConflict
	}
	if err != nil {
		return User{}, fmt.Errorf("create user: %w", err)
	}
	if u.ID, err = res.LastInsertId(); err != nil {
		return User{}, fmt.Errorf("create user: %w", err)
	}
	return u, nil
}

func (s *SQLite) GetUser(ctx context.Context, id int64) (User, error) {
	u, err := scanUser(s.read.QueryRowContext(ctx, "SELECT "+userColumns+" FROM users WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("get user: %w", err)
	}
	return u, nil
}

func (s *SQLite) GetUserByUsername(ctx context.Context, username string) (User, error) {
	u, err := scanUser(s.read.QueryRowContext(ctx, "SELECT "+userColumns+" FROM users WHERE username = ?", strings.TrimSpace(username)))
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("get user: %w", err)
	}
	return u, nil
}

func (s *SQLite) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.read.QueryContext(ctx, "SELECT "+userColumns+" FROM users ORDER BY username COLLATE NOCASE")
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	var users []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("list users: %w", err)
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s *SQLite) CountUsers(ctx context.Context) (int, error) {
	var n int
	if err := s.read.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&n); err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return n, nil
}

func (s *SQLite) UpdateUser(ctx context.Context, u User) (User, error) {
	namespaces, err := userFields(&u)
	if err != nil {
		return User{}, fmt.Errorf("update user: %w", err)
	}
	var lastLogin sql.NullInt64
	if !u.LastLoginAt.IsZero() {
		lastLogin = sql.NullInt64{Int64: u.LastLoginAt.UnixMilli(), Valid: true}
	}
	res, err := s.write.ExecContext(ctx,
		`UPDATE users SET username = ?, display_name = ?, email = ?, source = ?, role = ?, namespaces = ?, enabled = ?, last_login_at = ?
		 WHERE id = ?`,
		u.Username, u.DisplayName, u.Email, u.Source, u.Role, namespaces, u.Enabled, lastLogin, u.ID)
	if isUniqueViolation(err) {
		return User{}, ErrConflict
	}
	if err != nil {
		return User{}, fmt.Errorf("update user: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return User{}, fmt.Errorf("update user: %w", err)
	} else if n == 0 {
		return User{}, ErrNotFound
	}
	updated, err := scanUser(s.write.QueryRowContext(ctx, "SELECT "+userColumns+" FROM users WHERE id = ?", u.ID))
	if err != nil {
		return User{}, fmt.Errorf("update user: %w", err)
	}
	return updated, nil
}

func (s *SQLite) SetPassword(ctx context.Context, id int64, hash string) error {
	res, err := s.write.ExecContext(ctx, "UPDATE users SET password_hash = ? WHERE id = ?", hash, id)
	if err != nil {
		return fmt.Errorf("set password: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("set password: %w", err)
	} else if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLite) DeleteUser(ctx context.Context, id int64) error {
	res, err := s.write.ExecContext(ctx, "DELETE FROM users WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("delete user: %w", err)
	} else if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLite) CreateSession(ctx context.Context, sess Session) error {
	_, err := s.write.ExecContext(ctx,
		"INSERT INTO sessions (token_hash, user_id, created_at, expires_at, last_seen_at) VALUES (?, ?, ?, ?, ?)",
		sess.TokenHash, sess.UserID, sess.CreatedAt.UnixMilli(), sess.ExpiresAt.UnixMilli(), sess.LastSeenAt.UnixMilli())
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (s *SQLite) GetSession(ctx context.Context, tokenHash string, now time.Time) (Session, error) {
	var (
		sess                             Session
		createdAt, expiresAt, lastSeenAt int64
	)
	err := s.read.QueryRowContext(ctx,
		"SELECT token_hash, user_id, created_at, expires_at, last_seen_at FROM sessions WHERE token_hash = ? AND expires_at > ?",
		tokenHash, now.UnixMilli()).Scan(&sess.TokenHash, &sess.UserID, &createdAt, &expiresAt, &lastSeenAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("get session: %w", err)
	}
	sess.CreatedAt, sess.ExpiresAt, sess.LastSeenAt = time.UnixMilli(createdAt).UTC(), time.UnixMilli(expiresAt).UTC(), time.UnixMilli(lastSeenAt).UTC()
	return sess, nil
}

func (s *SQLite) TouchSession(ctx context.Context, tokenHash string, at time.Time) error {
	if _, err := s.write.ExecContext(ctx, "UPDATE sessions SET last_seen_at = ? WHERE token_hash = ?", at.UnixMilli(), tokenHash); err != nil {
		return fmt.Errorf("touch session: %w", err)
	}
	return nil
}

func (s *SQLite) DeleteSession(ctx context.Context, tokenHash string) error {
	if _, err := s.write.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash = ?", tokenHash); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (s *SQLite) DeleteUserSessions(ctx context.Context, userID int64) error {
	if _, err := s.write.ExecContext(ctx, "DELETE FROM sessions WHERE user_id = ?", userID); err != nil {
		return fmt.Errorf("delete sessions: %w", err)
	}
	return nil
}

func (s *SQLite) DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	res, err := s.write.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at <= ?", now.UnixMilli())
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	return res.RowsAffected()
}

const groupRoleColumns = "id, group_dn, role, namespaces, created_at"

func scanGroupRole(row rowScanner) (LDAPGroupRole, error) {
	var (
		g          LDAPGroupRole
		namespaces string
		createdAt  int64
		err        error
	)
	if err := row.Scan(&g.ID, &g.GroupDN, &g.Role, &namespaces, &createdAt); err != nil {
		return LDAPGroupRole{}, err
	}
	if g.Namespaces, err = decodeNamespaces(namespaces); err != nil {
		return LDAPGroupRole{}, fmt.Errorf("group role %d: %w", g.ID, err)
	}
	g.CreatedAt = time.UnixMilli(createdAt).UTC()
	return g, nil
}

func groupRoleFields(g *LDAPGroupRole) (string, error) {
	g.GroupDN = strings.TrimSpace(g.GroupDN)
	if g.GroupDN == "" {
		return "", errors.New("group DN is required")
	}
	if !slices.Contains(Roles, g.Role) {
		return "", fmt.Errorf("unknown role %q", g.Role)
	}
	if g.Role == RoleAdmin || len(g.Namespaces) == 0 {
		g.Namespaces = nil
	}
	return encodeNamespaces(g.Namespaces)
}

func (s *SQLite) CreateLDAPGroupRole(ctx context.Context, g LDAPGroupRole) (LDAPGroupRole, error) {
	namespaces, err := groupRoleFields(&g)
	if err != nil {
		return LDAPGroupRole{}, fmt.Errorf("create group role: %w", err)
	}
	g.CreatedAt = time.Now().UTC().Truncate(time.Millisecond)
	res, err := s.write.ExecContext(ctx,
		"INSERT INTO ldap_group_roles (group_dn, role, namespaces, created_at) VALUES (?, ?, ?, ?)",
		g.GroupDN, g.Role, namespaces, g.CreatedAt.UnixMilli())
	if isUniqueViolation(err) {
		return LDAPGroupRole{}, ErrConflict
	}
	if err != nil {
		return LDAPGroupRole{}, fmt.Errorf("create group role: %w", err)
	}
	if g.ID, err = res.LastInsertId(); err != nil {
		return LDAPGroupRole{}, fmt.Errorf("create group role: %w", err)
	}
	return g, nil
}

func (s *SQLite) ListLDAPGroupRoles(ctx context.Context) ([]LDAPGroupRole, error) {
	rows, err := s.read.QueryContext(ctx, "SELECT "+groupRoleColumns+" FROM ldap_group_roles ORDER BY group_dn COLLATE NOCASE")
	if err != nil {
		return nil, fmt.Errorf("list group roles: %w", err)
	}
	defer rows.Close()
	var groups []LDAPGroupRole
	for rows.Next() {
		g, err := scanGroupRole(rows)
		if err != nil {
			return nil, fmt.Errorf("list group roles: %w", err)
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

func (s *SQLite) UpdateLDAPGroupRole(ctx context.Context, g LDAPGroupRole) (LDAPGroupRole, error) {
	namespaces, err := groupRoleFields(&g)
	if err != nil {
		return LDAPGroupRole{}, fmt.Errorf("update group role: %w", err)
	}
	res, err := s.write.ExecContext(ctx, "UPDATE ldap_group_roles SET group_dn = ?, role = ?, namespaces = ? WHERE id = ?",
		g.GroupDN, g.Role, namespaces, g.ID)
	if isUniqueViolation(err) {
		return LDAPGroupRole{}, ErrConflict
	}
	if err != nil {
		return LDAPGroupRole{}, fmt.Errorf("update group role: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return LDAPGroupRole{}, fmt.Errorf("update group role: %w", err)
	} else if n == 0 {
		return LDAPGroupRole{}, ErrNotFound
	}
	updated, err := scanGroupRole(s.write.QueryRowContext(ctx, "SELECT "+groupRoleColumns+" FROM ldap_group_roles WHERE id = ?", g.ID))
	if err != nil {
		return LDAPGroupRole{}, fmt.Errorf("update group role: %w", err)
	}
	return updated, nil
}

func (s *SQLite) DeleteLDAPGroupRole(ctx context.Context, id int64) error {
	res, err := s.write.ExecContext(ctx, "DELETE FROM ldap_group_roles WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete group role: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("delete group role: %w", err)
	} else if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLite) AppendAudit(ctx context.Context, e AuditEntry) error {
	var userID sql.NullInt64
	if e.UserID != 0 {
		userID = sql.NullInt64{Int64: e.UserID, Valid: true}
	}
	if e.At.IsZero() {
		e.At = time.Now()
	}
	_, err := s.write.ExecContext(ctx,
		"INSERT INTO audit_log (at, user_id, username, action, object_type, object_id, summary) VALUES (?, ?, ?, ?, ?, ?, ?)",
		e.At.UnixMilli(), userID, e.Username, e.Action, e.ObjectType, e.ObjectID, e.Summary)
	if err != nil {
		return fmt.Errorf("append audit: %w", err)
	}
	return nil
}

func (s *SQLite) ListAudit(ctx context.Context, limit int) ([]AuditEntry, error) {
	rows, err := s.read.QueryContext(ctx,
		"SELECT id, at, user_id, username, action, object_type, object_id, summary FROM audit_log ORDER BY at DESC, id DESC LIMIT ?", limit)
	if err != nil {
		return nil, fmt.Errorf("list audit: %w", err)
	}
	defer rows.Close()
	var entries []AuditEntry
	for rows.Next() {
		var (
			e      AuditEntry
			at     int64
			userID sql.NullInt64
		)
		if err := rows.Scan(&e.ID, &at, &userID, &e.Username, &e.Action, &e.ObjectType, &e.ObjectID, &e.Summary); err != nil {
			return nil, fmt.Errorf("list audit: %w", err)
		}
		e.At, e.UserID = time.UnixMilli(at).UTC(), userID.Int64
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (s *SQLite) DeleteAuditBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := s.write.ExecContext(ctx, "DELETE FROM audit_log WHERE at < ?", cutoff.UnixMilli())
	if err != nil {
		return 0, fmt.Errorf("delete audit: %w", err)
	}
	return res.RowsAffected()
}
