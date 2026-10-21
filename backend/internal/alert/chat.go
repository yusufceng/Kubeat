package alert

import (
	"context"
	"net/http"
	"strings"
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
// same format (MattermostSender).
type SlackSender struct {
	Client    *http.Client
	UserAgent string
}

// MattermostSender posts Slack's body to a Mattermost incoming webhook,
// escaped for what Mattermost reads on top of Slack's syntax
// (mattermostEscaper).
type MattermostSender struct {
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

// chatEscaper escapes the characters Slack gives a meaning in text, and
// Google Chat in its own: "<" opens a mention such as <!channel> or
// <users/all>, or a link such as <https://…|label> whose label hides the
// address, and "&" starts the escape of either. Names, targets and errors,
// a push report's message among them, are escaped with it, so that these
// forms read as typed instead of notifying a whole channel or disguising a
// link. Slack shows a plain @channel as text, since Kubeat does not ask it
// to parse names (link_names), and its text has no other way to write a
// link. The same three escapes make the text safe where Google Chat
// renders HTML.
var chatEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// mattermostEscaper is chatEscaper and more. Mattermost takes Slack's forms
// too, but it reads text as Markdown and knows mentions of its own: a
// plain @channel, @here, @all, or a user's or a group's @name. A zero-width
// space after every "@" keeps any of them from notifying anyone, and one
// after every "]" keeps [label](address) from turning into a link whose
// label hides its address, or an image the clients would fetch: neither
// is Markdown any longer, and the text reads as typed.
var mattermostEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "@", "@​", "]", "]​")

// newSlackBody is the body of a Slack or Mattermost message, with what came
// from a monitor or a target escaped by esc.
func newSlackBody(n Notification, now time.Time, esc func(string) string) slackBody {
	text := esc(Text(n))
	// The link is Kubeat's own address, which Slack takes as it is.
	attachment := slackAttachment{
		Color: Color(n), Fallback: text, Title: esc(subject(n)), TitleLink: URL(n),
		Footer: "Kubeat", Timestamp: now.Unix(),
	}
	for _, f := range Facts(n) {
		attachment.Fields = append(attachment.Fields, slackField{Title: esc(f.Label), Value: esc(f.Value), Short: shortValue(f.Value)})
	}
	return slackBody{Text: text, Attachments: []slackAttachment{attachment}}
}

func (s *SlackSender) Send(ctx context.Context, ch store.Channel, n Notification) error {
	return postJSON(ctx, s.Client, s.UserAgent, ch.Target, newSlackBody(n, time.Now(), chatEscaper.Replace))
}

func (s *MattermostSender) Send(ctx context.Context, ch store.Channel, n Notification) error {
	return postJSON(ctx, s.Client, s.UserAgent, ch.Target, newSlackBody(n, time.Now(), mattermostEscaper.Replace))
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
	esc := chatEscaper.Replace
	body := chatBody{Text: esc(Text(n))}
	facts := Facts(n)
	link := URL(n)
	if len(facts) == 0 && link == "" {
		return body
	}
	// Chat renders a subset of HTML in most of a card's text, so every value
	// placed in the card is escaped as well.
	section := chatSection{}
	for _, f := range facts {
		section.Widgets = append(section.Widgets, chatWidget{DecoratedText: &chatDecorated{TopLabel: esc(f.Label), Text: esc(f.Value), Wrap: true}})
	}
	if link != "" {
		section.Widgets = append(section.Widgets, chatWidget{ButtonList: &chatButtonList{
			Buttons: []chatButton{{Text: wording(n).openInKubeat, OnClick: chatOnClick{OpenLink: chatOpenLink{URL: link}}}},
		}})
	}
	body.CardsV2 = []chatCardWrap{{
		CardID: "kubeat",
		Card:   chatCard{Header: &chatHeader{Title: Emoji(n) + " " + esc(subject(n)), Subtitle: esc(n.Monitor.Target)}, Sections: []chatSection{section}},
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
	case EventUp, EventCertRenewed, EventSlowResolved:
		return "Good"
	case EventCertExpiring, EventSlow:
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
