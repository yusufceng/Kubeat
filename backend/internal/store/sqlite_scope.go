package store

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
)

// The record of a monitor's target scope (Monitor.Scope) is in the JSON of
// its settings. A monitor written before 0.51, or saved since by 0.50, has
// none; these find and fill those.

// withoutScope selects the monitors without a record: the key is absent, or
// null.
const withoutScope = "json_type(settings, '$.scope') IS NULL OR json_type(settings, '$.scope') = 'null'"

// MonitorsWithoutScope returns how many monitors have no record of a target
// scope.
func (s *SQLite) MonitorsWithoutScope(ctx context.Context) (int, error) {
	var n int
	if err := s.read.QueryRowContext(ctx, "SELECT COUNT(*) FROM monitors WHERE "+withoutScope).Scan(&n); err != nil {
		return 0, fmt.Errorf("count monitors without a target scope: %w", err)
	}
	return n, nil
}

// RecordScopes gives every monitor without a record of a target scope the
// record scope, and returns how many it gave one. The rest of their settings,
// sealed secrets among them, is left as it is.
func (s *SQLite) RecordScopes(ctx context.Context, scope TargetScope) (int, error) {
	record, err := json.Marshal(scope)
	if err != nil {
		return 0, fmt.Errorf("record target scopes: %w", err)
	}
	res, err := s.write.ExecContext(ctx, "UPDATE monitors SET settings = json_set(settings, '$.scope', json(?)) WHERE "+withoutScope, string(record))
	if err != nil {
		return 0, fmt.Errorf("record target scopes: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("record target scopes: %w", err)
	}
	return int(n), nil
}

// LimitedEditor reports whether a role with these namespaces may edit
// monitors in only some namespaces: an editor whose scope is not every
// namespace. Only such a user can have set a monitor's target outside the
// namespaces the monitor is in, the one a scope holds against.
func LimitedEditor(role Role, namespaces []string) bool {
	return role == RoleEditor && !slices.Contains(namespaces, AllNamespaces)
}

// LimitedEditors reports whether a local account (enabled or not) or a
// directory group's mapping makes a LimitedEditor. The roles the directory
// and the identity provider give by default are settings, not rows; the
// caller adds them.
func (s *SQLite) LimitedEditors(ctx context.Context) (bool, error) {
	users, err := s.ListUsers(ctx)
	if err != nil {
		return false, err
	}
	for _, u := range users {
		if LimitedEditor(u.Role, u.Namespaces) {
			return true, nil
		}
	}
	groups, err := s.ListLDAPGroupRoles(ctx)
	if err != nil {
		return false, err
	}
	for _, g := range groups {
		if LimitedEditor(g.Role, g.Namespaces) {
			return true, nil
		}
	}
	return false, nil
}
