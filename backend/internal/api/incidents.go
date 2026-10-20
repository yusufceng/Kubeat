package api

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yusufceng/Kubeat/backend/internal/alert"
	"github.com/yusufceng/Kubeat/backend/internal/auth"
	"github.com/yusufceng/Kubeat/backend/internal/k8s"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

const (
	// maxFilterNamespaces bounds the namespaces one incident query names.
	maxFilterNamespaces = 100
	// maxSearchLength bounds the text a query looks for in monitor names.
	maxSearchLength = 100
)

// Incident states a list can be limited to.
const (
	stateOpen     = "open"
	stateResolved = "resolved"
)

// readableNamespaces is what a principal's incident query is limited to:
// the namespaces asked for that they may read or, when none were asked
// for, every namespace they may read. nil means every namespace there is;
// an empty list, none (a scoped user asking only for others' namespaces).
func readableNamespaces(p auth.Principal, wanted []string, asked bool) []string {
	if !asked {
		if p.SeesAll() {
			return nil
		}
		return append([]string{}, p.Namespaces...)
	}
	out := []string{}
	for _, ns := range wanted {
		if p.CanRead(ns) && !slices.Contains(out, ns) {
			out = append(out, ns)
		}
	}
	return out
}

// incidentFilter reads what GET /api/v1/incidents selects, how it orders
// it and which page it wants, limited to what the principal may read. A
// list is a page of MaxResults at most; an export (all) has no bound
// unless the request sets one.
func (h *handler) incidentFilter(r *http.Request, all bool) (store.IncidentFilter, error) {
	q := r.URL.Query()
	f := store.IncidentFilter{Now: time.Now().UTC()}

	wanted, asked := q["namespace"], q.Has("namespace")
	if len(wanted) > maxFilterNamespaces {
		return f, fmt.Errorf("namespace: at most %d", maxFilterNamespaces)
	}
	f.Namespaces = readableNamespaces(principal(r), wanted, asked)

	if v := q.Get("monitor_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id < 1 {
			return f, errors.New("monitor_id must be a positive integer")
		}
		f.MonitorID = id
	}
	switch kind := store.IncidentKind(q.Get("kind")); kind {
	case "", store.IncidentDown, store.IncidentCert:
		f.Kind = kind
	default:
		return f, errors.New("kind must be down or cert")
	}
	switch state := q.Get("state"); state {
	case "":
	case stateOpen, stateResolved:
		open := state == stateOpen
		f.Open = &open
	default:
		return f, errors.New("state must be open or resolved")
	}
	if f.MonitorName = strings.TrimSpace(q.Get("q")); utf8.RuneCountInString(f.MonitorName) > maxSearchLength {
		return f, fmt.Errorf("q: at most %d characters", maxSearchLength)
	}
	for _, bound := range []struct {
		name string
		dst  *time.Time
	}{{"from", &f.From}, {"to", &f.To}} {
		if v := q.Get(bound.name); v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				return f, fmt.Errorf("%s must be an RFC 3339 time", bound.name)
			}
			*bound.dst = t.UTC()
		}
	}
	if !f.From.IsZero() && !f.To.IsZero() && !f.To.After(f.From) {
		return f, errors.New("to must be after from")
	}

	// The newest and the longest first, the monitors from A to Z.
	f.Sort = store.SortByStart
	if v := q.Get("sort"); v != "" {
		if f.Sort = store.IncidentSort(v); !slices.Contains(store.IncidentSorts, f.Sort) {
			return f, errors.New("sort must be started_at, duration or monitor")
		}
	}
	f.Desc = f.Sort != store.SortByMonitor
	switch q.Get("order") {
	case "":
	case "asc":
		f.Desc = false
	case "desc":
		f.Desc = true
	default:
		return f, errors.New("order must be asc or desc")
	}

	if !all || q.Has("limit") {
		limit, err := h.limit(r)
		if err != nil {
			return f, err
		}
		f.Limit = limit
	}
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return f, errors.New("offset must be a non-negative integer")
		}
		f.Offset = n
	}
	return f, nil
}

// listIncidents answers GET /api/v1/incidents: a page of the incidents the
// filters select, newest first unless asked otherwise, as JSON with the
// number of all matching ones in X-Total-Count, or with format=csv all of
// them as a file.
func (h *handler) listIncidents(w http.ResponseWriter, r *http.Request) {
	export := r.URL.Query().Get("format") == "csv"
	f, err := h.incidentFilter(r, export)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	incidents, err := h.opts.Store.FilterIncidents(r.Context(), f)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	if export {
		writeIncidentsCSV(w, incidents, f.Now)
		return
	}
	total, err := h.opts.Store.CountIncidents(r.Context(), f)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	resp := make([]incidentResponse, 0, len(incidents))
	for _, i := range incidents {
		resp = append(resp, toIncidentResponse(i))
	}
	w.Header().Set("X-Total-Count", strconv.Itoa(total))
	writeJSON(w, http.StatusOK, resp)
}

