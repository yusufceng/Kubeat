package api

import (
	"errors"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/auth"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

const (
	maxStatusPageMonitors = 200
	maxStatusPageTitle    = 100
	maxStatusPageText     = 2000
	// maxStatusPageIncidents bounds the incidents one page lists.
	maxStatusPageIncidents = 25
)

// A slug is part of an address: lower-case letters, digits and hyphens.
var statusPageSlug = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`)

type statusPageMonitor struct {
	MonitorID   int64  `json:"monitor_id"`
	DisplayName string `json:"display_name"`
}

type statusPageResponse struct {
	ID           int64               `json:"id"`
	Slug         string              `json:"slug"`
	Title        string              `json:"title"`
	Description  string              `json:"description"`
	Announcement string              `json:"announcement"`
	Published    bool                `json:"published"`
	Monitors     []statusPageMonitor `json:"monitors"`
	CreatedAt    time.Time           `json:"created_at"`
}

func toStatusPageResponse(p store.StatusPage) statusPageResponse {
	resp := statusPageResponse{
		ID: p.ID, Slug: p.Slug, Title: p.Title, Description: p.Description, Announcement: p.Announcement,
		Published: p.Published, CreatedAt: p.CreatedAt, Monitors: make([]statusPageMonitor, len(p.Monitors)),
	}
	for i, m := range p.Monitors {
		resp.Monitors[i] = statusPageMonitor{MonitorID: m.MonitorID, DisplayName: m.DisplayName}
	}
	return resp
}

// statusPageRequest carries the fields to set; on an update, absent ones
// keep their value.
type statusPageRequest struct {
	Slug         *string              `json:"slug"`
	Title        *string              `json:"title"`
	Description  *string              `json:"description"`
	Announcement *string              `json:"announcement"`
	Published    *bool                `json:"published"`
	Monitors     *[]statusPageMonitor `json:"monitors"`
}

// apply validates the request and sets its fields on p.
func (h *handler) applyStatusPage(r *http.Request, req statusPageRequest, p *store.StatusPage) error {
	if req.Slug != nil {
		p.Slug = strings.TrimSpace(*req.Slug)
	}
	if req.Title != nil {
		p.Title = strings.TrimSpace(*req.Title)
	}
	if req.Description != nil {
		p.Description = strings.TrimSpace(*req.Description)
	}
	if req.Announcement != nil {
		p.Announcement = strings.TrimSpace(*req.Announcement)
	}
	if req.Published != nil {
		p.Published = *req.Published
	}
	switch {
	case !statusPageSlug.MatchString(p.Slug):
		return errors.New("slug must be lower-case letters, digits and hyphens, such as shop-status")
	case p.Title == "" || len(p.Title) > maxStatusPageTitle:
		return fmt.Errorf("title is required and at most %d bytes", maxStatusPageTitle)
	case len(p.Description) > maxStatusPageText || len(p.Announcement) > maxStatusPageText:
		return fmt.Errorf("description and announcement are at most %d bytes each", maxStatusPageText)
	}
	if req.Monitors == nil {
		return nil
	}

	if len(*req.Monitors) > maxStatusPageMonitors {
		return fmt.Errorf("monitors: at most %d", maxStatusPageMonitors)
	}
	namespaces, err := h.monitorNamespaces(r.Context())
	if err != nil {
		return internalFailure{err}
	}
	who := principal(r)
	p.Monitors = make([]store.StatusPageMonitor, 0, len(*req.Monitors))
	seen := make(map[int64]bool)
	for _, m := range *req.Monitors {
		name := strings.TrimSpace(m.DisplayName)
		ns, known := namespaces[m.MonitorID]
		switch {
		case !known || !who.CanRead(ns):
			return fmt.Errorf("monitors: there is no monitor %d", m.MonitorID)
		case !who.CanEdit(ns):
			return fmt.Errorf("monitors: you may not publish monitor %d", m.MonitorID)
		case seen[m.MonitorID]:
			return fmt.Errorf("monitors: monitor %d is listed twice", m.MonitorID)
		case len(name) > maxStatusPageTitle:
			return fmt.Errorf("monitors: a display name is at most %d bytes", maxStatusPageTitle)
		}
		seen[m.MonitorID] = true
		p.Monitors = append(p.Monitors, store.StatusPageMonitor{MonitorID: m.MonitorID, DisplayName: name})
	}
	return nil
}

// internalFailure marks an error that is the server's, not the request's.
type internalFailure struct{ error }

// statusPageError answers for a failed write of a status page.
func (h *handler) statusPageError(w http.ResponseWriter, r *http.Request, err error) {
	var internal internalFailure
	switch {
	case errors.As(err, &internal):
		h.internalError(w, r, internal.error)
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "a status page with this slug already exists")
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "status page not found")
	default:
		h.internalError(w, r, err)
	}
}

func (h *handler) listStatusPages(w http.ResponseWriter, r *http.Request) {
	pages, err := h.opts.Store.ListStatusPages(r.Context())
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	who := principal(r)
	namespaces, err := h.pageScope(r, who)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	resp := make([]statusPageResponse, 0, len(pages))
	for _, p := range pages {
		if !canEditPage(who, p, namespaces) {
			continue
		}
		resp = append(resp, toStatusPageResponse(p))
	}
	writeJSON(w, http.StatusOK, resp)
}

// pageScope returns what canEditPage needs for the principal: nothing for
// an admin, who may edit every page.
func (h *handler) pageScope(r *http.Request, who auth.Principal) (map[int64]string, error) {
	if who.IsAdmin() {
		return nil, nil
	}
	return h.monitorNamespaces(r.Context())
}

// pageForEdit returns the page, or answers why it cannot be edited.
func (h *handler) pageForEdit(w http.ResponseWriter, r *http.Request, id int64) (store.StatusPage, bool) {
	p, err := h.opts.Store.GetStatusPage(r.Context(), id)
	if err != nil {
		h.statusPageError(w, r, err)
		return store.StatusPage{}, false
	}
	who := principal(r)
	namespaces, err := h.pageScope(r, who)
	if err != nil {
		h.internalError(w, r, err)
		return store.StatusPage{}, false
	}
	if !canEditPage(who, p, namespaces) {
		writeError(w, http.StatusNotFound, "status page not found")
		return store.StatusPage{}, false
	}
	return p, true
}

func (h *handler) getStatusPage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "status page not found")
		return
	}
	p, ok := h.pageForEdit(w, r, id)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, toStatusPageResponse(p))
}

func (h *handler) createStatusPage(w http.ResponseWriter, r *http.Request) {
	if who := principal(r); !who.IsAdmin() && who.Role != store.RoleEditor {
		forbidden(w)
		return
	}
	var req statusPageRequest
	if !decodeBody(w, r, &req) {
		return
	}
	p := store.StatusPage{Published: true}
	if err := h.applyStatusPage(r, req, &p); err != nil {
		var internal internalFailure
		if errors.As(err, &internal) {
			h.internalError(w, r, internal.error)
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	created, err := h.opts.Store.CreateStatusPage(r.Context(), p)
	if err != nil {
		h.statusPageError(w, r, err)
		return
	}
	h.audit(r, "create", "status_page", created.ID, created.Slug)
	writeJSON(w, http.StatusCreated, toStatusPageResponse(created))
}

func (h *handler) updateStatusPage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "status page not found")
		return
	}
	var req statusPageRequest
	if !decodeBody(w, r, &req) {
		return
	}
	p, ok := h.pageForEdit(w, r, id)
	if !ok {
		return
	}
	if err := h.applyStatusPage(r, req, &p); err != nil {
		var internal internalFailure
		if errors.As(err, &internal) {
			h.internalError(w, r, internal.error)
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := h.opts.Store.UpdateStatusPage(r.Context(), p)
	if err != nil {
		h.statusPageError(w, r, err)
		return
	}
	h.audit(r, "update", "status_page", updated.ID, updated.Slug)
	writeJSON(w, http.StatusOK, toStatusPageResponse(updated))
}

func (h *handler) deleteStatusPage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "status page not found")
		return
	}
	p, ok := h.pageForEdit(w, r, id)
	if !ok {
		return
	}
	if err := h.opts.Store.DeleteStatusPage(r.Context(), id); err != nil {
		h.statusPageError(w, r, err)
		return
	}
	h.audit(r, "delete", "status_page", id, p.Slug)
	w.WriteHeader(http.StatusNoContent)
}

// States a status page shows. They say less than the dashboard does: a
// single failed check is not news for the public.
const (
	publicUp          = "up"
	publicDown        = "down"
	publicDegraded    = "degraded" // of a page: some, not all, monitors are down
	publicMaintenance = "maintenance"
	publicPending     = "pending"
	publicPaused      = "paused"
)

// publicState is what a status page says about a monitor.
func publicState(st store.MonitorStatus) string {
	switch {
	case !st.Enabled:
		return publicPaused
	case st.Incident != nil:
		return publicDown
	case !st.MaintenanceUntil.IsZero():
		return publicMaintenance
	case st.Last == nil:
		return publicPending
	default:
		return publicUp
	}
}

// publicName is the name a page shows for a monitor that has no display
// name: for a discovered monitor the object's own name, without the
// namespace and kind in front of it.
func publicName(m store.Monitor) string {
	if _, object, found := strings.Cut(m.Source, "/"); found {
		return object
	}
	return m.Name
}

type statusPageView struct {
	Slug         string `json:"slug"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	Announcement string `json:"announcement"`
	// State is up, degraded, down or maintenance for the page as a whole.
	State string `json:"state"`
	// Days is the number of days each monitor's history covers, ending
	// today (UTC), and From the start of the first of them.
	Days      int                  `json:"days"`
	From      time.Time            `json:"from"`
	Monitors  []statusPageViewItem `json:"monitors"`
	Incidents []statusPageIncident `json:"incidents"`
}

