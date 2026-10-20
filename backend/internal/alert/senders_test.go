package alert

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/netguard"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

func sampleDown() Notification {
	return Notification{
		Event:    EventDown,
		Language: LanguageTurkish,
		Monitor:  store.Monitor{ID: 7, Name: "ödeme-api", Namespace: "shop", Type: store.MonitorHTTP, Target: "https://pay.example.com/healthz", FailureThreshold: 3},
		Incident: store.Incident{ID: 4, Kind: store.IncidentDown,
			StartedAt: time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC), Cause: "unexpected status 503"},
	}
}

// capture returns a server that records the JSON bodies posted to it.
func capture(t *testing.T) (*httptest.Server, func() []map[string]any) {
	t.Helper()
	var (
		mu     sync.Mutex
		bodies []map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("body is not JSON: %v", err)
		}
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
	}))
	t.Cleanup(srv.Close)
	return srv, func() []map[string]any {
		mu.Lock()
		defer mu.Unlock()
		return bodies
	}
}

func TestSlackSenderPostsTextAndAnAttachment(t *testing.T) {
	srv, bodies := capture(t)
	sender := &SlackSender{Client: srv.Client()}
	n := sampleDown()
	n.PublicURL = "https://kubeat.example/"

	if err := sender.Send(context.Background(), store.Channel{Type: store.ChannelSlack, Target: srv.URL}, n); err != nil {
		t.Fatal(err)
	}
	got := bodies()[0]
	// The text is what a notification preview shows.
	if got["text"] != Text(n) {
		t.Errorf("slack text = %v", got["text"])
	}
	attachment := got["attachments"].([]any)[0].(map[string]any)
	if attachment["color"] != colorDown || attachment["title"] != "ödeme-api" ||
		attachment["title_link"] != "https://kubeat.example/incidents/4" || attachment["footer"] != "Kubeat" {
		t.Errorf("attachment = %v", attachment)
	}
	var titles []string
	for _, f := range attachment["fields"].([]any) {
		titles = append(titles, f.(map[string]any)["title"].(string))
	}
	if strings.Join(titles, ",") != "Monitor,Hedef,Namespace,Başlangıç,Neden" {
		t.Errorf("field titles = %v", titles)
	}

	// Without a public address the attachment carries no link.
	n.PublicURL = ""
	if err := sender.Send(context.Background(), store.Channel{Target: srv.URL}, n); err != nil {
		t.Fatal(err)
	}
	if link := bodies()[1]["attachments"].([]any)[0].(map[string]any)["title_link"]; link != nil {
		t.Errorf("title_link without a public URL = %v", link)
	}
}

func TestGoogleChatSenderPostsACard(t *testing.T) {
	srv, bodies := capture(t)
	sender := &GoogleChatSender{Client: srv.Client()}
	n := sampleDown()
	n.PublicURL = "https://kubeat.example"

	if err := sender.Send(context.Background(), store.Channel{Type: store.ChannelGoogleChat, Target: srv.URL}, n); err != nil {
		t.Fatal(err)
	}
	got := bodies()[0]
	if got["text"] != Text(n) {
		t.Errorf("chat text = %v", got["text"])
	}
	card := got["cardsV2"].([]any)[0].(map[string]any)["card"].(map[string]any)
	header := card["header"].(map[string]any)
	if header["title"] != "🔴 ödeme-api" || header["subtitle"] != "https://pay.example.com/healthz" {
		t.Errorf("header = %v", header)
	}
	widgets := card["sections"].([]any)[0].(map[string]any)["widgets"].([]any)
	first := widgets[0].(map[string]any)["decoratedText"].(map[string]any)
	if first["topLabel"] != "Monitor" || first["text"] != "ödeme-api" {
		t.Errorf("first widget = %v", first)
	}
	button := widgets[len(widgets)-1].(map[string]any)["buttonList"].(map[string]any)["buttons"].([]any)[0].(map[string]any)
	if button["text"] != "Kubeat'te aç" ||
		button["onClick"].(map[string]any)["openLink"].(map[string]any)["url"] != "https://kubeat.example/incidents/4" {
		t.Errorf("button = %v", button)
	}

	// A test notification has no details: no card at all.
	if err := sender.Send(context.Background(), store.Channel{Target: srv.URL}, Notification{Event: EventTest}); err != nil {
		t.Fatal(err)
	}
	if cards := bodies()[1]["cardsV2"]; cards != nil {
		t.Errorf("test message carries a card: %v", cards)
	}
}

