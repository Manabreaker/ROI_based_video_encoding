package roi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildNVENCSDKEncodePlanUsesRawPipeAndHelper(t *testing.T) {
	t.Setenv("ROI_NVENC_BIN", "/opt/roi-nvenc")

	cfg := validTestConfig()
	cfg.Input = "input.mp4"
	cfg.Mode = "blocks"
	cfg.VideoEncoder = "h264_nvenc_sdk"
	cfg.ROIControl = "qp-map"
	cfg.FitROI = false
	cfg.ROIBlockSize = 64
	cfg.ROIBlocks = []QPMapBlock{
		{Col: 1, Row: 2, W: 3, H: 4, QOffset: -0.35},
		{Col: 5, Row: 6, QOffset: -0.10},
	}

	plan, err := buildNVENCSDKEncodePlan(
		cfg,
		VideoInfo{Width: 1920, Height: 1080, FPS: 29.97},
		5000,
		"/tmp/out.mp4",
		"/tmp/work",
	)
	if err != nil {
		t.Fatal(err)
	}

	if plan.decode.Name != "ffmpeg" {
		t.Fatalf("decode command = %q, want ffmpeg", plan.decode.Name)
	}
	decodeArgs := strings.Join(plan.decode.Args, " ")
	for _, part := range []string{"-i input.mp4", "-an", "-pix_fmt nv12", "-f rawvideo", "pipe:1"} {
		if !strings.Contains(decodeArgs, part) {
			t.Fatalf("decode args missing %q: %#v", part, plan.decode.Args)
		}
	}

	if plan.helper.Name != "/opt/roi-nvenc" {
		t.Fatalf("helper command = %q, want env override", plan.helper.Name)
	}
	helperArgs := strings.Join(plan.helper.Args, " ")
	for _, part := range []string{
		"--width 1920",
		"--height 1080",
		"--fps 29.970",
		"--bitrate-kbps 5000",
		"--block-size 64",
		"--roi-blocks 1,2,3,4,-0.3500;5,6,1,1,-0.1000",
		"--input-format nv12",
		"--codec h264",
		"--output /tmp/work/nvenc_sdk/roi.h264",
	} {
		if !strings.Contains(helperArgs, part) {
			t.Fatalf("helper args missing %q: %#v", part, plan.helper.Args)
		}
	}

	muxArgs := strings.Join(plan.mux.Args, " ")
	for _, part := range []string{"-fflags +genpts", "-r 29.970", "-i /tmp/work/nvenc_sdk/roi.h264", "-c:v copy", "-movflags +faststart", "/tmp/out.mp4"} {
		if !strings.Contains(muxArgs, part) {
			t.Fatalf("mux args missing %q: %#v", part, plan.mux.Args)
		}
	}

	allArgs := strings.Join(append(append([]string{}, plan.decode.Args...), append(plan.helper.Args, plan.mux.Args...)...), " ")
	for _, forbidden := range []string{"addroi", "blur", "scale", "candidate"} {
		if strings.Contains(allArgs, forbidden) {
			t.Fatalf("NVENC SDK plan should not contain %q: %s", forbidden, allArgs)
		}
	}
}

func TestBuildNVENCSDKEncodePlanRejectsMissingBlocks(t *testing.T) {
	cfg := validTestConfig()
	cfg.VideoEncoder = "h264_nvenc_sdk"
	cfg.ROIControl = "qp-map"
	cfg.FitROI = false
	cfg.Mode = "blocks"
	cfg.ROIBlocks = nil

	if _, err := buildNVENCSDKEncodePlan(cfg, VideoInfo{Width: 640, Height: 360, FPS: 30}, 500, "out.mp4", "work"); err == nil {
		t.Fatal("expected missing ROI blocks error")
	}
}

func TestRunNVENCSDKEncodePlanPipesDecodeIntoHelperAndMuxes(t *testing.T) {
	tmp := t.TempDir()
	ffmpeg := filepath.Join(tmp, "ffmpeg")
	helper := filepath.Join(tmp, "roi-nvenc")
	writeExecutable(t, ffmpeg, `#!/bin/sh
set -eu
case " $* " in
  *" -f rawvideo pipe:1"*)
    printf 'raw-nv12-frame'
    ;;
  *)
    in=''
    out=''
    prev=''
    for arg in "$@"; do
      if [ "$prev" = "-i" ]; then
        in="$arg"
      fi
      out="$arg"
      prev="$arg"
    done
    cp "$in" "$out"
    ;;
esac
`)
	writeExecutable(t, helper, `#!/bin/sh
set -eu
out=''
prev=''
for arg in "$@"; do
  if [ "$prev" = "--output" ]; then
    out="$arg"
  fi
  prev="$arg"
done
mkdir -p "$(dirname "$out")"
payload=$(cat)
printf 'h264:%s' "$payload" > "$out"
`)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ROI_NVENC_BIN", helper)

	cfg := validTestConfig()
	cfg.Input = "input.mp4"
	cfg.Mode = "blocks"
	cfg.VideoEncoder = "h264_nvenc_sdk"
	cfg.ROIControl = "qp-map"
	cfg.FitROI = false
	cfg.ROIBlocks = []QPMapBlock{{Col: 0, Row: 0, QOffset: -0.35}}

	output := filepath.Join(tmp, "out.mp4")
	plan, err := buildNVENCSDKEncodePlan(cfg, VideoInfo{Width: 64, Height: 64, FPS: 30}, 500, output, tmp)
	if err != nil {
		t.Fatal(err)
	}
	if err := runNVENCSDKEncodePlan(plan); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "h264:raw-nv12-frame" {
		t.Fatalf("output = %q, want piped helper payload", string(got))
	}
}

func TestNVENCSDKEncodeDecisionDescribesSDKEmphasisMap(t *testing.T) {
	cfg := validTestConfig()
	cfg.Mode = "blocks"
	cfg.VideoEncoder = "h264_nvenc_sdk"
	cfg.ROIControl = "qp-map"
	cfg.ROIBlockSize = 64
	cfg.ROIBlocks = []QPMapBlock{
		{Col: 0, Row: 0, QOffset: -0.35},
		{Col: 1, Row: 0, W: 2, H: 1, QOffset: -0.20},
	}

	decision := nvencSDKEncodeDecision(cfg, 5000, 4900, "abr")
	if decision.Encoder != "h264_nvenc_sdk" || decision.ROIControl != "qp-map" || decision.RateControl != "abr" {
		t.Fatalf("unexpected decision identity: %+v", decision)
	}
	if decision.ROIBlockSize != 64 || decision.ROIBlockCount != 3 {
		t.Fatalf("unexpected block summary: %+v", decision)
	}
	if !decision.WithinTolerance {
		t.Fatalf("expected bitrate within tolerance: %+v", decision)
	}
	for _, part := range []string{"NVIDIA Video Codec SDK", "Emphasis MAP", "static block map"} {
		if !strings.Contains(decision.Note, part) {
			t.Fatalf("decision note missing %q: %s", part, decision.Note)
		}
	}
	for _, forbidden := range []string{"FFmpeg addroi", "blur", "mask", "candidate"} {
		if strings.Contains(decision.Note, forbidden) {
			t.Fatalf("decision note should not contain %q: %s", forbidden, decision.Note)
		}
	}
}

func writeExecutable(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}
