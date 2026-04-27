package roi

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// computeQualityReport compares ROI crops from the original input and generated ROI output.
func computeQualityReport(cfg Config, roi ROI, baseline string, roiVideo string, tmpDir string) (QualityReport, error) {
	report := QualityReport{ROI: roi}

	baselineLog := filepath.Join(tmpDir, "psnr_baseline_roi.log")
	roiLog := filepath.Join(tmpDir, "psnr_roi_output_roi.log")

	m1, err := computeROIPSNR(cfg.Input, baseline, roi, baselineLog, "input-baseline", "roi-crop")
	if err != nil {
		return report, err
	}

	m2, err := computeROIPSNR(cfg.Input, roiVideo, roi, roiLog, "roi-output", "roi-crop")
	if err != nil {
		return report, err
	}

	report.PSNR = append(report.PSNR, m1, m2)

	return report, nil
}

// computeROIPSNR crops the ROI from two videos and runs FFmpeg's PSNR filter.
func computeROIPSNR(reference string, distorted string, roi ROI, logPath string, name string, scope string) (QualityMetric, error) {
	filter := fmt.Sprintf(
		"[0:v]crop=%d:%d:%d:%d,format=yuv420p[ref];"+
			"[1:v]crop=%d:%d:%d:%d,format=yuv420p[dist];"+
			"[ref][dist]psnr=stats_file=%s",
		roi.W,
		roi.H,
		roi.X,
		roi.Y,
		roi.W,
		roi.H,
		roi.X,
		roi.Y,
		escapeFilterPath(logPath),
	)

	args := []string{
		"-hide_banner",
		"-v", "info",
		"-i", reference,
		"-i", distorted,
		"-filter_complex", filter,
		"-f", "null",
		"-",
	}

	out, err := commandCombinedOutput("ffmpeg", args...)

	metric := QualityMetric{
		Name:   "psnr",
		Scope:  scope,
		Output: name,
		RawLog: strings.TrimSpace(string(out)),
	}
	metric.AverageYDB, metric.AverageYText = parsePSNRAverageY(string(out), logPath)

	if err != nil {
		return metric, fmt.Errorf("ffmpeg psnr failed for %s: %w\n%s", name, err, strings.TrimSpace(string(out)))
	}

	return metric, nil
}

// escapeFilterPath escapes paths used inside FFmpeg filter expressions.
func escapeFilterPath(path string) string {
	return strings.NewReplacer("\\", "\\\\", ":", "\\:", "'", "\\'").Replace(path)
}

// parsePSNRAverageY extracts average luma PSNR from FFmpeg output or stats logs.
func parsePSNRAverageY(ffmpegOutput string, logPath string) (float64, string) {
	marker := "PSNR y:"
	idx := strings.LastIndex(ffmpegOutput, marker)

	if idx >= 0 {
		rest := ffmpegOutput[idx+len(marker):]
		fields := strings.Fields(rest)

		if len(fields) > 0 {
			v, err := strconv.ParseFloat(strings.TrimSpace(fields[0]), 64)
			if err == nil {
				if math.IsInf(v, 0) {
					return 0, "+Inf"
				}
				if math.IsNaN(v) {
					return 0, "NaN"
				}
				return v, ""
			}
		}
	}

	b, err := os.ReadFile(logPath)
	if err != nil {
		return 0, ""
	}

	var sum float64
	var count int

	for _, line := range strings.Split(string(b), "\n") {
		for _, field := range strings.Fields(line) {
			if strings.HasPrefix(field, "psnr_y:") {
				v, err := strconv.ParseFloat(strings.TrimPrefix(field, "psnr_y:"), 64)
				if err == nil && !math.IsInf(v, 0) && !math.IsNaN(v) {
					sum += v
					count++
				}
			}
		}
	}

	if count == 0 {
		return 0, ""
	}

	return sum / float64(count), ""
}
