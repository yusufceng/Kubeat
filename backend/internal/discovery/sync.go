// Package discovery keeps monitors in step with annotated cluster objects:
// a Service, Ingress, Route, Deployment, StatefulSet or DaemonSet carrying
// kubeat.io/monitor=true is monitored for as long as it carries it.
package discovery

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/k8s"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// The annotations understood on cluster objects.
const (
	// AnnotationMonitor asks for the object to be monitored when "true".
	AnnotationMonitor = "kubeat.io/monitor"
	// AnnotationInterval and AnnotationTimeout are durations, e.g. "30s".
	AnnotationInterval = "kubeat.io/interval"
	AnnotationTimeout  = "kubeat.io/timeout"
	// AnnotationFailureThreshold is how many failed checks open an incident.
	AnnotationFailureThreshold = "kubeat.io/failure-threshold"
	// AnnotationPort selects one port of a Service, by number or name;
	// without it every TCP port is monitored.
	AnnotationPort = "kubeat.io/port"
	// AnnotationPath makes the check of a Service port an HTTP request to
	// this path, whether or not a pod probe was found.
	AnnotationPath = "kubeat.io/path"
	// AnnotationEachPod, when "true" on a Service, checks every ready pod
	// behind the port instead of the service's address.
	AnnotationEachPod = "kubeat.io/each-pod"
)

// Actor is recorded in the audit log for the syncer's changes.
const Actor = "discovery"

// Cluster lists the monitorable objects; an empty namespace means all.
type Cluster interface {
	Candidates(ctx context.Context, namespace string) ([]k8s.Candidate, error)
}

// Store is the part of the monitor store the syncer uses.
type Store interface {
	ListMonitors(ctx context.Context) ([]store.Monitor, error)
	CreateMonitor(ctx context.Context, m store.Monitor) (store.Monitor, error)
	UpdateMonitor(ctx context.Context, m store.Monitor) (store.Monitor, error)
	DeleteMonitor(ctx context.Context, id int64) error
	AppendAudit(ctx context.Context, e store.AuditEntry) error
	// The objects whose monitor was deleted by hand, and forgetting one.
	ListDismissedSources(ctx context.Context) ([]store.DismissedSource, error)
	RestoreSource(ctx context.Context, namespace, source string) error
}

// Options configures a Syncer.
type Options struct {
	Cluster Cluster
	Store   Store
	// Interval is how often the cluster is read.
	Interval time.Duration
	// The check settings of a monitor whose object does not set them, and
	// the shortest interval an annotation may ask for.
	DefaultInterval         time.Duration
	DefaultTimeout          time.Duration
	DefaultFailureThreshold int
	MinInterval             time.Duration
	Logger                  *slog.Logger
}

// Skip says why an annotated object was not monitored.
type Skip struct {
	Namespace string `json:"namespace"`
	Source    string `json:"source"`
	Reason    string `json:"reason"`
	// Dismissed says the monitor was deleted in Kubeat although the
	// annotation still asks for it; it can be restored.
	Dismissed bool `json:"dismissed"`
}

// Status is the outcome of the last run.
type Status struct {
	IntervalSeconds int `json:"interval_seconds"`
	// LastRunAt is zero before the first run.
	LastRunAt time.Time `json:"last_run_at"`
	// Error is set when the cluster could not be read; nothing was changed.
	// Cause is the error it was written from, so that a reader who may not
	// see its text can still be told what kind of failure it was.
	Error string `json:"error"`
	Cause error  `json:"-"`
	// Managed is how many monitors the syncer keeps; the counts below are
	// of the last run.
	Managed int    `json:"managed"`
	Created int    `json:"created"`
	Updated int    `json:"updated"`
	Removed int    `json:"removed"`
	Skipped []Skip `json:"skipped"`
	// ByNamespace splits the counts above by namespace, for a reader who
	// may see only some namespaces (see Within).
	ByNamespace map[string]Counts `json:"-"`
}

// Counts are what a run did to the managed monitors of a namespace.
type Counts struct {
	Managed, Created, Updated, Removed int
}

