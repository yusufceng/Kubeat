// Package k8s discovers what can be monitored in a Kubernetes or OpenShift
// cluster. It only reads from the cluster.
package k8s

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// Kind is the type of cluster object a candidate comes from.
type Kind string

const (
	KindService Kind = "service"
	KindIngress Kind = "ingress"
	KindRoute   Kind = "route"
)

// Candidate is something in the cluster that could be monitored.
type Candidate struct {
	Kind      Kind
	Namespace string
	Name      string
	// Port is the service port; 0 for ingresses and routes.
	Port int32
	// Type and Target are the check proposed for it.
	Type   store.MonitorType
	Target string
	// Reason says how the check was chosen, for display.
	Reason string
}

// Source identifies the candidate within its namespace. It is stored on the
// monitor created from it, so the two can be matched later.
func (c Candidate) Source() string {
	switch c.Kind {
	case KindService:
		return fmt.Sprintf("%s/%s:%d", c.Kind, c.Name, c.Port)
	case KindIngress:
		// One ingress can expose several hosts and paths.
		return fmt.Sprintf("%s/%s@%s", c.Kind, c.Name, strings.SplitN(c.Target, "://", 2)[1])
	default:
		return fmt.Sprintf("%s/%s", c.Kind, c.Name)
	}
}

// Reasons a check was proposed.
const (
	ReasonReadinessProbe = "readiness-probe"
	ReasonLivenessProbe  = "liveness-probe"
	ReasonTCPFallback    = "tcp-fallback"
	ReasonRouteHost      = "route-host"
	ReasonIngressRule    = "ingress-rule"
)

var routeGVR = schema.GroupVersionResource{Group: "route.openshift.io", Version: "v1", Resource: "routes"}

// Client reads from one cluster.
type Client struct {
	core    kubernetes.Interface
	dynamic dynamic.Interface
	// Mode describes how the client connected, e.g. "in-cluster".
	Mode string
}

// NewClient wraps existing clientsets; used by tests.
func NewClient(core kubernetes.Interface, dyn dynamic.Interface, mode string) *Client {
	return &Client{core: core, dynamic: dyn, Mode: mode}
}

// Options configures Connect.
type Options struct {
	// Kubeconfig and Context select a cluster when not running inside one.
	// Context is required with Kubeconfig: the file's current context is
	// never used implicitly, since it may point at an unrelated cluster.
	Kubeconfig string
	Context    string
	Timeout    time.Duration
}

// ErrNotConfigured means there is no cluster to connect to: the process is
// not running in a pod and no kubeconfig was given.
var ErrNotConfigured = errors.New("k8s: not running in a cluster and no kubeconfig configured")

// Connect prefers the in-cluster service account and falls back to the given
// kubeconfig and context.
func Connect(opts Options) (*Client, error) {
	cfg, err := rest.InClusterConfig()
	mode := "in-cluster"
	if err != nil {
		if opts.Kubeconfig == "" {
			return nil, ErrNotConfigured
		}
		if opts.Context == "" {
			return nil, errors.New("k8s: a kubeconfig needs an explicit context")
		}
		cfg, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			&clientcmd.ClientConfigLoadingRules{ExplicitPath: opts.Kubeconfig},
			&clientcmd.ConfigOverrides{CurrentContext: opts.Context},
		).ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("k8s: load kubeconfig: %w", err)
		}
		mode = "kubeconfig:" + opts.Context
	}
	cfg.Timeout = opts.Timeout

	core, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("k8s: %w", err)
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("k8s: %w", err)
	}
	return &Client{core: core, dynamic: dyn, Mode: mode}, nil
}

// Namespaces lists namespace names in alphabetical order.
func (c *Client) Namespaces(ctx context.Context) ([]string, error) {
	list, err := c.core.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list namespaces: %w", err)
	}
	names := make([]string, 0, len(list.Items))
	for _, ns := range list.Items {
		names = append(names, ns.Name)
	}
	sort.Strings(names)
	return names, nil
}

