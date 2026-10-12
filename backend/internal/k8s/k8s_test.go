package k8s

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

func objMeta(ns, name string, lbls map[string]string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Namespace: ns, Name: name, Labels: lbls}
}

func pod(name string, lbls map[string]string, containers ...corev1.Container) *corev1.Pod {
	return &corev1.Pod{ObjectMeta: objMeta("shop", name, lbls), Spec: corev1.PodSpec{Containers: containers}}
}

func httpGet(path string, port intstr.IntOrString, scheme corev1.URIScheme) *corev1.Probe {
	return &corev1.Probe{ProbeHandler: corev1.ProbeHandler{
		HTTPGet: &corev1.HTTPGetAction{Path: path, Port: port, Scheme: scheme},
	}}
}

func service(name string, selector map[string]string, ports ...corev1.ServicePort) *corev1.Service {
	return &corev1.Service{ObjectMeta: objMeta("shop", name, nil), Spec: corev1.ServiceSpec{Selector: selector, Ports: ports}}
}

func routeObject(name, host, path string, tls bool) *unstructured.Unstructured {
	spec := map[string]any{"host": host}
	if path != "" {
		spec["path"] = path
	}
	if tls {
		spec["tls"] = map[string]any{"termination": "edge"}
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "route.openshift.io/v1",
		"kind":       "Route",
		"metadata":   map[string]any{"namespace": "shop", "name": name},
		"spec":       spec,
	}}
}

func newDynamic(objs ...runtime.Object) *dynamicfake.FakeDynamicClient {
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{routeGVR: "RouteList"}, objs...)
}

func find(t *testing.T, cands []Candidate, source string) Candidate {
	t.Helper()
	for _, c := range cands {
		if c.Source() == source {
			return c
		}
	}
	t.Fatalf("no candidate with source %q in %+v", source, cands)
	return Candidate{}
}

func TestNamespaces(t *testing.T) {
	core := fake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "shop"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "billing"}},
	)
	got, err := NewClient(core, newDynamic(), "test").Namespaces(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "billing" || got[1] != "shop" {
		t.Errorf("Namespaces = %v, want sorted [billing shop]", got)
	}
}

func TestServiceCandidates(t *testing.T) {
	core := fake.NewSimpleClientset(
		// Readiness probe on a named port that the service reaches by name.
		pod("api-1", map[string]string{"app": "api"}, corev1.Container{
			Ports:          []corev1.ContainerPort{{Name: "http", ContainerPort: 8080}, {Name: "metrics", ContainerPort: 9090}},
			ReadinessProbe: httpGet("/healthz", intstr.FromString("http"), ""),
			LivenessProbe:  httpGet("/livez", intstr.FromInt32(8080), ""),
		}),
		service("api", map[string]string{"app": "api"},
			corev1.ServicePort{Name: "web", Port: 80, TargetPort: intstr.FromString("http")},
			corev1.ServicePort{Name: "metrics", Port: 9090, TargetPort: intstr.FromInt32(9090)},
			corev1.ServicePort{Name: "dns", Port: 53, Protocol: corev1.ProtocolUDP},
		),

		// Only a liveness probe, HTTPS, path without a leading slash.
		pod("admin-1", map[string]string{"app": "admin"}, corev1.Container{
			LivenessProbe: httpGet("status", intstr.FromInt32(8443), corev1.URISchemeHTTPS),
		}),
		service("admin", map[string]string{"app": "admin"}, corev1.ServicePort{Port: 8443}),

		// TCP probe only: no URL to borrow.
		pod("redis-1", map[string]string{"app": "redis"}, corev1.Container{
			ReadinessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt32(6379)}}},
		}),
		service("redis", map[string]string{"app": "redis"}, corev1.ServicePort{Port: 6379}),

		// No selector, so no pods to inspect; and an alias to skip.
		service("external-db", nil, corev1.ServicePort{Port: 5432}),
		&corev1.Service{ObjectMeta: objMeta("shop", "alias", nil), Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeExternalName, ExternalName: "db.example.com", Ports: []corev1.ServicePort{{Port: 5432}}}},

		// Same labels in another namespace must not leak in.
		&corev1.Pod{ObjectMeta: objMeta("other", "redis-x", map[string]string{"app": "redis"}), Spec: corev1.PodSpec{
			Containers: []corev1.Container{{ReadinessProbe: httpGet("/wrong", intstr.FromInt32(6379), "")}}}},
	)

	got, err := NewClient(core, newDynamic(), "test").Candidates(context.Background(), "shop")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("got %d candidates, want 5: %+v", len(got), got)
	}

	web := find(t, got, "service/api:80")
	if web.Type != store.MonitorHTTP || web.Target != "http://api.shop.svc:80/healthz" || web.Reason != ReasonReadinessProbe {
		t.Errorf("api:80 = %+v", web)
	}
	// The probe is on 8080, not on the metrics port.
	if m := find(t, got, "service/api:9090"); m.Type != store.MonitorTCP || m.Target != "api.shop.svc:9090" || m.Reason != ReasonTCPFallback {
		t.Errorf("api:9090 = %+v", m)
	}
	if a := find(t, got, "service/admin:8443"); a.Target != "https://admin.shop.svc:8443/status" || a.Reason != ReasonLivenessProbe {
		t.Errorf("admin:8443 = %+v", a)
	}
	if r := find(t, got, "service/redis:6379"); r.Type != store.MonitorTCP || r.Target != "redis.shop.svc:6379" {
		t.Errorf("redis:6379 = %+v", r)
	}
	if e := find(t, got, "service/external-db:5432"); e.Type != store.MonitorTCP {
		t.Errorf("external-db:5432 = %+v", e)
	}
}

