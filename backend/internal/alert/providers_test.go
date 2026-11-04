package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"

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

// The instance goes into the key in ASCII: a Turkish or another accented
// Latin letter as the letter nearest it, so that Üretim is Uretim rather
// than retim and Test-Ç and Test-Ş are two keys; a run of what is neither
// as one dash, none at either end; a name of which nothing is left as no
// instance.
func TestDedupInstanceReadsInASCII(t *testing.T) {
	for _, tc := range []struct{ instance, key string }{
		{"", ""},
		{"prod-eu", "prod-eu"},
		{"Üretim", "Uretim"},
		{"üç", "uc"},
		{"Test-Ç", "Test-C"},
		{"Test-Ş", "Test-S"},
		{"Ön-üretim", "On-uretim"},
		{"İstanbul üretim", "Istanbul-uretim"},
		{"ÇĞIİÖŞÜ çğıiöşü", "CGIIOSU-cgiiosu"},
		{"ğüş", "gus"},
		{"Zürich São Paulo", "Zurich-Sao-Paulo"},
		{"Łódź", "Lodz"},
		{"Ærø", "AEro"},
		{"Straße", "Strasse"},
		{"u\u0308retim", "uretim"}, // ü not composed: u and a combining diaeresis
		{"kubeat.apps.example:8443", "kubeat.apps.example-8443"},
		{"Prod EU / 1", "Prod-EU-1"},
		{"-prod_2-", "prod_2"},
		{"東京", ""},
		{"Москва", ""},
		{"🙂", ""},
	} {
		if got := dedupInstance(tc.instance); got != tc.key {
			t.Errorf("dedupInstance(%q) = %q, want %q", tc.instance, got, tc.key)
		}
	}
	// Every letter of Latin-1 Supplement and Latin Extended-A has its ASCII.
	for r := rune(0xC0); r <= 0x17F; r++ {
		if unicode.IsLetter(r) && latinASCII[r] == "" {
			t.Errorf("no ASCII for %c (U+%04X)", r, r)
		}
	}
}

// The start says, in one line, which installation the notifications name
// and how it reads in PagerDuty's dedup key, so that the key an alert is
// open under can be found in the log: INFO when the key carries the name
// as it is, or there is none; WARN when the key reduces the name or keeps
// nothing of it, the keys then carrying no installation.
func TestLogInstanceSaysHowTheKeyReads(t *testing.T) {
	for _, tc := range []struct{ instance, level, key string }{
		{"", "INFO", ""},
		{"prod-eu", "INFO", "prod-eu"},
		{"kubeat.apps.example", "INFO", "kubeat.apps.example"},
		{"Üretim", "WARN", "Uretim"},
		{"kubeat.apps.example:8443", "WARN", "kubeat.apps.example-8443"},
		{"東京", "WARN", ""},
	} {
		var out bytes.Buffer
		LogInstance(slog.New(slog.NewJSONHandler(&out, nil)), tc.instance)
		lines := strings.Split(strings.TrimSpace(out.String()), "\n")
		if len(lines) != 1 {
			t.Fatalf("instance %q: %d lines logged:\n%s", tc.instance, len(lines), out.String())
		}
		var line map[string]any
		if err := json.Unmarshal([]byte(lines[0]), &line); err != nil {
			t.Fatalf("instance %q: %v: %s", tc.instance, err, lines[0])
		}
		if line["level"] != tc.level {
			t.Errorf("instance %q: level %v, want %s: %s", tc.instance, line["level"], tc.level, lines[0])
		}
		if tc.instance == "" {
			if _, ok := line["instance"]; ok {
				t.Errorf("instance %q: the line names an instance: %s", tc.instance, lines[0])
			}
			continue
		}
		if line["instance"] != tc.instance {
			t.Errorf("instance %q: the line says instance %v: %s", tc.instance, line["instance"], lines[0])
		}
		if key, ok := line["key"]; tc.key == "" && ok || tc.key != "" && key != tc.key {
			t.Errorf("instance %q: the line says key %v, want %q: %s", tc.instance, key, tc.key, lines[0])
		}
	}
}

