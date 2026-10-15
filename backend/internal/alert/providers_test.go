package alert

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// received is one request a provider's stand-in got.
type received struct {
	Path   string
	Header http.Header
	Body   map[string]any
}

// provider returns a server that records requests and answers with status
// and reply.
func provider(t *testing.T, status int, reply string) (*httptest.Server, func() []received) {
	t.Helper()
	var (
		mu   sync.Mutex
		seen []received
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("body is not JSON: %s", raw)
		}
		mu.Lock()
		seen = append(seen, received{Path: r.URL.Path, Header: r.Header.Clone(), Body: body})
		mu.Unlock()
		w.WriteHeader(status)
		io.WriteString(w, reply)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []received {
		mu.Lock()
		defer mu.Unlock()
		return append([]received(nil), seen...)
	}
}

func resolved() Notification {
	n := sampleDown()
	n.Event = EventUp
	n.Incident.ResolvedAt = n.Incident.StartedAt.Add(90 * time.Second)
	return n
}

func TestDiscordSender(t *testing.T) {
	srv, seen := provider(t, http.StatusNoContent, "")
	sender := &DiscordSender{Client: srv.Client(), UserAgent: "kubeat/test"}
	n := sampleDown()
	if err := sender.Send(context.Background(), store.Channel{Target: srv.URL + "/api/webhooks/1/abc"}, n); err != nil {
		t.Fatal(err)
	}
	got := seen()[0]
	embed := got.Body["embeds"].([]any)[0].(map[string]any)
	if got.Path != "/api/webhooks/1/abc" || embed["description"] != Text(n) || embed["title"] != "ödeme-api" ||
		embed["color"].(float64) == 0 || got.Header.Get("User-Agent") != "kubeat/test" {
		t.Errorf("request = %+v", got)
	}
	if fields := embed["fields"].([]any); len(fields) != 5 || fields[0].(map[string]any)["name"] != "Monitor" {
		t.Errorf("embed fields = %v", fields)
	}
	// Nothing in the message may ping anyone, @everyone included.
	mentions, _ := got.Body["allowed_mentions"].(map[string]any)
	if parse, ok := mentions["parse"].([]any); !ok || len(parse) != 0 {
		t.Errorf("allowed_mentions = %v, want an empty parse list", got.Body["allowed_mentions"])
	}

	// A message longer than Discord takes is cut, not refused.
	n.Incident.Cause = strings.Repeat("ş", 3000)
	if err := sender.Send(context.Background(), store.Channel{Target: srv.URL}, n); err != nil {
		t.Fatal(err)
	}
	description := seen()[1].Body["embeds"].([]any)[0].(map[string]any)["description"].(string)
	if len(description) > discordLimit+3 || !strings.HasSuffix(description, "…") {
		t.Errorf("long description: %d bytes, ends %q", len(description), description[len(description)-6:])
	}
}