type statusPageViewItem struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"`
	// UptimePercent is over all the days; null without any check.
	UptimePercent *float64 `json:"uptime_percent"`
	// History has one entry per day, oldest first: the uptime of the day,
	// or null for a day without checks.
	History []*float64 `json:"history"`
}

// statusPageIncident is an outage as the public sees it: when, and of what.
// The cause stays inside, since it often names internal addresses.
type statusPageIncident struct {
	MonitorName string     `json:"monitor_name"`
	StartedAt   time.Time  `json:"started_at"`
	ResolvedAt  *time.Time `json:"resolved_at"`
}

// viewStatusPage returns what a published status page shows. It is what
// the frontend serves to visitors without a session, so it carries nothing
// about a monitor beyond its name, state and uptime.
func (h *handler) viewStatusPage(w http.ResponseWriter, r *http.Request) {
	page, err := h.opts.Store.GetStatusPageBySlug(r.Context(), r.PathValue("slug"))
	if errors.Is(err, store.ErrNotFound) || (err == nil && !page.Published) {
		writeError(w, http.StatusNotFound, "status page not found")
		return
	}
	if err != nil {
		h.internalError(w, r, err)
		return
	}

	now := time.Now().UTC()
	days := h.opts.StatusPageDays
	const day = 24 * time.Hour
	from := now.Truncate(day).Add(-time.Duration(days-1) * day)

	statuses, err := h.opts.Store.ListMonitorStatuses(r.Context(), now)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	byID := make(map[int64]store.MonitorStatus, len(statuses))
	for _, st := range statuses {
		byID[st.ID] = st
	}
	daily, err := h.opts.Store.StatusPageDailyUptime(r.Context(), page.ID, from, now.Add(time.Hour))
	if err != nil {
		h.internalError(w, r, err)
		return
	}

	view := statusPageView{
		Slug: page.Slug, Title: page.Title, Description: page.Description, Announcement: page.Announcement,
		State: publicUp, Days: days, From: from,
		Monitors: make([]statusPageViewItem, 0, len(page.Monitors)), Incidents: []statusPageIncident{},
	}
	names := make(map[int64]string, len(page.Monitors))
	down, maintenance, counted := 0, 0, 0
	for _, pm := range page.Monitors {
		st, ok := byID[pm.MonitorID]
		if !ok {
			continue // deleted since the page was read
		}
		item := statusPageViewItem{ID: st.ID, Name: pm.DisplayName, State: publicState(st), History: make([]*float64, days)}
		if item.Name == "" {
			item.Name = publicName(st.Monitor)
		}
		names[st.ID] = item.Name

		checks, up := 0, 0
		for _, p := range daily[st.ID] {
			index := int(p.Start.Sub(from) / day)
			if index < 0 || index >= days || p.Checks == 0 {
				continue
			}
			pct := 100 * float64(p.UpChecks) / float64(p.Checks)
			item.History[index] = &pct
			checks += p.Checks
			up += p.UpChecks
		}
		if checks > 0 {
			pct := 100 * float64(up) / float64(checks)
			item.UptimePercent = &pct
		}
		switch item.State {
		case publicDown:
			down++
			counted++
		case publicMaintenance:
			maintenance++
			counted++
		case publicUp:
			counted++
		}
		view.Monitors = append(view.Monitors, item)
	}
	switch {
	case down > 0 && down == counted:
		view.State = publicDown
	case down > 0:
		view.State = publicDegraded
	case maintenance > 0:
		view.State = publicMaintenance
	}

	incidents, err := h.opts.Store.ListIncidents(r.Context(), 0, h.opts.MaxResults)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	since := now.Add(-h.opts.StatusPageIncidentWindow)
	for _, i := range incidents {
		name, shown := names[i.MonitorID]
		if !shown || i.Kind != store.IncidentDown || (i.StartedAt.Before(since) && !i.Open()) {
			continue
		}
		incident := statusPageIncident{MonitorName: name, StartedAt: i.StartedAt}
		if !i.Open() {
			incident.ResolvedAt = &i.ResolvedAt
		}
		if view.Incidents = append(view.Incidents, incident); len(view.Incidents) == maxStatusPageIncidents {
			break
		}
	}
	writeJSON(w, http.StatusOK, view)
}

