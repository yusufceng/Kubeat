package k8s

import (
	"context"
	"fmt"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
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

// WorkloadStatus is how many pods a workload is meant to have and has
// ready, and whether it is in the middle of a rollout.
type WorkloadStatus struct {
	Desired, Ready int
	// Rolling is set while an update is being rolled out: a new
	// generation not yet observed, pods not yet updated, or old pods still
	// around. Pods that fail after a rollout has completed do not count.
	Rolling bool
	// Stalled is set when the cluster has given up on the rollout: a
	// Deployment past its progress deadline.
	Stalled bool
}

// workloadState is what a workload says about its pods.
type workloadState struct {
	WorkloadStatus
	selector *metav1.LabelSelector
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
			st = workloadState{WorkloadStatus: WorkloadStatus{Desired: 1, Ready: int(d.Status.ReadyReplicas)}, selector: d.Spec.Selector}
			if d.Spec.Replicas != nil {
				st.Desired = int(*d.Spec.Replicas)
			}
			// The same notion of "complete" as kubectl rollout status, without
			// the availability part: a pod that dies after the rollout is a
			// failure, not a rollout.
			st.Rolling = d.Generation > d.Status.ObservedGeneration ||
				int(d.Status.UpdatedReplicas) < st.Desired || d.Status.Replicas > d.Status.UpdatedReplicas
			for _, cond := range d.Status.Conditions {
				if cond.Type == appsv1.DeploymentProgressing && cond.Status == corev1.ConditionFalse && cond.Reason == "ProgressDeadlineExceeded" {
					st.Stalled = true
				}
			}
		}
	case KindStatefulSet:
		s, getErr := apps.StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err = getErr; err == nil {
			st = workloadState{WorkloadStatus: WorkloadStatus{Desired: 1, Ready: int(s.Status.ReadyReplicas)}, selector: s.Spec.Selector}
			if s.Spec.Replicas != nil {
				st.Desired = int(*s.Spec.Replicas)
			}
			st.Rolling = s.Generation > s.Status.ObservedGeneration ||
				(s.Status.UpdateRevision != "" && s.Status.CurrentRevision != s.Status.UpdateRevision) ||
				int(s.Status.UpdatedReplicas) < st.Desired
		}
	case KindDaemonSet:
		d, getErr := apps.DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err = getErr; err == nil {
			// One pod per node it is meant to run on.
			st = workloadState{WorkloadStatus: WorkloadStatus{Desired: int(d.Status.DesiredNumberScheduled), Ready: int(d.Status.NumberReady)}, selector: d.Spec.Selector}
			st.Rolling = d.Generation > d.Status.ObservedGeneration || d.Status.UpdatedNumberScheduled < d.Status.DesiredNumberScheduled
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
// (namespace/kind/name) should have, how many of them are ready, and
// whether it is being rolled out.
func (c *Client) Replicas(ctx context.Context, target string) (WorkloadStatus, error) {
	namespace, kind, name, err := ParseWorkloadTarget(target)
	if err != nil {
		return WorkloadStatus{}, err
	}
	st, err := c.workload(ctx, namespace, kind, name)
	return st.WorkloadStatus, err
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
	add := func(kind Kind, meta metav1.ObjectMeta) {
		out = append(out, Candidate{
			Kind: kind, Namespace: meta.Namespace, Name: meta.Name, Annotations: meta.Annotations,
			Type: store.MonitorWorkload, Target: WorkloadTarget(meta.Namespace, kind, meta.Name), Reason: ReasonWorkload,
		})
	}
	apps := c.core.AppsV1()

	deployments, err := apps.Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list deployments: %w", err)
	}
	for _, d := range deployments.Items {
		add(KindDeployment, d.ObjectMeta)
	}
	statefulSets, err := apps.StatefulSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list statefulsets: %w", err)
	}
	for _, s := range statefulSets.Items {
		add(KindStatefulSet, s.ObjectMeta)
	}
	daemonSets, err := apps.DaemonSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list daemonsets: %w", err)
	}
	for _, d := range daemonSets.Items {
		add(KindDaemonSet, d.ObjectMeta)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Source() < out[j].Source() })
	return out, nil
}
