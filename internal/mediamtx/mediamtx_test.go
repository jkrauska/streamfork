package mediamtx

import "testing"

func TestSourceHost(t *testing.T) {
	tests := []struct {
		addr string
		want string
	}{
		{"192.168.1.10:54321", "192.168.1.10"},
		{"[::1]:58216", "::1"},
		{"", ""},
		{"no-port", "no-port"},
	}

	for _, tc := range tests {
		if got := sourceHost(tc.addr); got != tc.want {
			t.Fatalf("sourceHost(%q) = %q, want %q", tc.addr, got, tc.want)
		}
	}
}
