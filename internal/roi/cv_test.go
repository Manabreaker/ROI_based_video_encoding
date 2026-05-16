package roi

import (
	"image"
	"testing"

	pigo "github.com/esimov/pigo/core"
)

func TestLoadDefaultCVModelUnpacks(t *testing.T) {
	model, name, err := loadCVModel(defaultCVModelName)
	if err != nil {
		t.Fatal(err)
	}
	if name != defaultCVModelName {
		t.Fatalf("model name = %q, want %q", name, defaultCVModelName)
	}
	if len(model) == 0 {
		t.Fatal("default CV model is empty")
	}
	if _, err := pigo.NewPigo().Unpack(model); err != nil {
		t.Fatalf("default CV model does not unpack: %v", err)
	}
}

func TestCVSampleTimes(t *testing.T) {
	got := cvSampleTimes(10, 3)
	want := []float64{0.5, 5, 9.5}

	if len(got) != len(want) {
		t.Fatalf("len(cvSampleTimes) = %d, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("cvSampleTimes[%d] = %.2f, want %.2f", i, got[i], want[i])
		}
	}
}

func TestROIModeAcceptsCVAliases(t *testing.T) {
	for _, mode := range []string{"cv", "model", "face", "pigo-facefinder"} {
		got := roiMode(Config{Mode: mode})
		if got != "cv" {
			t.Fatalf("roiMode(%q) = %q, want cv", mode, got)
		}
	}
}

func TestValidateConfigAcceptsCVMode(t *testing.T) {
	cfg := validTestConfig()
	cfg.Mode = "cv"

	if err := validateConfig(cfg); err != nil {
		t.Fatalf("validateConfig rejected cv mode: %v", err)
	}
}

func TestValidateConfigRejectsBadCVSettings(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "negative score", mutate: func(cfg *Config) { cfg.CVMinScore = -1 }},
		{name: "zero samples", mutate: func(cfg *Config) { cfg.CVSampleCount = 0 }},
		{name: "too many samples", mutate: func(cfg *Config) { cfg.CVSampleCount = 121 }},
		{name: "oversized frame", mutate: func(cfg *Config) { cfg.CVFrameWidth = 4097 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validTestConfig()
			cfg.Mode = "cv"
			tt.mutate(&cfg)

			if err := validateConfig(cfg); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestTimedROIFromSamplesBuildsSegmentsAndMergesHolds(t *testing.T) {
	got := timedROIFromSamples(
		[]cvROISample{
			{Time: 0.5, ROI: ROI{X: 10, Y: 20, W: 30, H: 40}},
			{Time: 1.5, ROI: ROI{X: 10, Y: 20, W: 30, H: 40}},
			{Time: 2.5, ROI: ROI{X: 100, Y: 20, W: 30, H: 40}},
		},
		3.0,
	)

	if len(got) != 2 {
		t.Fatalf("timeline len = %d, want 2: %+v", len(got), got)
	}
	if got[0].StartSeconds != 0 || got[0].EndSeconds != 2.0 {
		t.Fatalf("first segment = %.1f..%.1f, want 0.0..2.0", got[0].StartSeconds, got[0].EndSeconds)
	}
	if got[1].StartSeconds != 2.0 || got[1].EndSeconds != 3.0 {
		t.Fatalf("second segment = %.1f..%.1f, want 2.0..3.0", got[1].StartSeconds, got[1].EndSeconds)
	}
}

func TestPigoDetectionROIMapsScaledFrameToSource(t *testing.T) {
	got := pigoDetectionROI(
		pigo.Detection{Col: 100, Row: 50, Scale: 40, Q: 10},
		image.Rect(0, 0, 200, 100),
		VideoInfo{Width: 400, Height: 200},
	)

	if got.X != 160 || got.Y != 60 || got.W != 80 || got.H != 80 {
		t.Fatalf("ROI = %+v, want x=160 y=60 w=80 h=80", got)
	}
}

func TestUnionROI(t *testing.T) {
	got := unionROI(
		ROI{X: 20, Y: 30, W: 50, H: 40},
		ROI{X: 10, Y: 50, W: 100, H: 20},
	)

	if got.X != 10 || got.Y != 30 || got.W != 100 || got.H != 40 {
		t.Fatalf("union ROI = %+v, want x=10 y=30 w=100 h=40", got)
	}
}
