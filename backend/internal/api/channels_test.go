package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChannelProviders(t *testing.T) {
	srv, db := newTestAPI(t)
	base := srv.URL + "/api/v1/channels"
	ctx := context.Background()

	create := func(body string) channelResponse {
		t.Helper()
		var c channelResponse
		if code := do(t, "POST", base, body, &c); code != http.StatusCreated {
			t.Fatalf("create %s: status = %d", body, code)
		}
		return c
	}

	// Chat tools: the URL is the credential and comes back masked.
	for _, typ := range []string{"discord", "mattermost", "googlechat"} {
		c := create(`{"name":"` + typ + `","type":"` + typ + `","target":"https://chat.example.com/hooks/s3cret","secret":"ignored"}`)
		if c.Target != "https://chat.example.com/…" || c.HasSecret || len(c.Namespaces) != 0 || !c.Enabled {
			t.Errorf("%s = %+v", typ, c)
		}
	}

	telegram := create(`{"name":"tg","type":"telegram","target":" -1001234567890 ","secret":"123456:AbC-dEf_1","namespaces":["shop","billing","shop"]}`)
	if telegram.Target != "-1001234567890" || !telegram.HasSecret || strings.Join(telegram.Namespaces, ",") != "billing,shop" {
		t.Errorf("telegram = %+v", telegram)
	}
	if stored, _ := db.GetChannel(ctx, telegram.ID); stored.Secret != "123456:AbC-dEf_1" {
		t.Errorf("stored secret = %q", stored.Secret)
	}
	create(`{"name":"tg-public","type":"telegram","target":"@kubeat_alarm","secret":"1:a"}`)

	pager := create(`{"name":"pd","type":"pagerduty","target":"ignored","secret":"R0UT1NGKEY"}`)
	if pager.Target != "" || !pager.HasSecret {
		t.Errorf("pagerduty = %+v", pager)
	}
	ntfy := create(`{"name":"ntfy","type":"ntfy","target":"https://ntfy.sh/kubeat-alarm"}`)
	if ntfy.Target != "https://ntfy.sh/…" || ntfy.HasSecret {
		t.Errorf("ntfy = %+v", ntfy)
	}
	if gotify := create(`{"name":"gotify","type":"gotify","target":"https://gotify.example.com","secret":"AppT0ken"}`); !gotify.HasSecret {
		t.Errorf("gotify = %+v", gotify)
	}

	// Nothing in a listing lets one post to a channel.
	listing := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", base, nil)
	srv.Config.Handler.ServeHTTP(listing, req)
	for _, secret := range []string{"s3cret", "123456:AbC-dEf_1", "R0UT1NGKEY", "kubeat-alarm", "AppT0ken"} {
		if strings.Contains(listing.Body.String(), secret) {
			t.Errorf("the channel list contains %q", secret)
		}
	}

	invalid := map[string]string{
		"unknown type":            `{"name":"x","type":"pigeon","target":"https://x"}`,
		"discord without a URL":   `{"name":"x","type":"discord","target":"chat"}`,
		"telegram chat":           `{"name":"x","type":"telegram","target":"my chat","secret":"1:a"}`,
		"telegram without token":  `{"name":"x","type":"telegram","target":"-100"}`,
		"telegram token":          `{"name":"x","type":"telegram","target":"-100","secret":"not-a-token"}`,
		"pagerduty without a key": `{"name":"x","type":"pagerduty"}`,
		"ntfy without a topic":    `{"name":"x","type":"ntfy","target":"https://ntfy.sh/"}`,
		"gotify without a token":  `{"name":"x","type":"gotify","target":"https://gotify.example.com"}`,
		"secret with a space":     `{"name":"x","type":"gotify","target":"https://g","secret":"a b"}`,
		"namespace name":          `{"name":"x","type":"discord","target":"https://x","namespaces":["shop/team"]}`,
		"empty namespace":         `{"name":"x","type":"discord","target":"https://x","namespaces":[""]}`,
	}
	for name, body := range invalid {
		if code := do(t, "POST", base, body, nil); code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, code)
		}
	}
}

