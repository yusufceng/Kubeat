package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
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

const (
	// maxRecipients bounds the addresses of one e-mail channel.
	maxRecipients = 20
	// maxChannelNamespaces bounds the namespaces one channel is limited to.
	maxChannelNamespaces = 50
	maxSecretLength      = 512
)

type channelResponse struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
	// Target is where the channel sends to. URLs are masked: a webhook URL
	// is a credential, and anyone who can read it can post to the channel.
	Target string `json:"target"`
	// HasSecret says whether a token or key is stored; the value itself
	// never leaves the server.
	HasSecret bool `json:"has_secret"`
	// Namespaces limits the channel to the monitors of these namespaces;
	// empty means every monitor.
	Namespaces []string  `json:"namespaces"`
	Enabled    bool      `json:"enabled"`
	CreatedAt  time.Time `json:"created_at"`
}

// maskURL keeps the scheme and host and hides the path, query and user info.
func maskURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "…"
	}
	return u.Scheme + "://" + u.Host + "/…"
}

// shownTarget is a channel's target as responses carry it.
func shownTarget(c store.Channel) string {
	if strings.Contains(c.Target, "://") {
		return maskURL(c.Target)
	}
	return c.Target // addresses, a chat: nothing that lets one post
}

func toChannelResponse(c store.Channel) channelResponse {
	namespaces := c.Namespaces
	if namespaces == nil {
		namespaces = []string{}
	}
	return channelResponse{
		ID: c.ID, Name: c.Name, Type: string(c.Type), Target: shownTarget(c), HasSecret: c.Secret != "",
		Namespaces: namespaces, Enabled: c.Enabled, CreatedAt: c.CreatedAt,
	}
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
	// Secret is the token or key of the types that have one.
	Secret     string   `json:"secret"`
	Namespaces []string `json:"namespaces"`
}

var (
	// A Telegram chat is a numeric id (negative for groups) or a public
	// channel's @name.
	telegramChat = regexp.MustCompile(`^(-?\d+|@[A-Za-z][A-Za-z0-9_]{3,})$`)
	// A bot token is the bot's id, a colon and a secret.
	telegramToken = regexp.MustCompile(`^\d+:[A-Za-z0-9_-]+$`)
)

func webURL(target string) error {
	u, err := url.Parse(target)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("target must be an http:// or https:// URL")
	}
	return nil
}

// channelSettings validates a channel's target and secret for its type and
// returns the forms to store.
func (h *handler) channelSettings(typ store.ChannelType, target, secret string) (string, string, error) {
	if len(secret) > maxSecretLength || strings.ContainsAny(secret, " \t\r\n") {
		return "", "", errors.New("secret is too long or contains white space")
	}
	switch typ {
	case store.ChannelWebhook, store.ChannelSlack, store.ChannelTeams, store.ChannelDiscord,
		store.ChannelMattermost, store.ChannelGoogleChat:
		// The URL is the credential; there is nothing else to keep.
		return target, "", webURL(target)
	case store.ChannelGotify:
		if secret == "" {
			return "", "", errors.New("secret is required: the application token of the Gotify server")
		}
		return target, secret, webURL(target)
	case store.ChannelNtfy:
		if _, _, err := alert.SplitNtfyTopic(target); err != nil {
			return "", "", fmt.Errorf("target %w", err)
		}
		return target, secret, nil
	case store.ChannelTelegram:
		if !telegramChat.MatchString(target) {
			return "", "", errors.New("target must be a chat id such as -1001234567890, or @name of a public channel")
		}
		if !telegramToken.MatchString(secret) {
			return "", "", errors.New("secret must be the bot token, as given by BotFather")
		}
		return target, secret, nil
	case store.ChannelPagerDuty:
		if secret == "" {
			return "", "", errors.New("secret is required: the integration key of an Events API v2 integration")
		}
		return "", secret, nil
	case store.ChannelEmail:
		if !h.opts.EmailEnabled {
			return "", "", errors.New("e-mail channels need an SMTP server; set UPTIME_SMTP_HOST and UPTIME_SMTP_FROM")
		}
		recipients, err := alert.ParseRecipients(target)
		if err != nil {
			return "", "", fmt.Errorf("target: %w", err)
		}
		if len(recipients) > maxRecipients {
			return "", "", fmt.Errorf("target: at most %d addresses", maxRecipients)
		}
		return strings.Join(recipients, ", "), "", nil
	default:
		types := make([]string, len(store.ChannelTypes))
		for i, t := range store.ChannelTypes {
			types[i] = string(t)
		}
		return "", "", fmt.Errorf("type must be one of %s", strings.Join(types, ", "))
	}
}

