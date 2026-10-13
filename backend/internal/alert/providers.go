package alert

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// Endpoints are the addresses of the hosted services notifications go to.
// They are settings so that a cluster without direct internet access can
// point them at a proxy.
type Endpoints struct {
	// Telegram is the Bot API, without the "/bot<token>" part.
	Telegram string
	// PagerDuty is the Events API v2 "enqueue" address.
	PagerDuty string
}

// accepted turns an answer outside 2xx into an error that names the
// provider and carries what it said, which usually tells what is wrong.
func accepted(provider string, status int, answer []byte) error {
	if status >= 200 && status < 300 {
		return nil
	}
	said := strings.TrimSpace(string(answer))
	if said == "" {
		return fmt.Errorf("%s answered %d", provider, status)
	}
	return fmt.Errorf("%s answered %d: %s", provider, status, truncate(said, 200))
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return strings.ToValidUTF8(s[:max], "") + "…"
}

// DiscordSender posts to a Discord webhook, which takes the message in
// "content".
type DiscordSender struct {
	Client    *http.Client
	UserAgent string
}

// discordLimit is the longest message Discord accepts.
const discordLimit = 2000

// What Discord accepts in an embed.
const (
	discordTitleLimit = 256
	discordFieldLimit = 1024
	discordFields     = 25
)

type discordEmbed struct {
	Title       string              `json:"title,omitempty"`
	URL         string              `json:"url,omitempty"`
	Description string              `json:"description,omitempty"`
	Color       int                 `json:"color"`
	Fields      []discordEmbedField `json:"fields,omitempty"`
	Footer      *discordFooter      `json:"footer,omitempty"`
	Timestamp   string              `json:"timestamp,omitempty"`
}

type discordEmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

type discordFooter struct {
	Text string `json:"text"`
}

func newDiscordBody(n Notification, now time.Time) map[string]any {
	embed := discordEmbed{
		Title:       truncate(subject(n), discordTitleLimit-1),
		URL:         URL(n),
		Description: truncate(Text(n), discordLimit-1),
		Color:       ColorInt(n),
		Footer:      &discordFooter{Text: "Kubeat"},
		Timestamp:   now.UTC().Format(time.RFC3339),
	}
	for _, f := range Facts(n) {
		if len(embed.Fields) == discordFields {
			break
		}
		embed.Fields = append(embed.Fields, discordEmbedField{Name: f.Label, Value: truncate(f.Value, discordFieldLimit-1), Inline: shortValue(f.Value)})
	}
	return map[string]any{"embeds": []discordEmbed{embed}}
}

func (s *DiscordSender) Send(ctx context.Context, ch store.Channel, n Notification) error {
	status, answer, err := post(ctx, s.Client, s.UserAgent, ch.Target, nil, newDiscordBody(n, time.Now()))
	if err != nil {
		return err
	}
	return accepted("Discord", status, answer)
}

// TelegramSender sends a message as a bot. The channel's target is the chat
// and its secret the bot's token.
type TelegramSender struct {
	Client    *http.Client
	UserAgent string
	BaseURL   string
}

func (s *TelegramSender) Send(ctx context.Context, ch store.Channel, n Notification) error {
	target := strings.TrimSuffix(s.BaseURL, "/") + "/bot" + ch.Secret + "/sendMessage"
	status, answer, err := post(ctx, s.Client, s.UserAgent, target, nil, map[string]any{
		"chat_id":                  ch.Target,
		"text":                     telegramText(n),
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	})
	if err != nil {
		return err
	}
	// Telegram explains a refusal in "description".
	var reply struct {
		Description string `json:"description"`
	}
	if json.Unmarshal(answer, &reply) == nil && reply.Description != "" {
		answer = []byte(reply.Description)
	}
	return accepted("Telegram", status, answer)
}

// telegramText is the message as Telegram's limited HTML: the subject in
// bold, the text, the details, and the link back. Everything that came
// from a monitor or a target is escaped, so a name with "<" is text.
func telegramText(n Notification) string {
	esc := html.EscapeString
	var b strings.Builder
	fmt.Fprintf(&b, "<b>%s %s</b>\n%s", Emoji(n), esc(subject(n)), esc(Text(n)))
	for _, f := range Facts(n) {
		fmt.Fprintf(&b, "\n%s: %s", esc(f.Label), esc(f.Value))
	}
	if link := URL(n); link != "" {
		fmt.Fprintf(&b, "\n\n<a href=\"%s\">%s</a>", esc(link), esc(wording(n).openInKubeat))
	}
	return b.String()
}

// PagerDutySender raises and resolves alerts through the Events API v2. The
// channel's secret is the integration's routing key. An incident here is
// one alert there: the events about it share a deduplication key, so the
// recovery closes what the outage opened.
type PagerDutySender struct {
	Client    *http.Client
	UserAgent string
	URL       string
}

type pagerDutyEvent struct {
	RoutingKey  string            `json:"routing_key"`
	EventAction string            `json:"event_action"`
	DedupKey    string            `json:"dedup_key"`
	Payload     *pagerDutyPayload `json:"payload,omitempty"`
	// Client and ClientURL name what raised the alert and where to look at
	// it; PagerDuty shows them on the incident.
	Client    string `json:"client,omitempty"`
	ClientURL string `json:"client_url,omitempty"`
}

type pagerDutyPayload struct {
	Summary       string            `json:"summary"`
	Source        string            `json:"source"`
	Severity      string            `json:"severity"`
	Component     string            `json:"component,omitempty"`
	Group         string            `json:"group,omitempty"`
	CustomDetails map[string]string `json:"custom_details,omitempty"`
}

