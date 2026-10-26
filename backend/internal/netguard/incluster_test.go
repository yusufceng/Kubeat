package netguard

import "testing"

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