// incidentDuration is how long an incident lasted, or has lasted by now.
func incidentDuration(i store.Incident, now time.Time) time.Duration {
	end := i.ResolvedAt
	if i.Open() {
		end = now
	}
	return max(end.Sub(i.StartedAt), 0)
}

func writeIncidentsCSV(w http.ResponseWriter, incidents []store.Incident, now time.Time) {
	out := newCSVDownload(w, fmt.Sprintf("kubeat-incidents-%s.csv", now.Format("2006-01-02")))
	out.row("id", "monitor_id", "monitor", "namespace", "kind", "state", "started_at", "opened_at", "resolved_at", "duration_seconds", "cause")
	for _, i := range incidents {
		state, opened, resolved := stateOpen, "", ""
		if !i.OpenedAt.IsZero() {
			opened = i.OpenedAt.Format(time.RFC3339)
		}
		if !i.Open() {
			state, resolved = stateResolved, i.ResolvedAt.Format(time.RFC3339)
		}
		out.row(strconv.FormatInt(i.ID, 10), strconv.FormatInt(i.MonitorID, 10), i.MonitorName, i.MonitorNamespace, string(i.Kind), state,
			i.StartedAt.Format(time.RFC3339), opened, resolved, strconv.FormatInt(int64(incidentDuration(i, now)/time.Second), 10), i.Cause)
	}
	out.flush()
}

// readableIncident loads the incident of the request's {id}, answering 404
// for one that does not exist or whose monitor the principal may not read.
func (h *handler) readableIncident(w http.ResponseWriter, r *http.Request) (store.Incident, bool) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "incident not found")
		return store.Incident{}, false
	}
	i, err := h.opts.Store.GetIncident(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) || (err == nil && !principal(r).CanRead(i.MonitorNamespace)) {
		writeError(w, http.StatusNotFound, "incident not found")
		return store.Incident{}, false
	}
	if err != nil {
		h.internalError(w, r, err)
		return store.Incident{}, false
	}
	return i, true
}

// incidentPageResponse is everything the incident page shows.
type incidentPageResponse struct {
	Incident incidentDetail          `json:"incident"`
	Monitor  incidentMonitorResponse `json:"monitor"`
	// Checks counts the checks from the incident's start to its end (or
	// now); null for a certificate incident, and for an outage that lies
	// wholly before the raw retention.
	Checks *checkSummaryResponse `json:"checks"`
	// Timeline is what happened, oldest first; see timelineItem.
	Timeline []timelineItem `json:"timeline"`
	// EventsTruncated says the cluster had more events in the span than
	// the timeline shows (EventLimit, the oldest first); EventsReason why
	// it has none when the cluster could not be asked. Only an outage is
	// told with the cluster's events: a certificate has none.
	EventsTruncated bool   `json:"events_truncated"`
	EventsReason    string `json:"events_reason,omitempty"`
	// CanNote says whether the caller may add notes.
	CanNote bool `json:"can_note"`
}

type incidentDetail struct {
	incidentResponse
	// DurationSeconds is how long the incident lasted, or has lasted so far.
	DurationSeconds int64 `json:"duration_seconds"`
}

// incidentMonitorResponse is the monitor an incident is about.
type incidentMonitorResponse struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Type      string `json:"type"`
	Target    string `json:"target"`
	Source    string `json:"source"`
	// Kind is the kind of cluster object the monitor follows: service,
	// ingress, route, deployment, statefulset or daemonset; empty for a
	// monitor entered by hand that names none.
	Kind    string `json:"kind"`
	Managed bool   `json:"managed"`
	// The monitor's settings now, which may have differed during the
	// incident.
	IntervalSeconds  int `json:"interval_seconds"`
	FailureThreshold int `json:"failure_threshold"`
}

// sourceKind is the kind of cluster object a monitor follows: that of its
// source ("service/api:80"), or for a workload monitor the kind its target
// names ("shop/deployment/web").
func sourceKind(m store.Monitor) string {
	if kind, _, ok := strings.Cut(m.Source, "/"); ok {
		return kind
	}
	if m.Type == store.MonitorWorkload {
		if _, kind, _, err := k8s.ParseWorkloadTarget(m.Target); err == nil {
			return string(kind)
		}
	}
	return ""
}

