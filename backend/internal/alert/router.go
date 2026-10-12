package alert

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// Router sends a notification with the sender that matches the channel's type.
type Router struct {
	senders map[store.ChannelType]Sender
	email   *EmailSender
}

// NewRouter returns a Router for every channel type.
func NewRouter(client *http.Client, userAgent string, smtp SMTPConfig, endpoints Endpoints) *Router {
	email := &EmailSender{SMTP: smtp}
	// Mattermost and Google Chat read a message from "text", like Slack.
	text := &SlackSender{Client: client, UserAgent: userAgent}
	return &Router{
		email: email,
		senders: map[store.ChannelType]Sender{
			store.ChannelWebhook:    &WebhookSender{Client: client, UserAgent: userAgent},
			store.ChannelSlack:      text,
			store.ChannelMattermost: text,
			store.ChannelGoogleChat: text,
			store.ChannelTeams:      &TeamsSender{Client: client, UserAgent: userAgent},
			store.ChannelEmail:      email,
			store.ChannelDiscord:    &DiscordSender{Client: client, UserAgent: userAgent},
			store.ChannelTelegram:   &TelegramSender{Client: client, UserAgent: userAgent, BaseURL: endpoints.Telegram},
			store.ChannelPagerDuty:  &PagerDutySender{Client: client, UserAgent: userAgent, URL: endpoints.PagerDuty},
			store.ChannelNtfy:       &NtfySender{Client: client, UserAgent: userAgent},
			store.ChannelGotify:     &GotifySender{Client: client, UserAgent: userAgent},
		},
	}
}

// EmailEnabled reports whether e-mail channels can deliver.
func (r *Router) EmailEnabled() bool { return r.email.Enabled() }

func (r *Router) Send(ctx context.Context, ch store.Channel, n Notification) error {
	sender, ok := r.senders[ch.Type]
	if !ok {
		return fmt.Errorf("no sender for channel type %q", ch.Type)
	}
	return sender.Send(ctx, ch, n)
}

// Fact is one labelled detail of a notification, for channels that can show
// structure (a Teams card, an e-mail body).
type Fact struct {
	Label, Value string
}

// Facts lists the details of a notification; none for a test.
func Facts(n Notification) []Fact {
	if n.Event == EventTest {
		return nil
	}
	facts := []Fact{{"Monitor", n.Monitor.Name}}
	if n.Monitor.Target != "" { // a push monitor has none
		facts = append(facts, Fact{"Hedef", n.Monitor.Target})
	}
	if n.Monitor.Namespace != "" {
		facts = append(facts, Fact{"Namespace", n.Monitor.Namespace})
	}
	facts = append(facts, Fact{"Başlangıç", n.Incident.StartedAt.UTC().Format(time.RFC3339)})
	if !n.Incident.Open() {
		facts = append(facts, Fact{"Bitiş", n.Incident.ResolvedAt.UTC().Format(time.RFC3339)})
	}
	if n.Incident.Cause != "" {
		facts = append(facts, Fact{"Neden", n.Incident.Cause})
	}
	return facts
}

// Subject is a short line for channels that have one.
func Subject(n Notification) string {
	switch n.Event {
	case EventDown:
		return fmt.Sprintf("[Kubeat] %s erişilemiyor", n.Monitor.Name)
	case EventUp:
		return fmt.Sprintf("[Kubeat] %s tekrar çalışıyor", n.Monitor.Name)
	case EventReminder:
		return fmt.Sprintf("[Kubeat] %s hâlâ erişilemiyor", n.Monitor.Name)
	case EventCertExpiring:
		return fmt.Sprintf("[Kubeat] %s sertifikasının süresi bitiyor", n.Monitor.Name)
	case EventCertRenewed:
		return fmt.Sprintf("[Kubeat] %s sertifikası yenilendi", n.Monitor.Name)
	default:
		return "[Kubeat] Test bildirimi"
	}
}
