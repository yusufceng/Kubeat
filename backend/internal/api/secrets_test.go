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

// An ingress controller or a router hands a request to the site its Host
// header names: changing the header moves the monitor to another server as
// much as changing the URL's host does, and so needs the secrets again.
func TestHostHeaderIsPartOfTheServer(t *testing.T) {
	srv, db := newTestAPI(t)
	base := srv.URL + "/api/v1/monitors"
	ctx := context.Background()
	addUser(t, srv.URL, "editor", store.RoleEditor, "shop")
	editor, _ := signIn(t, srv.URL, "editor", "parola-123")

	var routed, direct monitorResponse
	do(t, "POST", base, monitorJSON("routed", "http", "http://ingress-nginx-controller.ingress-nginx.svc/healthz",
		`"namespace":"shop","http":{"auth":"bearer","auth_secret":"ADMIN-TOKEN","headers":{"Host":"api.shop.example","X-Api-Key":"ADMIN-KEY"}}`), &routed)
	do(t, "POST", base, monitorJSON("direct", "http", "http://ingress-nginx-controller.ingress-nginx.svc/healthz",
		`"namespace":"shop","http":{"headers":{"X-Api-Key":"ADMIN-KEY"}}`), &direct)
	patch := func(id int64, target, headers string) (int, string) {
		t.Helper()
		var problem errorBody
		code := doAsError(t, editor, "PATCH", base+"/"+itoa(id),
			`{"target":"`+target+`","http":{"auth":"bearer","auth_secret":"`+maskedValue+`","headers":{`+headers+`}}}`, &problem)
		return code, problem.Error
	}
	healthz, ready := "http://ingress-nginx-controller.ingress-nginx.svc/healthz", "http://ingress-nginx-controller.ingress-nginx.svc/ready"
	masked := `"X-Api-Key":"` + maskedValue + `"`
	again := "enter http.auth_secret, http.headers.X-Api-Key again: stored secrets go only to the server they were entered for"

	for name, headers := range map[string]string{
		"another site":      `"Host":"capture.attacker.example",` + masked,
		"the site left out": masked,
	} {
		if code, problem := patch(routed.ID, healthz, headers); code != http.StatusBadRequest || problem != again {
			t.Errorf("%s: status = %d, %q", name, code, problem)
		}
	}
	if stored, _ := db.GetMonitor(ctx, routed.ID); stored.HTTP.Headers["Host"] != "api.shop.example" ||
		stored.HTTP.Headers["X-Api-Key"] != "ADMIN-KEY" || stored.HTTP.AuthSecret != "ADMIN-TOKEN" {
		t.Errorf("routed: stored = %+v", stored.HTTP)
	}
	// The same site, kept or however it is written, on another path too.
	for _, tc := range []struct{ target, headers string }{
		{healthz, `"Host":"` + maskedValue + `",` + masked},
		{ready, `"Host":"` + maskedValue + `",` + masked},
		{healthz, `"Host":"API.Shop.Example",` + masked},
	} {
		if code, problem := patch(routed.ID, tc.target, tc.headers); code != http.StatusOK {
			t.Errorf("the same site, %s %s: status = %d, %q", tc.target, tc.headers, code, problem)
		}
	}
	// A Host header added to a monitor that had none names another site.
	var problem errorBody
	if code := doAsError(t, editor, "PATCH", base+"/"+itoa(direct.ID),
		`{"http":{"headers":{"Host":"capture.attacker.example",`+masked+`}}}`, &problem); code != http.StatusBadRequest ||
		problem.Error != "enter http.headers.X-Api-Key again: stored secrets go only to the server they were entered for" {
		t.Errorf("a Host header added: status = %d, %q", code, problem.Error)
	}
	if stored, _ := db.GetMonitor(ctx, direct.ID); stored.HTTP.HostHeader() != "" || stored.HTTP.Headers["X-Api-Key"] != "ADMIN-KEY" {
		t.Errorf("direct: stored = %+v", stored.HTTP)
	}

	// The Host header is stored as it is sent, and as the destination
	// counts it: trimmed. net/http would send a padded one empty, to the
	// site a router keeps for requests without a name, and one that names
	// no host would go out the same way.
	if code, problem := patch(routed.ID, healthz, `"Host":"api.shop.example ",`+masked); code != http.StatusOK {
		t.Errorf("a padded Host: status = %d, %q", code, problem)
	}
	if stored, _ := db.GetMonitor(ctx, routed.ID); stored.HTTP.Headers["Host"] != "api.shop.example" ||
		stored.HTTP.Headers["X-Api-Key"] != "ADMIN-KEY" || stored.HTTP.AuthSecret != "ADMIN-TOKEN" {
		t.Errorf("a padded Host: stored = %+v", stored.HTTP)
	}
	notAHost := "http.headers: the value of Host must be a host name or an IP address, with an optional port"
	problem = errorBody{}
	if code := doAsError(t, editor, "PATCH", base+"/"+itoa(direct.ID), `{"http":{"headers":{"Host":" ",`+masked+`}}}`, &problem); code != http.StatusBadRequest ||
		problem.Error != notAHost {
		t.Errorf("a blank Host: status = %d, %q", code, problem.Error)
	}
	if stored, _ := db.GetMonitor(ctx, direct.ID); len(stored.HTTP.Headers) != 1 || stored.HTTP.Headers["X-Api-Key"] != "ADMIN-KEY" {
		t.Errorf("a blank Host: stored = %+v", stored.HTTP)
	}
	for value, valid := range map[string]bool{
		"api.shop.example": true, "api.shop.example:8443": true, "10.0.0.5": true, "[fd00::1]:8080": true, "[fd00::1]": true,
		"": false, "a b": false, "api.shop.example:": false, "api.shop.example:99999": false, "fd00::1": false,
		"[api.shop.example]": false, "[10.0.0.5]:80": false, "api.shop.example/x": false, ":80": false,
	} {
		if got := validHostHeader(value); got != valid {
			t.Errorf("validHostHeader(%q) = %v", value, got)
		}
	}
}