// The kinds of timeline item; each carries the field of the same name.
const (
	itemCheckFailed          = "check_failed"          // check
	itemChecksOmitted        = "checks_omitted"        // omitted
	itemOpened               = "opened"                // opened
	itemResolved             = "resolved"              // resolved
	itemReminder             = "reminder"              // reminder
	itemNotification         = "notification"          // notification
	itemNotificationsOmitted = "notifications_omitted" // omitted
	itemNote                 = "note"                  // note
	itemClusterEvent         = "cluster_event"         // event
	itemMaintenance          = "maintenance"           // maintenance
)

// itemOrder breaks ties between items of the same moment: what framed or
// caused a step comes before it, the incident's own steps in their order,
// and what people wrote after all of it.
var itemOrder = map[string]int{
	itemMaintenance: 0, itemClusterEvent: 1, itemCheckFailed: 2, itemChecksOmitted: 3, itemOpened: 4,
	itemResolved: 5, itemReminder: 6, itemNotification: 7, itemNotificationsOmitted: 8, itemNote: 9,
}

// timelineItem is one step of an incident's story. Kind says which and
// which one of the other fields is set:
//
//   - check_failed: a failed check during the incident (check); the first
//     is the one that began the streak, at the incident's started_at.
//   - checks_omitted: failed checks left out between the first and the
//     last ones shown (omitted), when there were more than TimelineLimit.
//   - opened: the incident was opened (opened): the check that reached the
//     failure threshold, or the certificate found to expire soon.
//   - notification: one channel's delivery attempt (notification).
//   - notifications_omitted: delivery attempts left out (omitted).
//   - reminder: a reminder fell due (reminder); its notifications follow.
//   - note: a note someone left (note), at its creation.
//   - cluster_event: what the cluster said about the monitor's pods and
//     what manages them (event).
//   - maintenance: an occurrence of a maintenance window covering the
//     monitor (maintenance), at its start, which may precede the incident.
//   - resolved: the first successful check of the recovery (resolved).
type timelineItem struct {
	At   time.Time `json:"at"`
	Kind string    `json:"kind"`

	Check        *resultResponse       `json:"check,omitempty"`
	Omitted      *omittedResponse      `json:"omitted,omitempty"`
	Opened       *openedResponse       `json:"opened,omitempty"`
	Notification *notificationResponse `json:"notification,omitempty"`
	Reminder     *reminderResponse     `json:"reminder,omitempty"`
	Note         *noteResponse         `json:"note,omitempty"`
	Event        *eventResponse        `json:"event,omitempty"`
	Maintenance  *timelineMaintenance  `json:"maintenance,omitempty"`
	Resolved     *resolvedResponse     `json:"resolved,omitempty"`
}

// omittedResponse stands for the entries left out of a long list: Count of
// them, all between From and To.
type omittedResponse struct {
	Count int       `json:"count"`
	From  time.Time `json:"from"`
	To    time.Time `json:"to"`
}

type openedResponse struct {
	// FailureThreshold is how many failed checks in a row open an outage
	// of the monitor (as set now); zero for a certificate incident.
	FailureThreshold int    `json:"failure_threshold"`
	Cause            string `json:"cause"`
}

// notificationResponse is one attempt to tell a channel about the
// incident, as it ended after its retries. The channel is named as it was
// then; ChannelDeleted says it is gone since. Error says why it was not
// delivered, redacted, and only to whoever may manage the channel (as in
// the channel list): an administrator, or an editor for a channel limited
// to their namespaces. Others see that it failed.
type notificationResponse struct {
	// Event is down, up, reminder, cert_expiring or cert_renewed; EventAt
	// is when that happened, which a grouped message waited after.
	Event          string    `json:"event"`
	EventAt        time.Time `json:"event_at"`
	ChannelID      int64     `json:"channel_id"`
	ChannelName    string    `json:"channel_name"`
	ChannelType    string    `json:"channel_type"`
	ChannelDeleted bool      `json:"channel_deleted"`
	// Grouped says the message was about GroupSize monitors of the
	// namespace at once, this one among them.
	Grouped   bool   `json:"grouped"`
	GroupSize int    `json:"group_size"`
	Delivered bool   `json:"delivered"`
	Error     string `json:"error,omitempty"`
}

type reminderResponse struct {
	// DownForSeconds is how long the outage had lasted when the reminder
	// fell due.
	DownForSeconds int64 `json:"down_for_seconds"`
}