// Candidates lists everything monitorable in a namespace: service ports,
// ingress rules and, on OpenShift, routes.
func (c *Client) Candidates(ctx context.Context, namespace string) ([]Candidate, error) {
	services, err := c.serviceCandidates(ctx, namespace)
	if err != nil {
		return nil, err
	}
	ingresses, err := c.ingressCandidates(ctx, namespace)
	if err != nil {
		return nil, err
	}
	routes, err := c.routeCandidates(ctx, namespace)
	if err != nil {
		return nil, err
	}

	all := append(append(routes, ingresses...), services...)
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Kind != all[j].Kind {
			return kindOrder[all[i].Kind] < kindOrder[all[j].Kind]
		}
		return all[i].Source() < all[j].Source()
	})
	return all, nil
}

// Externally reachable entry points first.
var kindOrder = map[Kind]int{KindRoute: 0, KindIngress: 1, KindService: 2}

func (c *Client) serviceCandidates(ctx context.Context, namespace string) ([]Candidate, error) {
	services, err := c.core.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	pods, err := c.core.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}

	var out []Candidate
	for _, svc := range services.Items {
		if svc.Spec.Type == corev1.ServiceTypeExternalName {
			continue // an alias for something outside the cluster
		}
		backing := podsFor(svc, pods.Items)
		host := fmt.Sprintf("%s.%s.svc", svc.Name, svc.Namespace)

		for _, port := range svc.Spec.Ports {
			if port.Protocol != "" && port.Protocol != corev1.ProtocolTCP {
				continue
			}
			cand := Candidate{
				Kind: KindService, Namespace: svc.Namespace, Name: svc.Name, Port: port.Port,
				Type:   store.MonitorTCP,
				Target: fmt.Sprintf("%s:%d", host, port.Port),
				Reason: ReasonTCPFallback,
			}
			// The application's own probe says which URL means "healthy";
			// guessing a path would raise false alarms on a 404.
			if probe, ok := httpProbeFor(port, backing); ok {
				cand.Type = store.MonitorHTTP
				cand.Target = fmt.Sprintf("%s://%s:%d%s", probe.scheme, host, port.Port, probe.path)
				cand.Reason = probe.reason
			}
			out = append(out, cand)
		}
	}
	return out, nil
}

// podsFor returns the pods a service selects.
func podsFor(svc corev1.Service, pods []corev1.Pod) []corev1.Pod {
	if len(svc.Spec.Selector) == 0 {
		return nil
	}
	selector := labels.SelectorFromSet(svc.Spec.Selector)
	var out []corev1.Pod
	for _, pod := range pods {
		if selector.Matches(labels.Set(pod.Labels)) {
			out = append(out, pod)
		}
	}
	return out
}

type httpProbe struct {
	scheme, path, reason string
}

// httpProbeFor finds an HTTP readiness (or else liveness) probe that targets
// the container port behind the given service port.
func httpProbeFor(port corev1.ServicePort, pods []corev1.Pod) (httpProbe, bool) {
	var liveness *httpProbe
	for _, pod := range pods {
		for _, container := range pod.Spec.Containers {
			target, ok := containerPort(port, container)
			if !ok {
				continue
			}
			if p, ok := probeOn(container.ReadinessProbe, container, target); ok {
				p.reason = ReasonReadinessProbe
				return p, true
			}
			if p, ok := probeOn(container.LivenessProbe, container, target); ok && liveness == nil {
				p.reason = ReasonLivenessProbe
				liveness = &p
			}
		}
	}
	if liveness != nil {
		return *liveness, true
	}
	return httpProbe{}, false
}

// containerPort resolves which port of the container a service port reaches.
func containerPort(port corev1.ServicePort, container corev1.Container) (int32, bool) {
	switch {
	case port.TargetPort.Type == intstr.String:
		return namedPort(container, port.TargetPort.StrVal)
	case port.TargetPort.IntVal != 0:
		return port.TargetPort.IntVal, true
	default:
		return port.Port, true // targetPort defaults to port
	}
}

