package checker

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

type fakeEndpoints struct {
	endpoints []Endpoint
	err       error
}

func (f fakeEndpoints) PodEndpoints(context.Context, string, string) ([]Endpoint, error) {
	return f.endpoints, f.err
}

func podServer(t *testing.T, handler http.HandlerFunc) Endpoint {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return Endpoint{Pod: "pod-" + strings.TrimPrefix(srv.URL, "http://127.0.0.1:"), Address: strings.TrimPrefix(srv.URL, "http://")}
}

func TestEachPodChecksEveryPodBehindTheService(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) {
		// The request keeps the service's URL: path and Host header.
		if r.URL.Path != "/healthz" || r.Host != "api.shop.svc:8080" {
			w.WriteHeader(http.StatusBadRequest)
		}
	}
	good1, good2 := podServer(t, ok), podServer(t, ok)
	bad := podServer(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) })

	m := httpMonitor("http://api.shop.svc:8080/healthz")
	m.Namespace, m.Source, m.EachPod = "shop", "service/api:8080", true

	c := New(Options{Endpoints: fakeEndpoints{endpoints: []Endpoint{good1, good2}}})
	got := c.Check(context.Background(), m)
	if !got.Up || !strings.HasPrefix(got.Detail, "2 pods ok: ") || !strings.Contains(got.Detail, good1.Pod) {
		t.Errorf("all good: %+v", got)
	}

	c = New(Options{Endpoints: fakeEndpoints{endpoints: []Endpoint{good1, bad, good2}}})
	got = c.Check(context.Background(), m)
	if got.Up || got.StatusCode != http.StatusServiceUnavailable || !strings.HasPrefix(got.Error, "1 of 3 pods failed: "+bad.Pod+" (unexpected status 503)") {
		t.Errorf("one bad: %+v", got)
	}

	// A pod that does not answer is named too, and the whole check is still
	// bounded by the context.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	silent := Endpoint{Pod: "pod-silent", Address: listener.Addr().String()}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	c = New(Options{Endpoints: fakeEndpoints{endpoints: []Endpoint{good1, silent}}})
	start := time.Now()
	got = c.Check(ctx, m)
	if got.Up || !strings.Contains(got.Error, "pod-silent (") || time.Since(start) > time.Second {
		t.Errorf("silent pod: %+v after %s", got, time.Since(start))
	}
}

func TestEachPodWithTCP(t *testing.T) {
	ep := podServer(t, func(http.ResponseWriter, *http.Request) {})
	m := store.Monitor{Type: store.MonitorTCP, Target: "api.shop.svc:8080", Namespace: "shop", Source: "service/api:8080", EachPod: true}
	c := New(Options{Endpoints: fakeEndpoints{endpoints: []Endpoint{ep}}})
	if got := c.Check(context.Background(), m); !got.Up || got.Detail != "1 pods ok: "+ep.Pod {
		t.Errorf("tcp: %+v", got)
	}
}

func TestEachPodFailsWithoutPods(t *testing.T) {
	m := httpMonitor("http://api.shop.svc:8080/healthz")
	m.Namespace, m.Source, m.EachPod = "shop", "service/api:8080", true

	for name, opts := range map[string]Options{
		"no cluster": {},
		"no pods":    {Endpoints: fakeEndpoints{}},
		"error":      {Endpoints: fakeEndpoints{err: errors.New("endpointslices is forbidden")}},
	} {
		if got := New(opts).Check(context.Background(), m); got.Up || got.Error == "" {
			t.Errorf("%s: %+v", name, got)
		}
	}

	// Without a service source the flag means nothing: the target is checked.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	plain := httpMonitor(srv.URL)
	plain.EachPod = true
	if got := New(Options{}).Check(context.Background(), plain); !got.Up {
		t.Errorf("plain: %+v", got)
	}
}
