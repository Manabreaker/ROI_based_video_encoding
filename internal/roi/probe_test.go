package roi

import (
	"math"
	"testing"
)

func TestParseRate(t *testing.T) {
	tests := []struct {
		in   string
		want float64
	}{
		{in: "30000/1001", want: 29.97002997},
		{in: "24/1", want: 24},
		{in: "25", want: 25},
		{in: "0/0", want: 0},
	}

	for _, tt := range tests {
		got := parseRate(tt.in)
		if math.Abs(got-tt.want) > 0.0001 {
			t.Fatalf("parseRate(%q) = %.8f, want %.8f", tt.in, got, tt.want)
		}
	}
}

func TestParseFloatOrNaN(t *testing.T) {
	if !math.IsNaN(parseFloatOrNaN("N/A")) {
		t.Fatal("N/A should parse as NaN")
	}
	if got := parseFloatOrZero("12.5"); got != 12.5 {
		t.Fatalf("parseFloatOrZero = %.1f, want 12.5", got)
	}
}