func TestTelegramSender(t *testing.T) {
	srv, seen := provider(t, http.StatusOK, `{"ok":true}`)
	sender := &TelegramSender{Client: srv.Client(), BaseURL: srv.URL + "/"}
	ch := store.Channel{Target: "-1001234", Secret: "123:abc"}
	n := sampleDown()
	if err := sender.Send(context.Background(), ch, n); err != nil {
		t.Fatal(err)
	}
	got := seen()[0]
	text, _ := got.Body["text"].(string)
	if got.Path != "/bot123:abc/sendMessage" || got.Body["chat_id"] != "-1001234" || got.Body["parse_mode"] != "HTML" ||
		!strings.HasPrefix(text, "<b>🔴 ödeme-api</b>\n") || !strings.Contains(text, "Hedef: https://pay.example.com/healthz") {
		t.Errorf("request = %+v", got)
	}

	// What came from a monitor is escaped, so a name cannot add markup.
	sharp := sampleDown()
	sharp.Monitor.Name = "<b>evil</b> & co"
	if err := sender.Send(context.Background(), ch, sharp); err != nil {
		t.Fatal(err)
	}
	escaped := seen()[1].Body["text"].(string)
	if strings.Contains(escaped, "<b>evil") || !strings.Contains(escaped, "&lt;b&gt;evil&lt;/b&gt; &amp; co") {
		t.Errorf("a monitor name was not escaped: %q", escaped)
	}

	// A refusal carries Telegram's own explanation, and never the token.
	refusing, _ := provider(t, http.StatusBadRequest, `{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`)
	err := (&TelegramSender{Client: refusing.Client(), BaseURL: refusing.URL}).Send(context.Background(), ch, n)
	if err == nil || err.Error() != "Telegram answered 400: Bad Request: chat not found" {
		t.Errorf("refusal: err = %v", err)
	}
	refusing.Close()
	err = (&TelegramSender{Client: refusing.Client(), BaseURL: refusing.URL}).Send(context.Background(), ch, n)
	if err == nil || strings.Contains(err.Error(), "123:abc") || strings.Contains(err.Error(), "/bot") {
		t.Errorf("unreachable: err = %v (must not carry the token or the URL)", err)
	}
}

func TestPagerDutySender(t *testing.T) {
	srv, seen := provider(t, http.StatusAccepted, `{"status":"success"}`)
	sender := &PagerDutySender{Client: srv.Client(), URL: srv.URL + "/v2/enqueue"}
	ch := store.Channel{Secret: "R0UT1NGKEY"}
	ctx := context.Background()

	down := sampleDown()
	if err := sender.Send(ctx, ch, down); err != nil {
		t.Fatal(err)
	}
	if err := sender.Send(ctx, ch, resolved()); err != nil {
		t.Fatal(err)
	}
	cert := sampleDown()
	cert.Event, cert.Incident.ID, cert.CertExpiresAt = EventCertExpiring, 5, cert.Incident.StartedAt.Add(72*time.Hour)
	if err := sender.Send(ctx, ch, cert); err != nil {
		t.Fatal(err)
	}
	got := seen()

	trigger := got[0].Body
	payload := trigger["payload"].(map[string]any)
	if got[0].Path != "/v2/enqueue" || trigger["routing_key"] != "R0UT1NGKEY" || trigger["event_action"] != "trigger" ||
		trigger["dedup_key"] != "kubeat-incident-4" || payload["severity"] != "critical" || payload["summary"] != Text(down) ||
		payload["source"] != down.Monitor.Target || payload["component"] != "ödeme-api" || payload["group"] != "shop" {
		t.Errorf("trigger = %+v", trigger)
	}
	if details := payload["custom_details"].(map[string]any); details["Neden"] != "unexpected status 503" {
		t.Errorf("details = %+v", details)
	}
	// The recovery closes the alert the outage opened.
	if resolve := got[1].Body; resolve["event_action"] != "resolve" || resolve["dedup_key"] != "kubeat-incident-4" || resolve["payload"] != nil {
		t.Errorf("resolve = %+v", resolve)
	}
	if warning := got[2].Body; warning["dedup_key"] != "kubeat-incident-5" || warning["payload"].(map[string]any)["severity"] != "warning" {
		t.Errorf("certificate warning = %+v", warning)
	}

	// A test raises an informational alert and resolves it at once.
	if err := sender.Send(ctx, ch, Notification{Event: EventTest}); err != nil {
		t.Fatal(err)
	}
	test := seen()[3:]
	if len(test) != 2 || test[0].Body["event_action"] != "trigger" || test[0].Body["payload"].(map[string]any)["severity"] != "info" ||
		test[1].Body["event_action"] != "resolve" || test[0].Body["dedup_key"] != test[1].Body["dedup_key"] {
		t.Errorf("test events = %+v", test)
	}

	refusing, _ := provider(t, http.StatusBadRequest, `{"status":"invalid event","message":"Event object is invalid","errors":["Length of 'routing_key' is incorrect"]}`)
	err := (&PagerDutySender{Client: refusing.Client(), URL: refusing.URL}).Send(ctx, ch, down)
	if err == nil || !strings.Contains(err.Error(), "PagerDuty answered 400") || !strings.Contains(err.Error(), "routing_key") {
		t.Errorf("refusal: err = %v", err)
	}
}

