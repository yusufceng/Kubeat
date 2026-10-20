// Package checker performs a single uptime check of a monitor.
package checker

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/netguard"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// maxDetail bounds the text a check stores about what it found, or why it
// failed.
const maxDetail = 300

// maxLoggedAnswer bounds how much of a target's own words a check logs
// (targetSaid): a first line may run to the size of a whole header.
const maxLoggedAnswer = 1 << 10

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
	// Endpoints is required for monitors that check each pod; nil without
	// a cluster.
	Endpoints Endpoints
	// RolloutGrace is how long a workload monitor tolerates missing pods
	// while a rollout is in progress; zero tolerates none.
	RolloutGrace time.Duration
	// Guard decides where checks may connect. Every engine dials through
	// the checker's dialer, which it checks; ping, which sends packets
	// without one, asks it about the address it resolved. A target is set
	// by whoever may edit a monitor, so without a guard any of them could
	// reach the backend's own loopback or the cloud's metadata service.
	// The addresses it trusts, such as the proxy's, are reached as they
	// are. Nil allows every address.
	Guard *netguard.Policy
	// Logger gets what a check keeps out of its result: the cluster API's
	// own error, where the result says only what kind of failure it was.
	// Nil logs with slog.Default.
	Logger *slog.Logger
}

// Checker runs the checks of every monitor type. It is safe for concurrent
// use.
type Checker struct {
	verifying *http.Transport
	insecure  *http.Transport
	dialer    *netguard.Dialer
	opts      Options

	// rollouts is when each workload target was first seen rolling out.
	mu       sync.Mutex
	rollouts map[string]time.Time
	now      func() time.Time
}

// New returns a Checker. Timeouts are not set here; every check is bounded
// by the context passed to Check.
func New(opts Options) *Checker {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	dialer := opts.Guard.Dialer(&net.Dialer{})
	transport := func(skipVerify bool) *http.Transport {
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.TLSClientConfig = &tls.Config{InsecureSkipVerify: skipVerify}
		// The checker's own dialer, so that the guard sees every connection
		// of an HTTP check too; a redirect is a new connection and is
		// checked like the first. Through a proxy the connection goes to
		// the proxy, so the guard checks the target before that.
		t.DialContext = dialer.DialContext
		t.Proxy = opts.Guard.Proxy(t.Proxy)
		return t
	}
	return &Checker{
		verifying: transport(opts.InsecureSkipVerify),
		insecure:  transport(true),
		dialer:    dialer,
		opts:      opts,
		rollouts:  make(map[string]time.Time),
		now:       time.Now,
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
	// diag is what the engine saw of why the check failed, or for an
	// inverted monitor of what answered; Check keeps it only for a failed
	// result, explained by err where the engine did not explain it.
	diag *store.Diagnostics
}

func failed(err error) outcome { return outcome{err: err} }

func failedf(format string, args ...any) outcome { return outcome{err: fmt.Errorf(format, args...)} }

// Check runs one check of m and reports the outcome. A failed check is not
// an error: it is a result with Up == false and Error set.
func (c *Checker) Check(ctx context.Context, m store.Monitor) store.CheckResult {
	start := time.Now()
	result := store.CheckResult{MonitorID: m.ID, CheckedAt: start.UTC().Truncate(time.Millisecond)}

	var out outcome
	switch {
	case eachPod(m):
		out = c.checkEachPod(ctx, m)
	case m.Type == store.MonitorHTTP:
		out = c.checkHTTP(ctx, m)
	case m.Type == store.MonitorTCP:
		out = c.checkTCP(ctx, m)
	case m.Type == store.MonitorPing:
		out = c.checkPing(ctx, m.Target)
	case m.Type == store.MonitorDNS:
		out = c.checkDNS(ctx, m)
	case m.Type == store.MonitorPush:
		out = c.checkPush(ctx, m, start)
	case m.Type == store.MonitorGRPC:
		out = c.checkGRPC(ctx, m)
	case m.Type == store.MonitorWebSocket:
		out = c.checkWebSocket(ctx, m)
	case m.Type == store.MonitorPostgres:
		out = c.checkPostgres(ctx, m)
	case m.Type == store.MonitorMySQL:
		out = c.checkMySQL(ctx, m)
	case m.Type == store.MonitorRedis:
		out = c.checkRedis(ctx, m)
	case m.Type == store.MonitorWorkload:
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
		result.Error = truncate(out.err.Error(), maxDetail)
	} else {
		result.Detail = truncate(out.detail, maxDetail)
	}

	// A connection the target policy refused never left Kubeat: whether
	// the network would have let it through is unknown, so it is not the
	// failure an inverted monitor expects, and the policy's error stands.
	if m.Inverted && !errors.Is(out.err, netguard.ErrBlocked) {
		invert(&result)
	}
	// A successful check keeps nothing more; a failed one what it found out
	// about why, which for an inverted monitor is what answered.
	if !result.Up {
		result.Diagnostics = diagnose(m.Type, out)
	}
	return result
}

// invert turns the outcome around for a monitor that expects its target to
// fail.
func invert(r *store.CheckResult) {
	if r.Up {
		r.Up = false
		r.Error = "the target responded, but this monitor expects it to fail"
		if r.Detail != "" {
			r.Error = truncate(r.Error+": "+r.Detail, maxDetail)
		}
		r.Detail = ""
		return
	}
	r.Up = true
	r.Detail = truncate("failed as expected: "+r.Error, maxDetail)
	r.Error = ""
}

// policyRefusal remembers the target policy's refusal of a connection, for
// the engines whose libraries put a failed connection into errors of their
// own, the DNS resolver's and gRPC's, which errors.Is cannot see through.
type policyRefusal struct {
	mu  sync.Mutex
	err error
}

// dial connects through the checker's dialer and remembers a refusal.
func (r *policyRefusal) dial(c *Checker) func(ctx context.Context, network, address string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := c.dialer.DialContext(ctx, network, address)
		if errors.Is(err, netguard.ErrBlocked) {
			r.mu.Lock()
			r.err = err
			r.mu.Unlock()
		}
		return conn, err
	}
}

