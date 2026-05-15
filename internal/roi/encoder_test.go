package roi

import (
	"reflect"
	"runtime"
	"strconv"
	"testing"
)

func TestEncoderListedMatchesStandaloneToken(t *testing.T) {
	encoders := `
	 V..... h264_nvenc           NVIDIA NVENC H.264 encoder
	 V..... h264_amf             AMD AMF H.264 encoder
	 V..... h264_videotoolbox    VideoToolbox H.264 Encoder
	 V..... libx264              libx264 H.264 / AVC
	`

	if !encoderListed(encoders, "h264_nvenc") {
		t.Fatal("expected h264_nvenc to be detected")
	}
	if !encoderListed(encoders, "h264_amf") {
		t.Fatal("expected h264_amf to be detected")
	}
	if !encoderListed(encoders, "h264_videotoolbox") {
		t.Fatal("expected h264_videotoolbox to be detected")
	}
	if encoderListed(encoders, "nvenc") {
		t.Fatal("partial encoder token should not match")
	}
}

func TestQualityEncoderArgs(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want []string
	}{
		{
			name: "libx264",
			cfg:  Config{VideoEncoder: "libx264", Preset: "veryfast"},
			want: []string{"-c:v", "libx264", "-preset", "veryfast", "-crf", "18"},
		},
		{
			name: "nvenc",
			cfg:  Config{VideoEncoder: "h264_nvenc", NVENCPreset: "p4"},
			want: []string{"-c:v", "h264_nvenc", "-preset", "p4", "-rc", "vbr", "-cq", "18", "-b:v", "0"},
		},
		{
			name: "amf",
			cfg:  Config{VideoEncoder: "h264_amf"},
			want: []string{"-c:v", "h264_amf", "-usage", "transcoding", "-quality", "balanced", "-rc", "cqp", "-qp_i", "18", "-qp_p", "18", "-qp_b", "18"},
		},
		{
			name: "videotoolbox",
			cfg:  Config{VideoEncoder: "h264_videotoolbox"},
			want: wantVideoToolboxQualityArgs(18),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := qualityEncoderArgs(tt.cfg, 18)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("qualityEncoderArgs = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestBitrateEncoderArgsForHardwareEncoders(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want []string
	}{
		{
			name: "nvenc",
			cfg:  Config{VideoEncoder: "h264_nvenc", NVENCPreset: "p5"},
			want: []string{"-c:v", "h264_nvenc", "-preset", "p5", "-rc", "vbr", "-b:v", "500k", "-maxrate", "575k", "-bufsize", "1000k"},
		},
		{
			name: "amf",
			cfg:  Config{VideoEncoder: "h264_amf"},
			want: []string{"-c:v", "h264_amf", "-usage", "transcoding", "-quality", "balanced", "-rc", "vbr_peak", "-b:v", "500k", "-maxrate", "575k", "-bufsize", "1000k"},
		},
		{
			name: "videotoolbox",
			cfg:  Config{VideoEncoder: "h264_videotoolbox"},
			want: []string{"-c:v", "h264_videotoolbox", "-b:v", "500k", "-maxrate", "575k", "-bufsize", "1000k"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := bitrateEncoderArgs(tt.cfg, "500k", "575k", "1000k")
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("bitrateEncoderArgs = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestQPMapEncoderArgsEnableAQ(t *testing.T) {
	x264 := qpMapBitrateEncoderArgs(Config{VideoEncoder: "libx264", Preset: "veryfast"}, "500k", "575k", "1000k")
	if !reflect.DeepEqual(x264[len(x264)-2:], []string{"-aq-mode", "1"}) {
		t.Fatalf("x264 QP-map args tail = %#v, want AQ enabled", x264)
	}

	nvenc := qpMapQualityEncoderArgs(Config{VideoEncoder: "h264_nvenc", NVENCPreset: "p4"}, 18)
	if !reflect.DeepEqual(nvenc[len(nvenc)-2:], []string{"-spatial-aq", "1"}) {
		t.Fatalf("NVENC QP-map args tail = %#v, want spatial AQ enabled", nvenc)
	}

	amf := qpMapQualityEncoderArgs(Config{VideoEncoder: "h264_amf"}, 18)
	if containsArg(amf, "-aq-mode") || containsArg(amf, "-spatial-aq") {
		t.Fatalf("AMF QP-map args should not include x264/NVENC AQ options: %#v", amf)
	}

	videoToolbox := qpMapQualityEncoderArgs(Config{VideoEncoder: "h264_videotoolbox"}, 18)
	if containsArg(videoToolbox, "-aq-mode") || containsArg(videoToolbox, "-spatial-aq") {
		t.Fatalf("VideoToolbox QP-map args should not include x264/NVENC AQ options: %#v", videoToolbox)
	}
}

func TestVideoToolboxQualityValue(t *testing.T) {
	tests := map[int]int{
		18: 82,
		0:  100,
		99: 1,
	}

	for quality, want := range tests {
		if got := videoToolboxQualityValue(quality); got != want {
			t.Fatalf("videoToolboxQualityValue(%d) = %d, want %d", quality, got, want)
		}
	}
}

func wantVideoToolboxQualityArgs(quality int) []string {
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		return []string{"-c:v", "h264_videotoolbox", "-q:v", strconv.Itoa(videoToolboxQualityValue(quality))}
	}
	return []string{"-c:v", "h264_videotoolbox", "-b:v", videoToolboxQualityKbps}
}

func containsArg(args []string, value string) bool {
	for _, arg := range args {
		if arg == value {
			return true
		}
	}
	return false
}
