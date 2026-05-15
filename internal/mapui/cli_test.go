package mapui

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestParseArgsRequiresInput(t *testing.T) {
	if _, err := ParseArgs([]string{"--no-open"}, &bytes.Buffer{}); err == nil {
		t.Fatal("ParseArgs returned nil error without --input")
	}
}

func TestParseArgsDefaultsAndNoOpen(t *testing.T) {
	video := filepath.Join(t.TempDir(), "video.mp4")
	writeTestFile(t, video)

	opts, err := ParseArgs([]string{"--input", video, "--no-open"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}

	if opts.ConfigOut != "config/roi_blocks_generated.yaml" {
		t.Fatalf("ConfigOut = %q", opts.ConfigOut)
	}
	if opts.OpenBrowser {
		t.Fatal("OpenBrowser = true, want false")
	}
	if opts.Addr != "127.0.0.1:8090" {
		t.Fatalf("Addr = %q", opts.Addr)
	}
	if opts.ROIBlockSize != defaultROIBlockSize {
		t.Fatalf("ROIBlockSize = %d", opts.ROIBlockSize)
	}
}

func TestValidateOptionsRejectsNonLocalAddress(t *testing.T) {
	video := filepath.Join(t.TempDir(), "video.mp4")
	writeTestFile(t, video)

	opts := DefaultOptions()
	opts.Input = video
	opts.Addr = "0.0.0.0:8090"

	if err := ValidateOptions(opts); err == nil {
		t.Fatal("ValidateOptions returned nil error for non-local addr")
	}
}