func TestUpdateChannel(t *testing.T) {
	srv, db := newTestAPI(t)
	base := srv.URL + "/api/v1/channels"
	ctx := context.Background()

	var tg, hook channelResponse
	do(t, "POST", base, `{"name":"tg","type":"telegram","target":"-100","secret":"1:first"}`, &tg)
	do(t, "POST", base, `{"name":"hook","type":"webhook","target":"https://hooks.example.com/first"}`, &hook)
	patch := func(id int64, body string, out any) int {
		t.Helper()
		return do(t, "PATCH", base+"/"+itoa(id), body, out)
	}

	// Pausing a channel changes nothing else.
	var got channelResponse
	if code := patch(tg.ID, `{"enabled":false}`, &got); code != http.StatusOK || got.Enabled || got.Target != "-100" || !got.HasSecret {
		t.Errorf("pause: status = %d, channel = %+v", code, got)
	}
	// What was shown, sent back, keeps what is stored.
	body := `{"name":"hook-2","target":"` + hook.Target + `","namespaces":["shop"]}`
	if code := patch(hook.ID, body, &got); code != http.StatusOK || got.Name != "hook-2" || strings.Join(got.Namespaces, ",") != "shop" {
		t.Errorf("rename: status = %d, channel = %+v", code, got)
	}
	if stored, _ := db.GetChannel(ctx, hook.ID); stored.Target != "https://hooks.example.com/first" {
		t.Errorf("target after a masked update = %q", stored.Target)
	}
	if code := patch(tg.ID, `{"target":"-200","secret":"`+maskedValue+`"}`, &got); code != http.StatusOK || got.Target != "-200" {
		t.Errorf("new chat, same token: status = %d, channel = %+v", code, got)
	}
	if stored, _ := db.GetChannel(ctx, tg.ID); stored.Secret != "1:first" || stored.Target != "-200" {
		t.Errorf("stored = %+v", stored)
	}
	// New values replace the old ones, and an empty scope means everything again.
	patch(hook.ID, `{"target":"https://hooks.example.com/second","namespaces":[]}`, &got)
	patch(tg.ID, `{"secret":"2:second"}`, nil)
	if stored, _ := db.GetChannel(ctx, hook.ID); stored.Target != "https://hooks.example.com/second" || len(stored.Namespaces) != 0 {
		t.Errorf("stored hook = %+v", stored)
	}
	if stored, _ := db.GetChannel(ctx, tg.ID); stored.Secret != "2:second" {
		t.Errorf("stored token = %q", stored.Secret)
	}

	for name, tc := range map[string]struct {
		id   int64
		body string
		want int
	}{
		"empty name":     {tg.ID, `{"name":" "}`, http.StatusBadRequest},
		"bad target":     {tg.ID, `{"target":"a chat"}`, http.StatusBadRequest},
		"token removed":  {tg.ID, `{"secret":""}`, http.StatusBadRequest},
		"name taken":     {tg.ID, `{"name":"hook-2"}`, http.StatusConflict},
		"type change":    {tg.ID, `{"type":"slack"}`, http.StatusBadRequest},
		"unknown":        {999, `{"enabled":true}`, http.StatusNotFound},
		"bad namespaces": {tg.ID, `{"namespaces":["a,b"]}`, http.StatusBadRequest},
	} {
		if code := patch(tc.id, tc.body, nil); code != tc.want {
			t.Errorf("%s: status = %d, want %d", name, code, tc.want)
		}
	}
}

// A failed test says what the provider said, without the token.
func TestChannelTestReportsTheProvidersAnswer(t *testing.T) {
	srv, db := newTestAPI(t)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		if r.Header.Get("X-Gotify-Key") != "right" {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"errorDescription": "you need to provide a valid access token"})
			return
		}
		w.Write([]byte(`{"id":1}`))
	}))
	defer provider.Close()

	var ch channelResponse
	do(t, "POST", srv.URL+"/api/v1/channels", `{"name":"g","type":"gotify","target":"`+provider.URL+`","secret":"wrong-t0ken"}`, &ch)
	var result testChannelResponse
	do(t, "POST", srv.URL+"/api/v1/channels/1/test", "", &result)
	if result.Delivered || !strings.Contains(result.Error, "Gotify answered 401") || strings.Contains(result.Error, "wrong-t0ken") {
		t.Errorf("failed test = %+v", result)
	}

	do(t, "PATCH", srv.URL+"/api/v1/channels/1", `{"secret":"right"}`, nil)
	result = testChannelResponse{}
	if do(t, "POST", srv.URL+"/api/v1/channels/1/test", "", &result); !result.Delivered || result.Error != "" {
		t.Errorf("test after fixing the token = %+v", result)
	}
	if stored, _ := db.GetChannel(context.Background(), 1); stored.Secret != "right" {
		t.Errorf("stored = %+v", stored)
	}
}
