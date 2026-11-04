package netguard

import "testing"

func TestNamespace(t *testing.T) {
	type want struct {
		ns string
		ok bool
	}
	for host, w := range map[string]want{
		// A Service or pod names its namespace in the label before svc/pod.
		"api.shop.svc":                     {"shop", true},
		"api.shop.svc.cluster.local":       {"shop", true},
		"api.shop.svc.":                    {"shop", true}, // rooted
		"API.Shop.SVC":                     {"shop", true}, // lower-cased
		"10-0-0-1.shop.pod.cluster.local":  {"shop", true},
		"pod-0.api.shop.svc.cluster.local": {"shop", true}, // a headless Service's pod
		"svc.shop.svc.cluster.local":       {"shop", true}, // a Service named "svc"
		"api.svc.svc.cluster.local":        {"svc", true},  // a namespace named "svc"
		// No namespace can be told from the host.
		"api":               {"", false}, // a short name, in Kubeat's own namespace
		"172.30.0.1":        {"", false}, // a ClusterIP
		"fd00::1":           {"", false},
		"::ffff:10.0.0.1":   {"", false},
		"foo.cluster.local": {"", false}, // no svc/pod label
		"example.com":       {"", false}, // external; not a cluster name
		"":                  {"", false},
	} {
		if ns, ok := Namespace(host); ns != w.ns || ok != w.ok {
			t.Errorf("Namespace(%q) = (%q, %v), want (%q, %v)", host, ns, ok, w.ns, w.ok)
		}
	}
}

func TestInCluster(t *testing.T) {
	for host, want := range map[string]bool{
		// Addresses in the ranges clusters are built from.
		"10.0.0.1":        true,
		"172.30.0.10":     true, // OpenShift's ClusterIP range
		"172.16.0.1":      true,
		"172.31.255.255":  true,
		"192.168.1.1":     true,
		"100.64.0.1":      true,
		"fd00::1":         true,
		"fc00::1":         true,
		"::ffff:10.0.0.1": true, // IPv4 written as IPv6
		"127.0.0.1":       true, // denied by the policy, but not external
		"169.254.169.254": true,
		"::1":             true,
		"fe80::1%eth0":    true,
		// Addresses outside them.
		"8.8.8.8":          false,
		"172.32.0.1":       false, // just past 172.16.0.0/12
		"172.15.255.255":   false,
		"100.128.0.1":      false, // just past 100.64.0.0/10
		"2001:db8::1":      false,
		"64:ff9b::808:808": false,
		// Names.
		"api":                             true, // a Service's short name
		"api.shop.svc":                    true,
		"api.shop.svc.cluster.local":      true,
		"API.Shop.SVC":                    true,
		"api.shop.svc.":                   true, // rooted
		"10-0-0-1.shop.pod.cluster.local": true,
		"example.com":                     false,
		"api.example.com":                 false,
		"svc.example.com":                 false, // .svc in the middle is not the suffix
		"example.svcs":                    false,
		"intranet.corp":                   false, // a public-looking name resolving privately counts as external
		"localhost":                       true,
		"":                                false,
	} {
		if got := InCluster(host); got != want {
			t.Errorf("InCluster(%q) = %v, want %v", host, got, want)
		}
	}
}
