package k8s

import (
	"context"
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func statusPod(name string, lbls map[string]string, ready bool, statuses ...corev1.ContainerStatus) *corev1.Pod {
	cond := corev1.ConditionFalse
	if ready {
		cond = corev1.ConditionTrue
	}
	start := metav1.NewTime(time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC))
	containers := make([]corev1.Container, len(statuses))
	return &corev1.Pod{
		ObjectMeta: objMeta("shop", name, lbls),
		Spec:       corev1.PodSpec{NodeName: "node-1", Containers: containers},
		Status: corev1.PodStatus{
			Phase:             corev1.PodRunning,
			StartTime:         &start,
			Conditions:        []corev1.PodCondition{{Type: corev1.PodReady, Status: cond}},
			ContainerStatuses: statuses,
		},
	}
}

func running(restarts int32) corev1.ContainerStatus {
	return corev1.ContainerStatus{Ready: true, RestartCount: restarts, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}
}

func waiting(reason string, restarts int32) corev1.ContainerStatus {
	return corev1.ContainerStatus{RestartCount: restarts, State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: reason}}}
}

func TestWorkloadOfAService(t *testing.T) {
	api := map[string]string{"app": "api"}
	terminating := statusPod("api-old", api, false, running(0))
	now := metav1.Now()
	terminating.DeletionTimestamp = &now
	pending := &corev1.Pod{ObjectMeta: objMeta("shop", "api-new", api), Spec: corev1.PodSpec{Containers: make([]corev1.Container, 1)},
		Status: corev1.PodStatus{Phase: corev1.PodPending}}

	core := fake.NewSimpleClientset(
		service("api", api, corev1.ServicePort{Port: 80}),
		statusPod("api-b", api, true, running(2), running(1)),
		statusPod("api-a", api, true, running(0)),
		statusPod("api-crash", api, false, waiting("CrashLoopBackOff", 17)),
		statusPod("api-half", api, false, running(0), waiting("ImagePullBackOff", 0)),
		terminating,
		pending,
		statusPod("other", map[string]string{"app": "web"}, true, running(0)),
		&corev1.Pod{ObjectMeta: objMeta("billing", "api-x", api)}, // same labels, another namespace
	)
	c := NewClient(core, newDynamic(), "test")

	w, err := c.Workload(context.Background(), "shop", "service/api:80")
	if err != nil {
		t.Fatal(err)
	}
	if w.Service != "api" || len(w.Pods) != 6 {
		t.Fatalf("workload = %+v, want service api with 6 pods", w)
	}
	var names []string
	byName := map[string]Pod{}
	for _, p := range w.Pods {
		names = append(names, p.Name)
		byName[p.Name] = p
	}
	// Not-ready pods first, each group by name.
	want := []string{"api-crash", "api-half", "api-new", "api-old", "api-a", "api-b"}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("order = %v, want %v", names, want)
		}
	}

	if p := byName["api-b"]; !p.Ready || p.ReadyContainers != 2 || p.Containers != 2 || p.Restarts != 3 || p.Status != "Running" || p.Node != "node-1" || p.StartedAt.IsZero() {
		t.Errorf("api-b = %+v", p)
	}
	if p := byName["api-crash"]; p.Ready || p.Status != "CrashLoopBackOff" || p.Restarts != 17 || p.ReadyContainers != 0 {
		t.Errorf("api-crash = %+v", p)
	}
	if p := byName["api-half"]; p.Ready || p.Status != "ImagePullBackOff" || p.ReadyContainers != 1 || p.Containers != 2 {
		t.Errorf("api-half = %+v", p)
	}
	if p := byName["api-old"]; p.Status != "Terminating" {
		t.Errorf("api-old = %+v", p)
	}
	if p := byName["api-new"]; p.Ready || p.Status != "Pending" || !p.StartedAt.IsZero() {
		t.Errorf("api-new = %+v", p)
	}
}

func TestWorkloadThroughRouteAndIngress(t *testing.T) {
	prefix := networkingv1.PathTypePrefix
	backend := func(name string) networkingv1.IngressBackend {
		return networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: name}}
	}
	core := fake.NewSimpleClientset(
		service("web", map[string]string{"app": "web"}, corev1.ServicePort{Port: 80}),
		service("api", map[string]string{"app": "api"}, corev1.ServicePort{Port: 80}),
		statusPod("web-1", map[string]string{"app": "web"}, true, running(0)),
		statusPod("api-1", map[string]string{"app": "api"}, true, running(0)),
		&networkingv1.Ingress{ObjectMeta: objMeta("shop", "front", nil), Spec: networkingv1.IngressSpec{Rules: []networkingv1.IngressRule{{
			Host: "shop.example.com",
			IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{
				{Path: "/", PathType: &prefix, Backend: backend("web")},
				{Path: "/api", PathType: &prefix, Backend: backend("api")},
			}}},
		}}}},
	)
	route := routeObject("store", "store.apps.example.com", "", true)
	route.Object["spec"].(map[string]any)["to"] = map[string]any{"kind": "Service", "name": "web"}
	c := NewClient(core, newDynamic(route), "test")
	ctx := context.Background()

	for source, wantPod := range map[string]string{
		"route/store":                        "web-1",
		"ingress/front@shop.example.com/":    "web-1",
		"ingress/front@shop.example.com/api": "api-1",
	} {
		w, err := c.Workload(ctx, "shop", source)
		if err != nil {
			t.Errorf("%s: %v", source, err)
			continue
		}
		if len(w.Pods) != 1 || w.Pods[0].Name != wantPod {
			t.Errorf("%s: pods = %+v, want %s", source, w.Pods, wantPod)
		}
	}
}

func TestWorkloadUnavailable(t *testing.T) {
	core := fake.NewSimpleClientset(
		service("external", nil, corev1.ServicePort{Port: 5432}), // no selector
		&networkingv1.Ingress{ObjectMeta: objMeta("shop", "front", nil)},
	)
	c := NewClient(core, newDynamic(routeObject("orphan", "x.example.com", "", false)), "test")

	for _, source := range []string{
		"service/gone:80",                    // deleted since the monitor was created
		"service/external:5432",              // nothing to select pods with
		"route/missing",                      // deleted route
		"route/orphan",                       // route without a target
		"ingress/missing@a.example.com/",     // deleted ingress
		"ingress/front@shop.example.com/api", // rule no longer there
		"configmap/x",
		"",
		"service/",
	} {
		_, err := c.Workload(context.Background(), "shop", source)
		if !errors.Is(err, ErrNoWorkload) {
			t.Errorf("%q: err = %v, want ErrNoWorkload", source, err)
		}
	}
}