// timelineMaintenance is one occurrence of a maintenance window.
type timelineMaintenance struct {
	ID       int64     `json:"id"`
	Name     string    `json:"name"`
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
	Repeat   string    `json:"repeat"`
	// Scope: one monitor, one namespace, or (both empty) every monitor.
	Namespace string `json:"namespace"`
	MonitorID *int64 `json:"monitor_id"`
}

type resolvedResponse struct {
	// DurationSeconds is how long the incident lasted, from its first
	// failed check to the first successful one.
	DurationSeconds int64 `json:"duration_seconds"`
}

// getIncident answers GET /api/v1/incidents/{id}: the incident, its
// monitor, and its story as one timeline. The checks come from the raw
// results (UPTIME_RETENTION), the deliveries from the delivery log
// (UPTIME_DELIVERY_RETENTION) and the events from the cluster, which keeps
// them for hours; an older incident has what is left of each.
func (h *handler) getIncident(w http.ResponseWriter, r *http.Request) {
	incident, ok := h.readableIncident(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	m, err := h.opts.Store.GetMonitor(ctx, incident.MonitorID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "incident not found") // deleted meanwhile
		return
	}
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	p := principal(r)
	now := time.Now().UTC()
	end := incident.ResolvedAt
	if incident.Open() {
		end = now
	}
	resp := incidentPageResponse{
		Incident: incidentDetail{incidentResponse: toIncidentResponse(incident), DurationSeconds: int64(incidentDuration(incident, now) / time.Second)},
		Monitor: incidentMonitorResponse{
			ID: m.ID, Name: m.Name, Namespace: m.Namespace, Type: string(m.Type), Target: m.Target, Source: m.Source,
			Kind: sourceKind(m), Managed: m.Managed, IntervalSeconds: int(m.Interval / time.Second), FailureThreshold: m.FailureThreshold,
		},
		CanNote: p.CanEdit(m.Namespace),
	}
	var items []timelineItem

	opened := openedResponse{Cause: incident.Cause}
	if incident.Kind == store.IncidentDown {
		opened.FailureThreshold = m.FailureThreshold
		checks, summary, err := h.incidentChecks(r, incident, end, now)
		if err != nil {
			h.internalError(w, r, err)
			return
		}
		items, resp.Checks = append(items, checks...), summary
		// What caused the outage happened up to one check before its first
		// failure, as on the chart's range panel.
		events, truncated, reason := h.eventsBetween(r, m, incident.StartedAt.Add(-m.Interval), end)
		for _, e := range events {
			items = append(items, timelineItem{At: e.At, Kind: itemClusterEvent, Event: &e})
		}
		resp.EventsTruncated, resp.EventsReason = truncated, reason
	}
	openedAt := incident.OpenedAt
	if openedAt.IsZero() {
		openedAt = incident.StartedAt // opened before the moment was recorded
	}
	items = append(items, timelineItem{At: openedAt, Kind: itemOpened, Opened: &opened})
	if !incident.Open() {
		items = append(items, timelineItem{At: incident.ResolvedAt, Kind: itemResolved,
			Resolved: &resolvedResponse{DurationSeconds: int64(incidentDuration(incident, now) / time.Second)}})
	}

	notified, err := h.incidentNotifications(r, incident)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	items = append(items, notified...)

	notes, err := h.opts.Store.ListIncidentNotes(ctx, incident.ID, h.opts.MaxResults)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	for _, n := range notes {
		note := toNoteResponse(p, incident.MonitorNamespace, n)
		items = append(items, timelineItem{At: n.CreatedAt, Kind: itemNote, Note: &note})
	}

	windows, err := h.opts.Store.ListMaintenanceBetween(ctx, incident.StartedAt, end)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	for _, mw := range windows {
		if !mw.Covers(m) || !maintenanceVisible(p, mw) {
			continue
		}
		for _, o := range mw.Occurrences(incident.StartedAt, end, h.opts.TimelineLimit) {
			item := timelineMaintenance{ID: mw.ID, Name: mw.Name, StartsAt: o.Start, EndsAt: o.End, Repeat: string(mw.Repeat), Namespace: mw.Namespace}
			if mw.MonitorID != 0 {
				item.MonitorID = &mw.MonitorID
			}
			items = append(items, timelineItem{At: o.Start, Kind: itemMaintenance, Maintenance: &item})
		}
	}

	sort.SliceStable(items, func(i, j int) bool {
		if a, b := items[i], items[j]; !a.At.Equal(b.At) {
			return a.At.Before(b.At)
		}
		return itemOrder[items[i].Kind] < itemOrder[items[j].Kind]
	})
	resp.Timeline = items
	if resp.Timeline == nil {
		resp.Timeline = []timelineItem{}
	}
	writeJSON(w, http.StatusOK, resp)
}

