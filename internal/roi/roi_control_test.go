package roi

import "testing"

func TestValidateROISideDataSupportRejectsUnsupportedQPMapEncoders(t *testing.T) {
	tests := []struct {
		encoder string
	}{
		{encoder: "h264_nvenc"},
		{encoder: "h264_amf"},
		{encoder: "h264_videotoolbox"},
	}

	for _, tt := range tests {
		t.Run(tt.encoder, func(t *testing.T) {
			err := validateROISideDataSupport(Config{
				ROIControl:   "qp-map",
				VideoEncoder: tt.encoder,
			})
			if err == nil {
				t.Fatal("expected unsupported encoder error")
			}
		})
	}
}

func TestValidateROISideDataSupportAcceptsX264QPMapAndHardwareMask(t *testing.T) {
	tests := []Config{
		{ROIControl: "qp-map", VideoEncoder: "libx264"},
		{ROIControl: "qp-map", VideoEncoder: "h264_nvenc_sdk"},
		{ROIControl: "mask", VideoEncoder: "h264_nvenc"},
		{ROIControl: "mask", VideoEncoder: "h264_amf"},
		{ROIControl: "mask", VideoEncoder: "h264_videotoolbox"},
	}

	for _, cfg := range tests {
		if err := validateROISideDataSupport(cfg); err != nil {
			t.Fatalf("validateROISideDataSupport(%s/%s) returned error: %v", cfg.ROIControl, cfg.VideoEncoder, err)
		}
	}
}