func TestTeamsSenderPostsAnAdaptiveCard(t *testing.T) {
	srv, bodies := capture(t)
	sender := &TeamsSender{Client: srv.Client()}
	n := sampleDown()

	if err := sender.Send(context.Background(), store.Channel{Type: store.ChannelTeams, Target: srv.URL}, n); err != nil {
		t.Fatal(err)
	}
	got := bodies()[0]
	attachments, _ := got["attachments"].([]any)
	if got["type"] != "message" || len(attachments) != 1 {
		t.Fatalf("teams body = %v", got)
	}
	attachment := attachments[0].(map[string]any)
	card, _ := attachment["content"].(map[string]any)
	if attachment["contentType"] != "application/vnd.microsoft.card.adaptive" || card["type"] != "AdaptiveCard" {
		t.Fatalf("attachment = %v", attachment)
	}
	elements := card["body"].([]any)
	if head := elements[0].(map[string]any); head["text"] != "🔴 ödeme-api" || head["color"] != "Attention" || head["weight"] != "Bolder" {
		t.Errorf("first element = %v", head)
	}
	if text := elements[1].(map[string]any); text["type"] != "TextBlock" || text["text"] != Text(n) || text["wrap"] != true {
		t.Errorf("second element = %v", text)
	}
	facts := elements[2].(map[string]any)["facts"].([]any)
	var titles []string
	for _, f := range facts {
		titles = append(titles, f.(map[string]any)["title"].(string))
	}
	if strings.Join(titles, ",") != "Monitor,Hedef,Namespace,Başlangıç,Neden" {
		t.Errorf("fact titles = %v", titles)
	}

	// A test notification has no facts and no empty FactSet.
	if err := sender.Send(context.Background(), store.Channel{Target: srv.URL}, Notification{Event: EventTest}); err != nil {
		t.Fatal(err)
	}
	testCard := bodies()[1]["attachments"].([]any)[0].(map[string]any)["content"].(map[string]any)
	if n := len(testCard["body"].([]any)); n != 2 {
		t.Errorf("test card has %d elements, want the heading and the text", n)
	}
}

func TestFactsForResolvedIncident(t *testing.T) {
	n := sampleDown()
	n.Event = EventUp
	n.Incident.ResolvedAt = n.Incident.StartedAt.Add(5 * time.Minute)
	n.Monitor.Namespace = ""

	var labels []string
	for _, f := range Facts(n) {
		labels = append(labels, f.Label)
	}
	if strings.Join(labels, ",") != "Monitor,Hedef,Başlangıç,Bitiş,Neden" {
		t.Errorf("labels = %v", labels)
	}
	if Facts(Notification{Event: EventTest}) != nil {
		t.Error("test notification has facts")
	}
}

func TestParseRecipients(t *testing.T) {
	got, err := ParseRecipients(" ops@example.com , Nöbetçi <oncall@example.com>,")
	if err != nil || len(got) != 2 || got[0] != "ops@example.com" || got[1] != "oncall@example.com" {
		t.Errorf("ParseRecipients = %v, %v", got, err)
	}
	for _, bad := range []string{"", " , ", "not-an-address", "a@example.com, b@", "a@example.com\r\nBcc: x@example.com"} {
		if _, err := ParseRecipients(bad); err == nil {
			t.Errorf("ParseRecipients(%q) was accepted", bad)
		}
	}
}

// fakeSMTP is a minimal SMTP server that records what it receives.
type fakeSMTP struct {
	listener net.Listener
	mu       sync.Mutex
	from     string
	rcpts    []string
	data     string
	commands []string
}

func newFakeSMTP(t *testing.T) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeSMTP{listener: ln}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(conn)
		}
	}()
	return f
}

