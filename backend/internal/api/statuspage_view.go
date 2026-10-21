package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

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

// What a page tells visitors, besides the state of its monitors.
type statusPageView struct {
	Slug         string `json:"slug"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	Announcement string `json:"announcement"`
	// State is up, degraded, down or maintenance for the page as a whole.
	State string `json:"state"`
	// Language is the page's own or, when it has none, the instance's.
	Language string `json:"language"`
	// NoIndex asks the frontend to render a robots meta that keeps search
	// engines away; DetailPages says whether a monitor can be opened on a
	// page of its own.
	NoIndex     bool `json:"noindex"`
	DetailPages bool `json:"detail_pages"`
	// Days is the number of days each monitor's history covers, ending
	// today (UTC), and From the start of the first of them.
	Days int       `json:"days"`
	From time.Time `json:"from"`
	// Uptime is the page's as a whole over each window: the mean of its
	// monitors' that have checks in the window.
	Uptime   []uptimeView            `json:"uptime"`
	Sections []statusPageViewSection `json:"sections"`
	// Incidents are the outages of the shown monitors, newest first.
	Incidents []statusPageIncident `json:"incidents"`
	// Updates are what the page told its visitors, newest first and
	// grouped by UTC day.
	Updates []statusPageDay `json:"updates"`
	// Maintenance lists the windows covering a shown monitor that are in
	// progress or start soon, earliest first.
	Maintenance []statusPageMaintenance `json:"maintenance"`
}

// statusPageViewSection is a group of the page with its monitors.
type statusPageViewSection struct {
	Name     string               `json:"name"`
	Monitors []statusPageViewItem `json:"monitors"`
}

type statusPageViewItem struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"`
	// UptimePercent is over all the days; null without any check.
	UptimePercent *float64 `json:"uptime_percent"`
	// Uptime is over each of the page's windows.
	Uptime []uptimeView `json:"uptime"`
	// History has one entry per day, oldest first: the uptime of the day,
	// or null for a day without checks.
	History []*float64 `json:"history"`
}

// uptimeView is the uptime over one window: null without any check.
type uptimeView struct {
	WindowSeconds int      `json:"window_seconds"`
	UptimePercent *float64 `json:"uptime_percent"`
}

// statusPageIncident is an outage as the public sees it: when, and of what.
// The cause stays inside, since it often names internal addresses.
type statusPageIncident struct {
	MonitorID   int64      `json:"monitor_id"`
	MonitorName string     `json:"monitor_name"`
	StartedAt   time.Time  `json:"started_at"`
	ResolvedAt  *time.Time `json:"resolved_at"`
}

// statusPageDay is the updates of one UTC day, newest first.
type statusPageDay struct {
	Day     string                 `json:"day"`
	Updates []statusPageUpdateView `json:"updates"`
}

// statusPageUpdateView is an update as visitors see it: one written for
// the page, or an incident note flagged for status pages (kind "note",
// titled after the incident's monitor as the page names it).
type statusPageUpdateView struct {
	Kind  string `json:"kind"`
	Title string `json:"title"`
	Body  string `json:"body"`
	// MonitorName is the page's name of the monitor the update is about,
	// when it is about an incident of a shown monitor.
	MonitorName string    `json:"monitor_name,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// statusPageMaintenance is a maintenance window as visitors see it; for a
// repeating one, its occurrence in progress or the next.
type statusPageMaintenance struct {
	Name     string    `json:"name"`
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
	// Monitors are the page's names of the shown monitors it covers.
	Monitors []string `json:"monitors"`
}

// uptimeWindows are the windows a page reports uptime over: a day, a
// week, a month and the page's own span, without repeats, shortest first.
func (h *handler) uptimeWindows() []time.Duration {
	const day = 24 * time.Hour
	windows := []time.Duration{day, 7 * day, 30 * day, time.Duration(h.opts.StatusPageDays) * day}
	slices.Sort(windows)
	return slices.Compact(windows)
}

// uptimeViews turns the counts of one monitor into the uptime over each
// window.
func uptimeViews(windows []time.Duration, counts []store.UptimeCount) []uptimeView {
	out := make([]uptimeView, len(windows))
	for i, w := range windows {
		out[i].WindowSeconds = int(w / time.Second)
		if i < len(counts) && counts[i].Checks > 0 {
			pct := 100 * float64(counts[i].UpChecks) / float64(counts[i].Checks)
			out[i].UptimePercent = &pct
		}
	}
	return out
}

// lockedResponse is the answer of a protected page to a visitor without
// its token: the title alone, so that the unlock screen can show it.
type lockedResponse struct {
	Error  string `json:"error"`
	Locked bool   `json:"locked"`
	Title  string `json:"title"`
}

func locked(w http.ResponseWriter, title string) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="kubeat-status-page"`)
	writeJSON(w, http.StatusUnauthorized, lockedResponse{Error: "this page asks for a password", Locked: true, Title: title})
}

