// Package k8s discovers what can be monitored in a Kubernetes or OpenShift
// cluster. It only reads from the cluster.
package k8s

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
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
	KindService     Kind = "service"
	KindIngress     Kind = "ingress"
	KindRoute       Kind = "route"
	KindDeployment  Kind = "deployment"
	KindStatefulSet Kind = "statefulset"
	KindDaemonSet   Kind = "daemonset"
)

// Candidate is something in the cluster that could be monitored.
type Candidate struct {
	Kind      Kind
	Namespace string
	Name      string
	// Port is the service port; 0 for ingresses and routes. PortName is
	// its name in the service, when it has one.
	Port     int32
	PortName string
	// Annotations are those of the cluster object, for the ones Kubeat
	// understands (see the discovery package).
	Annotations map[string]string
	// NoEndpoints is set on a Service candidate whose service has no ready
	// endpoint: a monitor made from it would go straight into an outage.
	NoEndpoints bool
	// Type and Target are the check proposed for it.
	Type   store.MonitorType
	Target string
	// Reason says how the check was chosen, for display.
	Reason string
	// GRPCService is the service name of the gRPC probe a gRPC check was
	// taken from; empty when the probe asks about the server as a whole.
	GRPCService string
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
	ReasonWorkload       = "workload"
)

var routeGVR = schema.GroupVersionResource{Group: "route.openshift.io", Version: "v1", Resource: "routes"}

// Client reads from one cluster.
type Client struct {
	core    kubernetes.Interface
	dynamic dynamic.Interface
	// Mode describes how the client connected, e.g. "in-cluster".
	Mode string
	// only, when set, limits the client to these namespaces: for an
	// installation whose service account was given Roles in them rather
	// than a ClusterRole.
	only []string
}

// Restrict limits the client to the given namespaces; none lifts the limit.
func (c *Client) Restrict(namespaces []string) {
	c.only = slices.Clone(namespaces)
	sort.Strings(c.only)
}

// ErrOutOfScope means a namespace outside the ones the client is limited to.
var ErrOutOfScope = errors.New("k8s: the namespace is outside the configured discovery namespaces")

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
	// Namespaces limits discovery to these namespaces; empty means the
	// whole cluster, which needs cluster-wide read access.
	Namespaces []string
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
	client := &Client{core: core, dynamic: dyn, Mode: mode}
	client.Restrict(opts.Namespaces)
	return client, nil
}

// Namespaces lists namespace names in alphabetical order: the ones the
// client is limited to, or else every namespace of the cluster.
func (c *Client) Namespaces(ctx context.Context) ([]string, error) {
	if len(c.only) > 0 {
		return slices.Clone(c.only), nil
	}
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
// ingress rules, on OpenShift routes, and the workloads themselves. An
// empty namespace lists everything the client may see: the whole cluster,
// or the namespaces it is limited to.
func (c *Client) Candidates(ctx context.Context, namespace string) ([]Candidate, error) {
	if len(c.only) > 0 {
		if namespace != "" {
			if !slices.Contains(c.only, namespace) {
				return nil, fmt.Errorf("%w: %s", ErrOutOfScope, namespace)
			}
			return c.candidates(ctx, namespace)
		}
		var all []Candidate
		for _, ns := range c.only {
			found, err := c.candidates(ctx, ns)
			if err != nil {
				return nil, err
			}
			all = append(all, found...)
		}
		return all, nil
	}
	return c.candidates(ctx, namespace)
}

func (c *Client) candidates(ctx context.Context, namespace string) ([]Candidate, error) {
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

	workloads, err := c.workloadCandidates(ctx, namespace)
	if err != nil {
		return nil, err
	}

	all := append(append(append(routes, ingresses...), services...), workloads...)
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Kind != all[j].Kind {
			return kindOrder[all[i].Kind] < kindOrder[all[j].Kind]
		}
		return all[i].Source() < all[j].Source()
	})
	return all, nil
}

// Externally reachable entry points first, what runs behind them last.
var kindOrder = map[Kind]int{
	KindRoute: 0, KindIngress: 1, KindService: 2, KindDeployment: 3, KindStatefulSet: 4, KindDaemonSet: 5,
}