func (f *fakeSMTP) serve(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	reply := func(s string) { io.WriteString(conn, s+"\r\n") }
	reply("220 fake ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		f.mu.Lock()
		f.commands = append(f.commands, strings.SplitN(line, " ", 2)[0])
		f.mu.Unlock()
		switch upper := strings.ToUpper(line); {
		case strings.HasPrefix(upper, "EHLO"):
			reply("250-fake\r\n250 8BITMIME")
		case strings.HasPrefix(upper, "MAIL FROM:"):
			f.mu.Lock()
			f.from = strings.Trim(strings.SplitN(line[10:], " ", 2)[0], "<>")
			f.mu.Unlock()
			reply("250 ok")
		case strings.HasPrefix(upper, "RCPT TO:"):
			f.mu.Lock()
			f.rcpts = append(f.rcpts, strings.Trim(line[8:], "<>"))
			f.mu.Unlock()
			reply("250 ok")
		case upper == "DATA":
			reply("354 go ahead")
			var data strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil || l == ".\r\n" {
					break
				}
				data.WriteString(l)
			}
			f.mu.Lock()
			f.data = data.String()
			f.mu.Unlock()
			reply("250 queued")
		case upper == "QUIT":
			reply("221 bye")
			return
		default:
			reply("502 not implemented")
		}
	}
}

func (f *fakeSMTP) config() SMTPConfig {
	host, port, _ := net.SplitHostPort(f.listener.Addr().String())
	p, _ := strconv.Atoi(port)
	return SMTPConfig{Host: host, Port: p, From: "Kubeat <kubeat@example.com>", Security: SMTPNone}
}

func TestEmailSender(t *testing.T) {
	server := newFakeSMTP(t)
	sender := &EmailSender{SMTP: server.config()}
	ch := store.Channel{Type: store.ChannelEmail, Target: "ops@example.com, oncall@example.com"}
	n := sampleDown()
	// A name that tries to smuggle in a header.
	n.Monitor.Name = "api\r\nBcc: attacker@example.com"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sender.Send(ctx, ch, n); err != nil {
		t.Fatalf("send: %v", err)
	}

	server.mu.Lock()
	defer server.mu.Unlock()
	if server.from != "kubeat@example.com" || strings.Join(server.rcpts, ",") != "ops@example.com,oncall@example.com" {
		t.Errorf("envelope: from=%q rcpts=%v", server.from, server.rcpts)
	}

	msg, err := mail.ReadMessage(strings.NewReader(server.data))
	if err != nil {
		t.Fatalf("message does not parse: %v\n%s", err, server.data)
	}
	if msg.Header.Get("Bcc") != "" {
		t.Errorf("monitor name injected a header: Bcc=%q", msg.Header.Get("Bcc"))
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if err != nil || !strings.HasPrefix(subject, "[Kubeat] api") || !strings.HasSuffix(subject, "erişilemiyor") {
		t.Errorf("subject = %q, %v", subject, err)
	}
	if msg.Header.Get("To") != "ops@example.com, oncall@example.com" || msg.Header.Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Errorf("headers: To=%q Content-Type=%q", msg.Header.Get("To"), msg.Header.Get("Content-Type"))
	}
	body, _ := io.ReadAll(quotedprintable.NewReader(msg.Body))
	for _, want := range []string{"erişilemiyor", "Hedef: https://pay.example.com/healthz", "Namespace: shop", "Neden: unexpected status 503"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("body lacks %q:\n%s", want, body)
		}
	}
}

func TestEmailSenderFailures(t *testing.T) {
	ch := store.Channel{Type: store.ChannelEmail, Target: "ops@example.com"}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	unconfigured := &EmailSender{}
	if unconfigured.Enabled() {
		t.Error("sender without a host reports enabled")
	}
	if err := unconfigured.Send(ctx, ch, sampleDown()); !errors.Is(err, ErrEmailNotConfigured) {
		t.Errorf("unconfigured: err = %v, want ErrEmailNotConfigured", err)
	}

	// The fake server offers no STARTTLS: the default mode must refuse
	// rather than fall back to an unencrypted session.
	server := newFakeSMTP(t)
	cfg := server.config()
	cfg.Security = SMTPStartTLS
	err := (&EmailSender{SMTP: cfg}).Send(ctx, ch, sampleDown())
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Errorf("starttls against a server without it: err = %v", err)
	}
	server.mu.Lock()
	sent := server.data
	server.mu.Unlock()
	if sent != "" {
		t.Error("message was sent unencrypted in starttls mode")
	}

	cfg = server.config()
	cfg.Port = 1
	if err := (&EmailSender{SMTP: cfg}).Send(ctx, ch, sampleDown()); err == nil {
		t.Error("unreachable server reported success")
	}
	if err := (&EmailSender{SMTP: server.config()}).Send(ctx, store.Channel{Target: "nope"}, sampleDown()); err == nil {
		t.Error("invalid recipient reported success")
	}
}

