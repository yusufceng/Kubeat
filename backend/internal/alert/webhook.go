package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// WebhookSender posts notifications as JSON. The body carries structured
// fields and a human-readable "text", the field chat tools such as Slack and
// Mattermost read from incoming webhooks.
type WebhookSender struct {
	Client    *http.Client
	UserAgent string
}

type webhookBody struct {
	Event Event  `json:"event"`
	Text  string `json:"text"`
	// URL is where to look at this in Kubeat; absent without a public
	// address (UPTIME_PUBLIC_URL).
	URL      string           `json:"url,omitempty"`
	Monitor  *webhookMonitor  `json:"monitor,omitempty"`
	Incident *webhookIncident `json:"incident,omitempty"`
	// Slow is set for the slow-response events, which are about no
	// incident.
	Slow *webhookSlow `json:"slow,omitempty"`
}

type webhookSlow struct {
	// Since is when the monitor turned slow; ThresholdMS its limit and
	// LatencyMS the response time of the check that turned it slow, or
	// fast again.
	Since       time.Time `json:"since"`
	ThresholdMS int64     `json:"threshold_ms"`
	LatencyMS   int64     `json:"latency_ms"`
}

type webhookMonitor struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Target string `json:"target"`
}

type webhookIncident struct {
	ID         int64      `json:"id"`
	Kind       string     `json:"kind"`
	StartedAt  time.Time  `json:"started_at"`
	ResolvedAt *time.Time `json:"resolved_at"`
	Cause      string     `json:"cause"`
}

func newWebhookBody(n Notification) webhookBody {
	body := webhookBody{Event: n.Event, Text: Text(n), URL: URL(n)}
	if n.Event == EventTest && n.Monitor.ID == 0 {
		return body
	}
	body.Monitor = &webhookMonitor{ID: n.Monitor.ID, Name: n.Monitor.Name, Type: string(n.Monitor.Type), Target: n.Monitor.Target}
	switch n.Event {
	case EventTest:
		return body
	case EventSlow, EventSlowResolved:
		body.Slow = &webhookSlow{Since: n.Since, ThresholdMS: n.Threshold.Milliseconds(), LatencyMS: n.Latency.Milliseconds()}
		return body
	}
	kind := n.Incident.Kind
	if kind == "" {
		kind = store.IncidentDown
	}
	body.Incident = &webhookIncident{ID: n.Incident.ID, Kind: string(kind), StartedAt: n.Incident.StartedAt, Cause: n.Incident.Cause}
	if !n.Incident.Open() {
		body.Incident.ResolvedAt = &n.Incident.ResolvedAt
	}
	return body
}

// target is the monitor's target in parentheses, with the space before it;
// empty for a monitor without one, such as a push monitor.
func target(m store.Monitor) string {
	if m.Target == "" {
		return ""
	}
	return " (" + m.Target + ")"
}

// maxGroupNamed bounds how many monitors a grouped message names.
const maxGroupNamed = 10

// groupDetail is what a grouped message says about one of its monitors.
func groupDetail(n Notification) string {
	w := wording(n)
	switch n.Event {
	case EventDown:
		return n.Incident.Cause
	case EventUp:
		return humanDuration(n.Incident.ResolvedAt.Sub(n.Incident.StartedAt), w)
	case EventReminder:
		return humanDuration(n.At.Sub(n.Incident.StartedAt), w)
	case EventCertExpiring, EventCertRenewed:
		return n.CertExpiresAt.Format("2006-01-02")
	case EventSlow:
		return latencyText(n.Latency, w)
	case EventSlowResolved:
		return humanDuration(n.At.Sub(n.Since), w)
	}
	return ""
}

// groupList names the monitors of a group with their details, up to
// maxGroupNamed of them.
func groupList(n Notification) string {
	w := wording(n)
	items := make([]string, 0, len(n.Group))
	for i, g := range n.Group {
		if i == maxGroupNamed {
			items = append(items, fmt.Sprintf(w.groupMore, len(n.Group)-maxGroupNamed))
			break
		}
		if detail := groupDetail(g); detail != "" {
			items = append(items, fmt.Sprintf("%s (%s)", g.Monitor.Name, detail))
		} else {
			items = append(items, g.Monitor.Name)
		}
	}
	return strings.Join(items, ", ")
}

