package store

import (
	"slices"
	"testing"
)

func TestDestination(t *testing.T) {
	web := func(target string, headers map[string]string) Monitor {
		return Monitor{Type: MonitorHTTP, Target: target, HTTP: HTTPOptions{Headers: headers}}
	}
	site := func(host string) map[string]string { return map[string]string{"Host": host, "X-Api-Key": "k"} }
	tcp := func(target string) Monitor { return Monitor{Type: MonitorTCP, Target: target} }

	same := [][2]Monitor{
		{web("https://api.example/x", nil), web("https://API.example:443/y?z=1", nil)},
		{web("http://ingress.svc/healthz", site("api.shop.example")), web("http://ingress.svc/ready", map[string]string{"host": " API.shop.example"})},
		{tcp("Db.shop.svc:5432"), tcp("db.shop.svc:5432")},
	}
	for _, pair := range same {
		if a, b := pair[0].Destination(), pair[1].Destination(); a != b {
			t.Errorf("%q and %q: one server, but %q and %q", pair[0].Target, pair[1].Target, a, b)
		}
	}
	differ := [][2]Monitor{
		{web("https://api.example/", nil), web("http://api.example/", nil)},
		{web("https://api.example/", nil), web("https://api.example:8443/", nil)},
		{web("http://ingress.svc/", site("api.shop.example")), web("http://ingress.svc/", site("capture.example"))},
		{web("http://ingress.svc/", site("api.shop.example")), web("http://ingress.svc/", nil)},
		{tcp("db.shop.svc:5432"), tcp("db.shop.svc:5433")},
		// strings.ToLower would fold the dotted capital I into an ASCII i,
		// but net/http sends these two names to different servers.
		{web("http://bİlling.apps.example/", nil), web("http://billing.apps.example/", nil)},
		{web("http://ingress.svc/", site("bİlling.apps.example")), web("http://ingress.svc/", site("billing.apps.example"))},
		{tcp("bİlling.shop.svc:5432"), tcp("billing.shop.svc:5432")},
	}
	for _, pair := range differ {
		if a, b := pair[0].Destination(), pair[1].Destination(); a == b {
			t.Errorf("%q %v and %q %v: two servers, but both %q", pair[0].Target, pair[0].HTTP.Headers, pair[1].Target, pair[1].HTTP.Headers, a)
		}
	}
}

// From plain HTTP to HTTPS on the same host, and the same site, is the
// same server; any other change, back to plain HTTP among them, is not.
func TestSameServer(t *testing.T) {
	web := func(target string, headers map[string]string) string {
		return Monitor{Type: MonitorHTTP, Target: target, HTTP: HTTPOptions{Headers: headers}}.Destination()
	}
	site := func(host string) map[string]string { return map[string]string{"Host": host} }
	for _, pair := range [][2]string{
		{web("http://api.example/x", nil), web("https://api.example/y", nil)},
		{web("http://API.example:80/", nil), web("https://api.example:443/", nil)},
		{web("http://[fd00::1]/", nil), web("https://[fd00::1]/", nil)},
		{web("http://ingress.svc/", site("api.shop.example")), web("https://ingress.svc/", site("API.shop.example"))},
		{web("https://api.example/", nil), web("https://api.example/z", nil)},
		{Endpoint("http://api.example/"), Endpoint("https://api.example/")},
		{"db.shop.svc:5432", "db.shop.svc:5432"},
	} {
		if !SameServer(pair[0], pair[1]) {
			t.Errorf("%q to %q: the same server, but not taken for it", pair[0], pair[1])
		}
	}
	for _, pair := range [][2]string{
		{web("https://api.example/", nil), web("http://api.example/", nil)},
		{web("http://api.example/", nil), web("https://www.api.example/", nil)},
		{web("http://api.example/", nil), web("https://api.example:8443/", nil)},
		{web("http://api.example:8080/", nil), web("https://api.example/", nil)},
		{web("http://api.example/", nil), web("http://api.example:8080/", nil)},
		{web("http://ingress.svc/", site("api.shop.example")), web("https://ingress.svc/", site("capture.example"))},
		{web("http://ingress.svc/", site("api.shop.example")), web("https://ingress.svc/", nil)},
		{web("http://ingress.svc/", nil), web("https://ingress.svc/", site("api.shop.example"))},
		{web("ws://api.example/", nil), web("https://api.example/", nil)},
		{"db.shop.svc:80", "db.shop.svc:443"},
		{"", "https://api.example:443"},
	} {
		if SameServer(pair[0], pair[1]) {
			t.Errorf("%q to %q: another server, but taken for the same", pair[0], pair[1])
		}
	}
}

func TestDropSecrets(t *testing.T) {
	m := Monitor{Type: MonitorHTTP, HTTP: HTTPOptions{
		Method: "POST", Auth: HTTPAuthBasic, AuthUser: "probe", AuthSecret: "pw", Body: `{"password":"pw"}`, ContentType: "application/json",
		Headers: map[string]string{"X-Api-Key": "k", "Host": "api.shop.example"},
	}}
	want := []string{"http.auth_secret", "http.headers.Host", "http.headers.X-Api-Key"}
	if got := m.SecretNames(); !slices.Equal(got, want) {
		t.Errorf("SecretNames = %v", got)
	}
	// The body is not masked, but it goes to the server all the same.
	if got := m.DropSecrets(); !slices.Equal(got, []string{"http.auth_secret", "http.body", "http.headers.Host", "http.headers.X-Api-Key"}) {
		t.Errorf("DropSecrets = %v", got)
	}
	if m.HTTP.AuthSecret != "" || m.HTTP.Headers != nil || m.HTTP.Body != "" || m.HTTP.ContentType != "" ||
		m.HTTP.Auth != HTTPAuthBasic || m.HTTP.AuthUser != "probe" || m.HTTP.Method != "POST" {
		t.Errorf("after DropSecrets: %+v", m.HTTP)
	}
	if got := m.DropSecrets(); len(got) != 0 {
		t.Errorf("nothing left to drop, but %v", got)
	}
	db := Monitor{Type: MonitorRedis, Database: DatabaseOptions{Password: "pw"}}
	if got := db.DropSecrets(); !slices.Equal(got, []string{"database.password"}) || db.Database.Password != "" {
		t.Errorf("redis: %v, %+v", got, db.Database)
	}
}