// incidentChecks are the failed checks of an outage as timeline items, the
// first and last of them when there are more than TimelineLimit, and the
// count of all its checks. Raw results reach back RawRetention: an outage
// that ended before that has neither.
func (h *handler) incidentChecks(r *http.Request, incident store.Incident, end, now time.Time) ([]timelineItem, *checkSummaryResponse, error) {
	retained := now.Add(-h.opts.RawRetention)
	if !end.After(retained) {
		return nil, nil, nil
	}
	sum, err := h.opts.Store.SummarizeChecks(r.Context(), incident.MonitorID, incident.StartedAt, end)
	if err != nil {
		return nil, nil, err
	}
	summary := &checkSummaryResponse{
		Total: sum.Total, Failed: sum.Failed, Partial: incident.StartedAt.Before(retained),
		AvgLatencyMS: float64(sum.AvgLatency) / float64(time.Millisecond), MaxLatencyMS: sum.MaxLatency.Milliseconds(),
	}
	// A resolved incident ends at its first success: the failures are
	// before it. An open one goes on to now.
	failures, total, err := h.opts.Store.FailedChecksEnds(r.Context(), incident.MonitorID, incident.StartedAt, end, h.opts.TimelineLimit)
	if err != nil {
		return nil, nil, err
	}
	oldest, _ := store.Ends(total, h.opts.TimelineLimit)
	items := make([]timelineItem, 0, len(failures)+1)
	for i, f := range failures {
		if i == oldest && total > len(failures) {
			items = append(items, timelineItem{At: failures[i-1].CheckedAt, Kind: itemChecksOmitted,
				Omitted: &omittedResponse{Count: total - len(failures), From: failures[i-1].CheckedAt, To: f.CheckedAt}})
		}
		check := toResultResponse(f)
		items = append(items, timelineItem{At: f.CheckedAt, Kind: itemCheckFailed, Check: &check})
	}
	return items, summary, nil
}

// incidentNotifications are the incident's delivery log as timeline items,
// with a reminder item for each reminder that went out.
func (h *handler) incidentNotifications(r *http.Request, incident store.Incident) ([]timelineItem, error) {
	deliveries, total, err := h.opts.Store.DeliveriesEnds(r.Context(), incident.ID, h.opts.TimelineLimit)
	if err != nil || len(deliveries) == 0 {
		return nil, err
	}
	channels, err := h.opts.Store.ListChannels(r.Context())
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]store.Channel, len(channels))
	for _, ch := range channels {
		byID[ch.ID] = ch
	}
	p := principal(r)
	oldest, _ := store.Ends(total, h.opts.TimelineLimit)
	var (
		items     []timelineItem
		reminders []time.Time
	)
	for i, d := range deliveries {
		if i == oldest && total > len(deliveries) {
			items = append(items, timelineItem{At: deliveries[i-1].At, Kind: itemNotificationsOmitted,
				Omitted: &omittedResponse{Count: total - len(deliveries), From: deliveries[i-1].At, To: d.At}})
		}
		n := notificationResponse{
			Event: d.Event, EventAt: d.EventAt, ChannelID: d.ChannelID, ChannelName: d.ChannelName, ChannelType: string(d.ChannelType),
			Grouped: d.GroupSize > 1, GroupSize: d.GroupSize, Delivered: d.Delivered,
		}
		// An id of a deleted channel can be taken by a new one; one of
		// another type is not the channel the entry is about.
		ch, exists := byID[d.ChannelID]
		exists = exists && ch.Type == d.ChannelType
		n.ChannelDeleted = !exists
		if (exists && channelVisible(p, ch)) || (!exists && p.IsAdmin()) {
			n.Error = d.Error
		}
		items = append(items, timelineItem{At: d.At, Kind: itemNotification, Notification: &n})
		if d.Event == string(alert.EventReminder) && !slices.ContainsFunc(reminders, d.EventAt.Equal) {
			reminders = append(reminders, d.EventAt)
		}
	}
	for _, at := range reminders {
		items = append(items, timelineItem{At: at, Kind: itemReminder,
			Reminder: &reminderResponse{DownForSeconds: int64(max(at.Sub(incident.StartedAt), 0) / time.Second)}})
	}
	return items, nil
}
