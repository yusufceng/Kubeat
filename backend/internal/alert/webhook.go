package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	Event    Event            `json:"event"`
	Text     string           `json:"text"`
	Monitor  *webhookMonitor  `json:"monitor,omitempty"`
	Incident *webhookIncident `json:"incident,omitempty"`
}

type webhookMonitor struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Target string `json:"target"`
}

type webhookIncident struct {
	ID         int64      `json:"id"`
	StartedAt  time.Time  `json:"started_at"`
	ResolvedAt *time.Time `json:"resolved_at"`
	Cause      string     `json:"cause"`
}

func newWebhookBody(n Notification) webhookBody {
	body := webhookBody{Event: n.Event, Text: Text(n)}
	if n.Event == EventTest {
		return body
	}
	body.Monitor = &webhookMonitor{ID: n.Monitor.ID, Name: n.Monitor.Name, Type: string(n.Monitor.Type), Target: n.Monitor.Target}
	body.Incident = &webhookIncident{ID: n.Incident.ID, StartedAt: n.Incident.StartedAt, Cause: n.Incident.Cause}
	if !n.Incident.Open() {
		body.Incident.ResolvedAt = &n.Incident.ResolvedAt
	}
	return body
}

// Text is the one-line human-readable form of a notification.
func Text(n Notification) string {
	switch n.Event {
	case EventDown:
		return fmt.Sprintf("🔴 %s erişilemiyor (%s). Art arda %d kontrol başarısız. Son hata: %s",
			n.Monitor.Name, n.Monitor.Target, n.Monitor.FailureThreshold, n.Incident.Cause)
	case EventUp:
		return fmt.Sprintf("🟢 %s tekrar çalışıyor (%s). Kesinti süresi: %s",
			n.Monitor.Name, n.Monitor.Target, n.Incident.ResolvedAt.Sub(n.Incident.StartedAt).Round(time.Second))
	default:
		return "Kubeat test bildirimi: bu kanal çalışıyor."
	}
}

// Send posts n to the channel's URL; any non-2xx answer is an error.
func (s *WebhookSender) Send(ctx context.Context, ch store.Channel, n Notification) error {
	payload, err := json.Marshal(newWebhookBody(n))
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ch.URL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.UserAgent != "" {
		req.Header.Set("User-Agent", s.UserAgent)
	}

	resp, err := s.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook answered %d", resp.StatusCode)
	}
	return nil
}
