package k8s

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
)

func ptr[T any](v T) *T { return &v }

func TestPodEndpoints(t *testing.T) {
	core := fake.NewSimpleClientset(
		service("api", map[string]string{"app": "api"},
			corev1.ServicePort{Name: "web", Port: 80, TargetPort: intstr.FromInt32(8080)},
			corev1.ServicePort{Name: "metrics", Port: 9090, TargetPort: intstr.FromInt32(9090)},
		),
		&discoveryv1.EndpointSlice{
			ObjectMeta:  metav1.ObjectMeta{Namespace: "shop", Name: "api-abc", Labels: map[string]string{discoveryv1.LabelServiceName: "api"}},
			AddressType: discoveryv1.AddressTypeIPv4,
			Ports: []discoveryv1.EndpointPort{
				{Name: ptr("web"), Port: ptr(int32(8080))},
				{Name: ptr("metrics"), Port: ptr(int32(9090))},
			},
			Endpoints: []discoveryv1.Endpoint{
				{Addresses: []string{"10.0.0.2"}, TargetRef: &corev1.ObjectReference{Kind: "Pod", Name: "api-2"}},
				{Addresses: []string{"10.0.0.1"}, TargetRef: &corev1.ObjectReference{Kind: "Pod", Name: "api-1"}, Conditions: discoveryv1.EndpointConditions{Ready: ptr(true)}},
				{Addresses: []string{"10.0.0.3"}, TargetRef: &corev1.ObjectReference{Kind: "Pod", Name: "api-3"}, Conditions: discoveryv1.EndpointConditions{Ready: ptr(false)}},
			},
		},
		// Another service's slice in the same namespace is not ours.
		&discoveryv1.EndpointSlice{
			ObjectMeta:  metav1.ObjectMeta{Namespace: "shop", Name: "other-xyz", Labels: map[string]string{discoveryv1.LabelServiceName: "other"}},
			AddressType: discoveryv1.AddressTypeIPv4,
			Ports:       []discoveryv1.EndpointPort{{Name: ptr("web"), Port: ptr(int32(8080))}},
			Endpoints:   []discoveryv1.Endpoint{{Addresses: []string{"10.0.9.9"}}},
		},
	)
	c := NewClient(core, newDynamic(), "test")

	got, err := c.PodEndpoints(context.Background(), "shop", "service/api:80")
	if err != nil {
		t.Fatal(err)
	}
	want := []Endpoint{{Pod: "api-1", Address: "10.0.0.1:8080"}, {Pod: "api-2", Address: "10.0.0.2:8080"}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("PodEndpoints = %+v, want %+v (ready pods only, sorted, on the target port)", got, want)
	}

	if got, err := c.PodEndpoints(context.Background(), "shop", "service/api:9090"); err != nil || len(got) != 2 || got[0].Address != "10.0.0.1:9090" {
		t.Errorf("metrics port = %+v, %v", got, err)
	}

	for _, source := range []string{"service/api:81", "service/nope:80", "deployment/api", "service/api"} {
		if _, err := c.PodEndpoints(context.Background(), "shop", source); !errors.Is(err, ErrNoWorkload) {
			t.Errorf("%s: err = %v, want ErrNoWorkload", source, err)
		}
	}
}
