package api

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

type updateResponse struct {
	ID     int64 `json:"id"`
	PageID int64 `json:"page_id"`
	// IncidentID is the incident the update is about; null for none.
	IncidentID *int64    `json:"incident_id"`
	Kind       string    `json:"kind"`
	Title      string    `json:"title"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"created_at"`
	CreatedBy  string    `json:"created_by"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func toUpdateResponse(u store.StatusPageUpdate) updateResponse {
	resp := updateResponse{
		ID: u.ID, PageID: u.PageID, Kind: string(u.Kind), Title: u.Title, Body: u.Body,
		CreatedAt: u.CreatedAt, CreatedBy: u.CreatedBy, UpdatedAt: u.UpdatedAt,
	}
	if u.IncidentID != 0 {
		resp.IncidentID = &u.IncidentID
	}
	return resp
}

// updateRequest carries a new update, or only the fields to change; a null
// incident_id on a change detaches the update from its incident.
type updateRequest struct {
	Kind       *string `json:"kind"`
	Title      *string `json:"title"`
	Body       *string `json:"body"`
	IncidentID *int64  `json:"incident_id"`
}

// applyUpdate validates the request and sets its fields on u. The
// incident, if any, must be one the principal may read: the update names
// it to the public under the page's name for its monitor.
func (h *handler) applyUpdate(r *http.Request, req updateRequest, u *store.StatusPageUpdate) error {
	if req.Kind != nil {
		u.Kind = store.UpdateKind(strings.TrimSpace(*req.Kind))
	}
	if req.Title != nil {
		u.Title = strings.TrimSpace(*req.Title)
	}
	if req.Body != nil {
		body, err := updateBody(*req.Body)
		if err != nil {
			return err
		}
		u.Body = body
	}
	if req.IncidentID != nil {
		u.IncidentID = *req.IncidentID
	}
	switch {
	case !slices.Contains(store.UpdateKinds, u.Kind):
		return errors.New("kind must be investigating, identified, monitoring, resolved, notice or maintenance")
	case u.Title == "" || len(u.Title) > maxStatusPageTitle:
		return fmt.Errorf("title is required and at most %d bytes", maxStatusPageTitle)
	case u.IncidentID < 0:
		return errors.New("incident_id must be positive")
	}
	if u.IncidentID != 0 {
		i, err := h.opts.Store.GetIncident(r.Context(), u.IncidentID)
		if errors.Is(err, store.ErrNotFound) || (err == nil && !principal(r).CanRead(i.MonitorNamespace)) {
			return fmt.Errorf("incident_id: there is no incident %d", u.IncidentID)
		}
		if err != nil {
			return internalFailure{err}
		}
	}
	return nil
}

// updateBody checks an update's text as noteText checks a note's, but an
// update may have none: its title can say it all.
func updateBody(body string) (string, error) {
	if strings.TrimSpace(body) == "" {
		return "", nil
	}
	text, err := noteText(body)
	if err != nil {
		return "", errors.New("body" + strings.TrimPrefix(err.Error(), "text"))
	}
	return text, nil
}

// updateSummary is what the audit log says an update was about.
func updateSummary(page store.StatusPage, u store.StatusPageUpdate) string {
	return fmt.Sprintf("%s: %s", page.Slug, u.Title)
}

// listStatusPageUpdates answers GET /api/v1/status-pages/{id}/updates,
// newest first.
func (h *handler) listStatusPageUpdates(w http.ResponseWriter, r *http.Request) {
	page, ok := h.pageForEdit(w, r)
	if !ok {
		return
	}
	limit, err := h.limit(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updates, err := h.opts.Store.ListStatusPageUpdates(r.Context(), page.ID, limit)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	resp := make([]updateResponse, 0, len(updates))
	for _, u := range updates {
		resp = append(resp, toUpdateResponse(u))
	}
	writeJSON(w, http.StatusOK, resp)
}

// createStatusPageUpdate answers POST /api/v1/status-pages/{id}/updates.
func (h *handler) createStatusPageUpdate(w http.ResponseWriter, r *http.Request) {
	page, ok := h.pageForEdit(w, r)
	if !ok {
		return
	}
	var req updateRequest
	if !decodeBody(w, r, &req) {
		return
	}
	u := store.StatusPageUpdate{PageID: page.ID, CreatedBy: principal(r).Username}
	if err := h.applyUpdate(r, req, &u); err != nil {
		h.updateError(w, r, err)
		return
	}
	created, err := h.opts.Store.CreateStatusPageUpdate(r.Context(), u)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "status page not found")
		return
	}
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	h.audit(r, "create", "status_page_update", created.ID, updateSummary(page, created))
	writeJSON(w, http.StatusCreated, toUpdateResponse(created))
}

// updateError answers for a request that applyUpdate refused.
func (h *handler) updateError(w http.ResponseWriter, r *http.Request, err error) {
	var internal internalFailure
	if errors.As(err, &internal) {
		h.internalError(w, r, internal.error)
		return
	}
	writeError(w, http.StatusBadRequest, err.Error())
}

// editableUpdate loads the page of the request's {id} for a change and the
// update of its {update}, answering 404 for one of another page.
func (h *handler) editableUpdate(w http.ResponseWriter, r *http.Request) (store.StatusPage, store.StatusPageUpdate, bool) {
	page, ok := h.pageForEdit(w, r)
	if !ok {
		return store.StatusPage{}, store.StatusPageUpdate{}, false
	}
	id, err := strconv.ParseInt(r.PathValue("update"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusNotFound, "update not found")
		return store.StatusPage{}, store.StatusPageUpdate{}, false
	}
	u, err := h.opts.Store.GetStatusPageUpdate(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) || (err == nil && u.PageID != page.ID) {
		writeError(w, http.StatusNotFound, "update not found")
		return store.StatusPage{}, store.StatusPageUpdate{}, false
	}
	if err != nil {
		h.internalError(w, r, err)
		return store.StatusPage{}, store.StatusPageUpdate{}, false
	}
	return page, u, true
}

// updateStatusPageUpdate answers PATCH
// /api/v1/status-pages/{id}/updates/{update}; the fields left out stay as
// they are.
func (h *handler) updateStatusPageUpdate(w http.ResponseWriter, r *http.Request) {
	page, u, ok := h.editableUpdate(w, r)
	if !ok {
		return
	}
	var req updateRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if err := h.applyUpdate(r, req, &u); err != nil {
		h.updateError(w, r, err)
		return
	}
	updated, err := h.opts.Store.UpdateStatusPageUpdate(r.Context(), u)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "update not found")
		return
	}
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	h.audit(r, "update", "status_page_update", updated.ID, updateSummary(page, updated))
	writeJSON(w, http.StatusOK, toUpdateResponse(updated))
}

// deleteStatusPageUpdate answers DELETE
// /api/v1/status-pages/{id}/updates/{update}.
func (h *handler) deleteStatusPageUpdate(w http.ResponseWriter, r *http.Request) {
	page, u, ok := h.editableUpdate(w, r)
	if !ok {
		return
	}
	err := h.opts.Store.DeleteStatusPageUpdate(r.Context(), u.ID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "update not found")
		return
	}
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	h.audit(r, "delete", "status_page_update", u.ID, updateSummary(page, u))
	w.WriteHeader(http.StatusNoContent)
}
