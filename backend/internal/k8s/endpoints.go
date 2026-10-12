package k8s

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"

	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Endpoint is one pod behind a service port, as the cluster routes to it.
type Endpoint struct {
	Pod string
	// Address is "ip:port" on the pod itself.
	Address string
}

// PodEndpoints lists the ready pods behind the service port a monitor was
// discovered from (source "service/name:port"), read from the service's
// EndpointSlices: the same addresses the cluster's own load balancing uses.
func (c *Client) PodEndpoints(ctx context.Context, namespace, source string) ([]Endpoint, error) {
	kind, rest, _ := strings.Cut(source, "/")
	name, portText, _ := strings.Cut(rest, ":")
	port, err := strconv.Atoi(portText)
	if Kind(kind) != KindService || name == "" || err != nil {
		return nil, fmt.Errorf("%w: %q is not a service port", ErrNoWorkload, source)
	}

	svc, err := c.core.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("%w: service %s/%s does not exist", ErrNoWorkload, namespace, name)
	}
	if err != nil {
		return nil, fmt.Errorf("get service: %w", err)
	}
	// Endpoint ports carry the service port's name, not its number.
	portName, found := "", false
	for _, p := range svc.Spec.Ports {
		if int(p.Port) == port {
			portName, found = p.Name, true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("%w: service %s/%s has no port %d", ErrNoWorkload, namespace, name, port)
	}

	slices, err := c.core.DiscoveryV1().EndpointSlices(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: discoveryv1.LabelServiceName + "=" + name,
	})
	if err != nil {
		return nil, fmt.Errorf("list endpointslices: %w", err)
	}
	var out []Endpoint
	for _, slice := range slices.Items {
		if slice.AddressType != discoveryv1.AddressTypeIPv4 && slice.AddressType != discoveryv1.AddressTypeIPv6 {
			continue
		}
		targetPort := 0
		for _, p := range slice.Ports {
			if p.Port != nil && ((p.Name == nil && portName == "") || (p.Name != nil && *p.Name == portName)) {
				targetPort = int(*p.Port)
				break
			}
		}
		if targetPort == 0 {
			continue
		}
		for _, ep := range slice.Endpoints {
			// Only what the service would send traffic to: a pod that is not
			// ready is the workload monitor's business.
			if len(ep.Addresses) == 0 || (ep.Conditions.Ready != nil && !*ep.Conditions.Ready) {
				continue
			}
			pod := ""
			if ep.TargetRef != nil {
				pod = ep.TargetRef.Name
			}
			if pod == "" {
				pod = ep.Addresses[0]
			}
			out = append(out, Endpoint{Pod: pod, Address: net.JoinHostPort(ep.Addresses[0], strconv.Itoa(targetPort))})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Pod < out[j].Pod })
	return out, nil
}
