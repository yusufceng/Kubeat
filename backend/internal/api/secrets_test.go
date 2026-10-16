package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// A stored secret goes only to the server it was entered for: an update
// that points the monitor at another server must enter it again, whoever
// sends it, or the secret could be sent to a server of the editor's choosing.
func TestMonitorSecretsStayWithTheirServer(t *testing.T) {
	srv, db := newTestAPI(t)
	base := srv.URL + "/api/v1/monitors"
	ctx := context.Background()
	addUser(t, srv.URL, "editor", store.RoleEditor, "shop")
	editor, _ := signIn(t, srv.URL, "editor", "parola-123")

	var billing monitorResponse
	body := monitorJSON("billing-api", "http", "https://billing.internal.example/health",
		`"namespace":"shop","http":{"auth":"bearer","auth_secret":"ADMIN-TOKEN","headers":{"X-Api-Key":"ADMIN-KEY"}}`)
	if code := do(t, "POST", base, body, &billing); code != http.StatusCreated {
		t.Fatalf("create: status = %d", code)
	}
	url := base + "/" + itoa(billing.ID)
	masked := func(target string) string {
		return `{"target":"` + target + `","http":{"auth":"bearer","auth_secret":"` + maskedValue + `","headers":{"X-Api-Key":"` + maskedValue + `"}}}`
	}
	refused := func(name, token, patch, want string) {
		t.Helper()
		var problem errorBody
		if code := doAsError(t, token, "PATCH", url, patch, &problem); code != http.StatusBadRequest || problem.Error != want {
			t.Errorf("%s: status = %d, error %q, want 400 %q", name, code, problem.Error, want)
		}
		if stored, _ := db.GetMonitor(ctx, billing.ID); stored.Target != "https://billing.internal.example/health" ||
			stored.HTTP.AuthSecret != "ADMIN-TOKEN" || stored.HTTP.Headers["X-Api-Key"] != "ADMIN-KEY" {
			t.Errorf("%s: stored = %s %+v", name, stored.Target, stored.HTTP)
		}
	}
	both := "enter http.auth_secret, http.headers.X-Api-Key again: stored secrets go only to the server they were entered for"

	refused("another host, options left out", editor, `{"target":"https://attacker.example/x"}`, both)
	refused("another host, secrets masked", editor, masked("https://attacker.example/x"), both)
	refused("another port", editor, masked("https://billing.internal.example:8443/health"), both)
	refused("another scheme", editor, masked("http://billing.internal.example/health"), both)
	refused("an administrator too", testToken, masked("https://attacker.example/x"), both)
	refused("one of them entered", editor,
		`{"target":"https://attacker.example/x","http":{"auth":"bearer","auth_secret":"new","headers":{"X-Api-Key":"`+maskedValue+`"}}}`,
		"enter http.headers.X-Api-Key again: stored secrets go only to the server they were entered for")

	// The same server, however it is written, keeps them.
	for _, target := range []string{"https://billing.internal.example/ready", "https://Billing.Internal.Example:443/health"} {
		if code := doAs(t, editor, "PATCH", url, masked(target), nil); code != http.StatusOK {
			t.Errorf("same server as %s: status = %d", target, code)
		}
		if stored, _ := db.GetMonitor(ctx, billing.ID); stored.HTTP.AuthSecret != "ADMIN-TOKEN" || stored.HTTP.Headers["X-Api-Key"] != "ADMIN-KEY" {
			t.Errorf("same server as %s: stored = %+v", target, stored.HTTP)
		}
	}
	// Another server with the secrets entered anew, or without any.
	moved := `{"target":"https://billing2.internal.example/health","http":{"auth":"bearer","auth_secret":"t2","headers":{"X-Api-Key":"k2"}}}`
	if code := doAs(t, editor, "PATCH", url, moved, nil); code != http.StatusOK {
		t.Errorf("moved with new secrets: status = %d", code)
	}
	if stored, _ := db.GetMonitor(ctx, billing.ID); stored.HTTP.AuthSecret != "t2" || stored.HTTP.Headers["X-Api-Key"] != "k2" {
		t.Errorf("after the move: %+v", stored.HTTP)
	}
	if code := doAs(t, editor, "PATCH", url, `{"target":"https://public.example/","http":{"method":"GET"}}`, nil); code != http.StatusOK {
		t.Errorf("moved without secrets: status = %d", code)
	}
	if stored, _ := db.GetMonitor(ctx, billing.ID); stored.HTTP.AuthSecret != "" || len(stored.HTTP.Headers) != 0 {
		t.Errorf("after the move without secrets: %+v", stored.HTTP)
	}

	// A copy keeps the secrets for the original's server, not for another.
	var withSecret, copied monitorResponse
	do(t, "POST", base, monitorJSON("ledger", "http", "https://ledger.internal.example/", `"namespace":"shop","http":{"auth":"basic","auth_user":"probe","auth_secret":"pw"}`), &withSecret)
	if code := doAs(t, editor, "POST", base+"/"+itoa(withSecret.ID)+"/clone", `{"name":"ledger-2"}`, &copied); code != http.StatusCreated {
		t.Fatalf("clone: status = %d", code)
	}
	var problem errorBody
	if code := doAsError(t, editor, "PATCH", base+"/"+itoa(copied.ID), `{"target":"https://attacker.example/"}`, &problem); code != http.StatusBadRequest {
		t.Errorf("retargeting the copy: status = %d, %q", code, problem.Error)
	}

	// Database passwords follow the same rule, by host and port.
	var cache monitorResponse
	do(t, "POST", base, monitorJSON("cache", "redis", "redis.shop.svc:6379", `"namespace":"shop","database":{"password":"REDIS-PW"}`), &cache)
	cacheURL := base + "/" + itoa(cache.ID)
	password := "enter database.password again: stored secrets go only to the server they were entered for"
	for name, patch := range map[string]string{
		"options left out": `{"target":"127.0.0.1:6379"}`,
		"password masked":  `{"target":"redis.shop.svc:6380","database":{"password":"` + maskedValue + `"}}`,
	} {
		problem = errorBody{}
		if code := doAsError(t, editor, "PATCH", cacheURL, patch, &problem); code != http.StatusBadRequest || problem.Error != password {
			t.Errorf("redis, %s: status = %d, %q", name, code, problem.Error)
		}
	}
	if code := doAs(t, editor, "PATCH", cacheURL, `{"target":"REDIS.shop.svc:6379","interval_seconds":120}`, nil); code != http.StatusOK {
		t.Errorf("redis, same server: status = %d", code)
	}
	if code := doAs(t, editor, "PATCH", cacheURL, `{"target":"redis-2.shop.svc:6379","database":{"password":"new-pw"}}`, nil); code != http.StatusOK {
		t.Errorf("redis, moved with a new password: status = %d", code)
	}
	if stored, _ := db.GetMonitor(ctx, cache.ID); stored.Database.Password != "new-pw" || stored.Target != "redis-2.shop.svc:6379" {
		t.Errorf("redis after the move: %s %+v", stored.Target, stored.Database)
	}

	// Nothing secret, nothing to enter again.
	var port monitorResponse
	do(t, "POST", base, monitorJSON("port", "tcp", "db.shop.svc:5432", `"namespace":"shop"`), &port)
	if code := doAs(t, editor, "PATCH", base+"/"+itoa(port.ID), `{"target":"db.billing.svc:5432"}`, nil); code != http.StatusOK {
		t.Errorf("tcp monitor moved: status = %d", code)
	}
}