func TestRouterPicksSenderByType(t *testing.T) {
	srv, bodies := capture(t)
	router := NewRouter(nil, "kubeat/test", SMTPConfig{}, Endpoints{})
	ctx := context.Background()
	n := sampleDown()

	for _, typ := range []store.ChannelType{store.ChannelWebhook, store.ChannelSlack, store.ChannelTeams} {
		if err := router.Send(ctx, store.Channel{Type: typ, Target: srv.URL}, n); err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
	}
	got := bodies()
	if got[0]["event"] != "down" || got[0]["monitor"] == nil {
		t.Errorf("webhook body = %v", got[0])
	}
	if got[1]["text"] != Text(n) || got[1]["attachments"] == nil {
		t.Errorf("slack body = %v", got[1])
	}
	if got[2]["attachments"] == nil {
		t.Errorf("teams body = %v", got[2])
	}

	if router.EmailEnabled() {
		t.Error("router without SMTP settings reports e-mail enabled")
	}
	if err := router.Send(ctx, store.Channel{Type: store.ChannelEmail, Target: "a@example.com"}, n); !errors.Is(err, ErrEmailNotConfigured) {
		t.Errorf("email without SMTP: err = %v", err)
	}
	if err := router.Send(ctx, store.Channel{Type: "pagerduty"}, n); err == nil {
		t.Error("unknown channel type reported success")
	}
}

// A push monitor has no target; its notifications do not show an empty one.
func TestNotificationWithoutATarget(t *testing.T) {
	n := sampleDown()
	n.Monitor = store.Monitor{ID: 9, Name: "gece-yedeği", Type: store.MonitorPush, FailureThreshold: 1}
	n.Incident.Cause = "no push within the expected 1m interval"

	want := "🔴 gece-yedeği erişilemiyor. Art arda 1 kontrol başarısız. Son hata: " + n.Incident.Cause
	if got := Text(n); got != want {
		t.Errorf("Text = %q\nwant   %q", got, want)
	}
	for _, fact := range Facts(n) {
		if fact.Label == "Hedef" {
			t.Errorf("an empty target is listed: %+v", fact)
		}
	}
	// With a target, both still carry it.
	if n := sampleDown(); !strings.Contains(Text(n), "erişilemiyor (https://pay.example.com/healthz).") || Facts(n)[1] != (Fact{"Hedef", n.Monitor.Target}) {
		t.Errorf("text = %q, facts = %+v", Text(n), Facts(n))
	}
}

// injected is a notification whose monitor name and cause, as a push
// report could set it, try to notify a whole chat channel and to disguise
// a link.
func injected() Notification {
	n := sampleDown()
	n.PublicURL = "https://kubeat.example"
	n.Monitor.Name = "api <!here> & co"
	n.Incident.Cause = "the source reported a failure: <!channel> <users/all> sign in again: <https://evil.example/login|https://kubeat.example/login>"
	return n
}