func TestNtfySender(t *testing.T) {
	srv, seen := provider(t, http.StatusOK, `{}`)
	sender := &NtfySender{Client: srv.Client()}
	ctx := context.Background()

	if err := sender.Send(ctx, store.Channel{Target: srv.URL + "/kubeat-alarm", Secret: "tk_abc"}, sampleDown()); err != nil {
		t.Fatal(err)
	}
	if err := sender.Send(ctx, store.Channel{Target: srv.URL + "/ntfy/kubeat-alarm/"}, resolved()); err != nil {
		t.Fatal(err)
	}
	got := seen()
	down := got[0]
	if down.Path != "/" || down.Body["topic"] != "kubeat-alarm" || down.Body["title"] != "[Kubeat] ödeme-api erişilemiyor" ||
		down.Body["priority"] != float64(5) || down.Header.Get("Authorization") != "Bearer tk_abc" {
		t.Errorf("down = %+v", down)
	}
	// A server under a path prefix, and no token.
	up := got[1]
	if up.Path != "/ntfy/" || up.Body["topic"] != "kubeat-alarm" || up.Body["priority"] != float64(3) || up.Header.Get("Authorization") != "" {
		t.Errorf("up = %+v", up)
	}

	for _, bad := range []string{"", "ntfy.sh/topic", "https://ntfy.sh", "https://ntfy.sh/", "ftp://ntfy.sh/topic"} {
		if _, _, err := SplitNtfyTopic(bad); err == nil {
			t.Errorf("SplitNtfyTopic(%q) was accepted", bad)
		}
	}
	if server, topic, err := SplitNtfyTopic("https://ntfy.example.com/alerts?x=1"); err != nil || server != "https://ntfy.example.com/" || topic != "alerts" {
		t.Errorf("SplitNtfyTopic = %q, %q, %v", server, topic, err)
	}
}

func TestGotifySender(t *testing.T) {
	srv, seen := provider(t, http.StatusOK, `{"id":1}`)
	sender := &GotifySender{Client: srv.Client()}
	if err := sender.Send(context.Background(), store.Channel{Target: srv.URL + "/gotify/", Secret: "AppT0ken"}, sampleDown()); err != nil {
		t.Fatal(err)
	}
	got := seen()[0]
	message, _ := got.Body["message"].(string)
	extras, _ := got.Body["extras"].(map[string]any)
	display, _ := extras["client::display"].(map[string]any)
	if got.Path != "/gotify/message" || got.Header.Get("X-Gotify-Key") != "AppT0ken" || got.Body["title"] != "[Kubeat] ödeme-api erişilemiyor" ||
		!strings.HasPrefix(message, Text(sampleDown())) || !strings.Contains(message, "- **Hedef:**") ||
		display["contentType"] != "text/markdown" || got.Body["priority"] != float64(8) {
		t.Errorf("request = %+v", got)
	}

	refusing, _ := provider(t, http.StatusUnauthorized, `{"error":"Unauthorized","errorDescription":"you need to provide a valid access token"}`)
	err := sender.Send(context.Background(), store.Channel{Target: refusing.URL, Secret: "wrong"}, sampleDown())
	if err == nil || !strings.Contains(err.Error(), "Gotify answered 401") {
		t.Errorf("refusal: err = %v", err)
	}
}

