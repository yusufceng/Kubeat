package alert

import (
	"net/url"
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
