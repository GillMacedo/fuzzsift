package broken

import "testing"

func TestSplit(t *testing.T) {
	host, port, err := Split("[::1]:9090")
	if host != "::1" || port != 9090 || err != nil {
		t.Fatalf("Split = %q, %d, %v", host, port, err)
	}
}

// FuzzSplit checks that Split doesn't panic, returns a valid port, and
// that Join and Split give the same host and port back.
func FuzzSplit(f *testing.F) {
	for _, s := range []string{"localhost:8080", ":80", "[::1]:9090"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		host, port, err := Split(s)
		if err != nil {
			return
		}
		if port < 1 || port > 65535 {
			t.Fatalf("Split(%q) port = %d", s, port)
		}
		h2, p2, err := Split(Join(host, port))
		if err != nil || h2 != host || p2 != port {
			t.Fatalf("Split(Join(%q, %d)) = %q, %d, %v", host, port, h2, p2, err)
		}
	})
}
