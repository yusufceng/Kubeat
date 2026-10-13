package k8s

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
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

// Events returns the events between since and until (zero: no bound) about
// a workload: its pods, the pods of the same manager that have since been
// replaced (their events are often the "why"), the manager itself and, for
// a Deployment, its ReplicaSets. Kubernetes folds a repeating event into
// one object with a count and a first and a last time, so an event is
// "between" when that span overlaps the window. Newest first, at most limit
// (zero: all).
func (c *Client) Events(ctx context.Context, namespace string, w Workload, since, until time.Time, limit int) ([]Event, error) {
	items, err := c.listEvents(ctx, namespace)
	if err != nil {
		return nil, err
	}
	managers := w.managers()
	var out []Event
	for _, e := range items {
		first, last, count := eventSpan(e)
		if last.Before(since) || (!until.IsZero() && !first.Before(until)) || !concerns(e.InvolvedObject, w, managers) {
			continue
		}
		out = append(out, Event{
			At: last, Type: e.Type, Reason: e.Reason,
			Object:  e.InvolvedObject.Kind + "/" + e.InvolvedObject.Name,
			Message: strings.TrimSpace(e.Message), Count: count,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// cachedEvents is one namespace's event list and when it was read.
type cachedEvents struct {
	at    time.Time
	items []corev1.Event
}

// listEvents reads a namespace's events, from the cache while a read is
// younger than EventCache: the list covers the whole namespace and the two
// panels of a monitor page, refreshed every few seconds, would otherwise
// each read it again.
func (c *Client) listEvents(ctx context.Context, namespace string) ([]corev1.Event, error) {
	if c.eventCache > 0 {
		c.eventsMu.Lock()
		cached, ok := c.events[namespace]
		c.eventsMu.Unlock()
		if ok && time.Since(cached.at) < c.eventCache {
			return cached.items, nil
		}
	}
	list, err := c.core.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	if c.eventCache > 0 {
		c.eventsMu.Lock()
		if c.events == nil {
			c.events = make(map[string]cachedEvents)
		}
		c.events[namespace] = cachedEvents{at: time.Now(), items: list.Items}
		c.eventsMu.Unlock()
	}
	return list.Items, nil
}

// eventSpan is when an event first and last happened and how often. The
// core API keeps FirstTimestamp/LastTimestamp with Count; the events.k8s.io
// form keeps EventTime with a Series of LastObservedTime and Count.
func eventSpan(e corev1.Event) (first, last time.Time, count int32) {
	last = eventTime(e)
	count = max(e.Count, 1)
	if e.Series != nil {
		if !e.Series.LastObservedTime.IsZero() {
			last = e.Series.LastObservedTime.Time.UTC()
		}
		count = max(e.Series.Count, count)
	}
	first = last
	for _, t := range []metav1.Time{e.FirstTimestamp, {Time: e.EventTime.Time}} {
		if !t.IsZero() && t.Time.Before(first) {
			first = t.Time.UTC()
		}
	}
	return first, last, count
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

// manager is a controller the workload's pods belong to.
type manager struct{ Kind, Name string }

// managers lists the controllers of the workload: the one it was named
// after, the owners of its pods, and the Deployment behind an owning
// ReplicaSet (named "<deployment>-<hash>"), so that pods of earlier
// ReplicaSets count as siblings.
func (w Workload) managers() []manager {
	var out []manager
	add := func(kind, name string) {
		if name == "" {
			return
		}
		for _, m := range out {
			if m.Kind == kind && m.Name == name {
				return
			}
		}
		out = append(out, manager{kind, name})
	}
	if kind, name, ok := strings.Cut(w.Manager, "/"); ok {
		add(kind, name)
	}
	for _, p := range w.Pods {
		if kind, name, ok := strings.Cut(p.Owner, "/"); ok {
			add(kind, name)
		}
	}
	for _, m := range out {
		if m.Kind == "ReplicaSet" {
			if deployment, hash, ok := cutLast(m.Name); ok && generatedHash(hash) {
				add("Deployment", deployment)
			}
		}
	}
	return out
}

// concerns says whether an event's object is one of the workload's pods, a
// pod of one of its managers (a sibling, perhaps since replaced), or one
// of the managers themselves. Pods without a known owner only match by
// name, and their managers by the shape of the pod name, as before owners
// were read.
func concerns(obj corev1.ObjectReference, w Workload, managers []manager) bool {
	switch obj.Kind {
	case "Pod":
		for _, p := range w.Pods {
			if p.Name == obj.Name {
				return true
			}
		}
		for _, m := range managers {
			if childOf(obj.Name, m) {
				return true
			}
		}
		return false
	case "ReplicaSet", "StatefulSet", "DaemonSet", "ReplicationController", "Deployment", "DeploymentConfig", "Job":
		for _, m := range managers {
			if m.Kind == obj.Kind && m.Name == obj.Name {
				return true
			}
			// A Deployment's other ReplicaSets, "<deployment>-<hash>".
			if m.Kind == "Deployment" && obj.Kind == "ReplicaSet" {
				if deployment, hash, ok := cutLast(obj.Name); ok && deployment == m.Name && generatedHash(hash) {
					return true
				}
			}
		}
		return managesByShape(obj, w.Pods)
	}
	return false
}

// childOf says whether a pod name has the shape of a pod the manager
// creates: its name plus the generated parts of its kind. The generated
// parts must look generated, so that "web-admin-0" is not a child of
// Deployment "web".
func childOf(pod string, m manager) bool {
	rest, ok := strings.CutPrefix(pod, m.Name+"-")
	if !ok {
		return false
	}
	switch m.Kind {
	case "StatefulSet":
		return ordinal(rest)
	case "ReplicaSet", "DaemonSet", "ReplicationController", "Job":
		return generatedSuffix(rest)
	case "Deployment":
		hash, suffix, ok := strings.Cut(rest, "-")
		return ok && generatedHash(hash) && generatedSuffix(suffix)
	case "DeploymentConfig":
		// "app-3-xk2p9": the deployment's number, then the pod's suffix.
		number, suffix, ok := strings.Cut(rest, "-")
		return ok && ordinal(number) && generatedSuffix(suffix)
	}
	return false
}

// managesByShape is the match for pods whose owner is not known: an event
// about a ReplicaSet, StatefulSet, DaemonSet or ReplicationController
// concerns them when a pod's name is the object's name plus one generated
// part, a Deployment's or DeploymentConfig's when plus two. Counting them
// keeps "web" from claiming the pods of "web-admin".
func managesByShape(obj corev1.ObjectReference, pods []Pod) bool {
	generated := 0
	switch obj.Kind {
	case "ReplicaSet", "StatefulSet", "DaemonSet", "ReplicationController":
		generated = 1
	case "Deployment", "DeploymentConfig":
		generated = 2
	default:
		return false
	}
	for _, p := range pods {
		if p.Owner != "" {
			continue
		}
		rest, ok := strings.CutPrefix(p.Name, obj.Name+"-")
		if ok && strings.Count(rest, "-") == generated-1 {
			return true
		}
	}
	return false
}

// generatedAlphabet is what Kubernetes makes generated names from: no
// vowels and no 0, 1 or 3, so that they do not spell anything.
const generatedAlphabet = "bcdfghjklmnpqrstvwxz2456789"

func generatedChars(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !strings.ContainsRune(generatedAlphabet, r) {
			return false
		}
	}
	return true
}

// generatedSuffix is the five-character tail of a pod created by a
// ReplicaSet, DaemonSet or Job.
func generatedSuffix(s string) bool { return len(s) == 5 && generatedChars(s) }

// generatedHash is a pod-template-hash: the encoding of a 32-bit hash,
// up to ten characters and in practice at least five.
func generatedHash(s string) bool { return len(s) >= 5 && len(s) <= 10 && generatedChars(s) }

// ordinal is a StatefulSet pod's number or a DeploymentConfig's.
func ordinal(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// cutLast splits a name at its last dash.
func cutLast(name string) (head, tail string, ok bool) {
	cut := strings.LastIndex(name, "-")
	if cut <= 0 || cut == len(name)-1 {
		return "", "", false
	}
	return name[:cut], name[cut+1:], true
}

// eventsMu guards the event cache; it lives here next to what uses it.
type eventCacheState struct {
	eventsMu   sync.Mutex
	events     map[string]cachedEvents
	eventCache time.Duration
}