// A refusal names the provider and the status, and of the answer repeats
// only the reason in the field the provider documents for it: a channel's
// address may point anywhere, and whatever answers there is not to be read
// back through a test delivery.
func TestRefusalsRepeatOnlyTheProvidersReason(t *testing.T) {
	const secret = "INTERNAL-SECRET-BODY: db_password=hunter2"
	send := func(typ store.ChannelType, status int, reply string) error {
		srv, _ := provider(t, status, reply)
		router := NewRouter(nil, "kubeat/test", SMTPConfig{}, Endpoints{Telegram: srv.URL, PagerDuty: srv.URL})
		return router.Send(context.Background(), store.Channel{Type: typ, Target: srv.URL + "/topic", Secret: "s"}, sampleDown())
	}
	for typ, tc := range map[store.ChannelType]struct{ name, reply, reason string }{
		store.ChannelDiscord:   {"Discord", `{"message":"Unknown Webhook","code":10015}`, "Unknown Webhook"},
		store.ChannelTelegram:  {"Telegram", `{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`, "Bad Request: chat not found"},
		store.ChannelNtfy:      {"ntfy", `{"code":40301,"http":403,"error":"forbidden"}`, "forbidden"},
		store.ChannelGotify:    {"Gotify", `{"error":"Unauthorized","errorCode":401,"errorDescription":"you need to provide a valid access token"}`, "you need to provide a valid access token"},
		store.ChannelPagerDuty: {"PagerDuty", `{"status":"invalid event","message":"Event object is invalid","errors":["Length of 'routing_key' is incorrect"]}`, "Length of 'routing_key' is incorrect"},
	} {
		if err := send(typ, http.StatusBadRequest, tc.reply); err == nil || err.Error() != tc.name+" answered 400: "+tc.reason {
			t.Errorf("%s with a reason: err = %v", typ, err)
		}
		for _, reply := range []string{
			secret,                                  // not JSON
			`{"secret":"` + secret + `"}`,           // JSON without the field
			`{"message":{"text":"` + secret + `"}}`, // the field, but not text
			`["` + secret + `"]`,
		} {
			if err := send(typ, http.StatusInternalServerError, reply); err == nil || err.Error() != tc.name+" answered 500" {
				t.Errorf("%s answering %s: err = %v, want the status alone", typ, reply, err)
			}
		}
	}
	// A long reason is cut.
	long := strings.Repeat("ş", 300)
	if err := send(store.ChannelDiscord, http.StatusBadRequest, `{"message":"`+long+`"}`); err == nil ||
		!strings.HasSuffix(err.Error(), "…") || len(err.Error()) > len("Discord answered 400: ")+reasonLimit+len("…") {
		t.Errorf("long reason: %v", err)
	}
}

// Every type has a sender, and the chat tools that read "text" get it.
func TestRouterKnowsEveryType(t *testing.T) {
	srv, seen := provider(t, http.StatusOK, `{}`)
	router := NewRouter(nil, "kubeat/test", SMTPConfig{}, Endpoints{Telegram: srv.URL, PagerDuty: srv.URL})
	n := sampleDown()
	for _, typ := range store.ChannelTypes {
		if typ == store.ChannelEmail {
			continue // needs an SMTP server; tested on its own
		}
		if err := router.Send(context.Background(), store.Channel{Type: typ, Target: srv.URL + "/topic", Secret: "s"}, n); err != nil {
			t.Errorf("%s: %v", typ, err)
		}
	}
	if got := len(seen()); got != len(store.ChannelTypes)-1 {
		t.Errorf("%d requests for %d types", got, len(store.ChannelTypes)-1)
	}
	for _, typ := range []store.ChannelType{store.ChannelMattermost, store.ChannelGoogleChat} {
		before := len(seen())
		if err := router.Send(context.Background(), store.Channel{Type: typ, Target: srv.URL}, n); err != nil {
			t.Fatal(err)
		}
		// Mattermost reads Slack's attachments, Google Chat its own card.
		body := seen()[before].Body
		rich := body["attachments"] != nil || body["cardsV2"] != nil
		if body["text"] != Text(n) || !rich {
			t.Errorf("%s body = %+v", typ, body)
		}
	}
}
