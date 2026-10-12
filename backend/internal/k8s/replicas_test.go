package k8s

import (
	"context"
	"errors"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

func selectorFor(app string) *metav1.LabelSelector {
	return &metav1.LabelSelector{MatchLabels: map[string]string{"app": app}}
}

func replicas(n int32) *int32 { return &n }

func readyPod(name string, lbls map[string]string, ready bool) *corev1.Pod {
	status := corev1.ConditionFalse
	if ready {
		status = corev1.ConditionTrue
	}
	return &corev1.Pod{
		ObjectMeta: objMeta("shop", name, lbls),
		Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: status}}},
	}
}

func workloadCluster() *Client {
	core := fake.NewSimpleClientset(
		&appsv1.Deployment{ObjectMeta: objMeta("shop", "web", nil),
			Spec:   appsv1.DeploymentSpec{Replicas: replicas(3), Selector: selectorFor("web")},
			Status: appsv1.DeploymentStatus{ReadyReplicas: 2}},
		// Replicas left out means one.
		&appsv1.Deployment{ObjectMeta: objMeta("shop", "worker", nil),
			Spec: appsv1.DeploymentSpec{Selector: selectorFor("worker")}, Status: appsv1.DeploymentStatus{ReadyReplicas: 1}},
		&appsv1.StatefulSet{ObjectMeta: objMeta("shop", "db", nil),
			Spec:   appsv1.StatefulSetSpec{Replicas: replicas(2), Selector: selectorFor("db")},
			Status: appsv1.StatefulSetStatus{ReadyReplicas: 2}},
		&appsv1.DaemonSet{ObjectMeta: objMeta("shop", "agent", nil),
			Spec:   appsv1.DaemonSetSpec{Selector: selectorFor("agent")},
			Status: appsv1.DaemonSetStatus{DesiredNumberScheduled: 5, NumberReady: 4}},
		&appsv1.Deployment{ObjectMeta: objMeta("other", "web", nil), Spec: appsv1.DeploymentSpec{Replicas: replicas(9), Selector: selectorFor("web")}},

		readyPod("web-1", map[string]string{"app": "web"}, true),
		readyPod("web-2", map[string]string{"app": "web"}, false),
		readyPod("db-0", map[string]string{"app": "db"}, true),
		&corev1.Pod{ObjectMeta: objMeta("other", "web-x", map[string]string{"app": "web"})},
	)
	return NewClient(core, newDynamic(), "test")
}

func TestReplicas(t *testing.T) {
	c := workloadCluster()
	ctx := context.Background()
	for target, want := range map[string][2]int{
		"shop/deployment/web":    {3, 2},
		"shop/deployment/worker": {1, 1},
		"shop/statefulset/db":    {2, 2},
		"shop/daemonset/agent":   {5, 4},
		"other/deployment/web":   {9, 0},
	} {
		desired, ready, err := c.Replicas(ctx, target)
		if err != nil || desired != want[0] || ready != want[1] {
			t.Errorf("%s: %d desired, %d ready, %v; want %v", target, desired, ready, err, want)
		}
	}
	if _, _, err := c.Replicas(ctx, "shop/deployment/gone"); !errors.Is(err, ErrNoWorkload) {
		t.Errorf("missing workload: err = %v, want ErrNoWorkload", err)
	}
	for _, bad := range []string{"", "shop/web", "shop/pod/web", "shop/deployment/", "/deployment/web", "a/deployment/b/c"} {
		if _, _, err := c.Replicas(ctx, bad); err == nil {
			t.Errorf("target %q was accepted", bad)
		}
	}
	if got := WorkloadTarget("shop", KindStatefulSet, "db"); got != "shop/statefulset/db" {
		t.Errorf("WorkloadTarget = %q", got)
	}
}

func TestWorkloadCandidates(t *testing.T) {
	got, err := workloadCluster().Candidates(context.Background(), "shop")
	if err != nil {
		t.Fatal(err)
	}
	var sources []string
	for _, c := range got {
		sources = append(sources, c.Source())
		if c.Type != store.MonitorWorkload || c.Reason != ReasonWorkload || c.Namespace != "shop" {
			t.Errorf("candidate = %+v", c)
		}
	}
	// By kind, then by name; nothing from the other namespace.
	want := []string{"deployment/web", "deployment/worker", "statefulset/db", "daemonset/agent"}
	if len(sources) != len(want) {
		t.Fatalf("sources = %v, want %v", sources, want)
	}
	for i := range want {
		if sources[i] != want[i] {
			t.Errorf("sources = %v, want %v", sources, want)
			break
		}
	}
	if c := find(t, got, "statefulset/db"); c.Target != "shop/statefulset/db" {
		t.Errorf("target = %q", c.Target)
	}
}

// The pods of a workload are found through its own selector.
func TestPodsOfAWorkload(t *testing.T) {
	c := workloadCluster()
	w, err := c.Workload(context.Background(), "shop", "deployment/web")
	if err != nil {
		t.Fatal(err)
	}
	// The pod that is not ready comes first; the other namespace stays out.
	if w.Service != "" || len(w.Pods) != 2 || w.Pods[0].Name != "web-2" || w.Pods[0].Ready || w.Pods[1].Name != "web-1" {
		t.Errorf("workload = %+v", w)
	}
	if w, err := c.Workload(context.Background(), "shop", "statefulset/db"); err != nil || len(w.Pods) != 1 {
		t.Errorf("statefulset pods = %+v, %v", w, err)
	}
	if _, err := c.Workload(context.Background(), "shop", "deployment/gone"); !errors.Is(err, ErrNoWorkload) {
		t.Errorf("missing workload: err = %v, want ErrNoWorkload", err)
	}
}