// The dedup key names the installation, so that installations sharing a
// PagerDuty service keep their alerts apart: the same incident of two
// installations is two alerts. Without an instance the key is the one it
// always was. The instance goes into the key in ASCII letters, digits,
// dots, dashes and underscores (dedupInstance), and as it is into the
// custom details.
func TestPagerDutyKeysNameTheInstance(t *testing.T) {
	srv, seen := provider(t, http.StatusAccepted, `{"status":"success"}`)
	sender := &PagerDutySender{Client: srv.Client(), URL: srv.URL}
	ch := store.Channel{Secret: "R0UT1NGKEY"}
	ctx := context.Background()
	slow := sampleDown()
	slow.Event, slow.Incident = EventSlow, store.Incident{}
	slow.Since, slow.Threshold, slow.Latency = time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC), 500*time.Millisecond, 1200*time.Millisecond

	for _, tc := range []struct{ instance, incident, slowness string }{
		{"", "kubeat-incident-4", "kubeat-slow-7"},
		{"prod-eu", "kubeat-prod-eu-incident-4", "kubeat-prod-eu-slow-7"},
		{"staging", "kubeat-staging-incident-4", "kubeat-staging-slow-7"},
		{"kubeat.apps.example:8443", "kubeat-kubeat.apps.example-8443-incident-4", "kubeat-kubeat.apps.example-8443-slow-7"},
		{"Prod EU / 1", "kubeat-Prod-EU-1-incident-4", "kubeat-Prod-EU-1-slow-7"},
		{"İstanbul üretim", "kubeat-Istanbul-uretim-incident-4", "kubeat-Istanbul-uretim-slow-7"},
		{"-prod_2-", "kubeat-prod_2-incident-4", "kubeat-prod_2-slow-7"},
		{"東京", "kubeat-incident-4", "kubeat-slow-7"}, // nothing left for the key
	} {
		before := len(seen())
		down, slowed := sampleDown(), slow
		down.Instance, slowed.Instance = tc.instance, tc.instance
		for _, n := range []Notification{down, slowed} {
			if err := sender.Send(ctx, ch, n); err != nil {
				t.Fatal(err)
			}
		}
		got := seen()[before:]
		if got[0].Body["dedup_key"] != tc.incident || got[1].Body["dedup_key"] != tc.slowness {
			t.Errorf("instance %q: keys %v and %v, want %s and %s", tc.instance, got[0].Body["dedup_key"], got[1].Body["dedup_key"], tc.incident, tc.slowness)
		}
		for _, event := range got {
			details := event.Body["payload"].(map[string]any)["custom_details"].(map[string]any)
			if instance, ok := details["instance"]; tc.instance == "" && ok || tc.instance != "" && instance != tc.instance {
				t.Errorf("instance %q: custom_details = %v", tc.instance, details)
			}
		}
	}

	// A long name is cut, so that the key stays within PagerDuty's 255.
	long := sampleDown()
	key := dedupKey(strings.Repeat("a", 300), long)
	if want := "kubeat-" + strings.Repeat("a", dedupInstanceLimit) + "-incident-4"; key != want || len(key) > 255 {
		t.Errorf("long instance: key %q (%d characters)", key, len(key))
	}
}

