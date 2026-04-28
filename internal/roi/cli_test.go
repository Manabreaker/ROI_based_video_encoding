package roi

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseArgsLoadsYAMLConfigAndFlagsOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roi.yaml")
	content := []byte(`
input: from-yaml.mp4
out: out/from-yaml
mode: motion
target-bitrate: 700k
fit-roi: false
periphery-scale: 0.44
blur: 3
metrics: false
encoder: libx264
fit-iterations: 5
`)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := ParseArgs([]string{
		"--config", path,
		"--target-bitrate", "900k",
		"--metrics=true",
		"--blur", "5",
	})
	if err != nil {
		t.Fatalf("ParseArgs returned error: %v", err)
	}

	if cfg.Input != "from-yaml.mp4" {
		t.Fatalf("Input = %q, want from-yaml.mp4", cfg.Input)
	}
	if cfg.OutDir != "out/from-yaml" {
		t.Fatalf("OutDir = %q, want out/from-yaml", cfg.OutDir)
	}
	if cfg.Mode != "motion" {
		t.Fatalf("Mode = %q, want motion", cfg.Mode)
	}
	if cfg.TargetBitrate != "900k" {
		t.Fatalf("TargetBitrate = %q, want flag override 900k", cfg.TargetBitrate)
	}
	if cfg.FitROI {
		t.Fatal("FitROI = true, want false from YAML")
	}
	if cfg.ManualPeripheryScale != 0.44 {
		t.Fatalf("ManualPeripheryScale = %.2f, want 0.44", cfg.ManualPeripheryScale)
	}
	if cfg.ManualBlurRadius != 5 {
		t.Fatalf("ManualBlurRadius = %d, want flag override 5", cfg.ManualBlurRadius)
	}
	if !cfg.Metrics {
		t.Fatal("Metrics = false, want flag override true")
	}
	if cfg.VideoEncoder != "libx264" {
		t.Fatalf("VideoEncoder = %q, want libx264", cfg.VideoEncoder)
	}
	if cfg.FitIterations != 5 {
		t.Fatalf("FitIterations = %d, want 5", cfg.FitIterations)
	}
}

func TestParseArgsRejectsUnknownYAMLField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roi.yaml")
	content := []byte(`
input: video.mp4
target_bitrate: 700k
`)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := ParseArgs([]string{"--config", path}); err == nil {
		t.Fatal("ParseArgs returned nil error for unknown YAML field")
	}
}

func TestParseArgsSupportsConfigEqualsSyntax(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roi.yaml")
	content := []byte(`
input: equals.mp4
metrics: false
`)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := ParseArgs([]string{"--config=" + path})
	if err != nil {
		t.Fatalf("ParseArgs returned error: %v", err)
	}

	if cfg.Input != "equals.mp4" {
		t.Fatalf("Input = %q, want equals.mp4", cfg.Input)
	}
	if cfg.Metrics {
		t.Fatal("Metrics = true, want false from YAML")
	}
}

func TestParseArgsSupportsPositionalYAMLConfigWithFlagOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roi.yml")
	content := []byte(`
input: positional.mp4
target-bitrate: 500k
`)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := ParseArgs([]string{path, "--target-bitrate", "850k"})
	if err != nil {
		t.Fatalf("ParseArgs returned error: %v", err)
	}

	if cfg.Input != "positional.mp4" {
		t.Fatalf("Input = %q, want positional.mp4", cfg.Input)
	}
	if cfg.TargetBitrate != "850k" {
		t.Fatalf("TargetBitrate = %q, want flag override 850k", cfg.TargetBitrate)
	}
}

func TestParseArgsDoesNotTreatFlagValueYAMLPathAsConfig(t *testing.T) {
	cfg, err := ParseArgs([]string{"--input", "clip.yaml", "--target-bitrate", "450k"})
	if err != nil {
		t.Fatalf("ParseArgs returned error: %v", err)
	}

	if cfg.Input != "clip.yaml" {
		t.Fatalf("Input = %q, want clip.yaml", cfg.Input)
	}
	if cfg.TargetBitrate != "450k" {
		t.Fatalf("TargetBitrate = %q, want 450k", cfg.TargetBitrate)
	}
}
