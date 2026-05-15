package roi

import (
	"strings"
	"testing"
)

func TestParseROIWithFractions(t *testing.T) {
	got, err := parseROI("0.25,0.10,0.50,0.40", VideoInfo{Width: 1920, Height: 1080})
	if err != nil {
		t.Fatalf("parseROI returned error: %v", err)
	}

	want := ROI{X: 480, Y: 108, W: 960, H: 432}
	if got.X != want.X || got.Y != want.Y || got.W != want.W || got.H != want.H {
		t.Fatalf("ROI = %+v, want %+v", got, want)
	}
}

func TestClampROIKeepsEvenCoordinatesInsideFrame(t *testing.T) {
	got := clampROI(ROI{X: -3, Y: 97, W: 99, H: 99}, VideoInfo{Width: 100, Height: 100})

	if got.X < 0 || got.Y < 0 || got.X+got.W > 100 || got.Y+got.H > 100 {
		t.Fatalf("ROI is outside frame: %+v", got)
	}
	if got.X%2 != 0 || got.Y%2 != 0 || got.W%2 != 0 || got.H%2 != 0 {
		t.Fatalf("ROI should use even yuv420p-compatible values: %+v", got)
	}
}

func TestMiddleQualitySettingsNeverWorseThanLowLayer(t *testing.T) {
	cfg := Config{MiddleScale: 0.67, MiddleBlurRadius: 1}

	scale, blur := middleQualitySettings(cfg, 0.80, 0)
	if scale != 0.80 {
		t.Fatalf("scale = %.2f, want 0.80", scale)
	}
	if blur != 0 {
		t.Fatalf("blur = %d, want 0", blur)
	}

	scale, blur = middleQualitySettings(cfg, 0.35, 4)
	if scale != 0.67 || blur != 1 {
		t.Fatalf("middle settings = %.2f/%d, want 0.67/1", scale, blur)
	}
}

func TestBuildROIFilterUsesLowMiddleAndROIOverlays(t *testing.T) {
	cfg := Config{MiddleMargin: 0.25, MiddleScale: 0.67, MiddleBlurRadius: 1}
	filter := buildROIFilter(
		cfg,
		VideoInfo{Width: 640, Height: 360},
		ROI{X: 160, Y: 90, W: 160, H: 90},
		0.35,
		4,
	)

	for _, part := range []string{
		"split=3[lowsrc][middlesrc][roisrc]",
		"[lowsrc]scale=",
		"[middlesrc]scale=",
		"crop=240:134:120:68[mid]",
		"crop=160:90:160:90",
		"[low][mid]overlay=120:68",
		"[withmid][roi]overlay=160:90",
	} {
		if !strings.Contains(filter, part) {
			t.Fatalf("filter does not contain %q:\n%s", part, filter)
		}
	}
}

func TestBuildROIQPMapFilterUsesAddROI(t *testing.T) {
	cfg := Config{
		MiddleMargin:     0.25,
		ROIQOffset:       -0.30,
		ROIMiddleQOffset: -0.10,
	}

	filter, err := buildROIQPMapFilter(
		cfg,
		VideoInfo{Width: 640, Height: 360},
		ROI{X: 160, Y: 90, W: 160, H: 90},
	)
	if err != nil {
		t.Fatal(err)
	}

	for _, part := range []string{
		"addroi=x=120:y=68:w=240:h=134:qoffset=-0.1000:clear=1",
		"addroi=x=160:y=90:w=160:h=90:qoffset=-0.3000",
		"format=yuv420p[v]",
	} {
		if !strings.Contains(filter, part) {
			t.Fatalf("QP-map filter does not contain %q:\n%s", part, filter)
		}
	}
}

func TestBuildROIQPMapFilterUsesBlockMap(t *testing.T) {
	cfg := Config{
		ROIBlockSize: defaultROIBlockSize,
		ROIBlocks: []QPMapBlock{
			{Col: 1, Row: 2, QOffset: -0.40},
			{Col: 2, Row: 2, W: 2, H: 1, QOffset: -0.20},
		},
	}

	filter, err := buildROIQPMapFilter(
		cfg,
		VideoInfo{Width: 320, Height: 256},
		ROI{},
	)
	if err != nil {
		t.Fatal(err)
	}

	for _, part := range []string{
		"addroi=x=64:y=128:w=64:h=64:qoffset=-0.4000:clear=1",
		"addroi=x=128:y=128:w=64:h=64:qoffset=-0.2000",
		"addroi=x=192:y=128:w=64:h=64:qoffset=-0.2000",
		"format=yuv420p[v]",
	} {
		if !strings.Contains(filter, part) {
			t.Fatalf("block QP-map filter does not contain %q:\n%s", part, filter)
		}
	}
}

func TestSelectROIUsesBlockMapBoundingBox(t *testing.T) {
	cfg := Config{
		Mode:         "blocks",
		ROIBlockSize: defaultROIBlockSize,
		ROIBlocks: []QPMapBlock{
			{Col: 1, Row: 1, W: 2, H: 1, QOffset: -0.30},
			{Col: 4, Row: 3, QOffset: -0.15},
		},
	}

	got, err := selectROI(cfg, VideoInfo{Width: 384, Height: 320}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	if got.X != 64 || got.Y != 64 || got.W != 256 || got.H != 192 {
		t.Fatalf("block ROI = %+v, want x=64 y=64 w=256 h=192", got)
	}
	if got.Source != "qp-blocks-64px" {
		t.Fatalf("Source = %q, want qp-blocks-64px", got.Source)
	}
}
