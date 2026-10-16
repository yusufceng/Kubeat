package api

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/yusufceng/Kubeat/backend/internal/auth"
	"github.com/yusufceng/Kubeat/backend/internal/discovery"
	"github.com/yusufceng/Kubeat/backend/internal/k8s"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// rbacRefusal is the API server's refusal of Kubeat's service account, as
// client-go returns it; an empty namespace is a cluster-wide request.
func rbacRefusal(verb, resource, group, namespace string) error {
	where := "at the cluster scope"
	if namespace != "" {
		where = fmt.Sprintf("in the namespace %q", namespace)
	}
	return apierrors.NewForbidden(schema.GroupResource{Group: group, Resource: resource}, "",
		fmt.Errorf(`User "system:serviceaccount:kubeat:kubeat" cannot %s resource %q in API group %q %s`, verb, resource, group, where))
}

// The upstream errors of the tests, wrapped the way the cluster and
// metrics clients wrap them.
var (
	errRefused = &url.Error{Op: "Get", URL: "https://172.30.0.1:443/api/v1/namespaces",
		Err: &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connect: connection refused")}}
	errUntrusted = fmt.Errorf("prometheus cpu query: %w", &url.Error{
		Op: "Post", URL: "https://thanos-querier.openshift-monitoring.svc:9092/api/v1/query?namespace=shop",
		Err: &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}},
	})
)

// The words that must not reach anyone but an administrator: the service
// account, the addresses of the API and metrics servers, the kubeconfig
// context.
var upstreamSecrets = []string{"system:serviceaccount", "172.30.0.1", "thanos-querier", "10.0.0.7", "admin@prod"}

func TestUpstreamReason(t *testing.T) {
	viewer := auth.Principal{Username: "v", Role: store.RoleViewer, Namespaces: []string{"shop"}}
	proxyRefusal := apierrors.NewGenericServerResponse(http.StatusForbidden, "get", schema.GroupResource{Resource: "pods"}, "",
		"Forbidden (user=system:serviceaccount:kubeat:kubeat, verb=get, resource=pods)", 0, true)

	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"forbidden in their namespace", fmt.Errorf("list services: %w", rbacRefusal("list", "services", "", "shop")),
			"Kubeat's service account may not list services in shop; ask an administrator"},
		{"forbidden in another namespace", fmt.Errorf("list services: %w", rbacRefusal("list", "services", "", "finance")),
			"Kubeat's service account may not list services; ask an administrator"},
		{"forbidden cluster-wide", fmt.Errorf("list namespaces: %w", rbacRefusal("list", "namespaces", "", "")),
			"Kubeat's service account may not list namespaces; ask an administrator"},
		{"forbidden, of a group", fmt.Errorf("get route: %w", rbacRefusal("get", "routes", "route.openshift.io", "shop")),
			"Kubeat's service account may not get routes.route.openshift.io in shop; ask an administrator"},
		{"forbidden by a proxy in between", proxyRefusal,
			"Kubeat's service account may not access pods; ask an administrator"},
		{"credentials refused", apierrors.NewUnauthorized("Unauthorized"),
			"the server did not accept Kubeat's credentials; ask an administrator"},
		{"nothing listening", errRefused, "the server could not be reached"},
		{"no answer in time", fmt.Errorf("list events: %w", context.DeadlineExceeded), "the server could not be reached"},
		{"untrusted certificate", errUntrusted, "the server's certificate is not trusted; ask an administrator"},
		{"wrong host in the certificate", &url.Error{Op: "Get", URL: "https://10.0.0.7", Err: x509.HostnameError{Host: "10.0.0.7", Certificate: &x509.Certificate{}}},
			"the server's certificate is not trusted; ask an administrator"},
		{"Kubeat's own words", fmt.Errorf("%w: service shop/api does not exist", k8s.ErrNoWorkload),
			"k8s: no workload behind this source: service shop/api does not exist"},
		{"anything else", errors.New("prometheus: unexpected answer from 10.0.0.7"), logDetails},
		{"no error at hand", nil, logDetails},
	} {
		got := upstreamReason(viewer, tc.err)
		if got != tc.want {
			t.Errorf("%s: viewer is told %q, want %q", tc.name, got, tc.want)
		}
		for _, secret := range upstreamSecrets {
			if strings.Contains(got, secret) {
				t.Errorf("%s: viewer is told %q", tc.name, got)
			}
		}
		if tc.err != nil {
			if got := upstreamReason(auth.ServicePrincipal, tc.err); got != tc.err.Error() {
				t.Errorf("%s: admin is told %q, want the error itself", tc.name, got)
			}
		}
	}
}

