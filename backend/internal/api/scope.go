package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/yusufceng/Kubeat/backend/internal/auth"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// What a principal may see and change, beyond their own namespaces.

// scopeError answers 403 and, for an editor of some namespaces, says which
// ones they are.
func scopeError(w http.ResponseWriter, p auth.Principal) {
	if p.Role == store.RoleEditor && !p.SeesAll() && len(p.Namespaces) > 0 {
		writeError(w, http.StatusForbidden, "you may change monitors only in: "+strings.Join(p.Namespaces, ", "))
		return
	}
	forbidden(w)
}

// channelVisible reports whether the principal may see and manage a
// channel: admins all of them, editors those limited to their namespaces.
func channelVisible(p auth.Principal, ch store.Channel) bool {
	return p.IsAdmin() || (len(ch.Namespaces) > 0 && p.CanEditEach(ch.Namespaces))
}

// maintenanceVisible reports whether the principal is concerned by a
// window: it covers something they may see.
func maintenanceVisible(p auth.Principal, m store.Maintenance) bool {
	switch {
	case m.MonitorID != 0:
		return p.CanRead(m.MonitorNamespace)
	case m.Namespace != "":
		return p.CanRead(m.Namespace)
	default:
		return true
	}
}

// canEditMaintenance reports whether the principal may create or end a
// window with the given scope.
func canEditMaintenance(p auth.Principal, m store.Maintenance) bool {
	switch {
	case m.MonitorID != 0:
		return p.CanEdit(m.MonitorNamespace)
	case m.Namespace != "":
		return p.CanEdit(m.Namespace)
	default:
		return p.CanEditAll()
	}
}

// monitorNamespaces maps every monitor to its namespace or group.
func (h *handler) monitorNamespaces(ctx context.Context) (map[int64]string, error) {
	monitors, err := h.opts.Store.ListMonitors(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]string, len(monitors))
	for _, m := range monitors {
		out[m.ID] = m.Namespace
	}
	return out, nil
}

// canEditPage reports whether the principal may manage a status page:
// admins any, editors those showing only monitors they may change, by
// name or by rule (a rule for any namespace reaches every monitor, so it
// takes an editor of every namespace). A page that shows no monitor and
// has no rule, whether made so or emptied when its monitors were deleted,
// belongs to no namespace and so to no editor: it is the admins'.
// Otherwise every editor, of whatever namespace, could rewrite or remove
// what such a page tells the public.
func canEditPage(p auth.Principal, page store.StatusPage, namespaces map[int64]string) bool {
	if p.IsAdmin() {
		return true
	}
	if p.Role != store.RoleEditor || (len(page.Monitors) == 0 && len(page.Rules) == 0) {
		return false
	}
	for _, m := range page.Monitors {
		if ns, ok := namespaces[m.MonitorID]; !ok || !p.CanEdit(ns) {
			return false
		}
	}
	for _, r := range page.Rules {
		if !canEditRule(p, r) {
			return false
		}
	}
	return true
}

// canEditRule reports whether the principal may write a membership rule:
// one for a namespace takes an editor of it, one for any namespace an
// editor of all of them.
func canEditRule(p auth.Principal, r store.StatusPageRule) bool {
	if r.Namespace == "" {
		return p.CanEditAll()
	}
	return p.CanEdit(r.Namespace)
}