// statusPageToken is what a visitor holds after giving a protected page's
// password: "<slug>.<expires>.<mac>", the MAC over the slug, the expiry
// and the page's password hash, so that a changed password ends the
// tokens given for the old one. The frontend keeps it in a cookie.
func (h *handler) statusPageToken(page store.StatusPage, expires time.Time) string {
	stamp := strconv.FormatInt(expires.Unix(), 10)
	return page.Slug + "." + stamp + "." + base64.RawURLEncoding.EncodeToString(h.statusPageMAC(page, stamp))
}

func (h *handler) statusPageMAC(page store.StatusPage, stamp string) []byte {
	mac := hmac.New(sha256.New, h.opts.StatusPageTokenKey)
	mac.Write([]byte("kubeat status page\x00" + page.Slug + "\x00" + stamp + "\x00" + page.PasswordHash))
	return mac.Sum(nil)
}

// statusPageOpen reports whether one of the tokens a request presents
// opens page at now.
func (h *handler) statusPageOpen(page store.StatusPage, tokens []string, now time.Time) bool {
	for _, token := range tokens {
		if h.statusPageTokenValid(page, token, now) {
			return true
		}
	}
	return false
}

// statusPageTokenValid reports whether token opens page at now.
func (h *handler) statusPageTokenValid(page store.StatusPage, token string, now time.Time) bool {
	if token == "" || len(h.opts.StatusPageTokenKey) == 0 {
		return false
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != page.Slug {
		return false
	}
	expires, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || now.Unix() >= expires {
		return false
	}
	given, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	return hmac.Equal(given, h.statusPageMAC(page, parts[1]))
}

// statusPageTokensFrom reads the tokens a request presents: in
// X-Status-Page-Token, and as a bearer token. Both are tried, so that a
// client whose Authorization header carries its Kubeat session or API
// token can pass the page's token in the other.
func statusPageTokensFrom(r *http.Request) []string {
	var tokens []string
	if token := r.Header.Get("X-Status-Page-Token"); token != "" {
		tokens = append(tokens, token)
	}
	if token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok && token != "" {
		tokens = append(tokens, token)
	}
	return tokens
}

// publicPage loads the published page of the request's {slug} for a
// visitor, answering 404 for one that is not served and 401 for a
// protected one the visitor has no token for.
func (h *handler) publicPage(w http.ResponseWriter, r *http.Request) (store.StatusPage, bool) {
	page, err := h.opts.Store.GetStatusPageBySlug(r.Context(), r.PathValue("slug"))
	if errors.Is(err, store.ErrNotFound) || (err == nil && !page.Published) {
		writeError(w, http.StatusNotFound, "status page not found")
		return store.StatusPage{}, false
	}
	if err != nil {
		h.internalError(w, r, err)
		return store.StatusPage{}, false
	}
	if page.PasswordHash != "" && !h.statusPageOpen(page, statusPageTokensFrom(r), time.Now()) {
		locked(w, page.Title)
		return store.StatusPage{}, false
	}
	return page, true
}

type unlockRequest struct {
	Password string `json:"password"`
}

type unlockResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// unlockStatusPage answers POST /api/v1/status-pages/by-slug/{slug}/unlock
// with a token for the page's password. Attempts are throttled per page,
// as sign-ins are per name: the throttle counts the attempt from the start
// and takes it back when it turns out not to be a guess.
func (h *handler) unlockStatusPage(w http.ResponseWriter, r *http.Request) {
	var req unlockRequest
	if !decodeBody(w, r, &req) {
		return
	}
	slug := r.PathValue("slug")
	if !statusPageSlug.MatchString(slug) {
		writeError(w, http.StatusNotFound, "status page not found")
		return
	}
	// A key of its own: usernames have no colon, and "pw:" is the password
	// changes'.
	key := "sp:" + slug
	now := time.Now().UTC()
	attempt, lockedFor, ok := h.opts.Throttle.Begin(key, now)
	if !ok {
		writeError(w, http.StatusTooManyRequests, tooManyAttempts(lockedFor))
		return
	}
	undo := func() { h.opts.Throttle.Undo(key, now) }

	page, err := h.opts.Store.GetStatusPageBySlug(r.Context(), slug)
	if errors.Is(err, store.ErrNotFound) || (err == nil && !page.Published) {
		undo()
		writeError(w, http.StatusNotFound, "status page not found")
		return
	}
	if err != nil {
		undo()
		h.internalError(w, r, err)
		return
	}
	if page.PasswordHash == "" {
		undo()
		writeError(w, http.StatusBadRequest, "this page has no password")
		return
	}
	if len(h.opts.StatusPageTokenKey) == 0 {
		undo()
		writeError(w, http.StatusServiceUnavailable, "the server cannot issue tokens for protected pages: UPTIME_SECRET_KEY is not set")
		return
	}
	right, err := h.checkPassword(r.Context(), page.PasswordHash, req.Password)
	if err != nil {
		undo()
		writeError(w, http.StatusServiceUnavailable, "too many password checks at once; try again")
		return
	}
	if !right {
		h.opts.Logger.Info("status page unlock refused", "slug", slug, "attempt", attempt)
		writeError(w, http.StatusUnauthorized, "wrong password")
		return
	}
	h.opts.Throttle.Reset(key)
	expires := now.Add(h.opts.StatusPageSession).Truncate(time.Second)
	writeJSON(w, http.StatusOK, unlockResponse{Token: h.statusPageToken(page, expires), ExpiresAt: expires})
}

// pageLanguage is the language a page is shown in: its own, else the
// instance's.
func (h *handler) pageLanguage(page store.StatusPage) string {
	switch {
	case page.Language != "":
		return page.Language
	case h.opts.Language != "":
		return h.opts.Language
	default:
		return "en"
	}
}

// shownMonitors is what the view and the detail page work from: the
// sections a page shows now, with every monitor's status, and the page's
// names of the shown monitors by id.
type shownMonitors struct {
	sections []shownSection
	status   map[int64]store.MonitorStatus
	names    map[int64]string
	ids      []int64
}

// resolveShown reads the statuses of every monitor and resolves which of
// them the page shows.
func (h *handler) resolveShown(r *http.Request, page store.StatusPage, now time.Time) (shownMonitors, error) {
	statuses, err := h.opts.Store.ListMonitorStatuses(r.Context(), now)
	if err != nil {
		return shownMonitors{}, err
	}
	out := shownMonitors{status: make(map[int64]store.MonitorStatus, len(statuses)), names: make(map[int64]string)}
	monitors := make([]store.Monitor, 0, len(statuses))
	for _, st := range statuses {
		out.status[st.ID] = st
		monitors = append(monitors, st.Monitor)
	}
	for _, s := range resolvePage(page, monitors) {
		if len(s.Monitors) == 0 {
			continue
		}
		out.sections = append(out.sections, s)
		for _, m := range s.Monitors {
			out.names[m.ID] = m.Name
			out.ids = append(out.ids, m.ID)
		}
	}
	return out, nil
}

// viewStatusPage returns what a published status page shows. It is what
// the frontend serves to visitors without a session, so it carries nothing
// about a monitor beyond its name, state and uptime.
func (h *handler) viewStatusPage(w http.ResponseWriter, r *http.Request) {
	page, ok := h.publicPage(w, r)
	if !ok {
		return
	}
	now := time.Now().UTC()
	days := h.opts.StatusPageDays
	const day = 24 * time.Hour
	from := now.Truncate(day).Add(-time.Duration(days-1) * day)

	shown, err := h.resolveShown(r, page, now)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	daily, err := h.opts.Store.DailyUptime(r.Context(), shown.ids, from, now.Add(time.Hour))
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	windows := h.uptimeWindows()
	counts, err := h.opts.Store.UptimeWindows(r.Context(), shown.ids, now, windows)
	if err != nil {
		h.internalError(w, r, err)
		return
	}

	view := statusPageView{
		Slug: page.Slug, Title: page.Title, Description: page.Description, Announcement: page.Announcement,
		State: publicUp, Language: h.pageLanguage(page), NoIndex: page.NoIndex, DetailPages: page.DetailPages,
		Days: days, From: from, Sections: make([]statusPageViewSection, 0, len(shown.sections)),
		Incidents: []statusPageIncident{}, Updates: []statusPageDay{}, Maintenance: []statusPageMaintenance{},
	}
	// The page's uptime over each window: the mean of its monitors'.
	sums, measured := make([]float64, len(windows)), make([]int, len(windows))
	down, maintenance, counted := 0, 0, 0
	for _, s := range shown.sections {
		section := statusPageViewSection{Name: s.Name, Monitors: make([]statusPageViewItem, 0, len(s.Monitors))}
		for _, m := range s.Monitors {
			st := shown.status[m.ID]
			item := statusPageViewItem{ID: m.ID, Name: m.Name, State: publicState(st), History: make([]*float64, days)}
			item.UptimePercent = dailyHistory(item.History, daily[m.ID], from, days)
			item.Uptime = uptimeViews(windows, counts[m.ID])
			for i, u := range item.Uptime {
				if u.UptimePercent != nil {
					sums[i] += *u.UptimePercent
					measured[i]++
				}
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
			section.Monitors = append(section.Monitors, item)
		}
		view.Sections = append(view.Sections, section)
	}
	view.Uptime = make([]uptimeView, len(windows))
	for i, w := range windows {
		view.Uptime[i].WindowSeconds = int(w / time.Second)
		if measured[i] > 0 {
			pct := sums[i] / float64(measured[i])
			view.Uptime[i].UptimePercent = &pct
		}
	}
	switch {
	case down > 0 && down == counted:
		view.State = publicDown
	case down > 0:
		view.State = publicDegraded
	case maintenance > 0:
		view.State = publicMaintenance
	}

	if view.Incidents, err = h.pageIncidents(r, shown.names, now); err != nil {
		h.internalError(w, r, err)
		return
	}
	if view.Updates, err = h.pageUpdates(r, page, shown); err != nil {
		h.internalError(w, r, err)
		return
	}
	if view.Maintenance, err = h.pageMaintenance(r, shown, now); err != nil {
		h.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// dailyHistory fills history, one entry per day from `from`, with the
// uptime of each day that has checks, and returns the uptime over all of
// them, or nil without any check.
func dailyHistory(history []*float64, points []store.SeriesPoint, from time.Time, days int) *float64 {
	const day = 24 * time.Hour
	checks, up := 0, 0
	for _, p := range points {
		index := int(p.Start.Sub(from) / day)
		if index < 0 || index >= days || p.Checks == 0 {
			continue
		}
		pct := 100 * float64(p.UpChecks) / float64(p.Checks)
		history[index] = &pct
		checks += p.Checks
		up += p.UpChecks
	}
	if checks == 0 {
		return nil
	}
	pct := 100 * float64(up) / float64(checks)
	return &pct
}

// pageIncidents lists the outages of the shown monitors: the open ones,
// and those that began within the incident window, newest first.
func (h *handler) pageIncidents(r *http.Request, names map[int64]string, now time.Time) ([]statusPageIncident, error) {
	incidents, err := h.opts.Store.ListIncidents(r.Context(), 0, h.opts.MaxResults)
	if err != nil {
		return nil, err
	}
	out := []statusPageIncident{}
	since := now.Add(-h.opts.StatusPageIncidentWindow)
	for _, i := range incidents {
		name, shown := names[i.MonitorID]
		if !shown || i.Kind != store.IncidentDown || (i.StartedAt.Before(since) && !i.Open()) {
			continue
		}
		incident := statusPageIncident{MonitorID: i.MonitorID, MonitorName: name, StartedAt: i.StartedAt}
		if !i.Open() {
			incident.ResolvedAt = &i.ResolvedAt
		}
		if out = append(out, incident); len(out) == maxStatusPageIncidents {
			break
		}
	}
	return out, nil
}

// pageUpdates lists the latest updates of the page and the incident notes
// flagged for status pages about its shown monitors, together, newest
// first and grouped by UTC day.
func (h *handler) pageUpdates(r *http.Request, page store.StatusPage, shown shownMonitors) ([]statusPageDay, error) {
	limit := h.opts.StatusPageUpdates
	updates, err := h.opts.Store.ListStatusPageUpdates(r.Context(), page.ID, limit)
	if err != nil {
		return nil, err
	}
	notes, err := h.opts.Store.ListStatusPageNotes(r.Context(), shown.ids, limit)
	if err != nil {
		return nil, err
	}
	items := make([]statusPageUpdateView, 0, len(updates)+len(notes))
	for _, u := range updates {
		item := statusPageUpdateView{Kind: string(u.Kind), Title: u.Title, Body: u.Body, CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt}
		if u.IncidentID != 0 {
			// Which monitor the incident is about, if the page shows it.
			i, err := h.opts.Store.GetIncident(r.Context(), u.IncidentID)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return nil, err
			}
			if err == nil {
				item.MonitorName = shown.names[i.MonitorID]
			}
		}
		items = append(items, item)
	}
	for _, n := range notes {
		name := shown.names[n.MonitorID]
		items = append(items, statusPageUpdateView{
			Kind: "note", Title: name, Body: n.Text, MonitorName: name, CreatedAt: n.CreatedAt, UpdatedAt: n.UpdatedAt,
		})
	}
	slices.SortFunc(items, func(a, b statusPageUpdateView) int { return b.CreatedAt.Compare(a.CreatedAt) })
	if len(items) > limit {
		items = items[:limit]
	}
	days := []statusPageDay{}
	for _, item := range items {
		day := item.CreatedAt.UTC().Format(time.DateOnly)
		if n := len(days); n > 0 && days[n-1].Day == day {
			days[n-1].Updates = append(days[n-1].Updates, item)
			continue
		}
		days = append(days, statusPageDay{Day: day, Updates: []statusPageUpdateView{item}})
	}
	return days, nil
}

// pageMaintenance lists the maintenance windows that cover a shown monitor
// and are in progress or begin within the upcoming period, earliest first;
// a repeating window by its occurrence in progress or the next.
func (h *handler) pageMaintenance(r *http.Request, shown shownMonitors, now time.Time) ([]statusPageMaintenance, error) {
	until := now.Add(h.opts.StatusPageUpcoming)
	windows, err := h.opts.Store.ListMaintenanceBetween(r.Context(), now, until)
	if err != nil {
		return nil, err
	}
	out := []statusPageMaintenance{}
	for _, w := range windows {
		start, end, ok := w.Occurrence(now)
		if !ok || !start.Before(until) {
			continue
		}
		item := statusPageMaintenance{Name: w.Name, StartsAt: start, EndsAt: end, Monitors: []string{}}
		for _, s := range shown.sections {
			for _, m := range s.Monitors {
				if w.Covers(m.Monitor) {
					item.Monitors = append(item.Monitors, m.Name)
				}
			}
		}
		if len(item.Monitors) > 0 {
			out = append(out, item)
		}
	}
	slices.SortStableFunc(out, func(a, b statusPageMaintenance) int { return a.StartsAt.Compare(b.StartsAt) })
	return out, nil
}

// statusPageDetail is a monitor on a page of its own: what the page says
// about it, its response times, and its outages as start, end and
// duration alone.
type statusPageDetail struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
	// Language and NoIndex are the page's, as in the view.
	Language string             `json:"language"`
	NoIndex  bool               `json:"noindex"`
	Monitor  statusPageViewItem `json:"monitor"`
	Days     int                `json:"days"`
	From     time.Time          `json:"from"`
	// Series is the response time over the last StatusPageSeries, in
	// round buckets.
	Series seriesResponse `json:"series"`
	// Incidents are the outages of the last Days days, newest first.
	Incidents []statusPageOutage `json:"incidents"`
}

type statusPageOutage struct {
	StartedAt  time.Time  `json:"started_at"`
	ResolvedAt *time.Time `json:"resolved_at"`
	// DurationSeconds is how long the outage lasted, or has so far.
	DurationSeconds int `json:"duration_seconds"`
}

// statusPageMonitorDetail answers GET
// /api/v1/status-pages/by-slug/{slug}/monitors/{monitor}: 404 unless the
// page has detail pages and shows the monitor.
func (h *handler) statusPageMonitorDetail(w http.ResponseWriter, r *http.Request) {
	page, ok := h.publicPage(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("monitor"), 10, 64)
	if err != nil || id < 1 || !page.DetailPages {
		writeError(w, http.StatusNotFound, "monitor not found")
		return
	}
	now := time.Now().UTC()
	shown, err := h.resolveShown(r, page, now)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	name, onPage := shown.names[id]
	if !onPage {
		writeError(w, http.StatusNotFound, "monitor not found")
		return
	}

	days := h.opts.StatusPageDays
	const day = 24 * time.Hour
	from := now.Truncate(day).Add(-time.Duration(days-1) * day)
	daily, err := h.opts.Store.DailyUptime(r.Context(), []int64{id}, from, now.Add(time.Hour))
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	windows := h.uptimeWindows()
	counts, err := h.opts.Store.UptimeWindows(r.Context(), []int64{id}, now, windows)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	series, err := h.seriesBetween(r.Context(), id, now.Add(-h.opts.StatusPageSeries), now, now)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	incidents, err := h.opts.Store.ListIncidentsBetween(r.Context(), id, from, now, h.opts.MaxResults)
	if err != nil {
		h.internalError(w, r, err)
		return
	}

	st := shown.status[id]
	detail := statusPageDetail{
		Slug: page.Slug, Title: page.Title, Language: h.pageLanguage(page), NoIndex: page.NoIndex,
		Monitor: statusPageViewItem{ID: id, Name: name, State: publicState(st), History: make([]*float64, days)},
		Days:    days, From: from, Series: series, Incidents: []statusPageOutage{},
	}
	detail.Monitor.UptimePercent = dailyHistory(detail.Monitor.History, daily[id], from, days)
	detail.Monitor.Uptime = uptimeViews(windows, counts[id])
	for _, i := range incidents {
		if i.Kind != store.IncidentDown {
			continue
		}
		outage := statusPageOutage{StartedAt: i.StartedAt, DurationSeconds: int(incidentDuration(i, now) / time.Second)}
		if !i.Open() {
			outage.ResolvedAt = &i.ResolvedAt
		}
		if detail.Incidents = append(detail.Incidents, outage); len(detail.Incidents) == maxStatusPageIncidents {
			break
		}
	}
	writeJSON(w, http.StatusOK, detail)
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
// have one: anything else would tell the world about internal services. A
// monitor shown only on protected pages has one for whoever holds a token
// of one of them.
func (h *handler) badge(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	kind := r.PathValue("kind")
	if !ok || (kind != "status" && kind != "uptime") {
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
	pages, err := h.opts.Store.ListStatusPages(r.Context())
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	public, allowed := false, false
	tokens, now := statusPageTokensFrom(r), time.Now()
	for _, p := range pages {
		if !p.Published || !p.Shows(st.Monitor) || (!st.Enabled && p.HidePaused) {
			continue
		}
		public = true
		if p.PasswordHash == "" || h.statusPageOpen(p, tokens, now) {
			allowed = true
		}
	}
	if !public {
		writeError(w, http.StatusNotFound, "badge not found")
		return
	}
	if !allowed {
		locked(w, "")
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
