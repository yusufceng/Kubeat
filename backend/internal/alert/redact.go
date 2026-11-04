package alert

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// SecretMask stands in for a secret wherever one would be shown: in a
// redacted error, and in the API's answers.
const SecretMask = "********"

// MaskURL keeps the scheme and host of an address and hides its path,
// query and user info: for most providers the address is the credential,
// and anyone who can read it can post to the channel.
func MaskURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "…"
	}
	return u.Scheme + "://" + u.Host + "/…"
}

// Redact removes the channel's address and secret from an error about it,
// should anything have put them there. The API redacts what it shows of a
// channel's last error with it, and the engine what it writes to the
// delivery log: there the channel may be changed or gone by the time
// anyone reads the entry, so it is redacted while the channel is at hand.
func Redact(msg string, ch store.Channel) string {
	if strings.Contains(ch.Target, "://") {
		msg = strings.ReplaceAll(msg, ch.Target, MaskURL(ch.Target))
	}
	if ch.Secret != "" {
		msg = strings.ReplaceAll(msg, ch.Secret, SecretMask)
	}
	return msg
}

// resolvedAddr matches an IPv4 address or a bracketed IPv6 address, each
// with an optional port. Go's network errors print the address they dialed
// (for example "dial tcp 10.0.0.1:8080: connect: connection refused"), which
// for an in-cluster target is the resolved pod or service IP.
var resolvedAddr = regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}(?::\d+)?\b|\[[0-9A-Fa-f:]+\](?::\d+)?`)

// MaskAddrs hides resolved IP addresses in a message, so that an error shown
// to a user does not disclose an internal cluster address. The error class
// around it ("connection refused", "i/o timeout") is kept.
func MaskAddrs(msg string) string {
	return resolvedAddr.ReplaceAllString(msg, "[address hidden]")
}
