package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/yusufceng/Kubeat/backend/internal/auth"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// maxNoteLength bounds a note, in characters: room for what was done and
// why, not for a log dump, which belongs in a link.
const maxNoteLength = 4000

type noteResponse struct {
	ID         int64  `json:"id"`
	IncidentID int64  `json:"incident_id"`
	Author     string `json:"author"`
	Text       string `json:"text"`
	// ShowOnStatusPage lets the status pages showing the monitor show the
	// note.
	ShowOnStatusPage bool      `json:"show_on_status_page"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	// CanEdit says whether the caller may change or delete the note.
	CanEdit bool `json:"can_edit"`
}

func toNoteResponse(p auth.Principal, namespace string, n store.IncidentNote) noteResponse {
	return noteResponse{
		ID: n.ID, IncidentID: n.IncidentID, Author: n.Author, Text: n.Text, ShowOnStatusPage: n.ShowOnStatusPage,
		CreatedAt: n.CreatedAt, UpdatedAt: n.UpdatedAt, CanEdit: canEditNote(p, namespace, n),
	}
}

// canEditNote reports whether the principal may change or delete a note on
// an incident of a monitor in namespace: they must be able to change the
// monitor, and either have written the note or be an administrator. A
// note is its author's word, which another editor should not rewrite; an
// administrator can still remove one that should not be there.
func canEditNote(p auth.Principal, namespace string, n store.IncidentNote) bool {
	if !p.CanEdit(namespace) {
		return false
	}
	return p.IsAdmin() || (n.AuthorID != 0 && n.AuthorID == p.UserID)
}

// noteRequest carries a new note, or only the fields to change.
type noteRequest struct {
	Text             *string `json:"text"`
	ShowOnStatusPage *bool   `json:"show_on_status_page"`
}

// noteText checks a note's text and returns it as stored: trimmed, with
// Windows line breaks made plain. Line breaks and tabs are the only
// control characters it may hold.
func noteText(text string) (string, error) {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	switch {
	case text == "":
		return "", errors.New("text is required")
	case utf8.RuneCountInString(text) > maxNoteLength:
		return "", fmt.Errorf("text: at most %d characters", maxNoteLength)
	}
	for _, r := range text {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return "", errors.New("text must not contain control characters other than line breaks and tabs")
		}
	}
	return text, nil
}

// noteSummary is what the audit log says a note was about.
func noteSummary(i store.Incident) string {
	return fmt.Sprintf("%s, incident %d", i.MonitorName, i.ID)
}

// listIncidentNotes answers GET /api/v1/incidents/{id}/notes, oldest first.
func (h *handler) listIncidentNotes(w http.ResponseWriter, r *http.Request) {
	incident, ok := h.readableIncident(w, r)
	if !ok {
		return
	}
	notes, err := h.opts.Store.ListIncidentNotes(r.Context(), incident.ID, h.opts.MaxResults)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	p := principal(r)
	resp := make([]noteResponse, 0, len(notes))
	for _, n := range notes {
		resp = append(resp, toNoteResponse(p, incident.MonitorNamespace, n))
	}
	writeJSON(w, http.StatusOK, resp)
}

// createIncidentNote answers POST /api/v1/incidents/{id}/notes. Whoever may
// change the monitor may note on its incidents, resolved ones included:
// the notes are the postmortem's record.
func (h *handler) createIncidentNote(w http.ResponseWriter, r *http.Request) {
	incident, ok := h.readableIncident(w, r)
	if !ok {
		return
	}
	p := principal(r)
	if !p.CanEdit(incident.MonitorNamespace) {
		scopeError(w, p)
		return
	}
	var req noteRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if req.Text == nil {
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}
	text, err := noteText(*req.Text)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	n, err := h.opts.Store.CreateIncidentNote(r.Context(), store.IncidentNote{
		IncidentID: incident.ID, AuthorID: p.UserID, Author: p.Username, Text: text,
		ShowOnStatusPage: req.ShowOnStatusPage != nil && *req.ShowOnStatusPage,
	})
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "incident not found")
		return
	}
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	h.audit(r, "create", "incident_note", n.ID, noteSummary(incident))
	writeJSON(w, http.StatusCreated, toNoteResponse(p, incident.MonitorNamespace, n))
}

// editableNote loads the incident and the note of the request's {id} and
// {note} for a change, answering 404 for a note of another incident and 403
// unless the principal may change it.
func (h *handler) editableNote(w http.ResponseWriter, r *http.Request) (store.Incident, store.IncidentNote, bool) {
	incident, ok := h.readableIncident(w, r)
	if !ok {
		return store.Incident{}, store.IncidentNote{}, false
	}
	id, err := strconv.ParseInt(r.PathValue("note"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusNotFound, "note not found")
		return store.Incident{}, store.IncidentNote{}, false
	}
	n, err := h.opts.Store.GetIncidentNote(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) || (err == nil && n.IncidentID != incident.ID) {
		writeError(w, http.StatusNotFound, "note not found")
		return store.Incident{}, store.IncidentNote{}, false
	}
	if err != nil {
		h.internalError(w, r, err)
		return store.Incident{}, store.IncidentNote{}, false
	}
	p := principal(r)
	switch {
	case !p.CanEdit(incident.MonitorNamespace):
		scopeError(w, p)
		return store.Incident{}, store.IncidentNote{}, false
	case !canEditNote(p, incident.MonitorNamespace, n):
		writeError(w, http.StatusForbidden, "only its author or an administrator may change a note")
		return store.Incident{}, store.IncidentNote{}, false
	}
	return incident, n, true
}

// updateIncidentNote answers PATCH /api/v1/incidents/{id}/notes/{note};
// the fields left out stay as they are.
func (h *handler) updateIncidentNote(w http.ResponseWriter, r *http.Request) {
	incident, n, ok := h.editableNote(w, r)
	if !ok {
		return
	}
	var req noteRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if req.Text != nil {
		text, err := noteText(*req.Text)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		n.Text = text
	}
	if req.ShowOnStatusPage != nil {
		n.ShowOnStatusPage = *req.ShowOnStatusPage
	}
	updated, err := h.opts.Store.UpdateIncidentNote(r.Context(), n)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "note not found")
		return
	}
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	h.audit(r, "update", "incident_note", updated.ID, noteSummary(incident))
	writeJSON(w, http.StatusOK, toNoteResponse(principal(r), incident.MonitorNamespace, updated))
}

// deleteIncidentNote answers DELETE /api/v1/incidents/{id}/notes/{note}.
func (h *handler) deleteIncidentNote(w http.ResponseWriter, r *http.Request) {
	incident, n, ok := h.editableNote(w, r)
	if !ok {
		return
	}
	err := h.opts.Store.DeleteIncidentNote(r.Context(), n.ID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "note not found")
		return
	}
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	h.audit(r, "delete", "incident_note", n.ID, noteSummary(incident))
	w.WriteHeader(http.StatusNoContent)
}
