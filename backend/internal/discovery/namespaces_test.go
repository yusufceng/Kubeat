package discovery

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/yusufceng/Kubeat/backend/internal/k8s"
	"github.com/yusufceng/Kubeat/backend/internal/metrics"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// inNamespace moves a candidate of the shop namespace to another one.
func inNamespace(namespace string, c k8s.Candidate) k8s.Candidate {
	c.Namespace, c.Target = namespace, strings.Replace(c.Target, ".shop.svc", "."+namespace+".svc", 1)
	return c
}

// refused is a namespace the cluster API would not let Kubeat's service
// account read, as client-go reports it: its Role was taken away, or the
// namespace was deleted together with its RoleBinding.
func refused(namespace string) k8s.NamespaceError {
	return k8s.NamespaceError{Namespace: namespace, Err: fmt.Errorf("list services: %w", apierrors.NewForbidden(schema.GroupResource{Resource: "services"}, "",
		fmt.Errorf(`User "system:serviceaccount:kubeat:kubeat" cannot list resource "services" in API group "" in the namespace %q`, namespace)))}
}

// gone is a namespace the cluster API no longer has.
func gone(namespace string) k8s.NamespaceError {
	return k8s.NamespaceError{Namespace: namespace, Err: fmt.Errorf("list services: %w", apierrors.NewNotFound(schema.GroupResource{Resource: "namespaces"}, namespace))}
}

// watched gives the syncer metrics and a log to read.
func watched(s *Syncer) (*metrics.Metrics, *bytes.Buffer) {
	meter, logs := metrics.New("test"), &bytes.Buffer{}
	s.opts.Metrics, s.opts.Logger = meter, slog.New(slog.NewTextHandler(logs, nil))
	return meter, logs
}

// discoverySeries are the annotation sync's series as a scrape reads them,
// one a line, in order.
func discoverySeries(t *testing.T, meter *metrics.Metrics) string {
	t.Helper()
	got, err := meter.Gather()
	if err != nil {
		t.Fatal(err)
	}
	var series []string
	for name, value := range got {
		if strings.HasPrefix(name, "kubeat_discovery_") {
			series = append(series, fmt.Sprintf("%s %v", name, value))
		}
	}
	sort.Strings(series)
	return strings.Join(series, "\n")
}

// seriesWith is discoverySeries with these values.
func seriesWith(skipped, forbidden, notFound, other, timeout int) string {
	return fmt.Sprintf(`kubeat_discovery_namespaces_skipped %d
kubeat_discovery_sync_errors_total{reason=forbidden} %d
kubeat_discovery_sync_errors_total{reason=not_found} %d
kubeat_discovery_sync_errors_total{reason=other} %d
kubeat_discovery_sync_errors_total{reason=timeout} %d`, skipped, forbidden, notFound, other, timeout)
}

func namespaceSkips(st Status) string {
	var out []string
	for _, n := range st.SkippedNamespaces {
		out = append(out, n.Namespace+":"+n.Reason)
	}
	return strings.Join(out, ",")
}