// Badge colors, the ones the UI uses for the same states.
const (
	badgeGreen = "#12a06c"
	badgeRed   = "#d93d3d"
	badgeAmber = "#c98206"
	badgeBlue  = "#2f74d0"
	badgeGray  = "#7a8490"
)

// badge returns a small SVG image saying a monitor's state or its uptime,
// for a README or a wiki page. Only monitors on a published status page
// have one: anything else would tell the world about internal services.
func (h *handler) badge(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	kind := r.PathValue("kind")
	if !ok || (kind != "status" && kind != "uptime") {
		writeError(w, http.StatusNotFound, "badge not found")
		return
	}
	public, err := h.opts.Store.MonitorIsPublic(r.Context(), id)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	if !public {
		writeError(w, http.StatusNotFound, "badge not found")
		return
	}
	st, err := h.opts.Store.GetMonitorStatus(r.Context(), id, time.Now().Add(-h.opts.StatsWindow))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "badge not found")
		return
	}
	if err != nil {
		h.internalError(w, r, err)
		return
	}

	label, value, color := "status", "", badgeGray
	if kind == "status" {
		switch publicState(st) {
		case publicUp:
			value, color = "up", badgeGreen
		case publicDown:
			value, color = "down", badgeRed
		case publicMaintenance:
			value, color = "maintenance", badgeBlue
		case publicPaused:
			value = "paused"
		default:
			value = "pending"
		}
	} else {
		label = fmt.Sprintf("uptime %s", shortDuration(h.opts.StatsWindow))
		value = "n/a"
		if st.Checks > 0 {
			pct := 100 * float64(st.UpChecks) / float64(st.Checks)
			// Floor so that 99.996% is not shown as a perfect 100%.
			value = fmt.Sprintf("%.2f%%", float64(int(pct*100))/100)
			switch {
			case pct >= 99.9:
				color = badgeGreen
			case pct >= 99:
				color = badgeAmber
			default:
				color = badgeRed
			}
		}
	}
	if custom := strings.TrimSpace(r.URL.Query().Get("label")); custom != "" && len(custom) <= 40 {
		label = custom
	}

	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	// A badge is embedded in pages that are loaded often; let caches hold
	// it for a moment, but not so long that it lies.
	w.Header().Set("Cache-Control", "public, max-age=60")
	_, _ = w.Write([]byte(badgeSVG(label, value, color)))
}

