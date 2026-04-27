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
