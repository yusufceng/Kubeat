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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
)

// ErrNoWorkload means the pods behind a monitor cannot be determined: its
// source no longer exists, or points at a service without a selector.
var ErrNoWorkload = errors.New("k8s: no workload behind this source")

// Workload is the set of pods that serve a discovered monitor.
type Workload struct {
	// Service is the service the pods were found through.
	Service string
	Pods    []Pod
}

// Pod is the state of one pod, as the cluster reports it.
type Pod struct {
	Name string
	// Ready is the pod's Ready condition: whether it receives traffic.
	Ready           bool
	ReadyContainers int
	Containers      int
	Restarts        int32
	// Status is the pod's phase, or the reason it is not running normally
	// (CrashLoopBackOff, ImagePullBackOff, Terminating, ...).
	Status    string
	StartedAt time.Time // zero if the pod has not started
	Node      string
}

// Workload returns the pods behind a discovered monitor, identified by the
// namespace and source stored on it (see Candidate.Source).
func (c *Client) Workload(ctx context.Context, namespace, source string) (Workload, error) {
	service, err := c.serviceFor(ctx, namespace, source)
	if err != nil {
		return Workload{}, err
	}

	svc, err := c.core.CoreV1().Services(namespace).Get(ctx, service, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return Workload{}, fmt.Errorf("%w: service %s/%s does not exist", ErrNoWorkload, namespace, service)
	}
	if err != nil {
		return Workload{}, fmt.Errorf("get service: %w", err)
	}
	if len(svc.Spec.Selector) == 0 {
		return Workload{}, fmt.Errorf("%w: service %s/%s has no selector", ErrNoWorkload, namespace, service)
	}

	list, err := c.core.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(svc.Spec.Selector).String(),
	})
	if err != nil {
		return Workload{}, fmt.Errorf("list pods: %w", err)
	}

	w := Workload{Service: service, Pods: make([]Pod, 0, len(list.Items))}
	for _, p := range list.Items {
		w.Pods = append(w.Pods, podInfo(p))
	}
	// Pods that need attention first, then by name.
	sort.Slice(w.Pods, func(i, j int) bool {
		if w.Pods[i].Ready != w.Pods[j].Ready {
			return !w.Pods[i].Ready
		}
		return w.Pods[i].Name < w.Pods[j].Name
	})
	return w, nil
}

// serviceFor resolves a monitor's source to the name of the service whose
// pods serve it.
func (c *Client) serviceFor(ctx context.Context, namespace, source string) (string, error) {
	kind, rest, ok := strings.Cut(source, "/")
	if !ok || rest == "" {
		return "", fmt.Errorf("%w: unrecognized source %q", ErrNoWorkload, source)
	}

	switch Kind(kind) {
	case KindService:
		// "name:port"
		name, _, _ := strings.Cut(rest, ":")
		return name, nil

	case KindRoute:
		route, err := c.dynamic.Resource(routeGVR).Namespace(namespace).Get(ctx, rest, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return "", fmt.Errorf("%w: route %s/%s does not exist", ErrNoWorkload, namespace, rest)
		}
		if err != nil {
			return "", fmt.Errorf("get route: %w", err)
		}
		toKind, _, _ := unstructured.NestedString(route.Object, "spec", "to", "kind")
		toName, _, _ := unstructured.NestedString(route.Object, "spec", "to", "name")
		if toName == "" || (toKind != "" && toKind != "Service") {
			return "", fmt.Errorf("%w: route %s/%s does not point at a service", ErrNoWorkload, namespace, rest)
		}
		return toName, nil

	case KindIngress:
		// "name@host/path"
		name, url, _ := strings.Cut(rest, "@")
		ing, err := c.core.NetworkingV1().Ingresses(namespace).Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return "", fmt.Errorf("%w: ingress %s/%s does not exist", ErrNoWorkload, namespace, name)
		}
		if err != nil {
			return "", fmt.Errorf("get ingress: %w", err)
		}
		for _, rule := range ing.Spec.Rules {
			if rule.HTTP == nil {
				continue
			}
			for _, path := range rule.HTTP.Paths {
				if rule.Host+plainPath(path.Path) == url && path.Backend.Service != nil {
					return path.Backend.Service.Name, nil
				}
			}
		}
		return "", fmt.Errorf("%w: ingress %s/%s has no rule for %s", ErrNoWorkload, namespace, name, url)
	}
	return "", fmt.Errorf("%w: unrecognized source %q", ErrNoWorkload, source)
}

func podInfo(p corev1.Pod) Pod {
	info := Pod{
		Name:       p.Name,
		Containers: len(p.Spec.Containers),
		Status:     string(p.Status.Phase),
		Node:       p.Spec.NodeName,
	}
	if p.Status.StartTime != nil {
		info.StartedAt = p.Status.StartTime.UTC()
	}
	for _, cond := range p.Status.Conditions {
		if cond.Type == corev1.PodReady {
			info.Ready = cond.Status == corev1.ConditionTrue
		}
	}
	for _, cs := range p.Status.ContainerStatuses {
		info.Restarts += cs.RestartCount
		if cs.Ready {
			info.ReadyContainers++
		}
		// The reason a container is not running says more than the phase.
		switch {
		case cs.State.Waiting != nil && cs.State.Waiting.Reason != "":
			info.Status = cs.State.Waiting.Reason
		case cs.State.Terminated != nil && cs.State.Terminated.Reason != "" && p.Status.Phase != corev1.PodSucceeded:
			info.Status = cs.State.Terminated.Reason
		}
	}
	if p.Status.Reason != "" {
		info.Status = p.Status.Reason // e.g. Evicted
	}
	if p.DeletionTimestamp != nil {
		info.Status = "Terminating"
	}
	return info
}
