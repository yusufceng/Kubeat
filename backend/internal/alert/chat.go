package alert

import (
	"context"
	"net/http"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// SlackSender posts to a Slack incoming webhook, which takes a JSON object
// with the message in "text".
type SlackSender struct {
	Client    *http.Client
	UserAgent string
}

func (s *SlackSender) Send(ctx context.Context, ch store.Channel, n Notification) error {
	return postJSON(ctx, s.Client, s.UserAgent, ch.Target, map[string]string{"text": Text(n)})
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
	MSTeams map[string]any `json:"msteams,omitempty"`
}

// teamsElement is a TextBlock or a FactSet.
type teamsElement struct {
	Type   string      `json:"type"`
	Text   string      `json:"text,omitempty"`
	Wrap   bool        `json:"wrap,omitempty"`
	Weight string      `json:"weight,omitempty"`
	Facts  []teamsFact `json:"facts,omitempty"`
}

type teamsFact struct {
	Title string `json:"title"`
	Value string `json:"value"`
}

func newTeamsBody(n Notification) teamsBody {
	body := []teamsElement{{Type: "TextBlock", Text: Text(n), Wrap: true, Weight: "Bolder"}}
	if facts := Facts(n); len(facts) > 0 {
		set := teamsElement{Type: "FactSet"}
		for _, f := range facts {
			set.Facts = append(set.Facts, teamsFact{Title: f.Label, Value: f.Value})
		}
		body = append(body, set)
	}
	return teamsBody{
		Type: "message",
		Attachments: []teamsAttachment{{
			ContentType: "application/vnd.microsoft.card.adaptive",
			Content: teamsCard{
				Schema:  "http://adaptivecards.io/schemas/adaptive-card.json",
				Type:    "AdaptiveCard",
				Version: "1.4",
				Body:    body,
				MSTeams: map[string]any{"width": "Full"},
			},
		}},
	}
}