// A namespace that cannot be read is skipped and the run goes on in the
// others. Its monitors stay as they are, and its dismissals, whatever its
// objects now ask for; the status, the log and the metrics say so, the
// metrics without its name. Once it can be read again, the next run brings
// it in step.
func TestSyncSkipsANamespaceItCannotRead(t *testing.T) {
	ctx := context.Background()
	cluster := &fakeCluster{candidates: []k8s.Candidate{
		serviceCandidate("api", 80, "", monitored(nil)),
		inNamespace("billing", serviceCandidate("ledger", 80, "", monitored(nil))),
		inNamespace("billing", serviceCandidate("invoices", 81, "", monitored(nil))),
	}}
	s, db := newSyncer(t, cluster)
	meter, logs := watched(s)
	if err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if got := discoverySeries(t, meter); got != seriesWith(0, 0, 0, 0, 0) {
		t.Errorf("metrics after a run that read everything:\n%s", got)
	}
	// The invoices' monitor is deleted in Kubeat.
	invoices := names(t, db)["billing/service/invoices:81"]
	if err := db.DismissSource(ctx, store.DismissedSource{Namespace: "billing", Source: invoices.Source, At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteMonitor(ctx, invoices.ID); err != nil {
		t.Fatal(err)
	}
	ledger := names(t, db)["billing/service/ledger:80"]

	// billing's Role is taken away. Meanwhile in shop api's annotation goes
	// and web is annotated, and in billing the annotations go too, which
	// the runs do not see.
	cluster.candidates = []k8s.Candidate{serviceCandidate("web", 81, "", monitored(nil))}
	cluster.skipped = []k8s.NamespaceError{refused("billing")}
	for run := 1; run <= 2; run++ {
		if err := s.Sync(ctx); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		st := s.Status()
		if st.Error != "" || namespaceSkips(st) != "billing:forbidden" || st.SkippedNamespaces[0].Cause == nil ||
			!strings.Contains(st.SkippedNamespaces[0].Error, `cannot list resource "services"`) {
			t.Errorf("run %d: error %q, skipped namespaces %+v", run, st.Error, st.SkippedNamespaces)
		}
		if st.ByNamespace["billing"] != (Counts{Managed: 1}) || st.Managed != 2 {
			t.Errorf("run %d: counts %+v, want billing's monitor counted as kept", run, st.ByNamespace)
		}
		if run == 1 && st.ByNamespace["shop"] != (Counts{Managed: 1, Created: 1, Removed: 1}) {
			t.Errorf("run 1: shop %+v, want web created and api removed", st.ByNamespace["shop"])
		}
		got := names(t, db)
		if after, ok := got["billing/service/ledger:80"]; !ok || !reflect.DeepEqual(after, ledger) {
			t.Errorf("run %d: billing's monitor %+v (there: %v), want it as it was: %+v", run, after, ok, ledger)
		}
		if _, ok := got["shop/service/web:81"]; !ok {
			t.Errorf("run %d: shop was not kept in step: %v", run, got)
		}
		if _, ok := got["shop/service/api:80"]; ok {
			t.Errorf("run %d: api kept its monitor without its annotation", run)
		}
		if left, _ := db.ListDismissedSources(ctx); len(left) != 1 {
			t.Errorf("run %d: dismissals %+v, want billing's kept", run, left)
		}
		if got, want := discoverySeries(t, meter), seriesWith(1, run, 0, 0, 0); got != want {
			t.Errorf("run %d: metrics\n%s\nwant\n%s", run, got, want)
		}
	}
	if !strings.Contains(logs.String(), "level=WARN") || !strings.Contains(logs.String(), "namespace=billing reason=forbidden") {
		t.Errorf("log: %s", logs.String())
	}
	if shop := s.Status().Within(func(ns string) bool { return ns == "shop" }); shop.SkippedNamespaces == nil || len(shop.SkippedNamespaces) != 0 {
		t.Errorf("a reader of shop is told of %+v", shop.SkippedNamespaces)
	}
	if billing := s.Status().Within(func(ns string) bool { return ns == "billing" }); namespaceSkips(billing) != "billing:forbidden" {
		t.Errorf("a reader of billing is told of %+v", billing.SkippedNamespaces)
	}

	// The Role is back: billing's changes are applied.
	cluster.skipped = nil
	cluster.candidates = append(cluster.candidates,
		inNamespace("billing", serviceCandidate("ledger", 80, "", nil)),
		inNamespace("billing", serviceCandidate("invoices", 81, "", nil)))
	if err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	st := s.Status()
	if st.SkippedNamespaces == nil || len(st.SkippedNamespaces) != 0 || st.ByNamespace["billing"] != (Counts{Removed: 1}) {
		t.Errorf("billing read again: %+v", st)
	}
	if _, ok := names(t, db)["billing/service/ledger:80"]; ok {
		t.Error("the ledger kept its monitor without its annotation")
	}
	if left, _ := db.ListDismissedSources(ctx); len(left) != 0 {
		t.Errorf("dismissals %+v outlived their annotation", left)
	}
	if got, want := discoverySeries(t, meter), seriesWith(0, 2, 0, 0, 0); got != want {
		t.Errorf("metrics after billing came back:\n%s\nwant\n%s", got, want)
	}
}

// A namespace deleted is skipped like one refused (deleted with its
// RoleBinding, it is refused): its monitors stay until it is taken off the
// discovery list, and the run that no longer reads it removes them.
func TestSyncKeepsTheMonitorsOfADeletedNamespace(t *testing.T) {
	ctx := context.Background()
	cluster := &fakeCluster{candidates: []k8s.Candidate{
		serviceCandidate("api", 80, "", monitored(nil)),
		inNamespace("legacy", serviceCandidate("ledger", 80, "", monitored(nil))),
	}}
	s, db := newSyncer(t, cluster)
	meter, _ := watched(s)
	if err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	cluster.candidates = cluster.candidates[:1]
	cluster.skipped = []k8s.NamespaceError{gone("legacy")}
	if err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if st := s.Status(); namespaceSkips(st) != "legacy:not_found" || st.Removed != 0 || st.Error != "" {
		t.Errorf("status %+v", st)
	}
	if _, ok := names(t, db)["legacy/service/ledger:80"]; !ok {
		t.Error("the deleted namespace's monitor was removed")
	}
	if got, want := discoverySeries(t, meter), seriesWith(1, 0, 1, 0, 0); got != want {
		t.Errorf("metrics\n%s\nwant\n%s", got, want)
	}

	// Off the discovery list (UPTIME_DISCOVERY_NAMESPACES): neither read
	// nor skipped.
	cluster.skipped = nil
	if err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if st := s.Status(); st.Removed != 1 || len(st.SkippedNamespaces) != 0 {
		t.Errorf("status %+v", st)
	}
	if _, ok := names(t, db)["legacy/service/ledger:80"]; ok {
		t.Error("the monitor stayed after its namespace left the list")
	}
}

// When none of the namespaces can be read, the cluster cannot be read:
// nothing is changed, as when it is away, and every namespace is reported.
func TestSyncChangesNothingWhenNoNamespaceCanBeRead(t *testing.T) {
	ctx := context.Background()
	cluster := &fakeCluster{candidates: []k8s.Candidate{
		serviceCandidate("api", 80, "", monitored(nil)),
		inNamespace("billing", serviceCandidate("ledger", 80, "", monitored(nil))),
	}}
	s, db := newSyncer(t, cluster)
	meter, _ := watched(s)
	if err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	before := names(t, db)

	cluster.skipped = []k8s.NamespaceError{gone("billing"), refused("shop")}
	cluster.err = fmt.Errorf("no namespace could be read: %w", &cluster.skipped[0])
	if err := s.Sync(ctx); !errors.Is(err, cluster.err) {
		t.Fatalf("err = %v, want the cluster's", err)
	}
	if got := names(t, db); !reflect.DeepEqual(got, before) {
		t.Errorf("monitors changed: %v, were %v", got, before)
	}
	st := s.Status()
	if !errors.Is(st.Cause, cluster.err) || st.Managed != 2 || st.Created != 2 || namespaceSkips(st) != "billing:not_found,shop:forbidden" {
		t.Errorf("status %+v", st)
	}
	if got, want := discoverySeries(t, meter), seriesWith(2, 1, 1, 0, 0); got != want {
		t.Errorf("metrics\n%s\nwant\n%s", got, want)
	}
}

// A run that gets no answer from the cluster counts once, by reason, and
// knows nothing of its namespaces: the last run's skipped namespaces stand,
// in the status and in the gauge. One that ends with the process counts
// nothing.
func TestSyncKeepsWhatItKnewOfTheNamespacesWhenTheClusterIsAway(t *testing.T) {
	ctx := context.Background()
	cluster := &fakeCluster{
		candidates: []k8s.Candidate{serviceCandidate("api", 80, "", monitored(nil))},
		skipped:    []k8s.NamespaceError{refused("billing")},
	}
	s, _ := newSyncer(t, cluster)
	meter, _ := watched(s)
	if err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	cluster.skipped = nil
	cluster.err = &k8s.NamespaceError{Namespace: "billing", Err: fmt.Errorf("list services: %w", &url.Error{
		Op: "Get", URL: "https://172.30.0.1:443/api/v1/namespaces/billing/services",
		Err: &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connect: connection refused")},
	})}
	if err := s.Sync(ctx); err == nil {
		t.Fatal("expected the cluster error")
	}
	if st := s.Status(); st.Error == "" || namespaceSkips(st) != "billing:forbidden" || st.Managed != 1 {
		t.Errorf("status %+v", st)
	}
	if got, want := discoverySeries(t, meter), seriesWith(1, 1, 0, 1, 0); got != want {
		t.Errorf("metrics\n%s\nwant\n%s", got, want)
	}

	// No answer in time is a timeout.
	cluster.err = fmt.Errorf("list services: %w", context.DeadlineExceeded)
	if err := s.Sync(ctx); err == nil {
		t.Fatal("expected the cluster error")
	}
	if got, want := discoverySeries(t, meter), seriesWith(1, 1, 0, 1, 1); got != want {
		t.Errorf("metrics after a timeout\n%s\nwant\n%s", got, want)
	}

	// Shutting down is not the cluster failing.
	stopped, stop := context.WithCancel(ctx)
	stop()
	cluster.err = fmt.Errorf("list services: %w", context.Canceled)
	if err := s.Sync(stopped); err == nil {
		t.Fatal("expected the run to end")
	}
	if got, want := discoverySeries(t, meter), seriesWith(1, 1, 0, 1, 1); got != want {
		t.Errorf("metrics after the run ended\n%s\nwant\n%s", got, want)
	}
}
