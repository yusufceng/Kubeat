// Package addrmask hides resolved IP addresses in text shown to a user, so
// that an error does not disclose an internal cluster address. Go's network
// errors print the address they dialed (for example "dial tcp 10.0.0.1:8080:
// connect: connection refused"), which for an in-cluster target is the
// resolved pod or service IP. The package is shared by the alert engine,
// which masks a channel's delivery error, and the checker, which masks a
// failed check's error before it is stored; it depends on nothing of either,
// so neither import forms a cycle.
package addrmask

import "regexp"

// resolvedAddr matches an IPv4 address or a bracketed IPv6 address, each with
// an optional port.
var resolvedAddr = regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}(?::\d+)?\b|\[[0-9A-Fa-f:]+\](?::\d+)?`)

// Mask replaces every resolved IP address in msg with a placeholder. The
// error class around it ("connection refused", "i/o timeout") and any host
// name are kept.
func Mask(msg string) string {
	return resolvedAddr.ReplaceAllString(msg, "[address hidden]")
}