// What came from a monitor, a target or a push report reads as typed in
// Slack and Mattermost: Slack's forms, a broadcast mention such as
// <!channel> or a link <address|label> whose label hides its address, are
// escaped for both.
func TestSlackTextIsEscaped(t *testing.T) {
	srv, bodies := capture(t)
	router := NewRouter(nil, "kubeat/test", SMTPConfig{}, Endpoints{})
	n := injected()
	for _, typ := range []store.ChannelType{store.ChannelSlack, store.ChannelMattermost} {
		if err := router.Send(context.Background(), store.Channel{Type: typ, Target: srv.URL}, n); err != nil {
			t.Fatal(err)
		}
	}
	for i, got := range bodies() {
		text, _ := got["text"].(string)
		attachment := got["attachments"].([]any)[0].(map[string]any)
		if strings.ContainsAny(text, "<>") || !strings.Contains(text, "api &lt;!here&gt; &amp; co") ||
			!strings.Contains(text, "&lt;!channel&gt; &lt;users/all&gt;") || !strings.Contains(text, "&lt;https://evil.example/login|https://kubeat.example/login&gt;") {
			t.Errorf("body %d: text = %q", i, text)
		}
		if attachment["fallback"] != text || attachment["title"] != "api &lt;!here&gt; &amp; co" {
			t.Errorf("body %d: fallback = %q, title = %q", i, attachment["fallback"], attachment["title"])
		}
		for _, f := range attachment["fields"].([]any) {
			field := f.(map[string]any)
			if title, value := field["title"].(string), field["value"].(string); strings.ContainsAny(title+value, "<>") {
				t.Errorf("body %d: field %q = %q", i, title, value)
			}
		}
	}

	// Kubeat's own link stays a link: its "&" is not escaped. Without an
	// incident id to link to, the link is the monitor's chart over the
	// span, the one with a "&" in it.
	up := injected()
	up.Event, up.Incident.ResolvedAt = EventUp, up.Incident.StartedAt.Add(time.Minute)
	up.Incident.ID = 0
	for _, esc := range []func(string) string{chatEscaper.Replace, mattermostEscaper.Replace} {
		link := newSlackBody(up, time.Now(), esc).Attachments[0].TitleLink
		if link != URL(up) || !strings.Contains(link, "&to=") {
			t.Errorf("title_link = %q, want %q", link, URL(up))
		}
	}
}

// Mattermost reads Markdown and mentions of its own as well: an @channel,
// @here, @all or @name and a [label](address) from a monitor's name or a
// push report reach it broken by a zero-width space, so that they notify
// no one and link nowhere. Slack's text is left as it was.
func TestMattermostMentionsAndLinksReadAsTyped(t *testing.T) {
	srv, bodies := capture(t)
	router := NewRouter(nil, "kubeat/test", SMTPConfig{}, Endpoints{})
	n := sampleDown()
	n.Monitor.Name = "nightly-backup @all"
	n.Incident.Cause = "the source reported a failure: @channel @Here x.@all @ops sign in again: " +
		"[https://kubeat.example/login](https://evil.example/login) ![p](https://evil.example/p.png) [a][ref]\n[ref]: https://evil.example/r"
	for _, typ := range []store.ChannelType{store.ChannelMattermost, store.ChannelSlack} {
		if err := router.Send(context.Background(), store.Channel{Type: typ, Target: srv.URL}, n); err != nil {
			t.Fatal(err)
		}
	}
	// Every text of a body: the message, the fallback, the title, the fields.
	texts := func(body map[string]any) []string {
		attachment := body["attachments"].([]any)[0].(map[string]any)
		out := []string{body["text"].(string), attachment["fallback"].(string), attachment["title"].(string)}
		for _, f := range attachment["fields"].([]any) {
			field := f.(map[string]any)
			out = append(out, field["title"].(string), field["value"].(string))
		}
		return out
	}
	got := bodies()
	mattermost, slack := texts(got[0]), texts(got[1])
	for _, text := range mattermost {
		lower := strings.ToLower(text)
		for _, form := range []string{"@channel", "@here", "@all", "@ops", "](", "][", "]:"} {
			if strings.Contains(lower, form) {
				t.Errorf("mattermost: %q in %q", form, text)
			}
		}
	}
	// Both read as typed: without the zero-width spaces, the texts are Slack's.
	if want := chatEscaper.Replace(Text(n)); slack[0] != want ||
		strings.ReplaceAll(mattermost[0], "​", "") != want || !strings.Contains(mattermost[0], "nightly-backup @​all") {
		t.Errorf("text: mattermost %q, slack %q, want %q", mattermost[0], slack[0], want)
	}
	for i := range slack {
		if strings.Contains(slack[i], "​") || strings.ReplaceAll(mattermost[i], "​", "") != slack[i] {
			t.Errorf("text %d: mattermost %q, slack %q", i, mattermost[i], slack[i])
		}
	}
}

