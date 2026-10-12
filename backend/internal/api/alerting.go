package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/alert"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

type incidentResponse struct {
	ID          int64      `json:"id"`
	MonitorID   int64      `json:"monitor_id"`
	MonitorName string     `json:"monitor_name"`
	Kind        string     `json:"kind"` // "down" or "cert"
	StartedAt   time.Time  `json:"started_at"`
	ResolvedAt  *time.Time `json:"resolved_at"` // null while open
	Cause       string     `json:"cause"`
}

func toIncidentResponse(i store.Incident) incidentResponse {
	resp := incidentResponse{ID: i.ID, MonitorID: i.MonitorID, MonitorName: i.MonitorName, Kind: string(i.Kind), StartedAt: i.StartedAt, Cause: i.Cause}
	if !i.Open() {
		resp.ResolvedAt = &i.ResolvedAt
	}
	return resp
}

func (h *handler) listIncidents(w http.ResponseWriter, r *http.Request) {
	limit, err := h.limit(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var monitorID int64
	if v := r.URL.Query().Get("monitor_id"); v != "" {
		if monitorID, err = strconv.ParseInt(v, 10, 64); err != nil || monitorID < 1 {
			writeError(w, http.StatusBadRequest, "monitor_id must be a positive integer")
			return
		}
	}

	incidents, err := h.opts.Store.ListIncidents(r.Context(), monitorID, limit)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	resp := make([]incidentResponse, 0, len(incidents))
	for _, i := range incidents {
		resp = append(resp, toIncidentResponse(i))
	}
	writeJSON(w, http.StatusOK, resp)
}

// maxRecipients bounds the addresses of one e-mail channel.
const maxRecipients = 20

type channelResponse struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
	// Target is the channel's URL or, for e-mail, its addresses. URLs are
	// masked: a webhook URL is a credential, and anyone who can read it can
	// post to the channel.
	Target    string    `json:"target"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

// maskURL keeps the scheme and host and hides the path, query and user info.
func maskURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "…"
	}
	return u.Scheme + "://" + u.Host + "/…"
}

func toChannelResponse(c store.Channel) channelResponse {
	target := c.Target
	if c.Type != store.ChannelEmail {
		target = maskURL(target)
	}
	return channelResponse{ID: c.ID, Name: c.Name, Type: string(c.Type), Target: target, Enabled: c.Enabled, CreatedAt: c.CreatedAt}
}

func (h *handler) listChannels(w http.ResponseWriter, r *http.Request) {
	channels, err := h.opts.Store.ListChannels(r.Context())
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	resp := make([]channelResponse, 0, len(channels))
	for _, c := range channels {
		resp = append(resp, toChannelResponse(c))
	}
	writeJSON(w, http.StatusOK, resp)
}

type createChannelRequest struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Target string `json:"target"`
}

// channelTarget validates a channel's target for its type and returns the
// form to store.
func (h *handler) channelTarget(typ store.ChannelType, target string) (string, error) {
	switch typ {
	case store.ChannelWebhook, store.ChannelSlack, store.ChannelTeams:
		u, err := url.Parse(target)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return "", errors.New("target must be an http:// or https:// URL")
		}
		return target, nil
	case store.ChannelEmail:
		if !h.opts.EmailEnabled {
			return "", errors.New("e-mail channels need an SMTP server; set UPTIME_SMTP_HOST and UPTIME_SMTP_FROM")
		}
		recipients, err := alert.ParseRecipients(target)
		if err != nil {
			return "", fmt.Errorf("target: %w", err)
		}
		if len(recipients) > maxRecipients {
			return "", fmt.Errorf("target: at most %d addresses", maxRecipients)
		}
		return strings.Join(recipients, ", "), nil
	default:
		return "", errors.New("type must be webhook, slack, teams or email")
	}
}

func (h *handler) createChannel(w http.ResponseWriter, r *http.Request) {
	var req createChannelRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	typ := store.ChannelType(req.Type)
	target, err := h.channelTarget(typ, strings.TrimSpace(req.Target))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	c, err := h.opts.Store.CreateChannel(r.Context(), store.Channel{Name: req.Name, Type: typ, Target: target, Enabled: true})
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "a channel with this name already exists")
		return
	}
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toChannelResponse(c))
}

func (h *handler) deleteChannel(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}
	err := h.opts.Store.DeleteChannel(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type testChannelResponse struct {
	Delivered bool `json:"delivered"`
	// Error says why delivery failed; it never includes the channel URL.
	Error string `json:"error,omitempty"`
}

// testChannel sends a test notification now and reports whether the channel
// accepted it. A failed delivery is a normal 200 answer with delivered=false.
func (h *handler) testChannel(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}
	ch, err := h.opts.Store.GetChannel(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}
	if err != nil {
		h.internalError(w, r, err)
		return
	}

	if err := h.opts.Notifier.SendOnce(r.Context(), ch, alert.Notification{Event: alert.EventTest}); err != nil {
		h.opts.Logger.Warn("channel test failed", "channel", ch.Name, "error", err)
		writeJSON(w, http.StatusOK, testChannelResponse{Error: redactURL(err.Error(), ch.Target)})
		return
	}
	writeJSON(w, http.StatusOK, testChannelResponse{Delivered: true})
}

// redactURL removes the webhook URL that net/http embeds in its errors.
func redactURL(msg, rawURL string) string {
	if !strings.Contains(rawURL, "://") {
		return msg // an e-mail channel: nothing secret in the target
	}
	return strings.ReplaceAll(msg, rawURL, maskURL(rawURL))
}
