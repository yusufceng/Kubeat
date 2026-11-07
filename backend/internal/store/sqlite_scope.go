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
const withoutScope = "(json_type(settings, '$.scope') IS NULL OR json_type(settings, '$.scope') = 'null')"

// ObjectScope is the record of the target scope of a monitor discovery made
// from an annotated object (Monitor.Managed): the object's namespace, its own
// host as discovery found it. Whoever may annotate the object sets where the
// checks connect, and owns that namespace; Kubeat cannot tell who that is,
// so the record is the same in every install, whoever saves the monitor
// (ADR-0003 c).
func ObjectScope(namespace string) *TargetScope {
	return &TargetScope{Namespaces: []string{namespace}, Discovered: true}
}

// HeldToObject reports whether m's checks are held as ObjectScope holds
// them: its record is that one, or it has none, which reads the same for a
// monitor with a source (Monitor.TargetScope).
func HeldToObject(m Monitor) bool {
	if m.Scope == nil {
		return m.Source != ""
	}
	return m.Scope.Discovered && slices.Equal(m.Scope.Namespaces, []string{m.Namespace})
}

// SameConnections reports whether a and b name the same hosts for their
// checks to connect to: of one type, at one target, the server and site an
// HTTP request names (Destination), the resolver a DNS check asks. Whoever
// names these chooses where the checks connect. The rest of a check's
// options name no host — what it sends, how it reads the answers, each pod
// or not — and leave where it connects to the target and its cluster
// object, whoever set those.
func SameConnections(a, b Monitor) bool {
	return a.Type == b.Type && a.Target == b.Target && a.Destination() == b.Destination() &&
		(a.Type != MonitorDNS || a.DNS.Resolver == b.DNS.Resolver)
}

// SameCheck reports whether a and b are checked alike: of one type, at one
// target, each pod or not alike, with the same options of their type as
// they are stored, defaults filled in. Everything else of a monitor — its
// name, tags, intervals, state and record of a scope — is left out: a save
// that changes only those leaves the checks as they were.
func SameCheck(a, b Monitor) bool {
	if a.Type != b.Type || a.Target != b.Target || a.EachPod != b.EachPod {
		return false
	}
	normalize(&a)
	normalize(&b)
	a.Scope, b.Scope = nil, nil
	ea, errA := encodeSettings(a)
	eb, errB := encodeSettings(b)
	return errA == nil && errB == nil && ea == eb
}

// MonitorsWithoutScope returns how many monitors have no record of a target
// scope.
func (s *SQLite) MonitorsWithoutScope(ctx context.Context) (int, error) {
	var n int
	if err := s.read.QueryRowContext(ctx, "SELECT COUNT(*) FROM monitors WHERE "+withoutScope).Scan(&n); err != nil {
		return 0, fmt.Errorf("count monitors without a target scope: %w", err)
	}
	return n, nil
}

// RecordObjectScopes gives every monitor discovery made from an annotated
// object (Monitor.Managed) without a record of a target scope its object's
// (ObjectScope), and returns how many it gave one. The rest of their
// settings, sealed secrets among them, is left as it is.
func (s *SQLite) RecordObjectScopes(ctx context.Context) (int, error) {
	res, err := s.write.ExecContext(ctx, "UPDATE monitors SET settings = json_set(settings, '$.scope', "+
		"json_object('namespaces', json_array(namespace), 'discovered', json('true'))) WHERE managed = 1 AND "+withoutScope)
	if err != nil {
		return 0, fmt.Errorf("record target scopes of annotation monitors: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("record target scopes of annotation monitors: %w", err)
	}
	return int(n), nil
}

// RecordScopes gives every other monitor without a record of a target scope
// the record scope — with Discovered for one whose target was taken from a
// cluster object (Source), as a monitor without a record reads it — and
// returns how many it gave one. Annotation monitors are left to
// RecordObjectScopes. The rest of their settings, sealed secrets among them,
// is left as it is.
func (s *SQLite) RecordScopes(ctx context.Context, scope TargetScope) (int, error) {
	scope.Discovered = false
	entered, err := json.Marshal(scope)
	if err != nil {
		return 0, fmt.Errorf("record target scopes: %w", err)
	}
	scope.Discovered = true
	discovered, err := json.Marshal(scope)
	if err != nil {
		return 0, fmt.Errorf("record target scopes: %w", err)
	}
	res, err := s.write.ExecContext(ctx, "UPDATE monitors SET settings = json_set(settings, '$.scope', "+
		"json(CASE WHEN source != '' THEN ? ELSE ? END)) WHERE managed = 0 AND "+withoutScope, string(discovered), string(entered))
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