// Within is the status as told to a reader who may see only the
// namespaces visible accepts: the objects skipped in those, and the counts
// of those alone. The rest would tell them what other teams run.
func (st Status) Within(visible func(namespace string) bool) Status {
	skipped := make([]Skip, 0, len(st.Skipped))
	for _, s := range st.Skipped {
		if visible(s.Namespace) {
			skipped = append(skipped, s)
		}
	}
	byNamespace := make(map[string]Counts)
	for ns, n := range st.ByNamespace {
		if visible(ns) {
			byNamespace[ns] = n
		}
	}
	st.Skipped, st.ByNamespace = skipped, byNamespace
	st.total()
	return st
}

// total sets the counts of the whole run from those of its namespaces.
func (st *Status) total() {
	st.Managed, st.Created, st.Updated, st.Removed = 0, 0, 0, 0
	for _, n := range st.ByNamespace {
		st.Managed += n.Managed
		st.Created += n.Created
		st.Updated += n.Updated
		st.Removed += n.Removed
	}
}

// Syncer creates, updates and removes managed monitors.
type Syncer struct {
	opts Options

	mu     sync.Mutex
	status Status
}

// New returns a syncer that has not run yet.
func New(opts Options) *Syncer {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Syncer{opts: opts, status: Status{
		IntervalSeconds: int(opts.Interval / time.Second), Skipped: []Skip{}, ByNamespace: map[string]Counts{},
	}}
}