func TestIngressAndRouteCandidates(t *testing.T) {
	prefix := networkingv1.PathTypePrefix
	path := func(p string) networkingv1.HTTPIngressPath {
		return networkingv1.HTTPIngressPath{Path: p, PathType: &prefix}
	}
	rule := func(host string, paths ...networkingv1.HTTPIngressPath) networkingv1.IngressRule {
		return networkingv1.IngressRule{Host: host, IngressRuleValue: networkingv1.IngressRuleValue{
			HTTP: &networkingv1.HTTPIngressRuleValue{Paths: paths}}}
	}
	core := fake.NewSimpleClientset(&networkingv1.Ingress{
		ObjectMeta: objMeta("shop", "web", nil),
		Spec: networkingv1.IngressSpec{
			TLS: []networkingv1.IngressTLS{{Hosts: []string{"shop.example.com"}}},
			Rules: []networkingv1.IngressRule{
				rule("shop.example.com", path("/"), path("/api"), path("/static(/|$)(.*)")),
				rule("plain.example.com", path("")),
				rule("*.wild.example.com", path("/")),
				rule("", path("/")),
			},
		},
	})
	dyn := newDynamic(
		routeObject("store", "store.apps.example.com", "", true),
		routeObject("docs", "docs.apps.example.com", "/guide", false),
		routeObject("pending", "", "", false),
	)

	got, err := NewClient(core, dyn, "test").Candidates(context.Background(), "shop")
	if err != nil {
		t.Fatal(err)
	}
	var targets []string
	for _, c := range got {
		targets = append(targets, string(c.Kind)+" "+c.Target)
		if c.Type != store.MonitorHTTP || c.Port != 0 {
			t.Errorf("%s: Type=%s Port=%d, want http and 0", c.Target, c.Type, c.Port)
		}
	}
	want := []string{
		"route http://docs.apps.example.com/guide",
		"route https://store.apps.example.com/",
		"ingress http://plain.example.com/",
		// The pattern path collapses to "/", which is already listed.
		"ingress https://shop.example.com/",
		"ingress https://shop.example.com/api",
	}
	if len(targets) != len(want) {
		t.Fatalf("targets = %q, want %q", targets, want)
	}
	for i := range want {
		if targets[i] != want[i] {
			t.Errorf("targets[%d] = %q, want %q", i, targets[i], want[i])
		}
	}

	// Sources are unique so each candidate maps to one monitor.
	seen := map[string]bool{}
	for _, c := range got {
		if seen[c.Source()] {
			t.Errorf("duplicate source %q", c.Source())
		}
		seen[c.Source()] = true
	}
	if s := find(t, got, "route/store"); s.Reason != ReasonRouteHost {
		t.Errorf("route/store = %+v", s)
	}
}

func TestClusterWithoutRoutesIsNotAnError(t *testing.T) {
	// A non-OpenShift API server answers 404 for the unknown resource.
	dyn := newDynamic()
	dyn.PrependReactor("list", "routes", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewNotFound(routeGVR.GroupResource(), "")
	})
	core := fake.NewSimpleClientset(service("redis", nil, corev1.ServicePort{Port: 6379}))

	got, err := NewClient(core, dyn, "test").Candidates(context.Background(), "shop")
	if err != nil {
		t.Fatalf("Candidates on a cluster without routes: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("got %+v, want only the service", got)
	}
}

func TestConnectRequiresExplicitContext(t *testing.T) {
	if _, err := Connect(Options{}); err != ErrNotConfigured {
		t.Errorf("no kubeconfig: err = %v, want ErrNotConfigured", err)
	}
	if _, err := Connect(Options{Kubeconfig: "/nonexistent/config"}); err == nil || err == ErrNotConfigured {
		t.Errorf("kubeconfig without context: err = %v, want a refusal", err)
	}
}
