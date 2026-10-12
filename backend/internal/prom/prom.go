// Package prom reads pod resource usage from a Prometheus-compatible API,
// such as OpenShift's Thanos Querier. Kubeat does not collect these metrics
// itself; the cluster's monitoring stack already has them.
package prom

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

// Placeholders replaced in query templates.
const (
	placeholderNamespace = "%NAMESPACE%"
	placeholderPods      = "%PODS%"
	placeholderWindow    = "%WINDOW%"
)

// Default queries. They read the pod-level cgroup series (the ones without
// a container label), which every container runtime exposes; each must
// return one series per pod, labelled "pod".
const (
	DefaultCPUQuery    = `max by (pod) (rate(container_cpu_usage_seconds_total{namespace="%NAMESPACE%",pod=~"%PODS%",container=""}[%WINDOW%]))`
	DefaultMemoryQuery = `max by (pod) (container_memory_working_set_bytes{namespace="%NAMESPACE%",pod=~"%PODS%",container=""})`
)

// Options configures a Client.
type Options struct {
	URL string
	// TokenFile, if set, is read on every request and sent as a bearer
	// token; service account tokens are rotated while the process runs.
	TokenFile string
	// CAFile, if set, is the CA bundle the server's certificate must chain to.
	CAFile             string
	InsecureSkipVerify bool
	Timeout            time.Duration
	// RateWindow is the range CPU usage is averaged over.
	RateWindow time.Duration
	// CPUQuery and MemoryQuery override the defaults; see the placeholders.
	CPUQuery    string
	MemoryQuery string
}

// Usage is the resource consumption of one pod. A nil field means the
// metric had no value for the pod.
type Usage struct {
	CPUCores    *float64
	MemoryBytes *float64
}

// Client queries one Prometheus-compatible server.
type Client struct {
	api  v1.API
	opts Options
}

// New returns a Client. It does not contact the server.
func New(opts Options) (*Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: opts.InsecureSkipVerify}
	if opts.CAFile != "" {
		pem, err := os.ReadFile(opts.CAFile)
		if err != nil {
			return nil, fmt.Errorf("prometheus CA file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("prometheus CA file %s contains no certificates", opts.CAFile)
		}
		transport.TLSClientConfig.RootCAs = pool
	}
	if opts.CPUQuery == "" {
		opts.CPUQuery = DefaultCPUQuery
	}
	if opts.MemoryQuery == "" {
		opts.MemoryQuery = DefaultMemoryQuery
	}

	client, err := api.NewClient(api.Config{
		Address:      opts.URL,
		RoundTripper: &tokenTransport{base: transport, tokenFile: opts.TokenFile},
	})
	if err != nil {
		return nil, fmt.Errorf("prometheus: %w", err)
	}
	return &Client{api: v1.NewAPI(client), opts: opts}, nil
}

// tokenTransport adds the bearer token from a file to every request.
type tokenTransport struct {
	base      http.RoundTripper
	tokenFile string
}

func (t *tokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.tokenFile == "" {
		return t.base.RoundTrip(req)
	}
	token, err := os.ReadFile(t.tokenFile)
	if err != nil {
		return nil, fmt.Errorf("prometheus token file: %w", err)
	}
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	return t.base.RoundTrip(req)
}

// Kubernetes names: lowercase alphanumerics, '-' and '.'.
var kubeName = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)

// PodUsage returns CPU and memory usage of the named pods, keyed by pod name.
// Pods the server knows nothing about are absent from the result.
func (c *Client) PodUsage(ctx context.Context, namespace string, pods []string) (map[string]Usage, error) {
	if len(pods) == 0 {
		return map[string]Usage{}, nil
	}
	// The names go into a query; anything that is not a plain Kubernetes
	// name could change what the query selects.
	if !kubeName.MatchString(namespace) {
		return nil, fmt.Errorf("prometheus: invalid namespace %q", namespace)
	}
	escaped := make([]string, len(pods))
	for i, pod := range pods {
		if !kubeName.MatchString(pod) {
			return nil, fmt.Errorf("prometheus: invalid pod name %q", pod)
		}
		escaped[i] = regexp.QuoteMeta(pod)
	}
	replacer := strings.NewReplacer(
		placeholderNamespace, namespace,
		// Backslashes are escaped again for the PromQL string literal.
		placeholderPods, strings.ReplaceAll(strings.Join(escaped, "|"), `\`, `\\`),
		placeholderWindow, fmt.Sprintf("%ds", int(c.opts.RateWindow.Seconds())),
	)

	ctx, cancel := context.WithTimeout(ctx, c.opts.Timeout)
	defer cancel()

	usage := make(map[string]Usage, len(pods))
	cpu, err := c.byPod(ctx, replacer.Replace(c.opts.CPUQuery))
	if err != nil {
		return nil, fmt.Errorf("prometheus cpu query: %w", err)
	}
	for pod, v := range cpu {
		u := usage[pod]
		u.CPUCores = &v
		usage[pod] = u
	}
	memory, err := c.byPod(ctx, replacer.Replace(c.opts.MemoryQuery))
	if err != nil {
		return nil, fmt.Errorf("prometheus memory query: %w", err)
	}
	for pod, v := range memory {
		u := usage[pod]
		u.MemoryBytes = &v
		usage[pod] = u
	}
	return usage, nil
}

// byPod runs an instant query and returns its samples keyed by "pod" label.
func (c *Client) byPod(ctx context.Context, query string) (map[string]float64, error) {
	value, _, _, err := c.api.Query(ctx, query, time.Now())
	if err != nil {
		return nil, err
	}
	vector, ok := value.(model.Vector)
	if !ok {
		return nil, errors.New("the query did not return an instant vector")
	}
	out := make(map[string]float64, len(vector))
	for _, sample := range vector {
		pod := string(sample.Metric["pod"])
		if pod == "" {
			return nil, errors.New(`the query returned a series without a "pod" label`)
		}
		out[pod] = float64(sample.Value)
	}
	return out, nil
}