// shortDuration renders 24h0m0s as "24h" and 168h as "7d".
func shortDuration(d time.Duration) string {
	if d >= 48*time.Hour && d%(24*time.Hour) == 0 {
		return fmt.Sprintf("%dd", d/(24*time.Hour))
	}
	if d%time.Hour == 0 {
		return fmt.Sprintf("%dh", d/time.Hour)
	}
	return d.String()
}

// badgeSVG draws a two-part badge. The widths are estimated from the number
// of characters: there is no font to measure with on the server.
func badgeSVG(label, value, color string) string {
	const charWidth, padding = 6.6, 12.0
	width := func(s string) float64 { return float64(len([]rune(s)))*charWidth + padding }
	lw, vw := width(label), width(value)
	label, value = html.EscapeString(label), html.EscapeString(value)
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%.0f" height="20" role="img" aria-label="%s: %s">`+
		`<title>%s: %s</title>`+
		`<clipPath id="r"><rect width="%.0f" height="20" rx="3"/></clipPath>`+
		`<g clip-path="url(#r)"><rect width="%.0f" height="20" fill="#3d4651"/><rect x="%.0f" width="%.0f" height="20" fill="%s"/></g>`+
		`<g fill="#fff" text-anchor="middle" font-family="Verdana,Geneva,DejaVu Sans,sans-serif" font-size="11">`+
		`<text x="%.1f" y="14">%s</text><text x="%.1f" y="14">%s</text></g></svg>`,
		lw+vw, label, value, label, value, lw+vw, lw, lw, vw, color, lw/2, label, lw+vw/2, value)
}