// groupWhere is the namespace prefix of a grouped message.
func groupWhere(n Notification) string {
	if n.Monitor.Namespace == "" {
		return ""
	}
	return n.Monitor.Namespace + ": "
}

func groupText(n Notification) string {
	w := wording(n)
	count, where, list := len(n.Group), groupWhere(n), groupList(n)
	switch n.Event {
	case EventDown:
		return fmt.Sprintf(w.groupDown, where, count, list)
	case EventUp:
		return fmt.Sprintf(w.groupUp, where, count, list)
	case EventReminder:
		return fmt.Sprintf(w.groupReminder, where, count, list)
	case EventCertExpiring:
		return fmt.Sprintf(w.groupCertExpires, where, count, list)
	case EventCertRenewed:
		return fmt.Sprintf(w.groupCertRenewed, where, count, list)
	case EventSlow:
		return fmt.Sprintf(w.groupSlow, where, count, list)
	case EventSlowResolved:
		return fmt.Sprintf(w.groupSlowResolved, where, count, list)
	}
	return ""
}

// Text is the one-line human-readable form of a notification, in its
// language.
func Text(n Notification) string {
	if len(n.Group) > 1 {
		return groupText(n)
	}
	w := wording(n)
	switch n.Event {
	case EventDown:
		return fmt.Sprintf(w.down, n.Monitor.Name, target(n.Monitor), n.Monitor.FailureThreshold, n.Incident.Cause)
	case EventUp:
		return fmt.Sprintf(w.up, n.Monitor.Name, target(n.Monitor), humanDuration(n.Incident.ResolvedAt.Sub(n.Incident.StartedAt), w))
	case EventReminder:
		return fmt.Sprintf(w.reminder, n.Monitor.Name, target(n.Monitor), humanDuration(n.At.Sub(n.Incident.StartedAt), w), n.Incident.Cause)
	case EventCertExpiring:
		days := int(n.CertExpiresAt.Sub(n.Incident.StartedAt).Hours() / 24)
		when := fmt.Sprintf(w.certDays, days)
		switch {
		case n.CertExpiresAt.Before(n.Incident.StartedAt):
			when = w.certExpired
		case days == 0:
			when = w.certToday
		}
		return fmt.Sprintf(w.certExpires, n.Monitor.Name, when, n.CertExpiresAt.Format("2006-01-02"), target(n.Monitor))
	case EventCertRenewed:
		return fmt.Sprintf(w.certRenewed, n.Monitor.Name, n.CertExpiresAt.Format("2006-01-02"), target(n.Monitor))
	case EventSlow:
		return fmt.Sprintf(w.slow, n.Monitor.Name, target(n.Monitor), n.Monitor.FailureThreshold, latencyText(n.Threshold, w), latencyText(n.Latency, w))
	case EventSlowResolved:
		return fmt.Sprintf(w.slowResolved, n.Monitor.Name, target(n.Monitor), latencyText(n.Latency, w), humanDuration(n.At.Sub(n.Since), w))
	default:
		if n.Monitor.Name != "" {
			return fmt.Sprintf(w.testMonitor, n.Monitor.Name, target(n.Monitor))
		}
		return w.test
	}
}

// Send posts n to the channel's URL; any non-2xx answer is an error.
func (s *WebhookSender) Send(ctx context.Context, ch store.Channel, n Notification) error {
	return postJSON(ctx, s.Client, s.UserAgent, ch.Target, newWebhookBody(n))
}

// postJSON posts body as JSON; any non-2xx answer is an error.
func postJSON(ctx context.Context, client *http.Client, userAgent, url string, body any) error {
	status, _, err := post(ctx, client, userAgent, url, nil, body)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("webhook answered %d", status)
	}
	return nil
}

// answerLimit bounds how much of a provider's answer is read: enough for
// the message of an error.
const answerLimit = 2 << 10

// post sends body as JSON with the given extra headers and returns the
// status of the answer and the beginning of its body. Its errors never
// carry the URL, which for most providers is or contains a credential.
func post(ctx context.Context, client *http.Client, userAgent, target string, headers map[string]string, body any) (int, []byte, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(payload))
	if err != nil {
		return 0, nil, errors.New("the address is not a valid URL")
	}
	req.Header.Set("Content-Type", "application/json")
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}

	resp, err := client.Do(req)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err // the cause without the URL around it
		}
		return 0, nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(resp.Body, answerLimit))
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	return resp.StatusCode, answer, nil
}