// A channel's token goes only to the server it was entered for, as a
// monitor's secrets do.
func TestChannelSecretsStayWithTheirServer(t *testing.T) {
	srv, db := newTestAPI(t)
	base := srv.URL + "/api/v1/channels"
	ctx := context.Background()
	addUser(t, srv.URL, "editor", store.RoleEditor, "shop")
	editor, _ := signIn(t, srv.URL, "editor", "parola-123")

	var gotify, ntfy, hook channelResponse
	do(t, "POST", base, `{"name":"gotify","type":"gotify","target":"https://gotify.internal.example","secret":"GOTIFY-TOKEN","namespaces":["shop"]}`, &gotify)
	do(t, "POST", base, `{"name":"ntfy","type":"ntfy","target":"https://ntfy.internal.example/alerts","secret":"NTFY-TOKEN","namespaces":["shop"]}`, &ntfy)
	do(t, "POST", base, `{"name":"hook","type":"webhook","target":"https://hooks.internal.example/in?token=WEBHOOK","namespaces":["shop"]}`, &hook)
	again := "enter secret again: a stored secret goes only to the server it was entered for"

	for name, tc := range map[string]struct {
		id     int64
		patch  string
		secret string
	}{
		"gotify, secret left out":      {gotify.ID, `{"target":"https://attacker.example"}`, "GOTIFY-TOKEN"},
		"gotify, secret masked":        {gotify.ID, `{"target":"https://attacker.example","secret":"` + maskedValue + `"}`, "GOTIFY-TOKEN"},
		"gotify, another port":         {gotify.ID, `{"target":"https://gotify.internal.example:8443","secret":"` + maskedValue + `"}`, "GOTIFY-TOKEN"},
		"ntfy, another server's topic": {ntfy.ID, `{"target":"https://attacker.example/alerts"}`, "NTFY-TOKEN"},
	} {
		var problem errorBody
		if code := doAsError(t, editor, "PATCH", base+"/"+itoa(tc.id), tc.patch, &problem); code != http.StatusBadRequest || problem.Error != again {
			t.Errorf("%s: status = %d, error %q", name, code, problem.Error)
		}
		if stored, _ := db.GetChannel(ctx, tc.id); stored.Secret != tc.secret {
			t.Errorf("%s: stored secret = %q", name, stored.Secret)
		}
	}

	// In this order: each step starts from what the one before it stored.
	for _, tc := range []struct {
		name          string
		id            int64
		patch         string
		target, token string
	}{
		{"gotify, as shown", gotify.ID, `{"target":"https://gotify.internal.example/…","secret":"` + maskedValue + `"}`, "https://gotify.internal.example", "GOTIFY-TOKEN"},
		{"gotify, moved with a token", gotify.ID, `{"target":"https://gotify2.internal.example","secret":"NEW-TOKEN"}`, "https://gotify2.internal.example", "NEW-TOKEN"},
		{"ntfy, another topic", ntfy.ID, `{"target":"https://ntfy.internal.example/deploys","secret":"` + maskedValue + `"}`, "https://ntfy.internal.example/deploys", "NTFY-TOKEN"},
		{"webhook, no secret to keep", hook.ID, `{"target":"https://other.example/in"}`, "https://other.example/in", ""},
	} {
		if code := doAs(t, editor, "PATCH", base+"/"+itoa(tc.id), tc.patch, nil); code != http.StatusOK {
			t.Errorf("%s: status = %d", tc.name, code)
		}
		if stored, _ := db.GetChannel(ctx, tc.id); stored.Target != tc.target || stored.Secret != tc.token {
			t.Errorf("%s: stored = %q %q", tc.name, stored.Target, stored.Secret)
		}
	}
}

// doAsError is doAs for a request that is to fail: it decodes the error
// answer into problem.
func doAsError(t *testing.T, token, method, url, body string, problem *errorBody) int {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		if err := json.NewDecoder(resp.Body).Decode(problem); err != nil {
			t.Fatalf("%s %s: decode: %v", method, url, err)
		}
	}
	return resp.StatusCode
}
