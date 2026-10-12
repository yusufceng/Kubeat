package checker

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// Endpoint is one pod behind a service port: its name and "ip:port".
type Endpoint struct {
	Pod     string
	Address string
}

// Endpoints finds the pods a discovered monitor's service routes to.
type Endpoints interface {
	// PodEndpoints lists the ready pods behind the monitor's source
	// ("service/name:port") in its namespace.
	PodEndpoints(ctx context.Context, namespace, source string) ([]Endpoint, error)
}

// maxPodsNamed bounds how many pods a result names, so that a large
// deployment does not fill the message.
const maxPodsNamed = 10

// eachPod says whether the monitor checks the pods one by one instead of
// the service: an HTTP or TCP monitor discovered from a service port.
func eachPod(m store.Monitor) bool {
	return m.EachPod && (m.Type == store.MonitorHTTP || m.Type == store.MonitorTCP) && strings.HasPrefix(m.Source, "service/")
}

// checkEachPod runs the monitor's check against every ready pod behind its
// service, all at once. The service's own address balances over the pods
// and so hides one that is ready but not answering; this does not. The
// outcome fails if any pod fails, and names the failing ones; its latency
// is the slowest pod's.
func (c *Checker) checkEachPod(ctx context.Context, m store.Monitor) outcome {
	if c.opts.Endpoints == nil {
		return failedf("checking each pod needs a cluster connection")
	}
	endpoints, err := c.opts.Endpoints.PodEndpoints(ctx, m.Namespace, m.Source)
	if err != nil {
		return failed(err)
	}
	if len(endpoints) == 0 {
		return failedf("no ready pod behind %s", m.Source)
	}

	type podOutcome struct {
		Endpoint
		outcome
		took time.Duration
	}
	results := make([]podOutcome, len(endpoints))
	var wg sync.WaitGroup
	for i, ep := range endpoints {
		wg.Add(1)
		go func() {
			defer wg.Done()
			start := time.Now()
			var out outcome
			switch m.Type {
			case store.MonitorHTTP:
				out = c.checkHTTPAt(ctx, m, ep.Address)
			case store.MonitorTCP:
				out = c.checkTCPAt(ctx, m, ep.Address)
			}
			results[i] = podOutcome{Endpoint: ep, outcome: out, took: time.Since(start)}
		}()
	}
	wg.Wait()

	var failures []string
	combined := outcome{hasTime: true}
	for _, r := range results {
		combined.latency = max(combined.latency, r.took)
		if combined.certExpiry.IsZero() || (!r.certExpiry.IsZero() && r.certExpiry.Before(combined.certExpiry)) {
			combined.certExpiry = r.certExpiry
		}
		if r.err != nil {
			failures = append(failures, fmt.Sprintf("%s (%v)", r.Pod, r.err))
			if combined.statusCode == 0 {
				combined.statusCode = r.statusCode
			}
		}
	}
	sort.Strings(failures)
	if len(failures) > 0 {
		combined.err = fmt.Errorf("%d of %d pods failed: %s", len(failures), len(results), listPods(failures))
		return combined
	}
	names := make([]string, 0, len(results))
	for _, r := range results {
		names = append(names, r.Pod)
	}
	combined.detail = fmt.Sprintf("%d pods ok: %s", len(names), listPods(names))
	return combined
}

func listPods(names []string) string {
	if len(names) > maxPodsNamed {
		return strings.Join(names[:maxPodsNamed], ", ") + fmt.Sprintf(", … %d more", len(names)-maxPodsNamed)
	}
	return strings.Join(names, ", ")
}

// dialTo returns a dialer that connects to addr whatever address it is
// asked for, so that a request keeps its URL (Host header, TLS server name)
// while reaching one pod.
func (c *Checker) dialTo(addr string) func(ctx context.Context, network, _ string) (net.Conn, error) {
	return func(ctx context.Context, network, _ string) (net.Conn, error) {
		return c.dialer.DialContext(ctx, network, addr)
	}
}