// The key that names the installation came with an upgrade, and which key
// an alert is open under is not kept: the end of an incident opened, or of
// a slowness begun, before this process started goes under the new key and
// under the one without the instance, in that order. What raises an alert,
// a reminder or an outage sent again included, goes under the new key
// alone, and so does the end of what opened since the start.
func TestPagerDutyEndsUnderBothKeysWhatOpenedBeforeTheStart(t *testing.T) {
	srv, seen := provider(t, http.StatusAccepted, `{"status":"success"}`)
	started := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	sender := &PagerDutySender{Client: srv.Client(), URL: srv.URL, Started: started}
	ch := store.Channel{Secret: "R0UT1NGKEY"}
	ctx := context.Background()
	before, after := started.Add(-time.Hour), started.Add(time.Minute)

	// incident is an event about incident 4, which failed first at begun
	// and opened at opened (zero for an incident from before openings were
	// recorded).
	incident := func(event Event, instance string, begun, opened time.Time) Notification {
		n := sampleDown()
		n.Event, n.Instance = event, instance
		n.Incident.StartedAt, n.Incident.OpenedAt = begun, opened
		if event == EventUp || event == EventCertRenewed {
			n.Incident.ResolvedAt = after.Add(time.Hour)
		}
		return n
	}
	// slowness is an event about monitor 7, slow since since.
	slowness := func(event Event, instance string, since time.Time) Notification {
		n := sampleDown()
		n.Event, n.Instance, n.Incident = event, instance, store.Incident{}
		n.Since, n.At, n.Threshold, n.Latency = since, after.Add(time.Hour), 500*time.Millisecond, 100*time.Millisecond
		return n
	}
	both := []string{"resolve kubeat-prod-eu-incident-4", "resolve kubeat-incident-4"}
	for _, tc := range []struct {
		name string
		n    Notification
		want []string
	}{
		{"an outage opened before the start ends", incident(EventUp, "prod-eu", before, before), both},
		{"a certificate warning opened before the start ends", incident(EventCertRenewed, "prod-eu", before, before), both},
		{"an outage from before openings were recorded ends", incident(EventUp, "prod-eu", before, time.Time{}), both},
		{"a slowness begun before the start ends", slowness(EventSlowResolved, "prod-eu", before),
			[]string{"resolve kubeat-prod-eu-slow-7", "resolve kubeat-slow-7"}},
		{"an outage opened since the start ends", incident(EventUp, "prod-eu", after, after), []string{"resolve kubeat-prod-eu-incident-4"}},
		{"an outage that failed first before the start but opened after it ends", incident(EventUp, "prod-eu", before, after),
			[]string{"resolve kubeat-prod-eu-incident-4"}},
		{"a slowness begun since the start ends", slowness(EventSlowResolved, "prod-eu", after), []string{"resolve kubeat-prod-eu-slow-7"}},
		{"an outage opened before the start is sent again", incident(EventDown, "prod-eu", before, before), []string{"trigger kubeat-prod-eu-incident-4"}},
		{"an outage opened before the start is reminded of", incident(EventReminder, "prod-eu", before, before), []string{"trigger kubeat-prod-eu-incident-4"}},
		{"a slowness begun before the start is sent again", slowness(EventSlow, "prod-eu", before), []string{"trigger kubeat-prod-eu-slow-7"}},
		{"without an instance the two keys are one", incident(EventUp, "", before, before), []string{"resolve kubeat-incident-4"}},
	} {
		from := len(seen())
		if err := sender.Send(ctx, ch, tc.n); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		var got []string
		for _, r := range seen()[from:] {
			got = append(got, r.Body["event_action"].(string)+" "+r.Body["dedup_key"].(string))
			if r.Body["event_action"] == "resolve" && r.Body["payload"] != nil {
				t.Errorf("%s: a resolve with a payload: %v", tc.name, r.Body)
			}
		}
		if strings.Join(got, ", ") != strings.Join(tc.want, ", ") {
			t.Errorf("%s: sent %q, want %q", tc.name, got, tc.want)
		}
	}

	// Without a start time, the one key.
	from := len(seen())
	if err := (&PagerDutySender{Client: srv.Client(), URL: srv.URL}).Send(ctx, ch, incident(EventUp, "prod-eu", before, before)); err != nil {
		t.Fatal(err)
	}
	if got := seen()[from:]; len(got) != 1 || got[0].Body["dedup_key"] != "kubeat-prod-eu-incident-4" {
		t.Errorf("without a start time: %+v", got)
	}

	// The second request failing fails the send, so that the engine sends
	// the end again, under both keys.
	var (
		mu   sync.Mutex
		keys []string
	)
	refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var event map[string]any
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			t.Errorf("body is not JSON: %v", err)
		}
		key, _ := event["dedup_key"].(string)
		mu.Lock()
		keys = append(keys, key)
		mu.Unlock()
		if key == "kubeat-incident-4" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer refusing.Close()
	err := (&PagerDutySender{Client: refusing.Client(), URL: refusing.URL, Started: started}).Send(ctx, ch, incident(EventUp, "prod-eu", before, before))
	mu.Lock()
	defer mu.Unlock()
	if err == nil || !strings.Contains(err.Error(), "PagerDuty answered 500") || strings.Join(keys, ",") != "kubeat-prod-eu-incident-4,kubeat-incident-4" {
		t.Errorf("the second request failed: err = %v, keys %v", err, keys)
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