// bodyAs sends a GET with the given bearer token and returns the status and
// the body as it came.
func bodyAs(t *testing.T, token, url string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(body)
}

// What the cluster and the metrics server answered goes to administrators
// as it was; everyone else learns only what kind of failure it was.
func TestClusterFailuresAsUsersSeeThem(t *testing.T) {
	syncErr := fmt.Errorf("read the cluster: list services: %w", rbacRefusal("list", "services", "", ""))
	workloads := &fakeWorkloads{
		workload:  k8s.Workload{Service: "api", Pods: []k8s.Pod{{Name: "api-a", Ready: true}}},
		eventsErr: fmt.Errorf("list events: %w", rbacRefusal("list", "events", "", "shop")),
	}
	srv, db := newTestAPIWith(t, func(o *Options) {
		o.Discovery = &fakeDiscovery{err: fmt.Errorf("list services: %w", rbacRefusal("list", "services", "", "shop"))}
		o.DiscoveryStatus = "kubeconfig:admin@prod-cluster"
		o.Sync = fakeSync{status: discovery.Status{IntervalSeconds: 60, Error: syncErr.Error(), Cause: syncErr, Skipped: []discovery.Skip{}}}
		o.Workloads, o.PodMetrics = workloads, &fakePodMetrics{err: errUntrusted}
		o.EventLimit = 10
	})
	m, err := db.CreateMonitor(context.Background(), store.Monitor{
		Name: "shop/service/api:80", Type: store.MonitorTCP, Target: "api.shop.svc:80", Namespace: "shop", Source: "service/api:80",
		Interval: 30 * time.Second, Timeout: 5 * time.Second, FailureThreshold: 3, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	addUser(t, srv.URL, "viewer", store.RoleViewer, "shop")
	viewer, _ := signIn(t, srv.URL, "viewer", "parola-123")
	api := srv.URL + "/api/v1"
	windowURL := api + "/monitors/" + itoa(m.ID) + "/window?from=" + time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)

	// The viewer: kinds of failure, nothing of the setup.
	var bodies []string
	get := func(url string, wantCode int, out any) {
		t.Helper()
		code, body := bodyAs(t, viewer, url)
		if code != wantCode {
			t.Errorf("GET %s: status = %d, want %d", url, code, wantCode)
		}
		if err := json.Unmarshal([]byte(body), out); err != nil {
			t.Fatalf("GET %s: %v", url, err)
		}
		bodies = append(bodies, body)
	}
	var problem errorBody
	get(api+"/discovery/candidates?namespace=shop", http.StatusBadGateway, &problem)
	if want := "cluster request failed: Kubeat's service account may not list services in shop; ask an administrator"; problem.Error != want {
		t.Errorf("candidates: %q, want %q", problem.Error, want)
	}
	var w workloadResponse
	get(api+"/monitors/"+itoa(m.ID)+"/workload", http.StatusOK, &w)
	if want := "the cluster's events could not be read: Kubeat's service account may not list events in shop; ask an administrator"; w.EventsReason != want {
		t.Errorf("events reason: %q, want %q", w.EventsReason, want)
	}
	if want := "the metrics server could not be queried: the server's certificate is not trusted; ask an administrator"; w.Metrics.Reason != want {
		t.Errorf("metrics reason: %q, want %q", w.Metrics.Reason, want)
	}
	var win windowResponse
	get(windowURL, http.StatusOK, &win)
	if win.EventsReason != w.EventsReason {
		t.Errorf("window's events reason: %q", win.EventsReason)
	}
	var status discoveryStatusResponse
	get(api+"/discovery", http.StatusOK, &status)
	if status.Detail != "" || status.Sync == nil || status.Sync.Error != "Kubeat's service account may not list services; ask an administrator" {
		t.Errorf("discovery status: detail %q, sync %+v", status.Detail, status.Sync)
	}
	workloads.err = fmt.Errorf("get service: %w", errRefused)
	get(api+"/monitors/"+itoa(m.ID)+"/workload", http.StatusBadGateway, &problem)
	if want := "cluster request failed: the server could not be reached"; problem.Error != want {
		t.Errorf("workload: %q, want %q", problem.Error, want)
	}
	get(windowURL, http.StatusOK, &win)
	if want := "the cluster could not be read: the server could not be reached"; win.EventsReason != want {
		t.Errorf("window: %q, want %q", win.EventsReason, want)
	}
	for _, body := range bodies {
		for _, secret := range upstreamSecrets {
			if strings.Contains(body, secret) {
				t.Errorf("the viewer read %q in %s", secret, body)
			}
		}
	}

	// The administrator: everything, as the servers said it.
	if _, body := bodyAs(t, testToken, api+"/monitors/"+itoa(m.ID)+"/workload"); !strings.Contains(body, "172.30.0.1") {
		t.Errorf("admin's workload error: %s", body)
	}
	workloads.err = nil
	if _, body := bodyAs(t, testToken, api+"/monitors/"+itoa(m.ID)+"/workload"); !strings.Contains(body, "system:serviceaccount:kubeat:kubeat") || !strings.Contains(body, "thanos-querier") {
		t.Errorf("admin's workload reasons: %s", body)
	}
	if _, body := bodyAs(t, testToken, api+"/discovery/candidates?namespace=shop"); !strings.Contains(body, "system:serviceaccount:kubeat:kubeat") {
		t.Errorf("admin's cluster error: %s", body)
	}
	status = discoveryStatusResponse{}
	do(t, "GET", api+"/discovery", "", &status)
	if status.Detail != "kubeconfig:admin@prod-cluster" || status.Sync == nil || status.Sync.Error != syncErr.Error() {
		t.Errorf("admin's discovery status: detail %q, sync %+v", status.Detail, status.Sync)
	}
}

// Without a cluster, why there is none is the administrators' business.
func TestNoClusterAsUsersSeeIt(t *testing.T) {
	srv, db := newTestAPIWith(t, func(o *Options) {
		o.DiscoveryStatus = "k8s: load kubeconfig: open /home/admin@prod/.kube/config: no such file"
	})
	m, err := db.CreateMonitor(context.Background(), store.Monitor{
		Name: "shop/service/api:80", Type: store.MonitorTCP, Target: "api.shop.svc:80", Namespace: "shop", Source: "service/api:80",
		Interval: 30 * time.Second, Timeout: 5 * time.Second, FailureThreshold: 3, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	addUser(t, srv.URL, "viewer", store.RoleViewer, "shop")
	viewer, _ := signIn(t, srv.URL, "viewer", "parola-123")
	api := srv.URL + "/api/v1"
	monitor := api + "/monitors/" + itoa(m.ID)

	for url, want := range map[string]string{
		api + "/discovery":            `"detail":""`,
		api + "/discovery/namespaces": `"discovery is not available"`,
		monitor + "/workload":         `"reason":"the backend is not connected to a cluster"`,
		monitor + "/window?from=" + time.Now().Add(-time.Hour).UTC().Format(time.RFC3339): `"events_reason":"the backend is not connected to a cluster"`,
	} {
		_, body := bodyAs(t, viewer, url)
		if !strings.Contains(body, want) || strings.Contains(body, "admin@prod") {
			t.Errorf("viewer's %s: %s", url, body)
		}
		if _, body := bodyAs(t, testToken, url); !strings.Contains(body, "admin@prod") {
			t.Errorf("admin's %s: %s", url, body)
		}
	}
}
