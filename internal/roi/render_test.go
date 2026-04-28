package roi

import (
	"strings"
	"testing"
)

func TestEscapeDrawText(t *testing.T) {
	got := escapeDrawText("a:b,c%'\\")
	want := `a\:b\,c\%\'\\`

	if got != want {
		t.Fatalf("escapeDrawText = %q, want %q", got, want)
	}
}

func TestPanelBaseFiltersForSourceBaseline(t *testing.T) {
	filters := panelBaseFilters("INPUT baseline", EncodeDecision{
		ActualKbps:  855,
		RateControl: "source",
		SizeBytes:   1024,
	}, false)
	joined := strings.Join(filters, ",")

	for _, part := range []string{
		"INPUT baseline",
		"source avg 855 kbps",
		"1.0 KB",
		`original input\, not re-encoded`,
	} {
		if !strings.Contains(joined, part) {
			t.Fatalf("source panel does not contain %q:\n%s", part, joined)
		}
	}
}

func TestPanelBaseFiltersForThreeZoneROI(t *testing.T) {
	filters := panelBaseFilters("ROI output", EncodeDecision{
		TargetKbps:      500,
		ActualKbps:      405,
		WithinTolerance: false,
		RateControl:     "abr",
		Scale:           0.44,
		Blur:            2,
		MiddleScale:     0.67,
		MiddleBlur:      1,
	}, true)
	joined := strings.Join(filters, ",")

	for _, part := range []string{
		"ROI output",
		"closest possible | ABR target-rate",
		"G ROI | O 0.67 b1 | R 0.44 b2",
	} {
		if !strings.Contains(joined, part) {
			t.Fatalf("ROI panel does not contain %q:\n%s", part, joined)
		}
	}
}

func TestPanelBaseFiltersForQPMapROI(t *testing.T) {
	filters := panelBaseFilters("ROI output", EncodeDecision{
		TargetKbps:      500,
		ActualKbps:      495,
		WithinTolerance: true,
		ROIControl:      "qp-map",
		RateControl:     "abr",
		ROIQOffset:      -0.30,
		MiddleQOffset:   -0.10,
	}, true)
	joined := strings.Join(filters, ",")

	for _, part := range []string{
		"ROI output",
		"within tolerance | ABR QP-map",
		"ROI qoffset -0.30 | MID -0.10",
	} {
		if !strings.Contains(joined, part) {
			t.Fatalf("QP-map ROI panel does not contain %q:\n%s", part, joined)
		}
	}
}

func TestBuildComparisonFilterScalesWideNVENCHStack(t *testing.T) {
	filter, scaled, err := buildComparisonFilter(
		Config{VideoEncoder: "h264_nvenc", NVENCPreset: "p4"},
		nil,
		nil,
		VideoInfo{Width: 3840, Height: 2160},
		ROI{X: 384, Y: 216, W: 960, H: 540},
		EncodeDecision{RateControl: "source", ActualKbps: 7000},
		EncodeDecision{ROIControl: "qp-map", RateControl: "abr", TargetKbps: 3500, ActualKbps: 3500},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !scaled {
		t.Fatal("expected wide h264_nvenc comparison to be scaled")
	}
	if !strings.Contains(filter, "hstack=inputs=2") {
		t.Fatalf("comparison filter does not contain hstack:\n%s", filter)
	}
	if !strings.Contains(filter, "scale=w=4096:h=-2") {
		t.Fatalf("comparison filter does not limit NVENC width:\n%s", filter)
	}
}

func TestBuildComparisonFilterKeepsWideX264HStackFullSize(t *testing.T) {
	filter, scaled, err := buildComparisonFilter(
		Config{VideoEncoder: "libx264", Preset: "veryfast"},
		nil,
		nil,
		VideoInfo{Width: 3840, Height: 2160},
		ROI{X: 384, Y: 216, W: 960, H: 540},
		EncodeDecision{RateControl: "source", ActualKbps: 7000},
		EncodeDecision{ROIControl: "qp-map", RateControl: "abr", TargetKbps: 3500, ActualKbps: 3500},
	)
	if err != nil {
		t.Fatal(err)
	}
	if scaled {
		t.Fatal("did not expect libx264 comparison to be scaled for NVENC width")
	}
	if strings.Contains(filter, "scale=w=4096:h=-2") {
		t.Fatalf("x264 comparison filter unexpectedly limits NVENC width:\n%s", filter)
	}
}