// Status reports the last run.
func (s *Syncer) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// Run syncs now and then every interval, until the context ends.
func (s *Syncer) Run(ctx context.Context) {
	ticker := time.NewTicker(s.opts.Interval)
	defer ticker.Stop()
	for {
		if err := s.Sync(ctx); err != nil && ctx.Err() == nil {
			s.opts.Logger.Warn("discovery sync failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Sync reads the cluster once and applies the difference. When the cluster
// cannot be read nothing is changed, so a cluster that is briefly away does
// not take its monitors with it.
func (s *Syncer) Sync(ctx context.Context) error {
	st := Status{
		IntervalSeconds: int(s.opts.Interval / time.Second), LastRunAt: time.Now().UTC(),
		Skipped: []Skip{}, ByNamespace: map[string]Counts{},
	}
	err := s.sync(ctx, &st)
	if err != nil {
		st.Error, st.Cause = err.Error(), err
	}
	s.mu.Lock()
	if err != nil {
		// Keep the counts of the last run that did something.
		st.Managed, st.Created, st.Updated, st.Removed, st.Skipped = s.status.Managed, s.status.Created, s.status.Updated, s.status.Removed, s.status.Skipped
		st.ByNamespace = s.status.ByNamespace
	}
	s.status = st
	s.mu.Unlock()
	return err
}

// key identifies a monitor's cluster object.
func key(namespace, source string) string { return namespace + "/" + source }

func (s *Syncer) sync(ctx context.Context, st *Status) error {
	candidates, err := s.opts.Cluster.Candidates(ctx, "")
	if err != nil {
		return fmt.Errorf("read the cluster: %w", err)
	}
	monitors, err := s.opts.Store.ListMonitors(ctx)
	if err != nil {
		return err
	}
	existing := make(map[string]store.Monitor)
	for _, m := range monitors {
		if m.Source != "" {
			existing[key(m.Namespace, m.Source)] = m
		}
	}

	dismissals, err := s.opts.Store.ListDismissedSources(ctx)
	if err != nil {
		return err
	}
	dismissed := make(map[string]bool, len(dismissals))
	for _, d := range dismissals {
		dismissed[key(d.Namespace, d.Source)] = true
	}

	wanted, annotated := s.wanted(candidates, st)
	// A dismissal lasts as long as the annotation it overrules: once the
	// object stops asking, annotating it again starts afresh.
	for _, d := range dismissals {
		if annotated[key(d.Namespace, d.Source)] {
			continue
		}
		if err := s.opts.Store.RestoreSource(ctx, d.Namespace, d.Source); err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
	}
	keys := make([]string, 0, len(wanted))
	for k := range wanted {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		want := wanted[k]
		current, found := existing[k]
		counts := st.ByNamespace[want.Namespace]
		switch {
		case !found && dismissed[k]:
			st.Skipped = append(st.Skipped, Skip{Namespace: want.Namespace, Source: want.Source, Reason: "deleted in Kubeat; restore it to monitor it again", Dismissed: true})
			continue
		case !found:
			if err := s.create(ctx, want); err != nil {
				return err
			}
			counts.Created++
		case !current.Managed:
			st.Skipped = append(st.Skipped, Skip{Namespace: want.Namespace, Source: want.Source, Reason: "already monitored by hand"})
			continue
		case current.Type != want.Type:
			// The type never changes on a monitor: a probe that appeared
			// turns a port check into an HTTP one by starting over.
			if err := s.remove(ctx, current, "check type changed to "+string(want.Type)); err != nil {
				return err
			}
			if err := s.create(ctx, want); err != nil {
				return err
			}
			counts.Updated++
		case differs(current, want):
			current.Target, current.Interval, current.Timeout, current.FailureThreshold = want.Target, want.Interval, want.Timeout, want.FailureThreshold
			current.GRPC.Service, current.EachPod = want.GRPC.Service, want.EachPod
			if _, err := s.opts.Store.UpdateMonitor(ctx, current); err != nil {
				return fmt.Errorf("update monitor %d: %w", current.ID, err)
			}
			s.audit(ctx, "update", current.ID, current.Name)
			counts.Updated++
		}
		counts.Managed++
		st.ByNamespace[want.Namespace] = counts
	}

	// What is managed but no longer asked for goes, incidents and history
	// with it: the annotation is the whole of the request.
	for _, m := range monitors {
		if !m.Managed || m.Source == "" {
			continue
		}
		if _, still := wanted[key(m.Namespace, m.Source)]; still {
			continue
		}
		if err := s.remove(ctx, m, "annotation removed or object gone"); err != nil {
			return err
		}
		counts := st.ByNamespace[m.Namespace]
		counts.Removed++
		st.ByNamespace[m.Namespace] = counts
	}
	st.total()
	return nil
}

// differs says whether the settings the syncer owns changed.
func differs(current, want store.Monitor) bool {
	return current.Target != want.Target || current.Interval != want.Interval || current.Timeout != want.Timeout ||
		current.FailureThreshold != want.FailureThreshold || current.GRPC.Service != want.GRPC.Service ||
		current.EachPod != want.EachPod
}

func (s *Syncer) create(ctx context.Context, want store.Monitor) error {
	m, err := s.opts.Store.CreateMonitor(ctx, want)
	if errors.Is(err, store.ErrConflict) {
		// A monitor by that name made by hand; leave it be.
		s.opts.Logger.Warn("discovery sync: name taken", "name", want.Name)
		return nil
	}
	if err != nil {
		return fmt.Errorf("create monitor %s: %w", want.Name, err)
	}
	s.audit(ctx, "create", m.ID, m.Name)
	return nil
}

func (s *Syncer) remove(ctx context.Context, m store.Monitor, why string) error {
	if err := s.opts.Store.DeleteMonitor(ctx, m.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("delete monitor %d: %w", m.ID, err)
	}
	s.audit(ctx, "delete", m.ID, m.Name+": "+why)
	return nil
}

func (s *Syncer) audit(ctx context.Context, action string, id int64, summary string) {
	err := s.opts.Store.AppendAudit(context.WithoutCancel(ctx), store.AuditEntry{
		At: time.Now().UTC(), Username: Actor, Action: action, ObjectType: "monitor", ObjectID: id, Summary: summary,
	})
	if err != nil {
		s.opts.Logger.Error("audit entry lost", "action", action, "error", err)
	}
}

// wanted turns the annotated candidates into the monitors they ask for,
// keyed by namespace and source. Candidates whose annotations cannot be
// honoured are reported, not guessed at; annotated names every candidate
// that asks, honoured or not.
func (s *Syncer) wanted(candidates []k8s.Candidate, st *Status) (out map[string]store.Monitor, annotated map[string]bool) {
	out = make(map[string]store.Monitor)
	annotated = make(map[string]bool)
	for _, c := range candidates {
		if !strings.EqualFold(strings.TrimSpace(c.Annotations[AnnotationMonitor]), "true") {
			continue
		}
		if c.Kind == k8s.KindService {
			if port, ok := c.Annotations[AnnotationPort]; ok && !portMatches(c, port) {
				continue
			}
		}
		annotated[key(c.Namespace, c.Source())] = true
		m, err := s.monitorFor(c)
		if err != nil {
			st.Skipped = append(st.Skipped, Skip{Namespace: c.Namespace, Source: c.Source(), Reason: err.Error()})
			continue
		}
		out[key(m.Namespace, m.Source)] = m
	}
	return out, annotated
}

func portMatches(c k8s.Candidate, port string) bool {
	port = strings.TrimSpace(port)
	return port == strconv.Itoa(int(c.Port)) || (port != "" && port == c.PortName)
}

func (s *Syncer) monitorFor(c k8s.Candidate) (store.Monitor, error) {
	m := store.Monitor{
		Name: c.Namespace + "/" + c.Source(), Type: c.Type, Target: c.Target,
		Namespace: c.Namespace, Source: c.Source(), Managed: true, Enabled: true,
		Interval: s.opts.DefaultInterval, Timeout: s.opts.DefaultTimeout, FailureThreshold: s.opts.DefaultFailureThreshold,
	}
	m.GRPC.Service = c.GRPCService

	var err error
	if m.Interval, err = duration(c.Annotations, AnnotationInterval, m.Interval); err != nil {
		return m, err
	}
	if m.Interval < s.opts.MinInterval {
		return m, fmt.Errorf("%s: must be at least %s", AnnotationInterval, s.opts.MinInterval)
	}
	if m.Timeout, err = duration(c.Annotations, AnnotationTimeout, m.Timeout); err != nil {
		return m, err
	}
	if m.Timeout <= 0 || m.Timeout >= m.Interval {
		return m, fmt.Errorf("%s: must be shorter than the interval", AnnotationTimeout)
	}
	if v, ok := c.Annotations[AnnotationFailureThreshold]; ok {
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil || n < 1 {
			return m, fmt.Errorf("%s: must be a whole number of at least 1", AnnotationFailureThreshold)
		}
		m.FailureThreshold = n
	}
	if path, ok := c.Annotations[AnnotationPath]; ok {
		if c.Kind != k8s.KindService {
			return m, fmt.Errorf("%s: only applies to a Service", AnnotationPath)
		}
		path = strings.TrimSpace(path)
		if !strings.HasPrefix(path, "/") || strings.ContainsAny(path, " #") {
			return m, fmt.Errorf("%s: must start with /", AnnotationPath)
		}
		target, err := withPath(c, path)
		if err != nil {
			return m, err
		}
		m.Type, m.Target, m.GRPC.Service = store.MonitorHTTP, target, ""
	}
	if v, ok := c.Annotations[AnnotationEachPod]; ok && strings.EqualFold(strings.TrimSpace(v), "true") {
		if c.Kind != k8s.KindService || (m.Type != store.MonitorHTTP && m.Type != store.MonitorTCP) {
			return m, fmt.Errorf("%s: only applies to a Service port checked over HTTP or TCP", AnnotationEachPod)
		}
		m.EachPod = true
	}
	return m, nil
}

// withPath is the HTTP check of a service port at the given path. A probe's
// scheme is kept; a port without one is taken to speak plain HTTP.
func withPath(c k8s.Candidate, path string) (string, error) {
	if c.Type == store.MonitorHTTP {
		u, err := url.Parse(c.Target)
		if err != nil {
			return "", fmt.Errorf("%s: %w", AnnotationPath, err)
		}
		u.Path, u.RawQuery = path, ""
		return u.String(), nil
	}
	return "http://" + c.Target + path, nil
}

func duration(annotations map[string]string, name string, fallback time.Duration) (time.Duration, error) {
	v, ok := annotations[name]
	if !ok {
		return fallback, nil
	}
	d, err := time.ParseDuration(strings.TrimSpace(v))
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s: must be a duration such as 30s", name)
	}
	return d.Truncate(time.Second), nil
}
