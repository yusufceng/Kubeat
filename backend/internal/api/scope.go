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
// admins any, editors those showing only monitors they may change.
func canEditPage(p auth.Principal, page store.StatusPage, namespaces map[int64]string) bool {
	if p.IsAdmin() {
		return true
	}
	if p.Role != store.RoleEditor {
		return false
	}
	for _, m := range page.Monitors {
		if ns, ok := namespaces[m.MonitorID]; !ok || !p.CanEdit(ns) {
			return false
		}
	}
	return true
}