func (c *Client) serviceCandidates(ctx context.Context, namespace string) ([]Candidate, error) {
	services, err := c.core.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	pods, err := c.core.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}
	ready, err := c.readyEndpoints(ctx, namespace)
	if err != nil {
		return nil, err
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
				Kind: KindService, Namespace: svc.Namespace, Name: svc.Name, Port: port.Port, PortName: port.Name,
				Annotations: svc.Annotations,
				NoEndpoints: ready[svc.Name] == 0,
				Type:        store.MonitorTCP,
				Target:      fmt.Sprintf("%s:%d", host, port.Port),
				Reason:      ReasonTCPFallback,
			}
			// The application's own probe says what "healthy" means: which
			// URL, or which gRPC service. Guessing a path would raise false
			// alarms on a 404.
			if probe, reason, ok := findProbe(port, backing, httpProbeOn); ok {
				cand.Type = store.MonitorHTTP
				cand.Target = fmt.Sprintf("%s://%s:%d%s", probe.scheme, host, port.Port, probe.path)
				cand.Reason = reason
			} else if service, reason, ok := findProbe(port, backing, grpcProbeOn); ok {
				cand.Type = store.MonitorGRPC
				cand.Reason = reason
				cand.GRPCService = service
			}
			out = append(out, cand)
		}
	}
	return out, nil
}

// readyEndpoints counts, per service of the namespace, the ready endpoints
// of its EndpointSlices: what the service would actually route to.
func (c *Client) readyEndpoints(ctx context.Context, namespace string) (map[string]int, error) {
	slices, err := c.core.DiscoveryV1().EndpointSlices(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list endpointslices: %w", err)
	}
	ready := make(map[string]int)
	for _, slice := range slices.Items {
		name := slice.Labels[discoveryv1.LabelServiceName]
		if name == "" {
			continue
		}
		for _, ep := range slice.Endpoints {
			if len(ep.Addresses) > 0 && (ep.Conditions.Ready == nil || *ep.Conditions.Ready) {
				ready[name]++
			}
		}
	}
	return ready, nil
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
	scheme, path string
}

// findProbe looks through the containers behind a service port for a
// readiness (or else liveness) probe that match accepts, and returns what
// match made of it together with the reason to show.
func findProbe[T any](port corev1.ServicePort, pods []corev1.Pod, match func(*corev1.Probe, corev1.Container, int32) (T, bool)) (T, string, bool) {
	var (
		liveness      T
		foundLiveness bool
	)
	for _, pod := range pods {
		for _, container := range pod.Spec.Containers {
			target, ok := containerPort(port, container)
			if !ok {
				continue
			}
			if p, ok := match(container.ReadinessProbe, container, target); ok {
				return p, ReasonReadinessProbe, true
			}
			if p, ok := match(container.LivenessProbe, container, target); ok && !foundLiveness {
				liveness, foundLiveness = p, true
			}
		}
	}
	return liveness, ReasonLivenessProbe, foundLiveness
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

// grpcProbeOn reports the service name of a gRPC probe on the target port.
func grpcProbeOn(probe *corev1.Probe, _ corev1.Container, target int32) (string, bool) {
	if probe == nil || probe.GRPC == nil || probe.GRPC.Port != target {
		return "", false
	}
	if probe.GRPC.Service == nil {
		return "", true
	}
	return *probe.GRPC.Service, true
}

// httpProbeOn reports the scheme and path of an HTTP probe on the target port.
func httpProbeOn(probe *corev1.Probe, container corev1.Container, target int32) (httpProbe, bool) {
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
					Kind: KindIngress, Namespace: ing.Namespace, Name: ing.Name, Annotations: ing.Annotations,
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
			Kind: KindRoute, Namespace: route.GetNamespace(), Name: route.GetName(), Annotations: route.GetAnnotations(),
			Type:   store.MonitorHTTP,
			Target: fmt.Sprintf("%s://%s%s", scheme, host, plainPath(path)),
			Reason: ReasonRouteHost,
		})
	}
	return out, nil
}
