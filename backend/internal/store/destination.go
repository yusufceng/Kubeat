package store

import (
	"net"
	"net/url"
	"slices"
	"strings"
)

// A secret stored with a monitor or a channel goes to the server its check
// or delivery connects to, and only to the one it was entered for: kept for
// another, anyone who may change the monitor, or the cluster object it
// follows, could have it sent to a server of their choosing and read it
// there. Destination and Endpoint say which server that is.

// defaultPorts are the ports a URL's scheme implies when it names none.
var defaultPorts = map[string]string{"http": "80", "https": "443", "ws": "80", "wss": "443"}

// Endpoint is the server a URL talks to: its scheme, host and port, with
// the port the scheme implies when it names none. It is "" for anything
// that is not a URL with a host.
func Endpoint(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	scheme := strings.ToLower(u.Scheme)
	port := u.Port()
	if port == "" {
		port = defaultPorts[scheme]
	}
	return scheme + "://" + net.JoinHostPort(lowerASCII(u.Hostname()), port)
}

// lowerASCII folds the case of ASCII letters only. strings.ToLower folds
// some other letters into ASCII ones (U+0130 'İ' into 'i'), so a name that
// net/http dials and sends in its Punycode form would compare equal to
// another name: "bİlling" is not "billing" to the network.
func lowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

// Destination is the server the monitor's check connects to, and so hands
// the monitor's secrets: the scheme, host and port of a URL, the host and
// port of a host:port target. An HTTP check's Host header is part of it: a
// server that routes by name, an ingress controller or a router, hands the
// request to the site the header names, whatever the URL says. The path is
// not; it is the same server.
func (m Monitor) Destination() string {
	switch m.Type {
	case MonitorHTTP:
		if host := m.HTTP.HostHeader(); host != "" {
			return Endpoint(m.Target) + " (Host: " + lowerASCII(host) + ")"
		}
		return Endpoint(m.Target)
	case MonitorWebSocket:
		return Endpoint(m.Target)
	case MonitorTCP, MonitorGRPC, MonitorPostgres, MonitorMySQL, MonitorRedis:
		if host, port, err := net.SplitHostPort(m.Target); err == nil {
			return net.JoinHostPort(lowerASCII(host), port)
		}
	}
	return m.Target
}

// SameServer reports whether a secret entered for the server from names
// may go to the server to names, both as Destination or Endpoint returns
// them: when they are one, and when to is from on TLS. Plain HTTP on port
// 80 sent on to HTTPS on port 443 of the same host, for the same Host site
// if one is set, is that: a router redirects a site there, an object that
// gains TLS leads there. The connection goes to the same name, with its
// certificate verified unless the monitor skips that, and reaches the site
// that has had the secrets, in the clear, all along. Back to plain HTTP is
// a move.
func SameServer(from, to string) bool {
	if from == to {
		return true
	}
	// An endpoint has no space; Destination puts the Host site after one.
	endpoint, site, _ := strings.Cut(from, " ")
	host, plain := strings.CutPrefix(endpoint, "http://")
	host, onPort80 := strings.CutSuffix(host, ":80")
	if !plain || !onPort80 {
		return false
	}
	secure := "https://" + host + ":443"
	if site != "" {
		secure += " " + site
	}
	return to == secure
}

// HostHeader is the Host header the request is sent with, which the
// checker takes for the request's host; "" when the options set none.
func (o HTTPOptions) HostHeader() string {
	for name, value := range o.Headers {
		if strings.EqualFold(name, "Host") {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// SecretNames names the secrets the monitor holds, as the API calls them:
// http.auth_secret, http.headers.<name>, where every header's value counts,
// and database.password; sorted. These are the values the API masks. The
// request body is not among them: whoever may change the monitor sees it.
func (m Monitor) SecretNames() []string {
	var names []string
	switch {
	case m.Type == MonitorHTTP:
		if m.HTTP.AuthSecret != "" {
			names = append(names, "http.auth_secret")
		}
		for name := range m.HTTP.Headers {
			names = append(names, "http.headers."+name)
		}
	case m.Type.IsDatabase():
		if m.Database.Password != "" {
			names = append(names, "database.password")
		}
	}
	slices.Sort(names)
	return names
}

// DropSecrets clears what the monitor's check would take to a server it
// was not entered for, and returns the names: the secrets SecretNames
// names, and an HTTP check's request body (http.body), which often carries
// a password or a token too. An HTTP check loses its headers; it keeps its
// method, its kind of authentication and the user name, without the
// password or token, for whoever changes the monitor next to enter again.
func (m *Monitor) DropSecrets() []string {
	names := m.SecretNames()
	if m.Type == MonitorHTTP && m.HTTP.Body != "" {
		names = append(names, "http.body")
		slices.Sort(names)
	}
	m.HTTP.Headers, m.HTTP.AuthSecret, m.HTTP.Body, m.HTTP.ContentType, m.Database.Password = nil, "", "", "", ""
	return names
}