// wrap returns err, its text unchanged, so that errors.Is finds in it the
// refusal remembered, if there is one.
func (r *policyRefusal) wrap(err error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return refusedTo(err, r.err)
}

// refusedTo returns err so that errors.Is also finds refusal, the target
// policy's error, in it; err itself when either is nil.
func refusedTo(err, refusal error) error {
	if err == nil || refusal == nil || errors.Is(err, netguard.ErrBlocked) {
		return err
	}
	return refused{err, refusal}
}

// refused is an error, as a library or a check put it, about a connection
// the target policy refused.
type refused struct {
	error
	refusal error
}

func (e refused) Unwrap() []error { return []error{e.error, e.refusal} }

// targetSaid is what a check stores when the target answered outside the
// monitor's protocol, or with an error of its own: kind, what sort of
// failure it was. Every reader of the monitor reads its results, while
// only an editor chose the target, which may be any service the target
// policy allows; its words, a banner or a message naming its software,
// hosts or builds, go to the log.
func (c *Checker) targetSaid(m store.Monitor, kind, words string) error {
	c.opts.Logger.Warn("a check's target answered outside the monitor's protocol", "monitor_id", m.ID, "monitor", m.Name,
		"failure", kind, "answer", truncate(words, maxLoggedAnswer))
	return errors.New(kind)
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
	return c.checkTCPAt(ctx, m, m.Target)
}

// checkTCPAt connects to addr; the TLS server name stays the target's.
func (c *Checker) checkTCPAt(ctx context.Context, m store.Monitor, addr string) outcome {
	conn, err := c.dialer.DialContext(ctx, "tcp", addr)
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
		return afterConnect(failedf("tls handshake: %w", err))
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
	// A report may be late by the monitor's timeout before it is missed. The
	// message stays the same check after check, so that the failures of one
	// silence read as one: how long it has lasted is the incident's to say.
	if age := now.Sub(report.ReceivedAt); age > m.Interval+m.Timeout {
		return failedf("no push within the expected %s interval", shortDuration(m.Interval))
	}
	if !report.Up {
		if report.Message == "" {
			return failedf("the source reported a failure")
		}
		return failedf("the source reported a failure: %s", report.Message)
	}
	return outcome{detail: report.Message, latency: report.Latency, hasTime: true}
}

// shortDuration writes an interval without its zero units: 1h0m0s as "1h",
// 1h30m0s as "1h30m", 5m0s as "5m", 45s as it is.
func shortDuration(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = s[:len(s)-2]
	}
	if strings.HasSuffix(s, "h0m") {
		s = s[:len(s)-2]
	}
	return s
}
