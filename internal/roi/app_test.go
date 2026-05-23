package roi

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRunWithoutDebugWritesOnlyROIOutput(t *testing.T) {
	requireFFmpegForRunTest(t)

	dir := t.TempDir()
	input := filepath.Join(dir, "input.mp4")
	createTinyRunTestVideo(t, input)

	outDir := filepath.Join(dir, "out")
	cfg := minimalRunTestConfig(input, outDir)
	cfg.Debug = false
	cfg.Metrics = true
	cfg.OverlayBitrate = true

	if err := Run(cfg); err != nil {
		t.Fatal(err)
	}

	assertExists(t, filepath.Join(outDir, "roi_high_quality_region.mp4"))
	assertNotExists(t, filepath.Join(outDir, "comparison_baseline_vs_roi.mp4"))
	assertNotExists(t, filepath.Join(outDir, "roi_preview.png"))
	assertNotExists(t, filepath.Join(outDir, "bitrate_windows.json"))
	assertNotExists(t, filepath.Join(outDir, "quality_roi_psnr.json"))
	assertNotExists(t, filepath.Join(outDir, "report.json"))
}

func TestRunWithDebugWritesDebugArtifacts(t *testing.T) {
	requireFFmpegForRunTest(t)

	dir := t.TempDir()
	input := filepath.Join(dir, "input.mp4")
	createTinyRunTestVideo(t, input)

	outDir := filepath.Join(dir, "out")
	cfg := minimalRunTestConfig(input, outDir)
	cfg.Debug = true
	cfg.Metrics = false
	cfg.OverlayBitrate = false

	if err := Run(cfg); err != nil {
		t.Fatal(err)
	}

	assertExists(t, filepath.Join(outDir, "roi_high_quality_region.mp4"))
	assertExists(t, filepath.Join(outDir, "comparison_baseline_vs_roi.mp4"))
	assertExists(t, filepath.Join(outDir, "roi_preview.png"))
	assertExists(t, filepath.Join(outDir, "bitrate_windows.json"))
	assertExists(t, filepath.Join(outDir, "report.json"))
	assertNotExists(t, filepath.Join(outDir, "quality_roi_psnr.json"))
}

func minimalRunTestConfig(input string, outDir string) Config {
	cfg := validTestConfig()
	cfg.Input = input
	cfg.OutDir = outDir
	cfg.Mode = "static"
	cfg.ROIString = "0.25,0.25,0.50,0.50"
	cfg.ROIControl = "mask"
	cfg.FitROI = false
	cfg.VideoEncoder = "libx264"
	cfg.TargetBitrate = "200k"
	cfg.BitrateWindow = 0.25
	cfg.Serve = false
	cfg.KeepTemp = false
	return cfg
}

func requireFFmpegForRunTest(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not available")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe is not available")
	}
	if !ffmpegHasEncoder(encoderX264) {
		t.Skip("ffmpeg libx264 encoder is not available")
	}
}

func createTinyRunTestVideo(t *testing.T, path string) {
	t.Helper()
	cmd := exec.Command(
		"ffmpeg",
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-f", "lavfi",
		"-i", "testsrc2=s=96x96:r=15:d=0.5",
		"-c:v", "libx264",
		"-pix_fmt", "yuv420p",
		path,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("create tiny video: %v\n%s", err, string(out))
	}
}

func assertExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

func assertNotExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to not exist, stat err=%v", path, err)
	}
}
