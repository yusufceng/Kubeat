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

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

func sampleDown() Notification {
	return Notification{
		Event:   EventDown,
		Monitor: store.Monitor{ID: 7, Name: "ödeme-api", Namespace: "shop", Type: store.MonitorHTTP, Target: "https://pay.example.com/healthz", FailureThreshold: 3},
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

func TestSlackSenderPostsOnlyText(t *testing.T) {
	srv, bodies := capture(t)
	sender := &SlackSender{Client: srv.Client()}
	n := sampleDown()

	if err := sender.Send(context.Background(), store.Channel{Type: store.ChannelSlack, Target: srv.URL}, n); err != nil {
		t.Fatal(err)
	}
	got := bodies()[0]
	if len(got) != 1 || got["text"] != Text(n) {
		t.Errorf("slack body = %v, want only the text field", got)
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
	if text := elements[0].(map[string]any); text["type"] != "TextBlock" || text["text"] != Text(n) || text["wrap"] != true {
		t.Errorf("first element = %v", text)
	}
	facts := elements[1].(map[string]any)["facts"].([]any)
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
	if n := len(testCard["body"].([]any)); n != 1 {
		t.Errorf("test card has %d elements, want 1", n)
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
	router := NewRouter(srv.Client(), "kubeat/test", SMTPConfig{})
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
	if len(got[1]) != 1 {
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
	n.Incident.Cause = "no push received for 2m5s; one is expected every 1m0s"

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
