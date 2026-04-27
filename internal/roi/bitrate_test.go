package roi

import (
	"math"
	"testing"
)

func TestParseBitrateKbps(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want float64
		ok   bool
	}{
		{name: "kilobits", in: "500k", want: 500, ok: true},
		{name: "megabits", in: "1.5M", want: 1500, ok: true},
		{name: "bits per second", in: "1000000", want: 1000, ok: true},
		{name: "with bps suffix", in: "750kbps", want: 750, ok: true},
		{name: "invalid", in: "fast", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseBitrateKbps(tt.in)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if ok && math.Abs(got-tt.want) > 0.001 {
				t.Fatalf("parseBitrateKbps(%q) = %.3f, want %.3f", tt.in, got, tt.want)
			}
		})
	}
}

func TestSummarizeBitrateUsesWindowDuration(t *testing.T) {
	summary := summarizeBitrate([]BitrateSample{
		{Start: 0, End: 1, Kbps: 100},
		{Start: 1, End: 3, Kbps: 400},
		{Start: 3, End: 4, Kbps: 700},
	})

	if math.Abs(summary.AverageKbps-400) > 0.001 {
		t.Fatalf("AverageKbps = %.3f, want 400", summary.AverageKbps)
	}
	if summary.MinKbps != 100 || summary.MaxKbps != 700 {
		t.Fatalf("min/max = %.1f/%.1f, want 100/700", summary.MinKbps, summary.MaxKbps)
	}
	if summary.P50Kbps != 400 {
		t.Fatalf("P50Kbps = %.1f, want 400", summary.P50Kbps)
	}
}

func TestInputBaselineDecisionUsesArtifactFallback(t *testing.T) {
	decision := inputBaselineDecision(BitrateSummary{}, Artifact{
		SizeBytes:   2048,
		BitrateKbps: 123.4,
	})

	if decision.Name != "input-baseline" {
		t.Fatalf("Name = %q", decision.Name)
	}
	if decision.ActualKbps != 123.4 {
		t.Fatalf("ActualKbps = %.1f, want 123.4", decision.ActualKbps)
	}
	if decision.RateControl != "source" {
		t.Fatalf("RateControl = %q, want source", decision.RateControl)
	}
	if decision.SizeBytes != 2048 {
		t.Fatalf("SizeBytes = %d, want 2048", decision.SizeBytes)
	}
}
