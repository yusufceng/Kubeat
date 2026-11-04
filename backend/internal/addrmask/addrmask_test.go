package addrmask

import "testing"

func TestMaskHidesResolvedAddresses(t *testing.T) {
	cases := map[string]string{
		"dial tcp 172.30.126.132:8080: connect: connection refused": "dial tcp [address hidden]: connect: connection refused",
		"lookup test-mail on 10.0.0.10:53: no such host":            "lookup test-mail on [address hidden]: no such host",
		"dial tcp [fd00::1]:443: i/o timeout":                       "dial tcp [address hidden]: i/o timeout",
		// No address: the class is kept untouched.
		"connection refused": "connection refused",
	}
	for in, want := range cases {
		if got := Mask(in); got != want {
			t.Errorf("Mask(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMaskLeavesHostnames(t *testing.T) {
	// A host name is not an address and must survive, so the user still sees
	// what the check or the channel tried to reach.
	in := "Post \"https://hooks.example.com/x\": dial tcp: lookup hooks.example.com: no such host"
	if got := Mask(in); got != in {
		t.Errorf("Mask changed a hostname-only message: %q", got)
	}
}
