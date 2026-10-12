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
	Kind       string     `json:"kind"`
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

// Text is the one-line human-readable form of a notification.
func Text(n Notification) string {
	switch n.Event {
	case EventDown:
		return fmt.Sprintf("🔴 %s erişilemiyor (%s). Art arda %d kontrol başarısız. Son hata: %s",
			n.Monitor.Name, n.Monitor.Target, n.Monitor.FailureThreshold, n.Incident.Cause)
	case EventUp:
		return fmt.Sprintf("🟢 %s tekrar çalışıyor (%s). Kesinti süresi: %s",
			n.Monitor.Name, n.Monitor.Target, n.Incident.ResolvedAt.Sub(n.Incident.StartedAt).Round(time.Second))
	case EventReminder:
		return fmt.Sprintf("🔴 %s hâlâ erişilemiyor (%s). Kesinti süresi: %s. Son hata: %s",
			n.Monitor.Name, n.Monitor.Target, n.At.Sub(n.Incident.StartedAt).Round(time.Second), n.Incident.Cause)
	case EventCertExpiring:
		days := int(n.CertExpiresAt.Sub(n.Incident.StartedAt).Hours() / 24)
		when := fmt.Sprintf("%d gün sonra", days)
		switch {
		case n.CertExpiresAt.Before(n.Incident.StartedAt):
			when = "süresi doldu"
		case days == 0:
			when = "24 saat içinde"
		}
		return fmt.Sprintf("🟡 %s sertifikasının süresi bitiyor: %s (%s) (%s)",
			n.Monitor.Name, when, n.CertExpiresAt.Format("2006-01-02"), n.Monitor.Target)
	case EventCertRenewed:
		return fmt.Sprintf("🟢 %s sertifikası yenilendi; yeni bitiş tarihi %s (%s)",
			n.Monitor.Name, n.CertExpiresAt.Format("2006-01-02"), n.Monitor.Target)
	default:
		return "Kubeat test bildirimi: bu kanal çalışıyor."
	}
}

// Send posts n to the channel's URL; any non-2xx answer is an error.
func (s *WebhookSender) Send(ctx context.Context, ch store.Channel, n Notification) error {
	return postJSON(ctx, s.Client, s.UserAgent, ch.Target, newWebhookBody(n))
}

// postJSON posts body as JSON; any non-2xx answer is an error.
func postJSON(ctx context.Context, client *http.Client, userAgent, url string, body any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}

	resp, err := client.Do(req)
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
