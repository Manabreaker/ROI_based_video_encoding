package roi

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNVENCSDKIntegrationEncode(t *testing.T) {
	if os.Getenv("ROI_NVENC_INTEGRATION") != "1" {
		t.Skip("set ROI_NVENC_INTEGRATION=1 to run NVENC SDK integration test")
	}

	root := testRepoRoot(t)
	runIntegrationCommand(t, "make", "-C", root, "roi-nvenc")
	t.Setenv("ROI_NVENC_BIN", filepath.Join(root, "native", "roi-nvenc", "roi-nvenc"))

	dir := t.TempDir()
	input := filepath.Join(dir, "input.mp4")
	output := filepath.Join(dir, "out.mp4")
	runIntegrationCommand(t,
		"ffmpeg",
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi",
		"-i", "testsrc2=s=256x256:r=30:d=1",
		"-c:v", "libx264",
		"-pix_fmt", "yuv420p",
		input,
	)

	cfg := validTestConfig()
	cfg.Input = input
	cfg.Mode = "blocks"
	cfg.VideoEncoder = encoderNVENCSDK
	cfg.ROIControl = "qp-map"
	cfg.FitROI = false
	cfg.ROIBlockSize = 64
	cfg.ROIBlocks = []QPMapBlock{{Col: 1, Row: 1, W: 2, H: 2, QOffset: -0.35}}

	plan, err := buildNVENCSDKEncodePlan(cfg, VideoInfo{Width: 256, Height: 256, FPS: 30}, 800, output, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := runNVENCSDKEncodePlan(plan); err != nil {
		t.Fatal(err)
	}

	out := runIntegrationCommand(t,
		"ffprobe",
		"-hide_banner", "-loglevel", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=codec_name,width,height,nb_frames",
		"-of", "default=nw=1",
		output,
	)
	for _, part := range []string{"codec_name=h264", "width=256", "height=256", "nb_frames=30"} {
		if !strings.Contains(out, part) {
			t.Fatalf("ffprobe output missing %q:\n%s", part, out)
		}
	}
}

func runIntegrationCommand(t *testing.T, name string, args ...string) string {
	t.Helper()

	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(args, " "), err, string(out))
	}
	return string(out)
}

func testRepoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root containing go.mod")
		}
		dir = parent
	}
}
