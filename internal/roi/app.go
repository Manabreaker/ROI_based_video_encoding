package roi

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Run orchestrating probing, ROI rendering, bitrate measurement, comparison rendering, and reports.
func Run(cfg Config) error {
	if usesROIBlockMap(cfg) {
		cfg.Mode = "blocks"
		cfg.ROIString = ""
	}
	if err := validateConfig(cfg); err != nil {
		return err
	}

	targetKbps, _ := parseBitrateKbps(cfg.TargetBitrate)

	if err := ensureTool("ffmpeg"); err != nil {
		return err
	}
	if err := ensureTool("ffprobe"); err != nil {
		return err
	}
	resolvedEncoder, err := resolveVideoEncoder(cfg.VideoEncoder)
	if err != nil {
		return err
	}
	cfg.VideoEncoder = resolvedEncoder

	if err := os.MkdirAll(cfg.OutDir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	fmt.Println("[1/7] Probing input video...")

	info, err := probeVideo(cfg.Input)
	if err != nil {
		return err
	}
	if info.Width <= 0 || info.Height <= 0 {
		return fmt.Errorf("ffprobe returned invalid video size: %+v", info)
	}
	if info.FPS <= 0 {
		info.FPS = 30
	}

	fmt.Printf("      input: %dx%d, duration %.2fs, fps %.2f\n", info.Width, info.Height, info.Duration, info.FPS)
	fmt.Printf("      target actual bitrate: %.1f kbps\n", targetKbps)
	fmt.Printf("      encoder: %s\n", cfg.VideoEncoder)
	if isNVENC(cfg) && cfg.ROITwoPass {
		fmt.Println("      note: x264 two-pass fitting is disabled for h264_nvenc; using NVENC single-pass ABR")
	}

	tmpDir := filepath.Join(cfg.OutDir, "_tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return err
	}
	if !cfg.KeepTemp {
		defer func() { _ = os.RemoveAll(tmpDir) }()
	}

	fmt.Println("[2/7] Selecting ROI...")

	roi, err := selectROI(cfg, info, tmpDir)
	if err != nil {
		return err
	}

	fmt.Printf("      ROI %s: x=%d y=%d w=%d h=%d\n", roi.Source, roi.X, roi.Y, roi.W, roi.H)
	if usesROIBlockMap(cfg) {
		fmt.Printf("      QP block map: %d blocks, %d px grid\n", countROIBlockCells(cfg.ROIBlocks), normalizedROIBlockSize(cfg))
	}

	baseline := cfg.Input
	roiVideo := filepath.Join(cfg.OutDir, "roi_high_quality_region.mp4")
	comparison := filepath.Join(cfg.OutDir, "comparison_baseline_vs_roi.mp4")
	preview := filepath.Join(cfg.OutDir, "roi_preview.png")
	reportPath := filepath.Join(cfg.OutDir, "report.json")
	bitrateReportPath := filepath.Join(cfg.OutDir, "bitrate_windows.json")
	qualityReportPath := filepath.Join(cfg.OutDir, "quality_roi_psnr.json")

	fmt.Println("[3/7] Using input video as baseline reference...")

	baselineArtifact := artifactFor(baseline)

	fmt.Printf("      baseline: original input, not re-encoded")
	if baselineArtifact.SizeBytes > 0 {
		fmt.Printf(", %s", formatBytes(baselineArtifact.SizeBytes))
	}
	fmt.Println()

	if roiControl(cfg) == "qp-map" {
		fmt.Println("[4/7] Rendering ROI using encoder QP-map side data...")
	} else {
		fmt.Println("[4/7] Rendering ROI fitted by changing periphery quality...")
	}

	roiDecision, err := fitROIToTarget(cfg, info, roi, targetKbps, roiVideo, filepath.Join(tmpDir, "roi_fit"))
	if err != nil {
		return err
	}

	if roiDecision.ROIControl == "qp-map" {
		if roiDecision.ROIBlockCount > 0 {
			fmt.Printf("      ROI: target %.1f kbps, actual %.1f kbps, ROI CRF %d, %d QP blocks at %d px grid\n",
				roiDecision.TargetKbps,
				roiDecision.ActualKbps,
				roiDecision.CRF,
				roiDecision.ROIBlockCount,
				roiDecision.ROIBlockSize,
			)
		} else {
			fmt.Printf("      ROI: target %.1f kbps, actual %.1f kbps, ROI CRF %d, middle qoffset %.3f, ROI qoffset %.3f\n",
				roiDecision.TargetKbps,
				roiDecision.ActualKbps,
				roiDecision.CRF,
				roiDecision.MiddleQOffset,
				roiDecision.ROIQOffset,
			)
		}
	} else {
		fmt.Printf("      ROI: target %.1f kbps, actual %.1f kbps, ROI CRF %d, middle scale %.2f blur %d, low scale %.2f blur %d\n",
			roiDecision.TargetKbps,
			roiDecision.ActualKbps,
			roiDecision.CRF,
			roiDecision.MiddleScale,
			roiDecision.MiddleBlur,
			roiDecision.Scale,
			roiDecision.Blur,
		)
	}
	if roiDecision.Note != "" {
		fmt.Printf("      note: %s\n", roiDecision.Note)
	}

	roiArtifact := artifactFor(roiVideo)
	roiDecision.SizeBytes = roiArtifact.SizeBytes
	if roiArtifact.BitrateKbps > 0 {
		roiDecision.ActualKbps = roiArtifact.BitrateKbps
	}

	fmt.Println("[5/7] Calculating bitrate windows from encoded packets...")

	baselineInfo, err := probeVideo(baseline)
	if err != nil {
		return fmt.Errorf("probe input baseline: %w", err)
	}
	roiInfo, err := probeVideo(roiVideo)
	if err != nil {
		return fmt.Errorf("probe ROI output: %w", err)
	}

	baselineSamples, err := computeBitrateWindows(baseline, cfg.BitrateWindow, baselineInfo.Duration)
	if err != nil {
		return err
	}
	roiSamples, err := computeBitrateWindows(roiVideo, cfg.BitrateWindow, roiInfo.Duration)
	if err != nil {
		return err
	}

	bitrateReport := BitrateReport{
		WindowSeconds: cfg.BitrateWindow,
		Baseline:      baselineSamples,
		ROI:           roiSamples,
	}
	bitrateReport.Summary.Baseline = summarizeBitrate(baselineSamples)
	bitrateReport.Summary.ROI = summarizeBitrate(roiSamples)

	baselineDecision := inputBaselineDecision(bitrateReport.Summary.Baseline, baselineArtifact)

	bitrateJSON, _ := json.MarshalIndent(bitrateReport, "", "  ")
	if err := os.WriteFile(bitrateReportPath, bitrateJSON, 0o644); err != nil {
		return fmt.Errorf("write bitrate report: %w", err)
	}

	fmt.Printf("      baseline avg %.1f kbps, p95 %.1f kbps\n",
		bitrateReport.Summary.Baseline.AverageKbps,
		bitrateReport.Summary.Baseline.P95Kbps,
	)
	fmt.Printf("      ROI avg      %.1f kbps, p95 %.1f kbps\n",
		bitrateReport.Summary.ROI.AverageKbps,
		bitrateReport.Summary.ROI.P95Kbps,
	)

	fmt.Println("[6/7] Rendering side-by-side comparison...")

	if err := renderComparison(
		cfg,
		baseline,
		roiVideo,
		comparison,
		baselineSamples,
		roiSamples,
		info,
		roi,
		baselineDecision,
		roiDecision,
	); err != nil {
		return err
	}
	if err := renderPreview(cfg, info, roi, preview); err != nil {
		return err
	}

	fmt.Println("[7/7] Calculating ROI quality metrics and writing report...")

	qualityMetricsWritten := false
	if cfg.Metrics {
		if qualityReport, err := computeQualityReport(cfg, roi, baseline, roiVideo, tmpDir); err != nil {
			fmt.Printf("      warning: quality metrics failed: %v\n", err)
		} else {
			qualityJSON, _ := json.MarshalIndent(qualityReport, "", "  ")
			if err := os.WriteFile(qualityReportPath, qualityJSON, 0o644); err != nil {
				return fmt.Errorf("write quality report: %w", err)
			}
			qualityMetricsWritten = true

			for _, m := range qualityReport.PSNR {
				if m.AverageYText != "" {
					fmt.Printf("      %s %s ROI PSNR-Y: %s dB\n", m.Output, m.Scope, m.AverageYText)
				} else {
					fmt.Printf("      %s %s ROI PSNR-Y: %.2f dB\n", m.Output, m.Scope, m.AverageYDB)
				}
			}
		}
	}

	notes := []string{
		"Baseline is the original input video and is not re-encoded by the PoC.",
		"The intended comparison is subjective quality near the ROI against a lower measured bitrate and smaller generated ROI file.",
		"Zone boxes plus text overlays are drawn only on the final comparison video and do not affect measured input/ROI bitrates.",
	}
	if roiDecision.ROIControl == "qp-map" {
		if roiDecision.ROIBlockCount > 0 {
			notes = append(notes,
				"ROI output uses FFmpeg addroi side data to request per-block encoder-level QP offsets.",
				"Block QP-map mode derives the report ROI from the bounding box of configured block cells, while each block keeps its own qoffset.",
				"QP-map support is encoder-dependent; libx264 requires adaptive quantization and NVENC uses spatial AQ.",
			)
		} else {
			notes = append(notes,
				"ROI output uses FFmpeg addroi side data to request encoder-level QP offsets for the selected ROI and middle ring.",
				"QP-map support is encoder-dependent; libx264 requires adaptive quantization and NVENC uses spatial AQ.",
			)
		}
	} else {
		notes = append(notes,
			"ROI output keeps the selected ROI from the original frame, adds a medium-quality ring around it, and uses stronger degradation outside that ring.",
			"The ROI is visually preserved by preprocessing, not by encoder-level ROI QP maps, so it is not mathematically lossless after final encoding.",
		)
	}

	report := Report{
		CreatedAt:     time.Now().Format(time.RFC3339),
		Input:         cfg.Input,
		Mode:          cfg.Mode,
		TargetBitrate: cfg.TargetBitrate,
		TargetKbps:    targetKbps,
		Video:         info,
		ROI:           roi,
		Decisions:     []EncodeDecision{baselineDecision, roiDecision},
		Notes:         notes,
	}
	if usesROIBlockMap(cfg) {
		report.ROIBlockSize = normalizedROIBlockSize(cfg)
		report.ROIBlocks = cfg.ROIBlocks
	}

	artifacts := []string{baseline, roiVideo, comparison, preview, bitrateReportPath}
	if qualityMetricsWritten {
		artifacts = append(artifacts, qualityReportPath)
	}
	for _, p := range artifacts {
		report.Artifacts = append(report.Artifacts, artifactFor(p))
	}

	reportJSON, _ := json.MarshalIndent(report, "", "  ")
	if err := os.WriteFile(reportPath, reportJSON, 0o644); err != nil {
		return fmt.Errorf("write report: %w", err)
	}

	fmt.Println("\nDone. Artifacts:")
	fmt.Printf("  - baseline input: %s\n", baseline)
	fmt.Printf("  - %s\n", roiVideo)
	fmt.Printf("  - %s\n", comparison)
	fmt.Printf("  - %s\n", preview)
	fmt.Printf("  - %s\n", bitrateReportPath)
	if qualityMetricsWritten {
		fmt.Printf("  - %s\n", qualityReportPath)
	}
	fmt.Printf("  - %s\n", reportPath)

	if cfg.Serve {
		fmt.Printf("\nServing %s at http://localhost%s/\n", cfg.OutDir, cfg.HTTPAddr)
		fmt.Printf("Open comparison: http://localhost%s/comparison_baseline_vs_roi.mp4\n", cfg.HTTPAddr)
		return serve(cfg.OutDir, cfg.HTTPAddr)
	}

	return nil
}
