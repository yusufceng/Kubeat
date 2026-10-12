package k8s

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func event(name, kind, object, typ, reason string, at time.Time, count int32) *corev1.Event {
	return &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Namespace: "shop", Name: name},
		InvolvedObject: corev1.ObjectReference{Kind: kind, Name: object},
		Type:           typ, Reason: reason, Message: "  " + reason + " happened ",
		LastTimestamp: metav1.NewTime(at), Count: count,
	}
}

func TestEventsAboutThePodsAndWhatManagesThem(t *testing.T) {
	now := time.Date(2026, 10, 12, 20, 0, 0, 0, time.UTC)
	core := fake.NewSimpleClientset(
		event("e1", "Pod", "web-7d9f-abc", "Warning", "BackOff", now.Add(-5*time.Minute), 12),
		event("e2", "Pod", "web-7d9f-xyz", "Normal", "Pulled", now.Add(-50*time.Minute), 1),
		event("e3", "ReplicaSet", "web-7d9f", "Normal", "SuccessfulCreate", now.Add(-2*time.Minute), 1),
		event("e4", "Deployment", "web", "Normal", "ScalingReplicaSet", now.Add(-1*time.Minute), 1),
		event("e5", "Pod", "other-123", "Warning", "Failed", now, 1),                         // another workload
		event("e6", "Pod", "web-7d9f-abc", "Warning", "Unhealthy", now.Add(-3*time.Hour), 1), // too old
		event("e7", "Deployment", "webshop", "Normal", "Scaled", now, 1),                     // name is a prefix but not a parent
	)
	c := NewClient(core, newDynamic(), "test")

	got, err := c.Events(context.Background(), "shop", []string{"web-7d9f-abc", "web-7d9f-xyz"}, now.Add(-time.Hour), 10)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Deployment/web ScalingReplicaSet", "ReplicaSet/web-7d9f SuccessfulCreate", "Pod/web-7d9f-abc BackOff", "Pod/web-7d9f-xyz Pulled"}
	if len(got) != len(want) {
		t.Fatalf("Events = %+v, want %v", got, want)
	}
	for i, w := range want {
		if got[i].Object+" "+got[i].Reason != w {
			t.Errorf("event %d = %s %s, want %s", i, got[i].Object, got[i].Reason, w)
		}
	}
	if got[2].Count != 12 || got[2].Message != "BackOff happened" || got[2].Type != "Warning" {
		t.Errorf("backoff event = %+v", got[2])
	}

	if got, _ := c.Events(context.Background(), "shop", []string{"web-7d9f-abc"}, now.Add(-time.Hour), 1); len(got) != 1 || got[0].Reason != "ScalingReplicaSet" {
		t.Errorf("limit 1 = %+v", got)
	}
}
