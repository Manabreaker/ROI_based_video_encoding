package roi

import "testing"

func validTestConfig() Config {
	return Config{
		Input:                "input.mp4",
		TargetBitrate:        "500k",
		Tolerance:            0.07,
		ROIControl:           "qp-map",
		ROIQOffset:           -0.30,
		ROIMiddleQOffset:     -0.10,
		ROIBlockSize:         defaultROIBlockSize,
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
		CVModel:              defaultCVModelName,
		CVMinScore:           5.0,
		CVSampleCount:        12,
		CVFrameWidth:         960,
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

func TestValidateConfigAcceptsHardwareEncoders(t *testing.T) {
	for _, encoder := range []string{"h264_nvenc", "h264_amf", "h264_videotoolbox"} {
		cfg := validTestConfig()
		cfg.VideoEncoder = encoder
		if err := validateConfig(cfg); err != nil {
			t.Fatalf("validateConfig rejected %s: %v", encoder, err)
		}
	}
}

func TestValidateConfigRejectsBadMiddleSettings(t *testing.T) {
	cfg := validTestConfig()
	cfg.MiddleScale = 1.5

	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected bad middle scale error")
	}
}

func TestValidateConfigRejectsBadROIControl(t *testing.T) {
	cfg := validTestConfig()
	cfg.ROIControl = "magic"

	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected bad ROI control error")
	}
}

func TestValidateConfigRejectsBadROIQOffset(t *testing.T) {
	cfg := validTestConfig()
	cfg.ROIQOffset = -1.5

	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected bad ROI qoffset error")
	}
}

func TestValidateConfigAcceptsBlockROI(t *testing.T) {
	cfg := validTestConfig()
	cfg.Mode = "blocks"
	cfg.ROIBlocks = []QPMapBlock{{Col: 1, Row: 2, QOffset: -0.35}}

	if err := validateConfig(cfg); err != nil {
		t.Fatalf("validateConfig returned error: %v", err)
	}
}

func TestValidateConfigRejectsBlockROIWithoutBlocks(t *testing.T) {
	cfg := validTestConfig()
	cfg.Mode = "blocks"

	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected missing ROI blocks error")
	}
}

func TestValidateConfigRejectsBadBlockQOffset(t *testing.T) {
	cfg := validTestConfig()
	cfg.Mode = "blocks"
	cfg.ROIBlocks = []QPMapBlock{{Col: 1, Row: 2, QOffset: -1.35}}

	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected bad block qoffset error")
	}
}