// Google Chat reads the same mentions and links in its text, and HTML in a
// card: what a monitor or a target wrote is escaped in both.
func TestGoogleChatTextIsEscaped(t *testing.T) {
	body := newChatBody(injected())
	if strings.ContainsAny(body.Text, "<>") || !strings.Contains(body.Text, "&lt;users/all&gt;") || !strings.Contains(body.Text, "&amp; co") {
		t.Errorf("text = %q", body.Text)
	}
	card := body.CardsV2[0].Card
	if card.Header.Title != "🔴 api &lt;!here&gt; &amp; co" {
		t.Errorf("header = %q", card.Header.Title)
	}
	for _, w := range card.Sections[0].Widgets {
		if d := w.DecoratedText; d != nil && strings.ContainsAny(d.TopLabel+d.Text, "<>") {
			t.Errorf("decorated text %q = %q", d.TopLabel, d.Text)
		}
	}
	// The cause, escaped, is still all there.
	cause := card.Sections[0].Widgets[4].DecoratedText
	if cause.TopLabel != "Neden" || cause.Text != chatEscaper.Replace(injected().Incident.Cause) {
		t.Errorf("cause = %+v", cause)
	}
}

// With a target policy no channel's address reaches the backend's own
// loopback; the Telegram, PagerDuty and mail server the operator set do,
// at their exact address. An exception lets the channels through too.
func TestRouterConnectsThroughTheGuard(t *testing.T) {
	srv, bodies := capture(t)
	mail := newFakeSMTP(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	send := func(router *Router, typ store.ChannelType) error {
		target := srv.URL + "/topic"
		if typ == store.ChannelEmail {
			target = "ops@example.com"
		}
		return router.Send(ctx, store.Channel{Type: typ, Target: target, Secret: "s"}, sampleDown())
	}
	endpoints := Endpoints{Telegram: srv.URL, PagerDuty: srv.URL}

	// The services the operator set are trusted at their exact address,
	// a sidecar's on loopback here, by their own senders alone: a channel
	// aimed at the same address is refused.
	strict, err := netguard.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(strict, "kubeat/test", mail.config(), endpoints)
	for _, typ := range store.ChannelTypes {
		err := send(router, typ)
		switch typ {
		case store.ChannelTelegram, store.ChannelPagerDuty, store.ChannelEmail:
			if err != nil {
				t.Errorf("%s to the operator's address: %v", typ, err)
			}
		default:
			if !errors.Is(err, netguard.ErrBlocked) {
				t.Errorf("%s: err = %v, want the target policy's", typ, err)
			}
		}
	}
	// Telegram and PagerDuty delivered, and the mail server took the mail.
	mail.mu.Lock()
	delivered := mail.data != ""
	mail.data = ""
	mail.mu.Unlock()
	if len(bodies()) != 2 || !delivered {
		t.Errorf("the operator's services: %d requests, mail delivered: %v", len(bodies()), delivered)
	}
	// A channel aimed at the mail server is refused as well.
	smtp := mail.config()
	hook := store.Channel{Type: store.ChannelWebhook, Target: "http://" + smtp.address() + "/"}
	if err := router.Send(ctx, hook, sampleDown()); !errors.Is(err, netguard.ErrBlocked) {
		t.Errorf("a webhook at the mail server: err = %v", err)
	}

	open, err := netguard.New(nil, []string{"127.0.0.0/8", "::1/128"})
	if err != nil {
		t.Fatal(err)
	}
	router = NewRouter(open, "kubeat/test", mail.config(), endpoints)
	for _, typ := range store.ChannelTypes {
		if err := send(router, typ); err != nil {
			t.Errorf("%s with an exception: %v", typ, err)
		}
	}
	mail.mu.Lock()
	delivered = mail.data != ""
	mail.mu.Unlock()
	if len(bodies()) != 2+len(store.ChannelTypes)-1 || !delivered {
		t.Errorf("with an exception: %d requests, mail delivered: %v", len(bodies()), delivered)
	}
}
