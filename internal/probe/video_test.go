package probe

import "testing"

func TestParseFrameRate(t *testing.T) {
	tests := []struct {
		in   string
		want float64
	}{
		{"30000/1001", 30000.0 / 1001.0},
		{"30/1", 30},
		{"0/0", 0},
		{"", 0},
		{"29.97", 29.97},
	}

	for _, tc := range tests {
		got := parseFrameRate(tc.in)
		if got != tc.want && !(tc.want != 0 && abs(got-tc.want) < 0.001) {
			t.Fatalf("parseFrameRate(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