func namedPort(container corev1.Container, name string) (int32, bool) {
	for _, p := range container.Ports {
		if p.Name == name {
			return p.ContainerPort, true
		}
	}
	return 0, false
}

func probeOn(probe *corev1.Probe, container corev1.Container, target int32) (httpProbe, bool) {
	if probe == nil || probe.HTTPGet == nil {
		return httpProbe{}, false
	}
	probePort := probe.HTTPGet.Port.IntVal
	if probe.HTTPGet.Port.Type == intstr.String {
		var ok bool
		if probePort, ok = namedPort(container, probe.HTTPGet.Port.StrVal); !ok {
			return httpProbe{}, false
		}
	}
	if probePort != target {
		return httpProbe{}, false
	}

	p := httpProbe{scheme: "http", path: probe.HTTPGet.Path}
	if probe.HTTPGet.Scheme == corev1.URISchemeHTTPS {
		p.scheme = "https"
	}
	if !strings.HasPrefix(p.path, "/") {
		p.path = "/" + p.path
	}
	return p, true
}

func (c *Client) ingressCandidates(ctx context.Context, namespace string) ([]Candidate, error) {
	list, err := c.core.NetworkingV1().Ingresses(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list ingresses: %w", err)
	}

	var out []Candidate
	for _, ing := range list.Items {
		tlsHosts := make(map[string]bool)
		for _, tls := range ing.Spec.TLS {
			for _, h := range tls.Hosts {
				tlsHosts[h] = true
			}
		}
		for _, rule := range ing.Spec.Rules {
			// A rule without a host or with a wildcard has no single URL to check.
			if rule.Host == "" || strings.Contains(rule.Host, "*") || rule.HTTP == nil {
				continue
			}
			scheme := "http"
			if tlsHosts[rule.Host] {
				scheme = "https"
			}
			seen := make(map[string]bool)
			for _, path := range rule.HTTP.Paths {
				p := plainPath(path.Path)
				if seen[p] {
					continue
				}
				seen[p] = true
				out = append(out, Candidate{
					Kind: KindIngress, Namespace: ing.Namespace, Name: ing.Name,
					Type:   store.MonitorHTTP,
					Target: fmt.Sprintf("%s://%s%s", scheme, rule.Host, p),
					Reason: ReasonIngressRule,
				})
			}
		}
	}
	return out, nil
}

// plainPath returns the path if it is a literal one, and "/" for empty or
// pattern paths (controllers allow regular expressions there).
func plainPath(p string) string {
	if !strings.HasPrefix(p, "/") || strings.ContainsAny(p, "*()[]$^|\\ ") {
		return "/"
	}
	return p
}

func (c *Client) routeCandidates(ctx context.Context, namespace string) ([]Candidate, error) {
	list, err := c.dynamic.Resource(routeGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
		return nil, nil // not an OpenShift cluster
	}
	if err != nil {
		return nil, fmt.Errorf("list routes: %w", err)
	}

	var out []Candidate
	for _, route := range list.Items {
		host, _, _ := unstructured.NestedString(route.Object, "spec", "host")
		if host == "" {
			continue
		}
		path, _, _ := unstructured.NestedString(route.Object, "spec", "path")
		_, tls, _ := unstructured.NestedMap(route.Object, "spec", "tls")
		scheme := "http"
		if tls {
			scheme = "https"
		}
		out = append(out, Candidate{
			Kind: KindRoute, Namespace: route.GetNamespace(), Name: route.GetName(),
			Type:   store.MonitorHTTP,
			Target: fmt.Sprintf("%s://%s%s", scheme, host, plainPath(path)),
			Reason: ReasonRouteHost,
		})
	}
	return out, nil
}
