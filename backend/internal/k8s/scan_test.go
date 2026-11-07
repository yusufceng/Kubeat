package k8s

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// A client limited to namespaces reads past a namespace the cluster API
// answers for with an error of its own (refused, or gone): the others'
// candidates come, and that one is skipped with why. Every namespace
// refused is the cluster unread; so is a request that gets no answer, or
// whose credentials are refused, in any namespace: that is not one
// namespace's to skip.
func TestScanGoesPastANamespaceItCannotRead(t *testing.T) {
	ctx := context.Background()
	billing := &corev1.Service{ObjectMeta: objMeta("billing", "db", nil), Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 5432}}}}
	core := fake.NewSimpleClientset(service("api", nil, corev1.ServicePort{Port: 80}), billing)
	failing := map[string]error{
		"legacy": apierrors.NewNotFound(schema.GroupResource{Resource: "namespaces"}, "legacy"),
		"team":   apierrors.NewForbidden(schema.GroupResource{Resource: "services"}, "", errors.New("its Role was taken away")),
	}
	core.PrependReactor("list", "*", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if err, ok := failing[action.GetNamespace()]; ok {
			return true, nil, err
		}
		return false, nil, nil
	})
	c := NewClient(core, newDynamic(), "test")
	c.Restrict([]string{"shop", "team", "billing", "legacy"})

	scan, err := c.Scan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(scan.Candidates) != 2 || find(t, scan.Candidates, "service/db:5432").Namespace != "billing" || find(t, scan.Candidates, "service/api:80").Namespace != "shop" {
		t.Errorf("candidates %+v, want those of billing and shop", scan.Candidates)
	}
	if len(scan.Skipped) != 2 || scan.Skipped[0].Namespace != "legacy" || !apierrors.IsNotFound(scan.Skipped[0].Err) ||
		scan.Skipped[1].Namespace != "team" || !apierrors.IsForbidden(scan.Skipped[1].Err) {
		t.Errorf("skipped %+v, want legacy (not found) and team (forbidden), in order", scan.Skipped)
	}
	if got := scan.Skipped[1].Error(); !strings.HasPrefix(got, "team: list services: ") {
		t.Errorf("skipped team: %q", got)
	}
	// Candidates of all the namespaces still asks for every one of them.
	if _, err := c.Candidates(ctx, ""); !apierrors.IsNotFound(err) {
		t.Errorf("Candidates(all): err = %v, want legacy's", err)
	}

	// None can be read.
	failing["billing"], failing["shop"] = failing["team"], failing["team"]
	scan, err = c.Scan(ctx)
	if !apierrors.IsForbidden(err) || !strings.HasPrefix(err.Error(), "no namespace could be read: billing: ") || len(scan.Candidates) != 0 || len(scan.Skipped) != 4 {
		t.Errorf("every namespace failing: scan %+v, err %v; want all four skipped and the first one's error", scan, err)
	}

	// Not one namespace's failure: the cluster's, and nothing is read.
	for name, failure := range map[string]error{
		"no connection": &url.Error{Op: "Get", URL: "https://172.30.0.1:443/api/v1/namespaces/team/services",
			Err: &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connect: connection refused")}},
		"no answer in time":   context.DeadlineExceeded,
		"credentials refused": apierrors.NewUnauthorized("Unauthorized"),
	} {
		failing = map[string]error{"team": failure}
		scan, err := c.Scan(ctx)
		var ns *NamespaceError
		if !errors.As(err, &ns) || ns.Namespace != "team" || !errors.Is(err, failure) || len(scan.Candidates) != 0 || len(scan.Skipped) != 0 {
			t.Errorf("%s: scan %+v, err %v; want nothing and team's error", name, scan, err)
		}
	}

	// A run that is ending skips nothing either.
	failing = map[string]error{"team": apierrors.NewForbidden(schema.GroupResource{Resource: "services"}, "", errors.New("refused"))}
	ended, end := context.WithCancel(ctx)
	end()
	if scan, err := c.Scan(ended); err == nil || len(scan.Skipped) != 0 {
		t.Errorf("an ended run: scan %+v, err %v", scan, err)
	}
}

// Over the whole cluster there is no namespace to skip: it is read at once,
// or not at all.
func TestScanReadsTheWholeClusterAtOnce(t *testing.T) {
	billing := &corev1.Service{ObjectMeta: objMeta("billing", "db", nil), Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 5432}}}}
	core := fake.NewSimpleClientset(service("api", nil, corev1.ServicePort{Port: 80}), billing)
	c := NewClient(core, newDynamic(), "test")
	scan, err := c.Scan(context.Background())
	if err != nil || len(scan.Candidates) != 2 || len(scan.Skipped) != 0 {
		t.Fatalf("scan %+v, err %v", scan, err)
	}

	core.PrependReactor("list", "services", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "services"}, "", errors.New("no ClusterRole"))
	})
	scan, err = c.Scan(context.Background())
	if !apierrors.IsForbidden(err) || len(scan.Candidates) != 0 || len(scan.Skipped) != 0 {
		t.Errorf("refused: scan %+v, err %v", scan, err)
	}
}
