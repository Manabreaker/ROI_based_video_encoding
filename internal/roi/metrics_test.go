package roi

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParsePSNRAverageYFromFFmpegOutput(t *testing.T) {
	got, text := parsePSNRAverageY("n:10 mse_avg:1.0 PSNR y:42.50 average:41.20", "")
	if text != "" {
		t.Fatalf("text = %q, want empty", text)
	}
	if got != 42.50 {
		t.Fatalf("AverageY = %.2f, want 42.50", got)
	}
}

func TestParsePSNRAverageYFromStatsFile(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "psnr.log")
	if err := os.WriteFile(logPath, []byte("n:1 psnr_y:40.0\nn:2 psnr_y:44.0\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	got, text := parsePSNRAverageY("no summary", logPath)
	if text != "" {
		t.Fatalf("text = %q, want empty", text)
	}
	if got != 42.0 {
		t.Fatalf("AverageY = %.2f, want 42.00", got)
	}
}

func TestParsePSNRAverageYInfinity(t *testing.T) {
	got, text := parsePSNRAverageY("PSNR y:inf average:inf", "")
	if got != 0 || text != "+Inf" {
		t.Fatalf("got %.2f/%q, want 0/+Inf", got, text)
	}
}
