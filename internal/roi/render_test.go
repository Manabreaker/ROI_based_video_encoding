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

func TestPanelBaseFiltersForBlockQPMapROI(t *testing.T) {
	filters := panelBaseFilters("ROI output", EncodeDecision{
		TargetKbps:      500,
		ActualKbps:      495,
		WithinTolerance: true,
		ROIControl:      "qp-map",
		RateControl:     "abr",
		ROIBlockSize:    64,
		ROIBlockCount:   7,
	}, true)
	joined := strings.Join(filters, ",")

	for _, part := range []string{
		"ROI output",
		"within tolerance | ABR QP-map",
		"QP blocks 7 | 64 px",
	} {
		if !strings.Contains(joined, part) {
			t.Fatalf("block QP-map ROI panel does not contain %q:\n%s", part, joined)
		}
	}
}

func TestROIBlockBoxColorMatchesUIPalette(t *testing.T) {
	tests := map[float64]string{
		-0.40: "lime@0.95",
		-0.25: "orange@0.95",
		-0.10: "yellow@0.95",
		0.15:  "red@0.95",
	}

	for qoffset, want := range tests {
		if got := roiBlockBoxColor(qoffset); got != want {
			t.Fatalf("roiBlockBoxColor(%.2f) = %q, want %q", qoffset, got, want)
		}
	}
}

