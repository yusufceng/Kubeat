package alert

import (
	"context"
	"net/http"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// subject is what a message is about in a few words: the monitor's name,
// or for a grouped notification the namespace and how many monitors.
func subject(n Notification) string {
	if len(n.Group) > 1 {
		w := wording(n)
		return groupWhere(n) + itoaCount(len(n.Group)) + " " + w.monitorsWord
	}
	return n.Monitor.Name
}

// SlackSender posts to a Slack incoming webhook: the one-line text, which
// is what a notification preview shows, and an attachment that carries the
// colour of the event, the details and the link back. Mattermost reads the
// same format.
type SlackSender struct {
	Client    *http.Client
	UserAgent string
}

type slackBody struct {
	Text        string            `json:"text"`
	Attachments []slackAttachment `json:"attachments,omitempty"`
}

type slackAttachment struct {
	Color     string       `json:"color"`
	Fallback  string       `json:"fallback"`
	Title     string       `json:"title,omitempty"`
	TitleLink string       `json:"title_link,omitempty"`
	Fields    []slackField `json:"fields,omitempty"`
	Footer    string       `json:"footer,omitempty"`
	Timestamp int64        `json:"ts,omitempty"`
}

type slackField struct {
	Title string `json:"title"`
	Value string `json:"value"`
	Short bool   `json:"short"`
}

// shortValue says whether a detail fits beside another one.
func shortValue(v string) bool { return len([]rune(v)) <= 40 }

func newSlackBody(n Notification, now time.Time) slackBody {
	text := Text(n)
	attachment := slackAttachment{
		Color: Color(n), Fallback: text, Title: subject(n), TitleLink: URL(n),
		Footer: "Kubeat", Timestamp: now.Unix(),
	}
	for _, f := range Facts(n) {
		attachment.Fields = append(attachment.Fields, slackField{Title: f.Label, Value: f.Value, Short: shortValue(f.Value)})
	}
	return slackBody{Text: text, Attachments: []slackAttachment{attachment}}
}

func (s *SlackSender) Send(ctx context.Context, ch store.Channel, n Notification) error {
	return postJSON(ctx, s.Client, s.UserAgent, ch.Target, newSlackBody(n, time.Now()))
}

// GoogleChatSender posts to a Google Chat space webhook: the one-line text
// and a card with the same details, which Chat renders itself.
type GoogleChatSender struct {
	Client    *http.Client
	UserAgent string
}

type chatBody struct {
	Text    string         `json:"text"`
	CardsV2 []chatCardWrap `json:"cardsV2,omitempty"`
}

type chatCardWrap struct {
	CardID string   `json:"cardId"`
	Card   chatCard `json:"card"`
}

type chatCard struct {
	Header   *chatHeader   `json:"header,omitempty"`
	Sections []chatSection `json:"sections,omitempty"`
}

type chatHeader struct {
	Title    string `json:"title"`
	Subtitle string `json:"subtitle,omitempty"`
}

type chatSection struct {
	Widgets []chatWidget `json:"widgets"`
}

type chatWidget struct {
	DecoratedText *chatDecorated  `json:"decoratedText,omitempty"`
	ButtonList    *chatButtonList `json:"buttonList,omitempty"`
}

type chatDecorated struct {
	TopLabel string `json:"topLabel"`
	Text     string `json:"text"`
	Wrap     bool   `json:"wrapText"`
}

type chatButtonList struct {
	Buttons []chatButton `json:"buttons"`
}

type chatButton struct {
	Text    string      `json:"text"`
	OnClick chatOnClick `json:"onClick"`
}

type chatOnClick struct {
	OpenLink chatOpenLink `json:"openLink"`
}

type chatOpenLink struct {
	URL string `json:"url"`
}

func newChatBody(n Notification) chatBody {
	text := Text(n)
	body := chatBody{Text: text}
	facts := Facts(n)
	link := URL(n)
	if len(facts) == 0 && link == "" {
		return body
	}
	section := chatSection{}
	for _, f := range facts {
		section.Widgets = append(section.Widgets, chatWidget{DecoratedText: &chatDecorated{TopLabel: f.Label, Text: f.Value, Wrap: true}})
	}
	if link != "" {
		section.Widgets = append(section.Widgets, chatWidget{ButtonList: &chatButtonList{
			Buttons: []chatButton{{Text: wording(n).openInKubeat, OnClick: chatOnClick{OpenLink: chatOpenLink{URL: link}}}},
		}})
	}
	body.CardsV2 = []chatCardWrap{{
		CardID: "kubeat",
		Card:   chatCard{Header: &chatHeader{Title: Emoji(n) + " " + subject(n), Subtitle: n.Monitor.Target}, Sections: []chatSection{section}},
	}}
	return body
}

func (s *GoogleChatSender) Send(ctx context.Context, ch store.Channel, n Notification) error {
	return postJSON(ctx, s.Client, s.UserAgent, ch.Target, newChatBody(n))
}

// TeamsSender posts to a Microsoft Teams workflow webhook ("Post to a
// channel when a webhook request is received"), which takes a message with
// an Adaptive Card attachment.
type TeamsSender struct {
	Client    *http.Client
	UserAgent string
}

func (s *TeamsSender) Send(ctx context.Context, ch store.Channel, n Notification) error {
	return postJSON(ctx, s.Client, s.UserAgent, ch.Target, newTeamsBody(n))
}

type teamsBody struct {
	Type        string            `json:"type"`
	Attachments []teamsAttachment `json:"attachments"`
}

type teamsAttachment struct {
	ContentType string    `json:"contentType"`
	Content     teamsCard `json:"content"`
}

type teamsCard struct {
	Schema  string         `json:"$schema"`
	Type    string         `json:"type"`
	Version string         `json:"version"`
	Body    []teamsElement `json:"body"`
	Actions []teamsAction  `json:"actions,omitempty"`
	MSTeams map[string]any `json:"msteams,omitempty"`
}

// teamsElement is a TextBlock or a FactSet.
type teamsElement struct {
	Type   string      `json:"type"`
	Text   string      `json:"text,omitempty"`
	Wrap   bool        `json:"wrap,omitempty"`
	Weight string      `json:"weight,omitempty"`
	Size   string      `json:"size,omitempty"`
	Color  string      `json:"color,omitempty"`
	Facts  []teamsFact `json:"facts,omitempty"`
}

type teamsFact struct {
	Title string `json:"title"`
	Value string `json:"value"`
}

// teamsAction is an Action.OpenUrl button under the card.
type teamsAction struct {
	Type  string `json:"type"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

// teamsColor is the Adaptive Card colour of the event.
func teamsColor(n Notification) string {
	switch n.Event {
	case EventDown, EventReminder:
		return "Attention"
	case EventUp, EventCertRenewed:
		return "Good"
	case EventCertExpiring:
		return "Warning"
	default:
		return "Default"
	}
}

func newTeamsBody(n Notification) teamsBody {
	body := []teamsElement{
		{Type: "TextBlock", Text: Emoji(n) + " " + subject(n), Wrap: true, Weight: "Bolder", Size: "Medium", Color: teamsColor(n)},
		{Type: "TextBlock", Text: Text(n), Wrap: true},
	}
	if facts := Facts(n); len(facts) > 0 {
		set := teamsElement{Type: "FactSet"}
		for _, f := range facts {
			set.Facts = append(set.Facts, teamsFact{Title: f.Label, Value: f.Value})
		}
		body = append(body, set)
	}
	card := teamsCard{
		Schema:  "http://adaptivecards.io/schemas/adaptive-card.json",
		Type:    "AdaptiveCard",
		Version: "1.4",
		Body:    body,
		MSTeams: map[string]any{"width": "Full"},
	}
	if link := URL(n); link != "" {
		card.Actions = []teamsAction{{Type: "Action.OpenUrl", Title: wording(n).openInKubeat, URL: link}}
	}
	return teamsBody{
		Type:        "message",
		Attachments: []teamsAttachment{{ContentType: "application/vnd.microsoft.card.adaptive", Content: card}},
	}
}
