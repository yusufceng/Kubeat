package alert

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/netguard"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// Router sends a notification with the sender that matches the channel's type.
type Router struct {
	senders map[store.ChannelType]Sender
	email   *EmailSender
}

// NewRouter returns a Router for every channel type. Every connection its
// senders make goes through guard. A channel's own address, which whoever
// may edit the channel sets, is held to it. The services the operator set
// in smtp and endpoints are trusted at their exact address, each by its
// own sender alone: one on the pod's loopback, a sidecar, needs no
// exception, and a channel aimed at its address is still refused. A nil
// guard lets them connect anywhere.
func NewRouter(guard *netguard.Policy, userAgent string, smtp SMTPConfig, endpoints Endpoints) *Router {
	client := &http.Client{Transport: guard.Transport()}
	trusting := func(service string) *http.Client {
		return &http.Client{Transport: guard.Trust(netguard.HostPort(service)).Transport()}
	}
	email := &EmailSender{SMTP: smtp, Dial: guard.Trust(smtp.address()).Dialer(&net.Dialer{}).DialContext}
	return &Router{
		email: email,
		senders: map[store.ChannelType]Sender{
			store.ChannelWebhook: &WebhookSender{Client: client, UserAgent: userAgent},
			store.ChannelSlack:   &SlackSender{Client: client, UserAgent: userAgent},
			// Mattermost reads Slack's format, Google Chat its own cards.
			store.ChannelMattermost: &MattermostSender{Client: client, UserAgent: userAgent},
			store.ChannelGoogleChat: &GoogleChatSender{Client: client, UserAgent: userAgent},
			store.ChannelTeams:      &TeamsSender{Client: client, UserAgent: userAgent},
			store.ChannelEmail:      email,
			store.ChannelDiscord:    &DiscordSender{Client: client, UserAgent: userAgent},
			store.ChannelTelegram:   &TelegramSender{Client: trusting(endpoints.Telegram), UserAgent: userAgent, BaseURL: endpoints.Telegram},
			store.ChannelPagerDuty:  &PagerDutySender{Client: trusting(endpoints.PagerDuty), UserAgent: userAgent, URL: endpoints.PagerDuty},
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

// Facts lists the details of a notification; none for a test. A grouped
// notification lists its monitors instead.
func Facts(n Notification) []Fact {
	if n.Event == EventTest {
		return nil
	}
	w := wording(n)
	if len(n.Group) > 1 {
		var facts []Fact
		if n.Monitor.Namespace != "" {
			facts = append(facts, Fact{w.factNamespace, n.Monitor.Namespace})
		}
		for _, g := range n.Group {
			value := groupDetail(g)
			if g.Monitor.Target != "" {
				value += " — " + g.Monitor.Target
			}
			facts = append(facts, Fact{g.Monitor.Name, value})
		}
		return facts
	}
	facts := []Fact{{w.factMonitor, n.Monitor.Name}}
	if n.Monitor.Target != "" { // a push monitor has none
		facts = append(facts, Fact{w.factTarget, n.Monitor.Target})
	}
	if n.Monitor.Namespace != "" {
		facts = append(facts, Fact{w.factNamespace, n.Monitor.Namespace})
	}
	facts = append(facts, Fact{w.factStart, n.Incident.StartedAt.UTC().Format(time.RFC3339)})
	if !n.Incident.Open() {
		facts = append(facts, Fact{w.factEnd, n.Incident.ResolvedAt.UTC().Format(time.RFC3339)})
	}
	if n.Incident.Cause != "" {
		facts = append(facts, Fact{w.factCause, n.Incident.Cause})
	}
	return facts
}

// Subject is a short line for channels that have one.
func Subject(n Notification) string {
	w := wording(n)
	if len(n.Group) > 1 {
		count, where := len(n.Group), groupWhere(n)
		switch n.Event {
		case EventDown:
			return fmt.Sprintf(w.subjectGroupDown, where, count)
		case EventUp:
			return fmt.Sprintf(w.subjectGroupUp, where, count)
		case EventReminder:
			return fmt.Sprintf(w.subjectGroupReminder, where, count)
		case EventCertExpiring:
			return fmt.Sprintf(w.subjectGroupCertExpires, where, count)
		case EventCertRenewed:
			return fmt.Sprintf(w.subjectGroupCertRenewed, where, count)
		}
	}
	switch n.Event {
	case EventDown:
		return fmt.Sprintf(w.subjectDown, n.Monitor.Name)
	case EventUp:
		return fmt.Sprintf(w.subjectUp, n.Monitor.Name)
	case EventReminder:
		return fmt.Sprintf(w.subjectReminder, n.Monitor.Name)
	case EventCertExpiring:
		return fmt.Sprintf(w.subjectCertExpires, n.Monitor.Name)
	case EventCertRenewed:
		return fmt.Sprintf(w.subjectCertRenewed, n.Monitor.Name)
	default:
		return w.subjectTest
	}
}
