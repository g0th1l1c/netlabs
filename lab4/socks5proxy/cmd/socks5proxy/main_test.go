package main

import "testing"

func TestParseResolver(t *testing.T) {
	a, err := parseResolver("127.0.0.1:5353")
	if err != nil || a.Port != 5353 || a.Addr != [4]byte{127, 0, 0, 1} {
		t.Fatalf("%v %v", a, err)
	}
	for _, s := range []string{"1.1.1.1", "localhost:53", "[::1]:53", "[::ffff:1.1.1.1]:53", "1.1.1.1:0", "1.1.1.1:65536", "1.1.1.1:-1", "256.1.1.1:53", "1.1.1.1:abc"} {
		if _, err := parseResolver(s); err == nil {
			t.Errorf("accepted %q", s)
		}
	}
}
