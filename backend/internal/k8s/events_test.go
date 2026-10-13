package k8s

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func event(name, kind, object, typ, reason string, at time.Time, count int32) *corev1.Event {
	return &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Namespace: "shop", Name: name},
		InvolvedObject: corev1.ObjectReference{Kind: kind, Name: object},
		Type:           typ, Reason: reason, Message: "  " + reason + " happened ",
		LastTimestamp: metav1.NewTime(at), Count: count,
	}
}

// deploymentPods are the pods of Deployment "web" through ReplicaSet
// "web-7d9f8c6b5", as the cluster names them.
func deploymentPods(names ...string) Workload {
	w := Workload{}
	for _, n := range names {
		w.Pods = append(w.Pods, Pod{Name: n, Owner: "ReplicaSet/web-7d9f8c6b5"})
	}
	return w
}

func objects(events []Event) string {
	var out []string
	for _, e := range events {
		out = append(out, e.Object+" "+e.Reason)
	}
	return strings.Join(out, ",")
}

func TestEventsAboutThePodsAndWhatManagesThem(t *testing.T) {
	now := time.Date(2026, 10, 12, 20, 0, 0, 0, time.UTC)
	core := fake.NewSimpleClientset(
		event("e1", "Pod", "web-7d9f8c6b5-x7k2m", "Warning", "BackOff", now.Add(-5*time.Minute), 12),
		event("e2", "Pod", "web-7d9f8c6b5-pq2r4", "Normal", "Pulled", now.Add(-50*time.Minute), 1),
		event("e3", "ReplicaSet", "web-7d9f8c6b5", "Normal", "SuccessfulCreate", now.Add(-2*time.Minute), 1),
		event("e4", "Deployment", "web", "Normal", "ScalingReplicaSet", now.Add(-1*time.Minute), 1),
		event("e5", "Pod", "other-123", "Warning", "Failed", now, 1),                                // another workload
		event("e6", "Pod", "web-7d9f8c6b5-x7k2m", "Warning", "Unhealthy", now.Add(-3*time.Hour), 1), // too old
		event("e7", "Deployment", "webshop", "Normal", "Scaled", now, 1),                            // name is a prefix but not a parent
		event("e8", "Deployment", "web", "Warning", "FailedCreate", now, 1),
		event("e9", "StatefulSet", "web", "Normal", "SuccessfulCreate", now, 1), // would own "web-0", not these pods
	)
	c := NewClient(core, newDynamic(), "test")

	got, err := c.Events(context.Background(), "shop", deploymentPods("web-7d9f8c6b5-x7k2m", "web-7d9f8c6b5-pq2r4"), now.Add(-time.Hour), time.Time{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	want := "Deployment/web FailedCreate,Deployment/web ScalingReplicaSet,ReplicaSet/web-7d9f8c6b5 SuccessfulCreate,Pod/web-7d9f8c6b5-x7k2m BackOff,Pod/web-7d9f8c6b5-pq2r4 Pulled"
	if got := objects(got); got != want {
		t.Fatalf("Events = %s\nwant     %s", got, want)
	}
	if got[3].Count != 12 || got[3].Message != "BackOff happened" || got[3].Type != "Warning" {
		t.Errorf("backoff event = %+v", got[3])
	}

	// The pods of "web-admin" belong to web-admin, not to "web".
	admin := Workload{Pods: []Pod{{Name: "web-admin-5c6d7b8f9-qwe4z", Owner: "ReplicaSet/web-admin-5c6d7b8f9"}}}
	got, err = c.Events(context.Background(), "shop", admin, now.Add(-time.Hour), time.Time{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("events for web-admin's pod = %+v, want none of web's", got)
	}

	// The limit keeps the newest.
	if got, _ := c.Events(context.Background(), "shop", deploymentPods("web-7d9f8c6b5-x7k2m"), now.Add(-time.Hour), time.Time{}, 1); len(got) != 1 || got[0].Object != "Deployment/web" {
		t.Errorf("limited = %+v", got)
	}
}

func TestEventsOfSiblingsByOwner(t *testing.T) {
	now := time.Date(2026, 10, 12, 20, 0, 0, 0, time.UTC)
	core := fake.NewSimpleClientset(
		event("e1", "Pod", "web-7d9f8c6b5-x7k2m", "Warning", "Unhealthy", now.Add(-5*time.Minute), 1),
		event("e2", "Pod", "web-7d9f8c6b5-zz9wq", "Normal", "Killing", now.Add(-20*time.Minute), 1), // replaced, same ReplicaSet
		event("e3", "Pod", "web-5b2c8f7d6-pq2r4", "Normal", "Killing", now.Add(-25*time.Minute), 1), // an earlier ReplicaSet of the Deployment
		event("e4", "ReplicaSet", "web-5b2c8f7d6", "Normal", "SuccessfulDelete", now.Add(-24*time.Minute), 1),
		event("e5", "Pod", "web-admin-0", "Warning", "BackOff", now.Add(-4*time.Minute), 1),       // StatefulSet web-admin
		event("e6", "Pod", "web-proxy-x7k2m", "Warning", "BackOff", now.Add(-3*time.Minute), 1),   // DaemonSet web-proxy
		event("e7", "Pod", "web-migrate-k2x9p", "Warning", "BackOff", now.Add(-3*time.Minute), 1), // Job web-migrate
		event("e8", "Pod", "web-db-2", "Warning", "BackOff", now.Add(-3*time.Minute), 1),          // StatefulSet web-db
		event("e9", "Pod", "web-0", "Warning", "BackOff", now.Add(-3*time.Minute), 1),             // StatefulSet web
		event("e10", "Pod", "web-7d9f8c6b5-x7k2m", "Normal", "Started", now.Add(-40*time.Minute), 1),
	)
	c := NewClient(core, newDynamic(), "test")

	got, err := c.Events(context.Background(), "shop", deploymentPods("web-7d9f8c6b5-x7k2m"), now.Add(-time.Hour), time.Time{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	want := "Pod/web-7d9f8c6b5-x7k2m Unhealthy,Pod/web-7d9f8c6b5-zz9wq Killing,ReplicaSet/web-5b2c8f7d6 SuccessfulDelete,Pod/web-5b2c8f7d6-pq2r4 Killing,Pod/web-7d9f8c6b5-x7k2m Started"
	if got := objects(got); got != want {
		t.Errorf("deployment's events = %s\nwant %s", got, want)
	}

	// The StatefulSet web-db's pods are not web's, and the reverse.
	db := Workload{Pods: []Pod{{Name: "web-db-0", Owner: "StatefulSet/web-db"}}}
	got, _ = c.Events(context.Background(), "shop", db, now.Add(-time.Hour), time.Time{}, 10)
	if got := objects(got); got != "Pod/web-db-2 BackOff" {
		t.Errorf("statefulset's events = %s, want its sibling only", got)
	}

	// A manager named on the workload matches even with no pods left,
	// which is what a workload scaled to zero looks like.
	scaled := Workload{Manager: "Deployment/web"}
	got, _ = c.Events(context.Background(), "shop", scaled, now.Add(-time.Hour), time.Time{}, 10)
	if got := objects(got); !strings.Contains(got, "ReplicaSet/web-5b2c8f7d6 SuccessfulDelete") || !strings.Contains(got, "Pod/web-7d9f8c6b5-zz9wq Killing") {
		t.Errorf("scaled-to-zero deployment's events = %s", got)
	}

	// A pod without a known owner matches by name only, and its managers
	// by the shape of its name.
	bare := Workload{Pods: []Pod{{Name: "web-7d9f8c6b5-x7k2m"}}}
	got, _ = c.Events(context.Background(), "shop", bare, now.Add(-time.Hour), time.Time{}, 10)
	if got := objects(got); got != "Pod/web-7d9f8c6b5-x7k2m Unhealthy,Pod/web-7d9f8c6b5-x7k2m Started" {
		t.Errorf("ownerless pod's events = %s", got)
	}
}

func TestChildOf(t *testing.T) {
	cases := []struct {
		pod     string
		manager manager
		want    bool
	}{
		{"web-7d9f8c6b5-x7k2m", manager{"Deployment", "web"}, true},
		{"web-5b2c8f7d6-pq2r4", manager{"Deployment", "web"}, true},
		{"web-admin-0", manager{"Deployment", "web"}, false},
		{"web-proxy-x7k2m", manager{"Deployment", "web"}, false},
		{"web-migrate-k2x9p", manager{"Deployment", "web"}, false},
		{"web-db-2", manager{"Deployment", "web"}, false},
		{"web-grpc-x7k2m", manager{"Deployment", "web"}, false}, // a four-letter word is no hash
		{"web-7d9f8c6b5-x7k2m", manager{"ReplicaSet", "web-7d9f8c6b5"}, true},
		{"web-7d9f8c6b5-x7k2", manager{"ReplicaSet", "web-7d9f8c6b5"}, false},
		{"web-0", manager{"StatefulSet", "web"}, true},
		{"web-12", manager{"StatefulSet", "web"}, true},
		{"web-db-0", manager{"StatefulSet", "web"}, false},
		{"api-db-0", manager{"Deployment", "api"}, false},
		{"api-6c9d7b8f5-x2k4p", manager{"StatefulSet", "api-db"}, false},
		{"node-x7k2m", manager{"DaemonSet", "node"}, true},
		{"app-3-xk2p9", manager{"DeploymentConfig", "app"}, true},
	}
	for _, c := range cases {
		if got := childOf(c.pod, c.manager); got != c.want {
			t.Errorf("childOf(%q, %v) = %v, want %v", c.pod, c.manager, got, c.want)
		}
	}
}

func TestEventsWindowAndRepeatingEvents(t *testing.T) {
	now := time.Date(2026, 10, 12, 20, 0, 0, 0, time.UTC)
	core := fake.NewSimpleClientset(
		event("e1", "Pod", "web-7d9f8c6b5-x7k2m", "Warning", "Unhealthy", now.Add(-5*time.Minute), 1),
		event("e2", "Pod", "web-7d9f8c6b5-zz9wq", "Normal", "Killing", now.Add(-20*time.Minute), 1),
		event("e3", "Pod", "web-5b2c8f7d6-pq2r4", "Normal", "Killing", now.Add(-25*time.Minute), 1),
		event("e4", "Pod", "web-7d9f8c6b5-x7k2m", "Normal", "Started", now.Add(-40*time.Minute), 1),
	)
	c := NewClient(core, newDynamic(), "test")
	web := deploymentPods("web-7d9f8c6b5-x7k2m")

	// A window: from 30 minutes ago until 10 minutes ago.
	got, err := c.Events(context.Background(), "shop", web, now.Add(-30*time.Minute), now.Add(-10*time.Minute), 10)
	if err != nil {
		t.Fatal(err)
	}
	if got := objects(got); got != "Pod/web-7d9f8c6b5-zz9wq Killing,Pod/web-5b2c8f7d6-pq2r4 Killing" {
		t.Errorf("windowed events = %s", got)
	}

	// A repeating event is one object whose first and last times span the
	// window: it happened in it, although its last time is after the end.
	repeating := event("e5", "Pod", "web-7d9f8c6b5-x7k2m", "Warning", "Unhealthy", now.Add(-2*time.Minute), 77)
	repeating.FirstTimestamp = metav1.NewTime(now.Add(-45 * time.Minute))
	if _, err := core.CoreV1().Events("shop").Create(context.Background(), repeating, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	got, _ = c.Events(context.Background(), "shop", web, now.Add(-30*time.Minute), now.Add(-10*time.Minute), 10)
	if len(got) != 3 || got[0].Reason != "Unhealthy" || got[0].Count != 77 {
		t.Errorf("windowed events with a repeating one = %+v", got)
	}
	// ...but not when it began after the window ended.
	if got, _ := c.Events(context.Background(), "shop", web, now.Add(-70*time.Minute), now.Add(-50*time.Minute), 10); len(got) != 0 {
		t.Errorf("events before the repeating one began = %+v", got)
	}

	// The events.k8s.io form: EventTime and a Series.
	series := &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Namespace: "shop", Name: "e6"},
		InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "web-7d9f8c6b5-x7k2m"},
		Type:           "Warning", Reason: "FailedScheduling", Message: "no nodes",
		EventTime: metav1.NewMicroTime(now.Add(-28 * time.Minute)),
		Series:    &corev1.EventSeries{Count: 9, LastObservedTime: metav1.NewMicroTime(now.Add(-26 * time.Minute))},
	}
	if _, err := core.CoreV1().Events("shop").Create(context.Background(), series, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	got, _ = c.Events(context.Background(), "shop", web, now.Add(-27*time.Minute), now.Add(-10*time.Minute), 10)
	var found *Event
	for i := range got {
		if got[i].Reason == "FailedScheduling" {
			found = &got[i]
		}
	}
	if found == nil || found.Count != 9 || !found.At.Equal(now.Add(-26*time.Minute)) {
		t.Errorf("series event = %+v (all: %s)", found, objects(got))
	}
}

func TestEventListIsCachedBriefly(t *testing.T) {
	now := time.Now()
	core := fake.NewSimpleClientset(event("e1", "Pod", "web-7d9f8c6b5-x7k2m", "Warning", "BackOff", now, 1))
	c := NewClient(core, newDynamic(), "test")
	c.CacheEvents(time.Minute)
	web := deploymentPods("web-7d9f8c6b5-x7k2m")
	if got, _ := c.Events(context.Background(), "shop", web, now.Add(-time.Hour), time.Time{}, 10); len(got) != 1 {
		t.Fatalf("first read = %+v", got)
	}
	// A new event is not seen until the cache expires...
	if _, err := core.CoreV1().Events("shop").Create(context.Background(), event("e2", "Pod", "web-7d9f8c6b5-x7k2m", "Normal", "Started", now, 1), metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.Events(context.Background(), "shop", web, now.Add(-time.Hour), time.Time{}, 10); len(got) != 1 {
		t.Errorf("cached read = %+v, want the one event", got)
	}
	// ...and is seen by a client that does not cache, or once it has.
	c.CacheEvents(0)
	if got, _ := c.Events(context.Background(), "shop", web, now.Add(-time.Hour), time.Time{}, 10); len(got) != 2 {
		t.Errorf("uncached read = %+v, want both", got)
	}
}
