// Package checker performs a single uptime check of a monitor.
package checker

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// maxDetail bounds the text a check stores about what it found.
const maxDetail = 300

// Pushes gives the latest report of a push monitor.
type Pushes interface {
	// LastPush returns store.ErrNotFound when the monitor never reported.
	LastPush(ctx context.Context, monitorID int64) (store.PushReport, error)
}

// Options configures a Checker.
type Options struct {
	// InsecureSkipVerify disables TLS certificate verification for every
	// check; monitors can also opt out individually.
	InsecureSkipVerify bool
	UserAgent          string
	// BodyLimit is how much of a response body is read to look for a
	// monitor's keyword or JSON value.
	BodyLimit int64
	// Pushes is required for push monitors.
	Pushes Pushes
	// Workloads is required for workload monitors; nil without a cluster.
	Workloads Workloads
}

// Checker runs the checks of every monitor type. It is safe for concurrent
// use.
type Checker struct {
	verifying *http.Transport
	insecure  *http.Transport
	dialer    *net.Dialer
	opts      Options
}

// New returns a Checker. Timeouts are not set here; every check is bounded
// by the context passed to Check.
func New(opts Options) *Checker {
	transport := func(skipVerify bool) *http.Transport {
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.TLSClientConfig = &tls.Config{InsecureSkipVerify: skipVerify}
		return t
	}
	return &Checker{
		verifying: transport(opts.InsecureSkipVerify),
		insecure:  transport(true),
		dialer:    &net.Dialer{},
		opts:      opts,
	}
}

// outcome is what one check found, before it is turned into a result.
type outcome struct {
	statusCode int
	certExpiry time.Time
	// detail describes a success, where there is something to say.
	detail string
	// latency replaces the measured duration of the check when the check
	// knows better: a round-trip time, a duration reported by a push.
	latency time.Duration
	hasTime bool
	err     error
}

func failed(err error) outcome { return outcome{err: err} }

func failedf(format string, args ...any) outcome { return outcome{err: fmt.Errorf(format, args...)} }

// Check runs one check of m and reports the outcome. A failed check is not
// an error: it is a result with Up == false and Error set.
func (c *Checker) Check(ctx context.Context, m store.Monitor) store.CheckResult {
	start := time.Now()
	result := store.CheckResult{MonitorID: m.ID, CheckedAt: start.UTC().Truncate(time.Millisecond)}

	var out outcome
	switch m.Type {
	case store.MonitorHTTP:
		out = c.checkHTTP(ctx, m)
	case store.MonitorTCP:
		out = c.checkTCP(ctx, m)
	case store.MonitorPing:
		out = c.checkPing(ctx, m.Target)
	case store.MonitorDNS:
		out = c.checkDNS(ctx, m)
	case store.MonitorPush:
		out = c.checkPush(ctx, m, start)
	case store.MonitorGRPC:
		out = c.checkGRPC(ctx, m)
	case store.MonitorWebSocket:
		out = c.checkWebSocket(ctx, m)
	case store.MonitorPostgres:
		out = c.checkPostgres(ctx, m)
	case store.MonitorMySQL:
		out = c.checkMySQL(ctx, m)
	case store.MonitorRedis:
		out = c.checkRedis(ctx, m)
	case store.MonitorWorkload:
		out = c.checkWorkload(ctx, m)
	default:
		out = failedf("unsupported monitor type %q", m.Type)
	}

	result.Latency = time.Since(start)
	if out.hasTime {
		result.Latency = out.latency
	}
	result.StatusCode = out.statusCode
	result.CertExpiresAt = out.certExpiry
	result.Up = out.err == nil
	if out.err != nil {
		result.Error = out.err.Error()
	} else {
		result.Detail = truncate(out.detail, maxDetail)
	}

	if m.Inverted {
		invert(&result)
	}
	return result
}

// invert turns the outcome around for a monitor that expects its target to
// fail. A check that could not run at all still counts as a failure.
func invert(r *store.CheckResult) {
	if r.Up {
		r.Up = false
		r.Error = "the target responded, but this monitor expects it to fail"
		if r.Detail != "" {
			r.Error += ": " + r.Detail
		}
		r.Detail = ""
		return
	}
	r.Up = true
	r.Detail = truncate("failed as expected: "+r.Error, maxDetail)
	r.Error = ""
}

// truncate cuts s to at most max bytes without splitting a character.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && s[cut]&0xC0 == 0x80 { // a continuation byte of UTF-8
		cut--
	}
	return s[:cut] + "…"
}

// tlsConfig returns the client configuration for a check that speaks TLS
// outside of the shared HTTP transports.
func (c *Checker) tlsConfig(serverName string, skipVerify bool) *tls.Config {
	return &tls.Config{ServerName: serverName, InsecureSkipVerify: skipVerify || c.opts.InsecureSkipVerify}
}

// leafExpiry returns when the certificate the peer presented expires.
func leafExpiry(state *tls.ConnectionState) time.Time {
	if state == nil || len(state.PeerCertificates) == 0 {
		return time.Time{}
	}
	return state.PeerCertificates[0].NotAfter.UTC()
}

func (c *Checker) checkTCP(ctx context.Context, m store.Monitor) outcome {
	conn, err := c.dialer.DialContext(ctx, "tcp", m.Target)
	if err != nil {
		return failed(err)
	}
	defer conn.Close()
	if !m.TCP.TLS {
		return outcome{}
	}

	host, _, err := net.SplitHostPort(m.Target)
	if err != nil {
		return failed(err)
	}
	secured := tls.Client(conn, c.tlsConfig(host, m.TCP.SkipTLSVerify))
	if err := secured.HandshakeContext(ctx); err != nil {
		return failedf("tls handshake: %w", err)
	}
	state := secured.ConnectionState()
	return outcome{certExpiry: leafExpiry(&state), detail: tls.VersionName(state.Version)}
}

func (c *Checker) checkPush(ctx context.Context, m store.Monitor, now time.Time) outcome {
	if c.opts.Pushes == nil {
		return failedf("push monitors are not available")
	}
	report, err := c.opts.Pushes.LastPush(ctx, m.ID)
	if errors.Is(err, store.ErrNotFound) {
		return failedf("no push received yet")
	}
	if err != nil {
		return failed(err)
	}
	// A report may be late by the monitor's timeout before it is missed.
	if age := now.Sub(report.ReceivedAt); age > m.Interval+m.Timeout {
		return failedf("no push received for %s; one is expected every %s", age.Round(time.Second), m.Interval)
	}
	if !report.Up {
		if report.Message == "" {
			return failedf("the source reported a failure")
		}
		return failedf("the source reported a failure: %s", report.Message)
	}
	return outcome{detail: report.Message, latency: report.Latency, hasTime: true}
}
