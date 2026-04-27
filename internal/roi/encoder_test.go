package roi

import (
	"reflect"
	"testing"
)

func TestEncoderListedMatchesStandaloneToken(t *testing.T) {
	encoders := `
 V..... h264_nvenc           NVIDIA NVENC H.264 encoder
 V..... libx264              libx264 H.264 / AVC
`

	if !encoderListed(encoders, "h264_nvenc") {
		t.Fatal("expected h264_nvenc to be detected")
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

func TestBitrateEncoderArgsForNVENC(t *testing.T) {
	got := bitrateEncoderArgs(Config{VideoEncoder: "h264_nvenc", NVENCPreset: "p5"}, "500k", "575k", "1000k")
	want := []string{
		"-c:v", "h264_nvenc",
		"-preset", "p5",
		"-rc", "vbr",
		"-b:v", "500k",
		"-maxrate", "575k",
		"-bufsize", "1000k",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("bitrateEncoderArgs = %#v, want %#v", got, want)
	}
}
