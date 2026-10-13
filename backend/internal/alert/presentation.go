package alert

import (
	"net/url"
	"strconv"
	"strings"
)

// Colors of the events, for the channels that show one. Red for an outage,
// green for a recovery, amber for a certificate running out, grey for a
// test. Chosen to stay legible on light and dark chat themes.
const (
	colorDown    = "#d7263d"
	colorUp      = "#1f9d55"
	colorWarning = "#d98014"
	colorNeutral = "#8a8f98"
)

// Color is the hex colour of a notification's event.
func Color(n Notification) string {
	switch n.Event {
	case EventDown, EventReminder:
		return colorDown
	case EventUp, EventCertRenewed:
		return colorUp
	case EventCertExpiring:
		return colorWarning
	default:
		return colorNeutral
	}
}

// ColorInt is Color as the integer Discord wants.
func ColorInt(n Notification) int {
	v := 0
	for _, c := range strings.TrimPrefix(Color(n), "#") {
		digit := strings.IndexRune("0123456789abcdef", c)
		if digit < 0 {
			return 0
		}
		v = v*16 + digit
	}
	return v
}

// Emoji is the sign in front of a notification's text, for channels that
// show the text without it.
func Emoji(n Notification) string {
	switch n.Event {
	case EventDown, EventReminder:
		return "🔴"
	case EventUp, EventCertRenewed:
		return "🟢"
	case EventCertExpiring:
		return "🟡"
	default:
		return "🔵"
	}
}

// URL is where the notification is best looked at in Kubeat: the monitor's
// page, or for a grouped notification the namespace it is about. Empty
// when no public address is configured (UPTIME_PUBLIC_URL), in which case
// the channels leave the link out rather than show a broken one.
func URL(n Notification) string {
	base := strings.TrimSuffix(n.PublicURL, "/")
	if base == "" {
		return ""
	}
	if len(n.Group) > 1 {
		// manualSlug stands for the monitors without a namespace; it
		// matches the frontend's /namespaces/[name] route.
		const manualSlug = "_"
		slug := manualSlug
		if n.Monitor.Namespace != "" {
			slug = url.PathEscape(n.Monitor.Namespace)
		}
		return base + "/namespaces/" + slug
	}
	if n.Monitor.ID == 0 {
		return base
	}
	return base + "/monitors/" + strconv.FormatInt(n.Monitor.ID, 10)
}

// itoaCount writes a small count for the texts that embed one.
func itoaCount(n int) string { return strconv.Itoa(n) }
