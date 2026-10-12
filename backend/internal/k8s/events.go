package k8s

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Event is a Kubernetes event about a pod or about what manages it.
type Event struct {
	At time.Time
	// Type is "Normal" or "Warning".
	Type   string
	Reason string
	// Object is "Kind/name" of what the event is about.
	Object  string
	Message string
	// Count is how many times the event repeated.
	Count int32
}

// Events returns the events of the last window about the given pods, and
// about the ReplicaSets, Deployments, StatefulSets and DaemonSets they
// belong to, recognised by the shape of the pod names. Newest first, at
// most limit.
func (c *Client) Events(ctx context.Context, namespace string, pods []string, since time.Time, limit int) ([]Event, error) {
	list, err := c.core.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	var out []Event
	for _, e := range list.Items {
		at := eventTime(e)
		if at.Before(since) || !concerns(e.InvolvedObject, pods) {
			continue
		}
		out = append(out, Event{
			At: at, Type: e.Type, Reason: e.Reason,
			Object:  e.InvolvedObject.Kind + "/" + e.InvolvedObject.Name,
			Message: strings.TrimSpace(e.Message), Count: max(e.Count, 1),
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// eventTime is when the event last happened: LastTimestamp for the core
// API, EventTime for the events.k8s.io form, FirstTimestamp as a fallback.
func eventTime(e corev1.Event) time.Time {
	switch {
	case !e.LastTimestamp.IsZero():
		return e.LastTimestamp.Time.UTC()
	case !e.EventTime.IsZero():
		return e.EventTime.Time.UTC()
	default:
		return e.FirstTimestamp.Time.UTC()
	}
}

// concerns says whether an event's object is one of the pods or manages
// one of them. A pod's name is its manager's name plus generated parts:
// one for a ReplicaSet, StatefulSet, DaemonSet or ReplicationController
// ("web-7d9f-abc12", "db-0"), two for a Deployment or DeploymentConfig
// ("web-7d9f-abc12", "app-3-xk2p9"). Counting them keeps "web" from
// claiming the pods of "web-admin".
func concerns(obj corev1.ObjectReference, pods []string) bool {
	generated := 0
	switch obj.Kind {
	case "Pod":
		for _, p := range pods {
			if p == obj.Name {
				return true
			}
		}
		return false
	case "ReplicaSet", "StatefulSet", "DaemonSet", "ReplicationController":
		generated = 1
	case "Deployment", "DeploymentConfig":
		generated = 2
	default:
		return false
	}
	for _, p := range pods {
		rest, ok := strings.CutPrefix(p, obj.Name+"-")
		if ok && strings.Count(rest, "-") == generated-1 {
			return true
		}
	}
	return false
}