func TestROIBlockDrawBoxesMergesAdjacentSameColorBlocks(t *testing.T) {
	boxes, err := roiBlockDrawBoxes(
		Config{
			ROIBlockSize: 64,
			ROIBlocks: []QPMapBlock{
				{Col: 0, Row: 0, QOffset: -0.40},
				{Col: 1, Row: 0, QOffset: -0.40},
				{Col: 0, Row: 1, QOffset: -0.40},
				{Col: 1, Row: 1, QOffset: -0.40},
				{Col: 3, Row: 0, QOffset: -0.25},
			},
		},
		VideoInfo{Width: 320, Height: 192},
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(boxes, ",")

	if len(boxes) != 2 {
		t.Fatalf("draw boxes = %d, want 2 merged boxes: %#v", len(boxes), boxes)
	}
	if !strings.Contains(joined, "drawbox=x=0:y=0:w=128:h=128:color=lime@0.95:t=3") {
		t.Fatalf("missing merged green 2x2 box:\n%s", joined)
	}
	if strings.Contains(joined, "x=64:y=0:w=64:h=64") || strings.Contains(joined, "x=0:y=64:w=64:h=64") {
		t.Fatalf("internal block edges should not be drawn:\n%s", joined)
	}
	if !strings.Contains(joined, "drawbox=x=192:y=0:w=64:h=64:color=orange@0.95:t=3") {
		t.Fatalf("missing separate orange box:\n%s", joined)
	}
}

func TestROIBlockDrawBoxesKeepsDifferentColorsSeparate(t *testing.T) {
	boxes, err := roiBlockDrawBoxes(
		Config{
			ROIBlockSize: 64,
			ROIBlocks: []QPMapBlock{
				{Col: 0, Row: 0, QOffset: -0.40},
				{Col: 1, Row: 0, QOffset: -0.25},
			},
		},
		VideoInfo{Width: 192, Height: 128},
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(boxes, ",")

	if len(boxes) != 2 {
		t.Fatalf("draw boxes = %d, want separate color boxes: %#v", len(boxes), boxes)
	}
	if !strings.Contains(joined, "color=lime@0.95") || !strings.Contains(joined, "color=orange@0.95") {
		t.Fatalf("different qoffset colors should remain visible:\n%s", joined)
	}
}

func TestComparisonDrawBoxesMirrorsStaticQPMapZones(t *testing.T) {
	boxes, err := comparisonDrawBoxes(
		Config{
			ROIControl:       "qp-map",
			ROIMiddleQOffset: -0.10,
			MiddleMargin:     0.25,
		},
		VideoInfo{Width: 640, Height: 360},
		ROI{X: 160, Y: 90, W: 160, H: 90},
		EncodeDecision{ROIControl: "qp-map"},
	)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(boxes, ",")

	if got := strings.Count(joined, "color=orange@0.95"); got != 2 {
		t.Fatalf("orange middle boxes = %d, want 2:\n%s", got, joined)
	}
	if got := strings.Count(joined, "color=lime@0.90"); got != 2 {
		t.Fatalf("green ROI boxes = %d, want 2:\n%s", got, joined)
	}
	if strings.Contains(joined, "color=red@0.90") {
		t.Fatalf("QP-map comparison should not draw mask periphery red:\n%s", joined)
	}
}

func TestComparisonDrawBoxesHidesDisabledQPMapMiddleZone(t *testing.T) {
	boxes, err := comparisonDrawBoxes(
		Config{
			ROIControl:       "qp-map",
			ROIMiddleQOffset: 0,
			MiddleMargin:     0.25,
		},
		VideoInfo{Width: 640, Height: 360},
		ROI{X: 160, Y: 90, W: 160, H: 90},
		EncodeDecision{ROIControl: "qp-map"},
	)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(boxes, ",")

	if strings.Contains(joined, "color=orange@0.95") {
		t.Fatalf("disabled QP-map middle qoffset should not draw orange middle box:\n%s", joined)
	}
	if got := strings.Count(joined, "color=lime@0.90"); got != 2 {
		t.Fatalf("green ROI boxes = %d, want 2:\n%s", got, joined)
	}
}

func TestComparisonDrawBoxesMirrorsMaskZones(t *testing.T) {
	boxes, err := comparisonDrawBoxes(
		Config{
			ROIControl:   "mask",
			MiddleMargin: 0.25,
		},
		VideoInfo{Width: 640, Height: 360},
		ROI{X: 160, Y: 90, W: 160, H: 90},
		EncodeDecision{ROIControl: "mask"},
	)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(boxes, ",")

	for color, want := range map[string]int{
		"color=red@0.90":    2,
		"color=orange@0.95": 2,
		"color=lime@0.90":   2,
	} {
		if got := strings.Count(joined, color); got != want {
			t.Fatalf("%s boxes = %d, want %d:\n%s", color, got, want, joined)
		}
	}
}

func TestComparisonDrawBoxesUsesMovingTimelineEnables(t *testing.T) {
	boxes, err := comparisonDrawBoxesForSelection(
		Config{
			ROIControl:       "qp-map",
			ROIMiddleQOffset: 0,
			MiddleMargin:     0.25,
		},
		VideoInfo{Width: 320, Height: 180},
		ROISelection{
			ROI: ROI{X: 10, Y: 20, W: 60, H: 40},
			Timeline: []TimedROI{
				{StartSeconds: 0, EndSeconds: 1.5, ROI: ROI{X: 10, Y: 20, W: 60, H: 40}},
				{StartSeconds: 1.5, EndSeconds: 3.0, ROI: ROI{X: 120, Y: 30, W: 60, H: 40}},
			},
		},
		EncodeDecision{ROIControl: "qp-map"},
	)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(boxes, ",")

	for _, part := range []string{
		"drawbox=x=10:y=20:w=60:h=40:color=lime@0.90:t=4:enable='between(t\\,0.000\\,1.500)'",
		"drawbox=x=120:y=30:w=60:h=40:color=lime@0.90:t=4:enable='between(t\\,1.500\\,3.000)'",
		"drawbox=x=330:y=20:w=60:h=40:color=lime@0.90:t=4:enable='between(t\\,0.000\\,1.500)'",
	} {
		if !strings.Contains(joined, part) {
			t.Fatalf("moving comparison boxes do not contain %q:\n%s", part, joined)
		}
	}
}

func TestBuildComparisonFilterScalesWideHardwareH264HStack(t *testing.T) {
	for _, cfg := range []Config{
		{VideoEncoder: "h264_nvenc", NVENCPreset: "p4"},
		{VideoEncoder: "h264_amf"},
		{VideoEncoder: "h264_videotoolbox"},
	} {
		filter, scaled, err := buildComparisonFilter(
			cfg,
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
			t.Fatalf("expected wide %s comparison to be scaled", cfg.VideoEncoder)
		}
		if !strings.Contains(filter, "hstack=inputs=2") {
			t.Fatalf("comparison filter does not contain hstack:\n%s", filter)
		}
		if !strings.Contains(filter, "scale=w=4096:h=-2") {
			t.Fatalf("comparison filter does not limit hardware H.264 width:\n%s", filter)
		}
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
		t.Fatal("did not expect libx264 comparison to be scaled for hardware H.264 width")
	}
	if strings.Contains(filter, "scale=w=4096:h=-2") {
		t.Fatalf("x264 comparison filter unexpectedly limits hardware H.264 width:\n%s", filter)
	}
}
