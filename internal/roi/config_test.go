package roi

import "testing"

func validTestConfig() Config {
	return Config{
		Input:                "input.mp4",
		TargetBitrate:        "500k",
		Tolerance:            0.07,
		ROIHighQualityCRF:    16,
		ROIMinCRF:            10,
		ROIMaxCRFIfNeeded:    36,
		ManualPeripheryScale: 0.35,
		ManualBlurRadius:     2,
		MiddleMargin:         0.35,
		MiddleScale:          0.67,
		MiddleBlurRadius:     1,
		ROIMinScale:          0.12,
		ROIMaxBlur:           10,
		ROIRateControl:       "abr",
		ROIPSNRTieDB:         0.25,
		ROIMaxrateMultiplier: 1.15,
		ROIBufsizeSeconds:    2,
		VideoEncoder:         "auto",
		NVENCPreset:          "p4",
		FitIterations:        9,
		BitrateWindow:        1,
		MaxBitrateOverlays:   300,
		OverlayBitrate:       true,
		Metrics:              true,
		AllowROIQualityLoss:  false,
		FitROI:               true,
		ROITwoPass:           true,
		ROIFitMetric:         true,
		MotionWindow:         0.6,
		MotionThresh:         34,
		ROIMargin:            0.18,
		Preset:               "veryfast",
		OutDir:               "out",
		HTTPAddr:             ":8080",
		Serve:                false,
		KeepTemp:             false,
	}
}

func TestValidateConfigAcceptsDefaultLikeConfig(t *testing.T) {
	if err := validateConfig(validTestConfig()); err != nil {
		t.Fatalf("validateConfig returned error: %v", err)
	}
}

func TestValidateConfigRejectsBadEncoder(t *testing.T) {
	cfg := validTestConfig()
	cfg.VideoEncoder = "h265_magic"

	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected bad encoder error")
	}
}

func TestValidateConfigRejectsBadMiddleSettings(t *testing.T) {
	cfg := validTestConfig()
	cfg.MiddleScale = 1.5

	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected bad middle scale error")
	}
}
