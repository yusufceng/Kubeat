package api

import (
	"cmp"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/auth"
	"github.com/yusufceng/Kubeat/backend/internal/license"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

const (
	maxStatusPageMonitors = 200
	maxStatusPageSections = 50
	maxStatusPageRules    = 50
	maxStatusPageTitle    = 100
	maxStatusPageText     = 2000
	// maxStatusPageIncidents bounds the incidents one page lists.
	maxStatusPageIncidents = 25
)

// A slug is part of an address: lower-case letters, digits and hyphens.
var statusPageSlug = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`)

// statusPageSection is a named group of a page, in the page's order.
type statusPageSection struct {
	Name string `json:"name"`
}

type statusPageMonitor struct {
	MonitorID   int64  `json:"monitor_id"`
	DisplayName string `json:"display_name"`
	// Section names the section the monitor is in; empty for the unnamed
	// group at the top of the page.
	Section string `json:"section"`
}

// statusPageRule adds the monitors of a namespace ("" for any) carrying a
// tag ("" for any) to a section, after its explicit monitors.
type statusPageRule struct {
	Namespace string `json:"namespace"`
	Tag       string `json:"tag"`
	Section   string `json:"section"`
}

type statusPageResponse struct {
	ID           int64               `json:"id"`
	Slug         string              `json:"slug"`
	Title        string              `json:"title"`
	Description  string              `json:"description"`
	Announcement string              `json:"announcement"`
	Published    bool                `json:"published"`
	Sections     []statusPageSection `json:"sections"`
	Monitors     []statusPageMonitor `json:"monitors"`
	Rules        []statusPageRule    `json:"rules"`
	// NoIndex asks search engines to stay away (the frontend renders the
	// robots meta); HidePaused leaves paused monitors off the page;
	// Language is "" for the instance default, else "tr" or "en";
	// DetailPages lets a monitor be opened on a page of its own.
	NoIndex     bool   `json:"noindex"`
	HidePaused  bool   `json:"hide_paused"`
	Language    string `json:"language"`
	DetailPages bool   `json:"detail_pages"`
	// HasPassword says whether visitors must give a password; the password
	// itself is never returned.
	HasPassword bool      `json:"has_password"`
	CreatedAt   time.Time `json:"created_at"`
}

func toStatusPageResponse(p store.StatusPage) statusPageResponse {
	resp := statusPageResponse{
		ID: p.ID, Slug: p.Slug, Title: p.Title, Description: p.Description, Announcement: p.Announcement,
		Published: p.Published, CreatedAt: p.CreatedAt,
		Sections: make([]statusPageSection, len(p.Sections)), Monitors: make([]statusPageMonitor, len(p.Monitors)),
		Rules:   make([]statusPageRule, len(p.Rules)),
		NoIndex: p.NoIndex, HidePaused: p.HidePaused, Language: p.Language, DetailPages: p.DetailPages,
		HasPassword: p.PasswordHash != "",
	}
	for i, s := range p.Sections {
		resp.Sections[i] = statusPageSection{Name: s.Name}
	}
	for i, m := range p.Monitors {
		resp.Monitors[i] = statusPageMonitor{MonitorID: m.MonitorID, DisplayName: m.DisplayName, Section: m.Section}
	}
	for i, r := range p.Rules {
		resp.Rules[i] = statusPageRule{Namespace: r.Namespace, Tag: r.Tag, Section: r.Section}
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
	Sections     *[]statusPageSection `json:"sections"`
	Monitors     *[]statusPageMonitor `json:"monitors"`
	Rules        *[]statusPageRule    `json:"rules"`
	NoIndex      *bool                `json:"noindex"`
	HidePaused   *bool                `json:"hide_paused"`
	Language     *string              `json:"language"`
	DetailPages  *bool                `json:"detail_pages"`
	// Password sets the password visitors must give; empty clears it. It is
	// never returned: the page answers has_password.
	Password *string `json:"password"`
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
	if req.NoIndex != nil {
		p.NoIndex = *req.NoIndex
	}
	if req.HidePaused != nil {
		p.HidePaused = *req.HidePaused
	}
	if req.DetailPages != nil {
		p.DetailPages = *req.DetailPages
	}
	if req.Language != nil {
		p.Language = strings.TrimSpace(*req.Language)
	}
	switch {
	case !statusPageSlug.MatchString(p.Slug):
		return errors.New("slug must be lower-case letters, digits and hyphens, such as shop-status")
	case p.Title == "" || len(p.Title) > maxStatusPageTitle:
		return fmt.Errorf("title is required and at most %d bytes", maxStatusPageTitle)
	case len(p.Description) > maxStatusPageText || len(p.Announcement) > maxStatusPageText:
		return fmt.Errorf("description and announcement are at most %d bytes each", maxStatusPageText)
	case p.Language != "" && p.Language != "tr" && p.Language != "en":
		return errors.New(`language must be "tr", "en" or empty for the instance default`)
	}
	who := principal(r)
	if req.Sections != nil {
		if err := applyPageSections(*req.Sections, p); err != nil {
			return err
		}
	}
	if req.Monitors != nil {
		if err := h.applyPageMonitors(r, who, *req.Monitors, p); err != nil {
			return err
		}
	}
	if req.Rules != nil {
		if err := applyPageRules(who, *req.Rules, p); err != nil {
			return err
		}
	}
	// Whichever of the three the request changed, every monitor and rule
	// must be in a section the page has.
	sections := make(map[string]bool, len(p.Sections))
	for _, s := range p.Sections {
		sections[s.Name] = true
	}
	for _, m := range p.Monitors {
		if m.Section != "" && !sections[m.Section] {
			return fmt.Errorf("monitors: monitor %d is in section %q, which the page does not have", m.MonitorID, m.Section)
		}
	}
	for _, rule := range p.Rules {
		if rule.Section != "" && !sections[rule.Section] {
			return fmt.Errorf("rules: a rule is in section %q, which the page does not have", rule.Section)
		}
	}
	if req.Password != nil {
		if err := h.applyPagePassword(*req.Password, p); err != nil {
			return err
		}
	}
	// An editor's page must stay theirs: one without monitors or rules
	// would be nobody's but the admins' (see canEditPage).
	if len(p.Monitors) == 0 && len(p.Rules) == 0 && !who.IsAdmin() {
		return errors.New("monitors: choose at least one monitor or rule; only an administrator manages a page without any")
	}
	return nil
}

// applyPageSections validates the sections of a page and sets them on p.
func applyPageSections(sections []statusPageSection, p *store.StatusPage) error {
	if len(sections) > maxStatusPageSections {
		return fmt.Errorf("sections: at most %d", maxStatusPageSections)
	}
	p.Sections = make([]store.StatusPageSection, 0, len(sections))
	seen := make(map[string]bool, len(sections))
	for _, s := range sections {
		name := strings.TrimSpace(s.Name)
		switch {
		case name == "" || len(name) > maxStatusPageTitle:
			return fmt.Errorf("sections: a section needs a name of at most %d bytes", maxStatusPageTitle)
		case seen[name]:
			return fmt.Errorf("sections: %q is listed twice", name)
		}
		seen[name] = true
		p.Sections = append(p.Sections, store.StatusPageSection{Name: name})
	}
	return nil
}

// applyPageMonitors validates the monitors a page is to show and sets them
// on p: each must be one the principal may change.
func (h *handler) applyPageMonitors(r *http.Request, who auth.Principal, monitors []statusPageMonitor, p *store.StatusPage) error {
	if len(monitors) > maxStatusPageMonitors {
		return fmt.Errorf("monitors: at most %d", maxStatusPageMonitors)
	}
	namespaces, err := h.monitorNamespaces(r.Context())
	if err != nil {
		return internalFailure{err}
	}
	p.Monitors = make([]store.StatusPageMonitor, 0, len(monitors))
	seen := make(map[int64]bool)
	for _, m := range monitors {
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
		p.Monitors = append(p.Monitors, store.StatusPageMonitor{MonitorID: m.MonitorID, DisplayName: name, Section: strings.TrimSpace(m.Section)})
	}
	return nil
}

// applyPageRules validates the membership rules of a page and sets them on
// p. An editor writes rules for their own namespaces only: a rule for any
// namespace reaches every monitor there is.
func applyPageRules(who auth.Principal, rules []statusPageRule, p *store.StatusPage) error {
	if len(rules) > maxStatusPageRules {
		return fmt.Errorf("rules: at most %d", maxStatusPageRules)
	}
	p.Rules = make([]store.StatusPageRule, 0, len(rules))
	for _, r := range rules {
		rule := store.StatusPageRule{Namespace: strings.TrimSpace(r.Namespace), Tag: strings.TrimSpace(r.Tag), Section: strings.TrimSpace(r.Section)}
		switch {
		case rule.Namespace != "" && !validGroup(rule.Namespace):
			return fmt.Errorf("rules: %q is not the name of a namespace or group", rule.Namespace)
		case rule.Tag != "" && !plainName(rule.Tag, maxTagLength):
			return fmt.Errorf("rules: %q is not a tag", rule.Tag)
		case !canEditRule(who, rule):
			if rule.Namespace == "" {
				return errors.New("rules: a rule for every namespace takes an editor of every namespace; name one of yours")
			}
			return fmt.Errorf("rules: you may not publish the monitors of %s", rule.Namespace)
		}
		p.Rules = append(p.Rules, rule)
	}
	return nil
}

// applyPagePassword sets or clears the password visitors must give. It is
// held to the rules of every other password: a weak one on a page that is
// otherwise public is still a weak password.
func (h *handler) applyPagePassword(password string, p *store.StatusPage) error {
	if password == "" {
		p.PasswordHash = ""
		return nil
	}
	if len(h.opts.StatusPageTokenKey) == 0 {
		return errors.New("password: a protected page needs UPTIME_SECRET_KEY, which signs its visitors' tokens")
	}
	if err := auth.ValidatePassword(p.Slug, password, h.opts.PasswordMinLength); err != nil {
		return fmt.Errorf("password: %w", err)
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return internalFailure{err}
	}
	p.PasswordHash = hash
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

// pageForEdit returns the page of the request's {id}, or answers why it
// cannot be edited.
func (h *handler) pageForEdit(w http.ResponseWriter, r *http.Request) (store.StatusPage, bool) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "status page not found")
		return store.StatusPage{}, false
	}
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
	p, ok := h.pageForEdit(w, r)
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
	if !h.licensed(license.StatusPages) {
		pages, err := h.opts.Store.ListStatusPages(r.Context())
		if err != nil {
			h.internalError(w, r, err)
			return
		}
		if len(pages) >= license.CommunityStatusPages &&
			!h.requireLicense(w, license.StatusPages, fmt.Sprintf("more than %d status page", license.CommunityStatusPages)) {
			return
		}
	}
	p := store.StatusPage{Published: true, DetailPages: true}
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
	var req statusPageRequest
	if !decodeBody(w, r, &req) {
		return
	}
	p, ok := h.pageForEdit(w, r)
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
	p, ok := h.pageForEdit(w, r)
	if !ok {
		return
	}
	if err := h.opts.Store.DeleteStatusPage(r.Context(), p.ID); err != nil {
		h.statusPageError(w, r, err)
		return
	}
	h.audit(r, "delete", "status_page", p.ID, p.Slug)
	w.WriteHeader(http.StatusNoContent)
}

// shownMonitor is a monitor a status page shows: under which name, and
// whether it was listed by name or matched by a rule.
type shownMonitor struct {
	store.Monitor
	Name     string
	Explicit bool
}

// shownSection is a section of a status page with the monitors it shows.
type shownSection struct {
	Name     string
	Monitors []shownMonitor
}

// resolvePage lists what a page shows, section by section, from the
// monitors there are: the unnamed group first, then the sections in their
// order, each with its explicit entries in their order and then the
// monitors its rules match, by name. A monitor appears once: listed by
// name it is not matched again, and of two rules matching it the first has
// it. With HidePaused, paused monitors are left out. Sections without a
// monitor are kept, empty.
func resolvePage(page store.StatusPage, monitors []store.Monitor) []shownSection {
	byID := make(map[int64]store.Monitor, len(monitors))
	for _, m := range monitors {
		byID[m.ID] = m
	}
	sections := make([]shownSection, 1+len(page.Sections))
	index := map[string]int{"": 0}
	for i, s := range page.Sections {
		sections[i+1].Name = s.Name
		index[s.Name] = i + 1
	}
	taken := make(map[int64]bool)
	shown := func(m store.Monitor) bool { return !taken[m.ID] && (m.Enabled || !page.HidePaused) }
	for _, pm := range page.Monitors {
		m, ok := byID[pm.MonitorID]
		if !ok || !shown(m) {
			continue // deleted since the page was read, or paused and hidden
		}
		taken[m.ID] = true
		name := pm.DisplayName
		if name == "" {
			name = publicName(m)
		}
		i := index[pm.Section]
		sections[i].Monitors = append(sections[i].Monitors, shownMonitor{Monitor: m, Name: name, Explicit: true})
	}
	for _, rule := range page.Rules {
		var matched []shownMonitor
		for _, m := range monitors {
			if shown(m) && rule.Matches(m) {
				taken[m.ID] = true
				matched = append(matched, shownMonitor{Monitor: m, Name: publicName(m)})
			}
		}
		slices.SortFunc(matched, func(a, b shownMonitor) int {
			return cmp.Or(strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)), strings.Compare(a.Name, b.Name), cmp.Compare(a.ID, b.ID))
		})
		i := index[rule.Section]
		sections[i].Monitors = append(sections[i].Monitors, matched...)
	}
	return sections
}

// publicName is the name a page shows for a monitor that has no display
// name: the monitor's own, as the UI shows it (monitorLabel). Only the
// name discovery gave, which is where the monitor came from
// ("shop/service/web:80"), is shortened to the object's own name
// ("web:80"); a discovered monitor the operator renamed goes by that name.
func publicName(m store.Monitor) string {
	if m.Name == m.Namespace+"/"+m.Source {
		if _, object, found := strings.Cut(m.Source, "/"); found {
			return object
		}
	}
	return m.Name
}

// statusPagePreview is what a page shows at this moment, for whoever edits
// it: the explicit monitors and those its rules match, in their sections.
type statusPagePreview struct {
	Sections []previewSection `json:"sections"`
}

type previewSection struct {
	Name     string           `json:"name"`
	Monitors []previewMonitor `json:"monitors"`
}

type previewMonitor struct {
	MonitorID int64 `json:"monitor_id"`
	// Name is what the page shows; MonitorName and Namespace say which
	// monitor that is.
	Name        string `json:"name"`
	MonitorName string `json:"monitor_name"`
	Namespace   string `json:"namespace"`
	// Explicit is true for a monitor listed by name, false for one a rule
	// matched.
	Explicit bool `json:"explicit"`
	Enabled  bool `json:"enabled"`
}

// previewStatusPage answers GET /api/v1/status-pages/{id}/monitors.
func (h *handler) previewStatusPage(w http.ResponseWriter, r *http.Request) {
	p, ok := h.pageForEdit(w, r)
	if !ok {
		return
	}
	monitors, err := h.opts.Store.ListMonitors(r.Context())
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	resp := statusPagePreview{Sections: []previewSection{}}
	for _, s := range resolvePage(p, monitors) {
		section := previewSection{Name: s.Name, Monitors: make([]previewMonitor, 0, len(s.Monitors))}
		for _, m := range s.Monitors {
			section.Monitors = append(section.Monitors, previewMonitor{
				MonitorID: m.ID, Name: m.Name, MonitorName: m.Monitor.Name, Namespace: m.Namespace, Explicit: m.Explicit, Enabled: m.Enabled,
			})
		}
		resp.Sections = append(resp.Sections, section)
	}
	writeJSON(w, http.StatusOK, resp)
}