// channelNamespaces validates a channel's scope and returns it sorted and
// without duplicates.
func channelNamespaces(namespaces []string) ([]string, error) {
	seen := make(map[string]bool, len(namespaces))
	var out []string
	for _, ns := range namespaces {
		ns = strings.TrimSpace(ns)
		if !validGroup(ns) {
			return nil, fmt.Errorf("namespaces: %q is not the name of a namespace or group", ns)
		}
		if !seen[ns] {
			seen[ns] = true
			out = append(out, ns)
		}
	}
	if len(out) > maxChannelNamespaces {
		return nil, fmt.Errorf("namespaces: at most %d", maxChannelNamespaces)
	}
	sort.Strings(out)
	return out, nil
}

func (h *handler) createChannel(w http.ResponseWriter, r *http.Request) {
	var req createChannelRequest
	if !decodeBody(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	typ := store.ChannelType(req.Type)
	target, secret, err := h.channelSettings(typ, strings.TrimSpace(req.Target), strings.TrimSpace(req.Secret))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	namespaces, err := channelNamespaces(req.Namespaces)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	c, err := h.opts.Store.CreateChannel(r.Context(), store.Channel{
		Name: req.Name, Type: typ, Target: target, Secret: secret, Namespaces: namespaces, Enabled: true,
	})
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

// updateChannelRequest carries only the fields to change.
type updateChannelRequest struct {
	Name *string `json:"name"`
	// Target sent back as it was shown (masked) keeps the stored one, and
	// so does a Secret sent as the mask.
	Target     *string   `json:"target"`
	Secret     *string   `json:"secret"`
	Namespaces *[]string `json:"namespaces"`
	Enabled    *bool     `json:"enabled"`
}

func (h *handler) updateChannel(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}
	var req updateChannelRequest
	if !decodeBody(w, r, &req) {
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

	if req.Name != nil {
		if ch.Name = strings.TrimSpace(*req.Name); ch.Name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}
	}
	if req.Target != nil || req.Secret != nil {
		target, secret := ch.Target, ch.Secret
		if req.Target != nil && strings.TrimSpace(*req.Target) != shownTarget(ch) {
			target = strings.TrimSpace(*req.Target)
		}
		if req.Secret != nil && *req.Secret != maskedValue {
			secret = strings.TrimSpace(*req.Secret)
		}
		if ch.Target, ch.Secret, err = h.channelSettings(ch.Type, target, secret); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if req.Namespaces != nil {
		if ch.Namespaces, err = channelNamespaces(*req.Namespaces); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if req.Enabled != nil {
		ch.Enabled = *req.Enabled
	}

	updated, err := h.opts.Store.UpdateChannel(r.Context(), ch)
	switch {
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "a channel with this name already exists")
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "channel not found")
	case err != nil:
		h.internalError(w, r, err)
	default:
		writeJSON(w, http.StatusOK, toChannelResponse(updated))
	}
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
		writeJSON(w, http.StatusOK, testChannelResponse{Error: redact(err.Error(), ch)})
		return
	}
	writeJSON(w, http.StatusOK, testChannelResponse{Delivered: true})
}

// redact removes the channel's URL and secret from an error text, should
// anything have put them there.
func redact(msg string, ch store.Channel) string {
	if strings.Contains(ch.Target, "://") {
		msg = strings.ReplaceAll(msg, ch.Target, maskURL(ch.Target))
	}
	if ch.Secret != "" {
		msg = strings.ReplaceAll(msg, ch.Secret, maskedValue)
	}
	return msg
}
