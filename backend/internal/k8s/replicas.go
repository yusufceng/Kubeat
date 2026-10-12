package k8s

import (
	"context"
	"fmt"
	"sort"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// WorkloadKinds are the kinds of object a workload monitor can watch.
var WorkloadKinds = []Kind{KindDeployment, KindStatefulSet, KindDaemonSet}

// IsWorkload reports whether objects of the kind run pods of their own.
func (k Kind) IsWorkload() bool {
	return k == KindDeployment || k == KindStatefulSet || k == KindDaemonSet
}

// WorkloadTarget is the target of a workload monitor: namespace/kind/name.
func WorkloadTarget(namespace string, kind Kind, name string) string {
	return fmt.Sprintf("%s/%s/%s", namespace, kind, name)
}

// ParseWorkloadTarget splits a workload monitor's target into its parts.
func ParseWorkloadTarget(target string) (namespace string, kind Kind, name string, err error) {
	parts := strings.Split(target, "/")
	if len(parts) != 3 || parts[0] == "" || parts[2] == "" || !Kind(parts[1]).IsWorkload() {
		return "", "", "", fmt.Errorf("target must be namespace/kind/name, with the kind deployment, statefulset or daemonset")
	}
	return parts[0], Kind(parts[1]), parts[2], nil
}

// workloadState is what a workload says about its pods.
type workloadState struct {
	desired, ready int
	selector       *metav1.LabelSelector
}

// workload reads a deployment, statefulset or daemonset.
func (c *Client) workload(ctx context.Context, namespace string, kind Kind, name string) (workloadState, error) {
	var (
		st  workloadState
		err error
	)
	apps := c.core.AppsV1()
	switch kind {
	case KindDeployment:
		d, getErr := apps.Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err = getErr; err == nil {
			st = workloadState{desired: 1, ready: int(d.Status.ReadyReplicas), selector: d.Spec.Selector}
			if d.Spec.Replicas != nil {
				st.desired = int(*d.Spec.Replicas)
			}
		}
	case KindStatefulSet:
		s, getErr := apps.StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err = getErr; err == nil {
			st = workloadState{desired: 1, ready: int(s.Status.ReadyReplicas), selector: s.Spec.Selector}
			if s.Spec.Replicas != nil {
				st.desired = int(*s.Spec.Replicas)
			}
		}
	case KindDaemonSet:
		d, getErr := apps.DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err = getErr; err == nil {
			// One pod per node it is meant to run on.
			st = workloadState{desired: int(d.Status.DesiredNumberScheduled), ready: int(d.Status.NumberReady), selector: d.Spec.Selector}
		}
	default:
		return workloadState{}, fmt.Errorf("%w: %q is not a kind of workload", ErrNoWorkload, kind)
	}
	if apierrors.IsNotFound(err) {
		return workloadState{}, fmt.Errorf("%w: %s %s/%s does not exist", ErrNoWorkload, kind, namespace, name)
	}
	if err != nil {
		return workloadState{}, fmt.Errorf("get %s: %w", kind, err)
	}
	return st, nil
}

// Replicas returns how many pods the workload in the target
// (namespace/kind/name) should have and how many of them are ready.
func (c *Client) Replicas(ctx context.Context, target string) (desired, ready int, err error) {
	namespace, kind, name, err := ParseWorkloadTarget(target)
	if err != nil {
		return 0, 0, err
	}
	st, err := c.workload(ctx, namespace, kind, name)
	return st.desired, st.ready, err
}

// workloadSelector returns the selector of a workload's pods.
func (c *Client) workloadSelector(ctx context.Context, namespace string, kind Kind, name string) (labels.Selector, error) {
	st, err := c.workload(ctx, namespace, kind, name)
	if err != nil {
		return nil, err
	}
	selector, err := metav1.LabelSelectorAsSelector(st.selector)
	if err != nil || selector.Empty() {
		return nil, fmt.Errorf("%w: %s %s/%s has no usable selector", ErrNoWorkload, kind, namespace, name)
	}
	return selector, nil
}

// workloadCandidates lists the deployments, statefulsets and daemonsets of
// a namespace: each can be watched for having its pods ready.
func (c *Client) workloadCandidates(ctx context.Context, namespace string) ([]Candidate, error) {
	var out []Candidate
	add := func(kind Kind, name string) {
		out = append(out, Candidate{
			Kind: kind, Namespace: namespace, Name: name,
			Type: store.MonitorWorkload, Target: WorkloadTarget(namespace, kind, name), Reason: ReasonWorkload,
		})
	}
	apps := c.core.AppsV1()

	deployments, err := apps.Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list deployments: %w", err)
	}
	for _, d := range deployments.Items {
		add(KindDeployment, d.Name)
	}
	statefulSets, err := apps.StatefulSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list statefulsets: %w", err)
	}
	for _, s := range statefulSets.Items {
		add(KindStatefulSet, s.Name)
	}
	daemonSets, err := apps.DaemonSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list daemonsets: %w", err)
	}
	for _, d := range daemonSets.Items {
		add(KindDaemonSet, d.Name)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Source() < out[j].Source() })
	return out, nil
}