// pagerDutySummaryLimit is the longest summary PagerDuty accepts.
const pagerDutySummaryLimit = 1024

func (s *PagerDutySender) Send(ctx context.Context, ch store.Channel, n Notification) error {
	event := pagerDutyEvent{
		RoutingKey:  ch.Secret,
		EventAction: "trigger",
		DedupKey:    fmt.Sprintf("kubeat-incident-%d", n.Incident.ID),
		Client:      "Kubeat",
		ClientURL:   URL(n),
	}
	switch n.Event {
	case EventUp, EventCertRenewed:
		event.EventAction = "resolve"
	case EventTest:
		// A test must not page anyone: it is raised as information and
		// resolved by the second request below.
		event.DedupKey = "kubeat-test"
	}

	if event.EventAction == "trigger" {
		source := n.Monitor.Target
		if source == "" {
			source = "kubeat"
		}
		details := make(map[string]string)
		for _, fact := range Facts(n) {
			details[fact.Label] = fact.Value
		}
		event.Payload = &pagerDutyPayload{
			Summary:       truncate(Text(n), pagerDutySummaryLimit-3),
			Source:        source,
			Severity:      pagerDutySeverity(n.Event),
			Component:     n.Monitor.Name,
			Group:         n.Monitor.Namespace,
			CustomDetails: details,
		}
	}
	if err := s.enqueue(ctx, event); err != nil {
		return err
	}
	if n.Event == EventTest {
		return s.enqueue(ctx, pagerDutyEvent{RoutingKey: ch.Secret, EventAction: "resolve", DedupKey: event.DedupKey})
	}
	return nil
}

func pagerDutySeverity(e Event) string {
	switch e {
	case EventCertExpiring:
		return "warning"
	case EventTest:
		return "info"
	default:
		return "critical"
	}
}

func (s *PagerDutySender) enqueue(ctx context.Context, event pagerDutyEvent) error {
	status, answer, err := post(ctx, s.Client, s.UserAgent, s.URL, nil, event)
	if err != nil {
		return err
	}
	return accepted("PagerDuty", status, answer)
}

// NtfySender publishes to an ntfy topic. The channel's target is the
// topic's address, such as https://ntfy.sh/alerts, and its secret an
// optional access token.
type NtfySender struct {
	Client    *http.Client
	UserAgent string
}

// SplitNtfyTopic splits a topic address into the server and the topic.
func SplitNtfyTopic(target string) (server, topic string, err error) {
	u, err := url.Parse(target)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", "", errors.New("must be an http:// or https:// URL")
	}
	path := strings.Trim(u.Path, "/")
	cut := strings.LastIndex(path, "/")
	topic = path[cut+1:]
	if topic == "" {
		return "", "", errors.New("must end with the topic, as in https://ntfy.sh/my-topic")
	}
	u.Path, u.RawQuery, u.Fragment = "/"+path[:cut+1], "", ""
	return u.String(), topic, nil
}

func (s *NtfySender) Send(ctx context.Context, ch store.Channel, n Notification) error {
	server, topic, err := SplitNtfyTopic(ch.Target)
	if err != nil {
		return fmt.Errorf("topic address %w", err)
	}
	// Published as JSON to the server's root: a title with non-ASCII
	// letters cannot travel in a header.
	message := map[string]any{"topic": topic, "title": Subject(n), "message": Text(n), "priority": 3}
	if link := URL(n); link != "" {
		message["click"] = link
		message["actions"] = []map[string]string{{"action": "view", "label": wording(n).openInKubeat, "url": link}}
	}
	switch n.Event {
	case EventDown, EventReminder:
		message["priority"], message["tags"] = 5, []string{"rotating_light"}
	case EventCertExpiring:
		message["priority"], message["tags"] = 4, []string{"warning"}
	case EventUp, EventCertRenewed:
		message["tags"] = []string{"white_check_mark"}
	}
	var headers map[string]string
	if ch.Secret != "" {
		headers = map[string]string{"Authorization": "Bearer " + ch.Secret}
	}
	status, answer, err := post(ctx, s.Client, s.UserAgent, server, headers, message)
	if err != nil {
		return err
	}
	return accepted("ntfy", status, answer)
}

// GotifySender creates a message on a Gotify server. The channel's target
// is the server's address and its secret an application token.
type GotifySender struct {
	Client    *http.Client
	UserAgent string
}

func (s *GotifySender) Send(ctx context.Context, ch store.Channel, n Notification) error {
	priority := 5
	if n.Event == EventDown || n.Event == EventReminder {
		priority = 8
	}
	message := map[string]any{"title": Subject(n), "message": gotifyMarkdown(n), "priority": priority,
		"extras": map[string]any{"client::display": map[string]string{"contentType": "text/markdown"}}}
	if link := URL(n); link != "" {
		message["extras"].(map[string]any)["client::notification"] = map[string]any{"click": map[string]string{"url": link}}
	}
	status, answer, err := post(ctx, s.Client, s.UserAgent, strings.TrimSuffix(ch.Target, "/")+"/message",
		map[string]string{"X-Gotify-Key": ch.Secret}, message)
	if err != nil {
		return err
	}
	return accepted("Gotify", status, answer)
}

// gotifyMarkdown is the message as Gotify renders markdown: the text, the
// details as a list, and the link back.
func gotifyMarkdown(n Notification) string {
	var b strings.Builder
	b.WriteString(Text(n))
	for _, f := range Facts(n) {
		fmt.Fprintf(&b, "\n- **%s:** %s", f.Label, f.Value)
	}
	if link := URL(n); link != "" {
		fmt.Fprintf(&b, "\n\n[%s](%s)", wording(n).openInKubeat, link)
	}
	return b.String()
}