// Plain HTTP on port 80 moved to HTTPS on port 443 of the same host, for
// the same site, is the same server: the secrets go there as they did,
// over TLS now. Back to plain HTTP, or on to another port, is a move.
func TestTLSOnTheSameHostKeepsTheSecrets(t *testing.T) {
	srv, db := newTestAPI(t)
	base := srv.URL + "/api/v1/monitors"
	ctx := context.Background()
	addUser(t, srv.URL, "editor", store.RoleEditor, "shop")
	editor, _ := signIn(t, srv.URL, "editor", "parola-123")

	var status, routed monitorResponse
	do(t, "POST", base, monitorJSON("status", "http", "http://status.internal.example/health",
		`"namespace":"shop","http":{"auth":"bearer","auth_secret":"ADMIN-TOKEN","headers":{"X-Api-Key":"ADMIN-KEY"}}`), &status)
	do(t, "POST", base, monitorJSON("routed", "http", "http://ingress.shop.svc/healthz",
		`"namespace":"shop","http":{"auth":"bearer","auth_secret":"ADMIN-TOKEN","headers":{"Host":"api.shop.example","X-Api-Key":"ADMIN-KEY"}}`), &routed)
	masked := func(target, host string) string {
		headers := `"X-Api-Key":"` + maskedValue + `"`
		if host != "" {
			headers += `,"Host":"` + host + `"`
		}
		return `{"target":"` + target + `","http":{"auth":"bearer","auth_secret":"` + maskedValue + `","headers":{` + headers + `}}}`
	}
	again := "enter http.auth_secret, http.headers.X-Api-Key again: stored secrets go only to the server they were entered for"

	// In this order: each step starts from what the one before it stored.
	for _, tc := range []struct {
		name    string
		id      int64
		patch   string
		refused string
		target  string
	}{
		{"another port over TLS", status.ID, masked("https://status.internal.example:8443/health", ""), again, "http://status.internal.example/health"},
		{"TLS on the same host", status.ID, masked("https://status.internal.example/health", ""), "", "https://status.internal.example/health"},
		{"back to plain HTTP", status.ID, masked("http://status.internal.example/health", ""), again, "https://status.internal.example/health"},
		{"TLS for another site", routed.ID, masked("https://ingress.shop.svc/healthz", "capture.attacker.example"), again, "http://ingress.shop.svc/healthz"},
		{"TLS for the same site", routed.ID, masked("https://ingress.shop.svc/healthz", maskedValue), "", "https://ingress.shop.svc/healthz"},
	} {
		var problem errorBody
		code := doAsError(t, editor, "PATCH", base+"/"+itoa(tc.id), tc.patch, &problem)
		if (tc.refused == "" && code != http.StatusOK) || (tc.refused != "" && (code != http.StatusBadRequest || problem.Error != tc.refused)) {
			t.Errorf("%s: status = %d, %q", tc.name, code, problem.Error)
		}
		if stored, _ := db.GetMonitor(ctx, tc.id); stored.Target != tc.target || stored.HTTP.AuthSecret != "ADMIN-TOKEN" || stored.HTTP.Headers["X-Api-Key"] != "ADMIN-KEY" {
			t.Errorf("%s: stored = %s %+v", tc.name, stored.Target, stored.HTTP)
		}
	}
	// The options left out keep the secrets as they are, for TLS too.
	var plain monitorResponse
	do(t, "POST", base, monitorJSON("plain", "http", "http://plain.internal.example/", `"namespace":"shop","http":{"headers":{"X-Api-Key":"ADMIN-KEY"}}`), &plain)
	if code := doAs(t, editor, "PATCH", base+"/"+itoa(plain.ID), `{"target":"https://plain.internal.example/"}`, nil); code != http.StatusOK {
		t.Errorf("options left out: status = %d", code)
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
