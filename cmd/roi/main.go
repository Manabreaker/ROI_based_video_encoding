package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Config holds CLI options for input/output, ROI selection, encoding, metrics, and serving.
type Config struct {
	// Input/output.
	Input  string
	OutDir string

	// ROI selection.
	Mode      string
	ROIString string

	// Bitrate target for the generated ROI output.
	TargetBitrate string
	Tolerance     float64

	// ROI quality policy.
	FitROI              bool
	ROIHighQualityCRF   int
	ROIMinCRF           int
	ROIMaxCRFIfNeeded   int
	AllowROIQualityLoss bool

	// Periphery degradation settings.
	ManualPeripheryScale float64
	ManualBlurRadius     int
	ROIMinScale          float64
	ROIMaxBlur           int

	// ROI encoder rate control and candidate scoring.
	ROIRateControl       string
	ROITwoPass           bool
	ROIFitMetric         bool
	ROIPSNRTieDB         float64
	ROIMaxrateMultiplier float64
	ROIBufsizeSeconds    float64

	// General x264 tuning.
	Preset        string
	FitIterations int

	// Motion-based ROI detection.
	MotionWindow float64
	MotionThresh int
	ROIMargin    float64

	// Dynamic bitrate overlay.
	OverlayBitrate     bool
	BitrateWindow      float64
	MaxBitrateOverlays int

	// Reports and local preview server.
	Metrics bool

	Serve    bool
	HTTPAddr string
	KeepTemp bool
}

// VideoInfo describes the probed primary video stream.
type VideoInfo struct {
	Width    int     `json:"width"`
	Height   int     `json:"height"`
	Duration float64 `json:"duration_seconds"`
	FPS      float64 `json:"fps"`
}

// ROI defines the rectangular region that should remain visually important.
type ROI struct {
	X      int    `json:"x"`
	Y      int    `json:"y"`
	W      int    `json:"w"`
	H      int    `json:"h"`
	Source string `json:"source"`
}

// Artifact describes a generated or referenced output file in the report.
type Artifact struct {
	Path        string  `json:"path"`
	SizeBytes   int64   `json:"size_bytes"`
	BitrateKbps float64 `json:"bitrate_kbps,omitempty"`
}

// Candidate records one tried ROI encoding variant before the final choice.
type Candidate struct {
	Kind        string  `json:"kind"`
	CRF         int     `json:"crf"`
	RateControl string  `json:"rate_control,omitempty"`
	Scale       float64 `json:"periphery_scale,omitempty"`
	Blur        int     `json:"periphery_blur,omitempty"`
	Kbps        float64 `json:"bitrate_kbps"`
	ROIYPSNR    float64 `json:"roi_psnr_y_db,omitempty"`
	Note        string  `json:"note,omitempty"`
	Path        string  `json:"-"`
}

// CandidateSummary is the report-safe view of a Candidate.
type CandidateSummary struct {
	Kind        string  `json:"kind"`
	CRF         int     `json:"crf"`
	RateControl string  `json:"rate_control,omitempty"`
	Scale       float64 `json:"periphery_scale,omitempty"`
	Blur        int     `json:"periphery_blur,omitempty"`
	Kbps        float64 `json:"bitrate_kbps"`
	ROIYPSNR    float64 `json:"roi_psnr_y_db,omitempty"`
	Note        string  `json:"note,omitempty"`
}

// EncodeDecision explains the chosen settings and measured result for one side of the comparison.
type EncodeDecision struct {
	Name            string             `json:"name"`
	TargetKbps      float64            `json:"target_kbps,omitempty"`
	ActualKbps      float64            `json:"actual_kbps"`
	WithinTolerance bool               `json:"within_tolerance"`
	CRF             int                `json:"crf,omitempty"`
	RateControl     string             `json:"rate_control,omitempty"`
	Scale           float64            `json:"periphery_scale,omitempty"`
	Blur            int                `json:"periphery_blur,omitempty"`
	ROIYPSNR        float64            `json:"roi_psnr_y_db,omitempty"`
	SizeBytes       int64              `json:"size_bytes,omitempty"`
	Note            string             `json:"note,omitempty"`
	Candidates      []CandidateSummary `json:"candidates,omitempty"`
}

// BitrateSample stores measured video-packet bitrate for one time window.
type BitrateSample struct {
	Start float64 `json:"start_seconds"`
	End   float64 `json:"end_seconds"`
	Kbps  float64 `json:"kbps"`
}

// BitrateSummary aggregates bitrate windows for reporting and overlays.
type BitrateSummary struct {
	AverageKbps float64 `json:"average_kbps"`
	P50Kbps     float64 `json:"p50_kbps"`
	P95Kbps     float64 `json:"p95_kbps"`
	MinKbps     float64 `json:"min_kbps"`
	MaxKbps     float64 `json:"max_kbps"`
}

// BitrateReport stores the dynamic bitrate measurements for baseline and ROI output.
type BitrateReport struct {
	WindowSeconds float64         `json:"window_seconds"`
	Baseline      []BitrateSample `json:"baseline"`
	ROI           []BitrateSample `json:"roi"`
	Summary       struct {
		Baseline BitrateSummary `json:"baseline"`
		ROI      BitrateSummary `json:"roi"`
	} `json:"summary"`
}

// QualityMetric stores one ROI-crop PSNR measurement.
type QualityMetric struct {
	Name         string  `json:"name"`
	Scope        string  `json:"scope"`
	Output       string  `json:"output"`
	AverageYDB   float64 `json:"average_y_db,omitempty"`
	AverageYText string  `json:"average_y_text,omitempty"`
	RawLog       string  `json:"raw_log,omitempty"`
}

// QualityReport groups objective ROI quality metrics.
type QualityReport struct {
	ROI  ROI             `json:"roi"`
	PSNR []QualityMetric `json:"psnr"`
}

// Report is the top-level JSON summary written after processing.
type Report struct {
	CreatedAt     string           `json:"created_at"`
	Input         string           `json:"input"`
	Mode          string           `json:"mode"`
	TargetBitrate string           `json:"target_bitrate"`
	TargetKbps    float64          `json:"target_kbps"`
	Video         VideoInfo        `json:"video"`
	ROI           ROI              `json:"roi"`
	Decisions     []EncodeDecision `json:"decisions"`
	Artifacts     []Artifact       `json:"artifacts"`
	Notes         []string         `json:"notes"`
}

// ffprobeVideoJSON mirrors the subset of ffprobe stream metadata this tool reads.
type ffprobeVideoJSON struct {
	Streams []struct {
		Width        int    `json:"width"`
		Height       int    `json:"height"`
		RFrameRate   string `json:"r_frame_rate"`
		AvgFrameRate string `json:"avg_frame_rate"`
		Duration     string `json:"duration"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

// ffprobePacketsJSON mirrors the packet data needed for bitrate windows.
type ffprobePacketsJSON struct {
	Packets []struct {
		PTS  string `json:"pts_time"`
		DTS  string `json:"dts_time"`
		Size string `json:"size"`
	} `json:"packets"`
}

// peripherySetting describes one candidate degradation level outside the ROI.
type peripherySetting struct {
	Scale float64
	Blur  int
}

// main parses flags and exits non-zero if the pipeline fails.
func main() {
	cfg := parseFlags()
	if err := run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "\n[ERROR] %v\n", err)
		os.Exit(1)
	}
}

// parseFlags builds Config from CLI flags.
func parseFlags() Config {
	var cfg Config

	flag.StringVar(&cfg.Input, "input", "", "input video file, camera URL, RTSP URL, or any FFmpeg-readable source")
	flag.StringVar(&cfg.OutDir, "out", "out", "output directory")
	flag.StringVar(&cfg.Mode, "mode", "static", "ROI mode: static or motion")
	flag.StringVar(&cfg.ROIString, "roi", "", "static ROI as x,y,w,h; pixels or fractions 0..1; if empty, center ROI is used")

	flag.StringVar(&cfg.TargetBitrate, "target-bitrate", "1000k", "target actual bitrate, e.g. 300k, 1000k, 1.5M")
	flag.Float64Var(&cfg.Tolerance, "tolerance", 0.07, "acceptable relative bitrate error, e.g. 0.07 means +-7%")

	flag.BoolVar(&cfg.FitROI, "fit-roi", true, "fit ROI output by changing periphery degradation")
	flag.IntVar(&cfg.ROIHighQualityCRF, "roi-crf", 16, "CRF used for final ROI output; lower means closer to original ROI")
	flag.IntVar(&cfg.ROIMinCRF, "roi-min-crf", 10, "minimum CRF used when the video is too simple and target bitrate is higher than full-detail output")
	flag.IntVar(&cfg.ROIMaxCRFIfNeeded, "roi-max-crf-if-needed", 36, "maximum CRF only when --allow-roi-quality-loss=true and target cannot be reached otherwise")
	flag.BoolVar(&cfg.AllowROIQualityLoss, "allow-roi-quality-loss", false, "if true, may increase ROI CRF when target is impossible while preserving high-quality ROI")

	flag.Float64Var(&cfg.ManualPeripheryScale, "periphery-scale", 0.35, "manual periphery scale when --fit-roi=false")
	flag.IntVar(&cfg.ManualBlurRadius, "blur", 2, "manual periphery blur when --fit-roi=false")
	flag.Float64Var(&cfg.ROIMinScale, "roi-min-scale", 0.12, "minimum periphery scale candidate for ROI fitting")
	flag.IntVar(&cfg.ROIMaxBlur, "roi-max-blur", 10, "maximum periphery blur candidate for ROI fitting")

	flag.StringVar(&cfg.ROIRateControl, "roi-rate-control", "abr", "ROI encoder rate control: abr keeps ROI output near --target-bitrate; crf preserves old fixed-CRF behavior")
	flag.BoolVar(&cfg.ROITwoPass, "roi-two-pass", true, "use x264 two-pass ABR for ROI output when --roi-rate-control=abr")
	flag.BoolVar(&cfg.ROIFitMetric, "roi-fit-metric", true, "during ROI fitting, measure ROI-crop PSNR for each candidate and pick the least degraded periphery near the best ROI score")
	flag.Float64Var(&cfg.ROIPSNRTieDB, "roi-psnr-tie-db", 0.25, "when fitting ROI by metric, prefer milder periphery if ROI PSNR is within this many dB of the best candidate")
	flag.Float64Var(&cfg.ROIMaxrateMultiplier, "roi-maxrate-multiplier", 1.15, "ABR maxrate as a multiplier of --target-bitrate for ROI output")
	flag.Float64Var(&cfg.ROIBufsizeSeconds, "roi-bufsize-seconds", 2.0, "ABR VBV buffer size in target-bitrate seconds for ROI output")

	flag.StringVar(&cfg.Preset, "preset", "veryfast", "x264 preset")
	flag.IntVar(&cfg.FitIterations, "fit-iterations", 9, "maximum CRF search iterations for emergency ROI fitting")

	flag.Float64Var(&cfg.MotionWindow, "motion-window", 0.6, "time gap in seconds between frames used for simple motion ROI detection")
	flag.IntVar(&cfg.MotionThresh, "motion-threshold", 34, "grayscale difference threshold for motion ROI detection")
	flag.Float64Var(&cfg.ROIMargin, "roi-margin", 0.18, "ROI expansion margin as fraction of detected bbox size")

	flag.BoolVar(&cfg.OverlayBitrate, "overlay-bitrate", true, "draw dynamic bitrate overlay on comparison video")
	flag.Float64Var(&cfg.BitrateWindow, "bitrate-window", 1.0, "window size in seconds for dynamic bitrate calculation")
	flag.IntVar(&cfg.MaxBitrateOverlays, "max-bitrate-overlays", 300, "safety cap for drawtext overlays; increase --bitrate-window for long videos")

	flag.BoolVar(&cfg.Metrics, "metrics", true, "calculate ROI PSNR against original for input baseline and ROI output")

	flag.BoolVar(&cfg.Serve, "serve", false, "start local HTTP server after processing")
	flag.StringVar(&cfg.HTTPAddr, "http", ":8080", "HTTP address for --serve")
	flag.BoolVar(&cfg.KeepTemp, "keep-temp", false, "keep temporary candidate files")

	flag.Parse()

	return cfg
}

// run orchestrates probing, ROI rendering, bitrate measurement, comparison rendering, and reports.
func run(cfg Config) error {
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

	tmpDir := filepath.Join(cfg.OutDir, "_tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return err
	}
	if !cfg.KeepTemp {
		defer os.RemoveAll(tmpDir)
	}

	fmt.Println("[2/7] Selecting ROI...")

	roi, err := selectROI(cfg, info, tmpDir)
	if err != nil {
		return err
	}

	fmt.Printf("      ROI %s: x=%d y=%d w=%d h=%d\n", roi.Source, roi.X, roi.Y, roi.W, roi.H)

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

	fmt.Println("[4/7] Rendering ROI fitted by changing periphery quality...")

	roiDecision, err := fitROIToTarget(cfg, info, roi, targetKbps, roiVideo, filepath.Join(tmpDir, "roi_fit"))
	if err != nil {
		return err
	}

	fmt.Printf("      ROI: target %.1f kbps, actual %.1f kbps, ROI CRF %d, periphery scale %.2f, blur %d\n",
		roiDecision.TargetKbps,
		roiDecision.ActualKbps,
		roiDecision.CRF,
		roiDecision.Scale,
		roiDecision.Blur,
	)
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

	report := Report{
		CreatedAt:     time.Now().Format(time.RFC3339),
		Input:         cfg.Input,
		Mode:          cfg.Mode,
		TargetBitrate: cfg.TargetBitrate,
		TargetKbps:    targetKbps,
		Video:         info,
		ROI:           roi,
		Decisions:     []EncodeDecision{baselineDecision, roiDecision},
		Notes: []string{
			"Baseline is the original input video and is not re-encoded by the PoC.",
			"ROI output keeps the selected ROI from the original frame before encoding and deliberately simplifies the periphery.",
			"The intended comparison is subjective quality near the ROI against a lower measured bitrate and smaller generated ROI file.",
			"The ROI is visually preserved by preprocessing, not by encoder-level ROI QP maps, so it is not mathematically lossless after final encoding.",
			"Colored ROI boxes and text overlays are drawn only on the final comparison video and do not affect measured input/ROI bitrates.",
		},
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

// validateConfig rejects flag combinations that would make the FFmpeg pipeline ambiguous or unsafe.
func validateConfig(cfg Config) error {
	if strings.TrimSpace(cfg.Input) == "" {
		return errors.New("missing --input")
	}
	if _, ok := parseBitrateKbps(cfg.TargetBitrate); !ok {
		return fmt.Errorf("cannot parse --target-bitrate %q; examples: 300k, 1000k, 1.5M", cfg.TargetBitrate)
	}
	if cfg.Tolerance <= 0 || cfg.Tolerance > 0.50 {
		return errors.New("--tolerance must be in range (0, 0.5]")
	}
	if cfg.ROIHighQualityCRF < 0 || cfg.ROIHighQualityCRF > 51 {
		return errors.New("--roi-crf must be in range 0..51")
	}
	if cfg.ROIMinCRF < 0 || cfg.ROIMinCRF > cfg.ROIHighQualityCRF {
		return errors.New("--roi-min-crf must be in range 0..roi-crf")
	}
	if cfg.ROIMaxCRFIfNeeded < cfg.ROIHighQualityCRF || cfg.ROIMaxCRFIfNeeded > 51 {
		return errors.New("--roi-max-crf-if-needed must be in range roi-crf..51")
	}
	if cfg.ManualPeripheryScale <= 0 || cfg.ManualPeripheryScale > 1 {
		return errors.New("--periphery-scale must be in range (0,1]")
	}
	if cfg.ManualBlurRadius < 0 || cfg.ManualBlurRadius > 40 {
		return errors.New("--blur must be in range 0..40")
	}
	if cfg.ROIMinScale <= 0 || cfg.ROIMinScale > 1 {
		return errors.New("--roi-min-scale must be in range (0,1]")
	}
	if cfg.ROIMaxBlur < 0 || cfg.ROIMaxBlur > 40 {
		return errors.New("--roi-max-blur must be in range 0..40")
	}
	switch roiRateControl(cfg) {
	case "abr", "crf":
	default:
		return errors.New("--roi-rate-control must be either abr or crf")
	}
	if cfg.ROIPSNRTieDB < 0 || cfg.ROIPSNRTieDB > 5 {
		return errors.New("--roi-psnr-tie-db must be in range 0..5")
	}
	if cfg.ROIMaxrateMultiplier < 1.0 || cfg.ROIMaxrateMultiplier > 5.0 {
		return errors.New("--roi-maxrate-multiplier must be in range 1..5")
	}
	if cfg.ROIBufsizeSeconds <= 0 || cfg.ROIBufsizeSeconds > 30 {
		return errors.New("--roi-bufsize-seconds must be in range (0,30]")
	}
	if cfg.FitIterations < 1 || cfg.FitIterations > 30 {
		return errors.New("--fit-iterations must be in range 1..30")
	}
	if cfg.BitrateWindow <= 0 {
		return errors.New("--bitrate-window must be greater than zero")
	}
	return nil
}

// ensureTool checks that an external binary is available in PATH.
func ensureTool(name string) error {
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("required tool %q not found in PATH", name)
	}
	return nil
}

// probeVideo reads stream size, duration, and frame rate through ffprobe.
func probeVideo(input string) (VideoInfo, error) {
	args := []string{
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height,r_frame_rate,avg_frame_rate,duration:format=duration",
		"-of", "json",
		input,
	}

	out, err := commandOutput("ffprobe", args...)
	if err != nil {
		return VideoInfo{}, err
	}

	var pj ffprobeVideoJSON
	if err := json.Unmarshal(out, &pj); err != nil {
		return VideoInfo{}, fmt.Errorf("parse ffprobe JSON: %w", err)
	}

	if len(pj.Streams) == 0 {
		return VideoInfo{}, errors.New("input has no video stream")
	}

	st := pj.Streams[0]

	duration := parseFloatOrZero(st.Duration)
	if duration <= 0 {
		duration = parseFloatOrZero(pj.Format.Duration)
	}

	fps := parseRate(st.AvgFrameRate)
	if fps <= 0 {
		fps = parseRate(st.RFrameRate)
	}

	return VideoInfo{
		Width:    st.Width,
		Height:   st.Height,
		Duration: duration,
		FPS:      fps,
	}, nil
}

// parseFloatOrZero converts ffprobe numeric strings and treats missing values as zero.
func parseFloatOrZero(s string) float64 {
	if s == "" || s == "N/A" {
		return 0
	}
	v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v
}

// parseFloatOrNaN converts packet timestamps and marks missing values as NaN.
func parseFloatOrNaN(s string) float64 {
	if s == "" || s == "N/A" {
		return math.NaN()
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return math.NaN()
	}
	return v
}

// parseRate parses ffprobe frame-rate strings such as "30000/1001".
func parseRate(rate string) float64 {
	if rate == "" || rate == "0/0" || rate == "N/A" {
		return 0
	}

	parts := strings.Split(rate, "/")
	if len(parts) == 1 {
		return parseFloatOrZero(parts[0])
	}

	num := parseFloatOrZero(parts[0])
	den := parseFloatOrZero(parts[1])
	if den == 0 {
		return 0
	}

	return num / den
}

// selectROI chooses either a static ROI or a simple motion-derived ROI.
func selectROI(cfg Config, info VideoInfo, tmpDir string) (ROI, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Mode)) {
	case "static":
		if strings.TrimSpace(cfg.ROIString) == "" {
			r := defaultCenterROI(info)
			r.Source = "static-default-center"
			return clampROI(r, info), nil
		}

		r, err := parseROI(cfg.ROIString, info)
		if err != nil {
			return ROI{}, err
		}

		r.Source = "static-cli"
		return clampROI(r, info), nil

	case "motion":
		r, err := detectMotionROI(cfg, info, tmpDir)
		if err != nil {
			return ROI{}, err
		}

		return clampROI(r, info), nil

	default:
		return ROI{}, fmt.Errorf("unknown --mode %q; use static or motion", cfg.Mode)
	}
}

// parseROI accepts x,y,w,h values as pixels or fractions of the frame.
func parseROI(s string, info VideoInfo) (ROI, error) {
	parts := strings.Split(s, ",")
	if len(parts) != 4 {
		return ROI{}, errors.New("--roi must be x,y,w,h")
	}

	vals := make([]float64, 4)
	allFraction := true

	for i, p := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			return ROI{}, fmt.Errorf("invalid ROI component %q: %w", p, err)
		}

		vals[i] = v

		if v < 0 || v > 1 {
			allFraction = false
		}
	}

	if allFraction {
		return ROI{
			X: int(math.Round(vals[0] * float64(info.Width))),
			Y: int(math.Round(vals[1] * float64(info.Height))),
			W: int(math.Round(vals[2] * float64(info.Width))),
			H: int(math.Round(vals[3] * float64(info.Height))),
		}, nil
	}

	return ROI{
		X: int(vals[0]),
		Y: int(vals[1]),
		W: int(vals[2]),
		H: int(vals[3]),
	}, nil
}

// defaultCenterROI returns an even-sized ROI centered in the frame.
func defaultCenterROI(info VideoInfo) ROI {
	w := evenInt(int(float64(info.Width) * 0.38))
	h := evenInt(int(float64(info.Height) * 0.38))
	x := evenInt((info.Width - w) / 2)
	y := evenInt((info.Height - h) / 2)

	return ROI{
		X: x,
		Y: y,
		W: w,
		H: h,
	}
}

// clampROI keeps ROI coordinates inside the frame and compatible with yuv420p.
func clampROI(r ROI, info VideoInfo) ROI {
	const minW = 32
	const minH = 32

	if r.W < minW {
		r.W = minW
	}
	if r.H < minH {
		r.H = minH
	}
	if r.W > info.Width {
		r.W = info.Width
	}
	if r.H > info.Height {
		r.H = info.Height
	}
	if r.X < 0 {
		r.X = 0
	}
	if r.Y < 0 {
		r.Y = 0
	}
	if r.X+r.W > info.Width {
		r.X = info.Width - r.W
	}
	if r.Y+r.H > info.Height {
		r.Y = info.Height - r.H
	}

	r.X = evenInt(r.X)
	r.Y = evenInt(r.Y)
	r.W = evenInt(r.W)
	r.H = evenInt(r.H)

	if r.X+r.W > info.Width {
		r.W = evenInt(info.Width - r.X)
	}
	if r.Y+r.H > info.Height {
		r.H = evenInt(info.Height - r.Y)
	}

	return r
}

// evenInt rounds down to a positive even integer for codec-friendly dimensions.
func evenInt(v int) int {
	if v < 0 {
		v = 0
	}
	if v%2 != 0 {
		v--
	}
	if v < 2 {
		return 2
	}
	return v
}

// detectMotionROI estimates ROI from the changed pixels between two sampled frames.
func detectMotionROI(cfg Config, info VideoInfo, tmpDir string) (ROI, error) {
	first := filepath.Join(tmpDir, "motion_a.png")
	second := filepath.Join(tmpDir, "motion_b.png")

	t1 := 0.0
	t2 := cfg.MotionWindow

	if info.Duration > 2*cfg.MotionWindow+0.2 {
		t1 = math.Max(0.0, info.Duration*0.25)
		t2 = math.Min(info.Duration-0.1, t1+cfg.MotionWindow)
	}

	if err := extractFrame(cfg.Input, t1, first); err != nil {
		return ROI{}, err
	}
	if err := extractFrame(cfg.Input, t2, second); err != nil {
		return ROI{}, err
	}

	imgA, err := loadImage(first)
	if err != nil {
		return ROI{}, err
	}
	imgB, err := loadImage(second)
	if err != nil {
		return ROI{}, err
	}

	r, changed := motionBBox(imgA, imgB, cfg.MotionThresh)
	if !changed {
		d := defaultCenterROI(info)
		d.Source = "motion-fallback-center"
		return d, nil
	}

	r = expandROI(r, cfg.ROIMargin, info)
	r.Source = "motion-diff"

	return r, nil
}

// extractFrame writes one RGB frame from the input at the requested timestamp.
func extractFrame(input string, sec float64, output string) error {
	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-ss", fmt.Sprintf("%.3f", sec),
		"-i", input,
		"-frames:v", "1",
		"-vf", "format=rgb24",
		output,
	}
	return runCommand("ffmpeg", args...)
}

// loadImage decodes a PNG or JPEG frame from disk.
func loadImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	return img, err
}

// motionBBox returns the bounding box of pixels whose luma changed enough.
func motionBBox(a, b image.Image, thresh int) (ROI, bool) {
	ba := a.Bounds()
	bb := b.Bounds()

	w := minInt(ba.Dx(), bb.Dx())
	h := minInt(ba.Dy(), bb.Dy())

	if w <= 0 || h <= 0 {
		return ROI{}, false
	}

	minX := w
	minY := h
	maxX := 0
	maxY := 0
	changed := 0
	step := 4

	for y := 0; y < h; y += step {
		for x := 0; x < w; x += step {
			ga := grayAt(a, ba.Min.X+x, ba.Min.Y+y)
			gb := grayAt(b, bb.Min.X+x, bb.Min.Y+y)

			if absInt(ga-gb) >= thresh {
				if x < minX {
					minX = x
				}
				if y < minY {
					minY = y
				}
				if x > maxX {
					maxX = x
				}
				if y > maxY {
					maxY = y
				}
				changed++
			}
		}
	}

	if changed < 50 {
		return ROI{}, false
	}

	return ROI{
		X: minX,
		Y: minY,
		W: maxX - minX + step,
		H: maxY - minY + step,
	}, true
}

// grayAt approximates luma for quick motion comparison.
func grayAt(img image.Image, x, y int) int {
	r, g, b, _ := img.At(x, y).RGBA()

	rr := int(r >> 8)
	gg := int(g >> 8)
	bb := int(b >> 8)

	return (299*rr + 587*gg + 114*bb) / 1000
}

// expandROI adds margin around a detected box and clamps it to the frame.
func expandROI(r ROI, margin float64, info VideoInfo) ROI {
	if margin < 0 {
		margin = 0
	}

	dx := int(float64(r.W) * margin)
	dy := int(float64(r.H) * margin)

	r.X -= dx
	r.Y -= dy
	r.W += 2 * dx
	r.H += 2 * dy

	return clampROI(r, info)
}

// fitROIToTarget tries periphery settings and writes the ROI output that best matches the target.
func fitROIToTarget(cfg Config, info VideoInfo, roi ROI, targetKbps float64, output string, workDir string) (EncodeDecision, error) {
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return EncodeDecision{}, err
	}

	rateControl := roiRateControl(cfg)

	if !cfg.FitROI {
		path := filepath.Join(workDir, fmt.Sprintf("roi_manual_%s.mp4", rateControl))
		if err := renderROICandidate(cfg, info, roi, path, cfg.ROIHighQualityCRF, cfg.ManualPeripheryScale, cfg.ManualBlurRadius, targetKbps); err != nil {
			return EncodeDecision{}, err
		}

		actual, err := measuredAverageBitrateKbps(path)
		if err != nil {
			return EncodeDecision{}, err
		}

		c := Candidate{
			Kind:        roiCandidateKind(rateControl),
			CRF:         cfg.ROIHighQualityCRF,
			RateControl: rateControl,
			Scale:       cfg.ManualPeripheryScale,
			Blur:        cfg.ManualBlurRadius,
			Kbps:        actual,
			Path:        path,
		}
		if cfg.ROIFitMetric {
			if err := attachROIPSNRMetric(cfg, roi, &c, filepath.Join(workDir, "roi_manual_psnr.log")); err != nil {
				fmt.Printf("      warning: ROI candidate metric failed: %v\n", err)
			}
		}

		if err := copyFile(path, output); err != nil {
			return EncodeDecision{}, err
		}

		return EncodeDecision{
			Name:            "roi",
			TargetKbps:      targetKbps,
			ActualKbps:      actual,
			WithinTolerance: withinTolerance(actual, targetKbps, cfg.Tolerance),
			CRF:             cfg.ROIHighQualityCRF,
			RateControl:     rateControl,
			Scale:           cfg.ManualPeripheryScale,
			Blur:            cfg.ManualBlurRadius,
			ROIYPSNR:        c.ROIYPSNR,
			Note:            fmt.Sprintf("manual ROI periphery settings; %s rate control", rateControl),
			Candidates:      []CandidateSummary{candidateSummary(c)},
		}, nil
	}

	settings := peripheryCandidates(cfg)

	var candidates []Candidate

	for idx, s := range settings {
		path := filepath.Join(workDir, fmt.Sprintf("roi_%s_crf_%02d_candidate_%02d_scale_%.2f_blur_%02d.mp4",
			rateControl,
			cfg.ROIHighQualityCRF,
			idx,
			s.Scale,
			s.Blur,
		))

		if err := renderROICandidate(cfg, info, roi, path, cfg.ROIHighQualityCRF, s.Scale, s.Blur, targetKbps); err != nil {
			return EncodeDecision{}, err
		}

		kbps, err := measuredAverageBitrateKbps(path)
		if err != nil {
			return EncodeDecision{}, err
		}

		c := Candidate{
			Kind:        roiCandidateKind(rateControl),
			CRF:         cfg.ROIHighQualityCRF,
			RateControl: rateControl,
			Scale:       s.Scale,
			Blur:        s.Blur,
			Kbps:        kbps,
			Path:        path,
		}

		if cfg.ROIFitMetric {
			logPath := filepath.Join(workDir, fmt.Sprintf("roi_candidate_%02d_psnr.log", idx))
			if err := attachROIPSNRMetric(cfg, roi, &c, logPath); err != nil {
				fmt.Printf("      warning: ROI candidate metric failed: %v\n", err)
			}
		}

		candidates = append(candidates, c)

		if c.ROIYPSNR > 0 {
			fmt.Printf("      ROI candidate %s, CRF %2d, scale %.2f, blur %2d -> %.1f kbps, ROI PSNR-Y %.2f dB\n",
				c.RateControl,
				c.CRF,
				c.Scale,
				c.Blur,
				c.Kbps,
				c.ROIYPSNR,
			)
		} else {
			fmt.Printf("      ROI candidate %s, CRF %2d, scale %.2f, blur %2d -> %.1f kbps\n",
				c.RateControl,
				c.CRF,
				c.Scale,
				c.Blur,
				c.Kbps,
			)
		}
	}

	var best Candidate

	if rateControl == "abr" {
		best = chooseBestROIPerceptualCandidate(
			candidates,
			targetKbps,
			cfg.Tolerance,
			cfg.ROIPSNRTieDB,
			cfg.ManualPeripheryScale,
			cfg.ManualBlurRadius,
		)
	} else {
		best = chooseHighestUnderTargetOrMinimum(candidates, targetKbps, cfg.Tolerance)

		if best.Kbps < targetKbps*(1-cfg.Tolerance) && nearlyEqual(best.Scale, 1.0) && best.Blur == 0 && cfg.ROIMinCRF < cfg.ROIHighQualityCRF {
			for crf := cfg.ROIHighQualityCRF - 1; crf >= cfg.ROIMinCRF; crf-- {
				path := filepath.Join(workDir, fmt.Sprintf("roi_full_detail_crf_%02d.mp4", crf))

				if err := renderROICandidateCRF(cfg, info, roi, path, crf, 1.0, 0); err != nil {
					return EncodeDecision{}, err
				}

				kbps, err := measuredAverageBitrateKbps(path)
				if err != nil {
					return EncodeDecision{}, err
				}

				c := Candidate{
					Kind:        "roi-full-detail-lower-crf",
					CRF:         crf,
					RateControl: "crf",
					Scale:       1.0,
					Blur:        0,
					Kbps:        kbps,
					Path:        path,
					Note:        "content is simple; tried lower CRF to use more of the requested budget without degrading periphery",
				}
				candidates = append(candidates, c)

				fmt.Printf("      ROI full-detail candidate CRF %2d, scale 1.00, blur 0 -> %.1f kbps\n", crf, kbps)
			}

			best = chooseHighestUnderTargetOrMinimum(candidates, targetKbps, cfg.Tolerance)
		}

		if best.Kbps > targetKbps*(1+cfg.Tolerance) && cfg.AllowROIQualityLoss {
			worst := chooseLowestBitrateCandidate(candidates)

			emergencyCandidates, err := fitROIEmergencyCRF(cfg, info, roi, targetKbps, workDir, worst.Scale, worst.Blur)
			if err != nil {
				return EncodeDecision{}, err
			}

			candidates = append(candidates, emergencyCandidates...)
			best = chooseClosestByBitrate(candidates, targetKbps)
		}
	}

	if best.Path == "" {
		return EncodeDecision{}, errors.New("no ROI candidate was produced")
	}

	if err := copyFile(best.Path, output); err != nil {
		return EncodeDecision{}, err
	}

	note := ""
	if rateControl == "abr" {
		note = "target-rate ROI encode: periphery is simplified before encoding so the same bitrate budget is spent preferentially inside the ROI"
		if best.ROIYPSNR > 0 {
			note += fmt.Sprintf("; selected the least degraded periphery within %.2f dB of the best ROI PSNR candidate", cfg.ROIPSNRTieDB)
		}
		if !withinTolerance(best.Kbps, targetKbps, cfg.Tolerance) {
			note += "; measured bitrate is the closest candidate but is outside tolerance"
		}
	} else {
		if best.Kbps > targetKbps*(1+cfg.Tolerance) {
			note = "target is lower than ROI-preserving minimum; ROI kept high-quality, but output exceeds target"
		}
		if best.Kbps < targetKbps*(1-cfg.Tolerance) && nearlyEqual(best.Scale, 1.0) && best.Blur == 0 {
			note = "target is higher than full-detail high-quality ROI output; output is below target because content is too simple"
		}
		if strings.Contains(best.Kind, "emergency") {
			note = "target required lowering ROI encode quality; this violates strict high-quality ROI assumption"
		}
	}

	return EncodeDecision{
		Name:            "roi",
		TargetKbps:      targetKbps,
		ActualKbps:      best.Kbps,
		WithinTolerance: withinTolerance(best.Kbps, targetKbps, cfg.Tolerance),
		CRF:             best.CRF,
		RateControl:     best.RateControl,
		Scale:           best.Scale,
		Blur:            best.Blur,
		ROIYPSNR:        best.ROIYPSNR,
		Note:            note,
		Candidates:      candidateSummaries(candidates),
	}, nil
}

// fitROIEmergencyCRF raises ROI CRF only when preserving high-quality ROI cannot reach the target.
func fitROIEmergencyCRF(cfg Config, info VideoInfo, roi ROI, targetKbps float64, workDir string, scale float64, blur int) ([]Candidate, error) {
	cache := map[int]Candidate{}

	eval := func(crf int) (Candidate, error) {
		if c, ok := cache[crf]; ok {
			return c, nil
		}

		path := filepath.Join(workDir, fmt.Sprintf("roi_emergency_crf_%02d_scale_%.2f_blur_%02d.mp4", crf, scale, blur))

		if err := renderROICandidateCRF(cfg, info, roi, path, crf, scale, blur); err != nil {
			return Candidate{}, err
		}

		kbps, err := measuredAverageBitrateKbps(path)
		if err != nil {
			return Candidate{}, err
		}

		c := Candidate{
			Kind:        "roi-emergency-quality-loss",
			CRF:         crf,
			RateControl: "crf",
			Scale:       scale,
			Blur:        blur,
			Kbps:        kbps,
			Path:        path,
			Note:        "ROI CRF increased to hit target; high-quality ROI no longer strictly preserved",
		}
		cache[crf] = c

		fmt.Printf("      ROI emergency candidate CRF %2d, scale %.2f, blur %2d -> %.1f kbps\n",
			c.CRF,
			c.Scale,
			c.Blur,
			c.Kbps,
		)

		return c, nil
	}

	var candidates []Candidate

	low := cfg.ROIHighQualityCRF
	high := cfg.ROIMaxCRFIfNeeded

	for i := 0; i < cfg.FitIterations && low <= high; i++ {
		mid := (low + high) / 2

		c, err := eval(mid)
		if err != nil {
			return nil, err
		}

		candidates = appendUniqueCRF(candidates, c)

		if withinTolerance(c.Kbps, targetKbps, cfg.Tolerance) {
			break
		}

		if c.Kbps > targetKbps {
			low = mid + 1
		} else {
			high = mid - 1
		}
	}

	return candidates, nil
}

// renderROICandidate dispatches to the selected ROI rate-control mode.
func renderROICandidate(cfg Config, info VideoInfo, roi ROI, output string, crf int, scale float64, blur int, targetKbps float64) error {
	if roiRateControl(cfg) == "abr" {
		return renderROICandidateABR(cfg, info, roi, output, scale, blur, targetKbps)
	}
	return renderROICandidateCRF(cfg, info, roi, output, crf, scale, blur)
}

// renderROICandidateCRF encodes an ROI candidate with fixed x264 CRF.
func renderROICandidateCRF(cfg Config, info VideoInfo, roi ROI, output string, crf int, scale float64, blur int) error {
	filter := buildROIFilter(info, roi, scale, blur)

	args := []string{
		"-hide_banner",
		"-y",
		"-i", cfg.Input,
		"-filter_complex", filter,
		"-map", "[v]",
		"-an",
		"-c:v", "libx264",
		"-preset", cfg.Preset,
		"-crf", strconv.Itoa(crf),
		"-pix_fmt", "yuv420p",
		"-movflags", "+faststart",
		output,
	}

	return runCommand("ffmpeg", args...)
}

// renderROICandidateABR encodes an ROI candidate around the requested average bitrate.
func renderROICandidateABR(cfg Config, info VideoInfo, roi ROI, output string, scale float64, blur int, targetKbps float64) error {
	if targetKbps <= 0 {
		return errors.New("targetKbps must be greater than zero for ROI ABR encoding")
	}

	filter := buildROIFilter(info, roi, scale, blur)
	bitrate, maxrate, bufsize := roiRateArgs(cfg, targetKbps)

	baseArgs := []string{
		"-hide_banner",
		"-y",
		"-i", cfg.Input,
		"-filter_complex", filter,
		"-map", "[v]",
		"-an",
		"-c:v", "libx264",
		"-preset", cfg.Preset,
		"-b:v", bitrate,
		"-maxrate", maxrate,
		"-bufsize", bufsize,
		"-pix_fmt", "yuv420p",
	}

	if gop := gopSize(info); gop > 0 {
		baseArgs = append(baseArgs, "-g", strconv.Itoa(gop))
	}

	if !cfg.ROITwoPass {
		args := append([]string{}, baseArgs...)
		args = append(args, "-movflags", "+faststart", output)
		return runCommand("ffmpeg", args...)
	}

	passlog := output + ".passlog"
	defer cleanupPassLogs(passlog)

	firstPass := append([]string{}, baseArgs...)
	firstPass = append(firstPass,
		"-pass", "1",
		"-passlogfile", passlog,
		"-f", "null",
		nullOutputName(),
	)
	if err := runCommand("ffmpeg", firstPass...); err != nil {
		return err
	}

	secondPass := append([]string{}, baseArgs...)
	secondPass = append(secondPass,
		"-pass", "2",
		"-passlogfile", passlog,
		"-movflags", "+faststart",
		output,
	)

	return runCommand("ffmpeg", secondPass...)
}

// buildROIFilter creates a degraded full-frame background and overlays the original ROI crop.
func buildROIFilter(info VideoInfo, roi ROI, scale float64, blur int) string {
	peripheryFilter := buildPeripheryFilter(info, scale, blur)

	return fmt.Sprintf(
		"[0:v]split=2[bgsrc][roisrc];"+
			"[bgsrc]%s[bg];"+
			"[roisrc]crop=%d:%d:%d:%d,format=yuv420p[roi];"+
			"[bg][roi]overlay=%d:%d,format=yuv420p[v]",
		peripheryFilter,
		roi.W,
		roi.H,
		roi.X,
		roi.Y,
		roi.X,
		roi.Y,
	)
}

// roiRateArgs derives bitrate, maxrate, and bufsize arguments for x264 ABR.
func roiRateArgs(cfg Config, targetKbps float64) (string, string, string) {
	maxrateKbps := targetKbps * cfg.ROIMaxrateMultiplier
	if maxrateKbps < targetKbps {
		maxrateKbps = targetKbps
	}

	bufsizeKbps := targetKbps * cfg.ROIBufsizeSeconds
	if bufsizeKbps < targetKbps {
		bufsizeKbps = targetKbps
	}

	return bitrateArgKbps(targetKbps), bitrateArgKbps(maxrateKbps), bitrateArgKbps(bufsizeKbps)
}

// bitrateArgKbps formats a kilobit value for FFmpeg arguments.
func bitrateArgKbps(kbps float64) string {
	if kbps < 1 {
		kbps = 1
	}
	return fmt.Sprintf("%dk", int(math.Round(kbps)))
}

// gopSize uses a roughly two-second GOP with conservative bounds.
func gopSize(info VideoInfo) int {
	fps := info.FPS
	if fps <= 0 {
		fps = 30
	}
	gop := int(math.Round(fps * 2.0))
	if gop < 12 {
		return 12
	}
	if gop > 300 {
		return 300
	}
	return gop
}

// cleanupPassLogs removes x264 two-pass side files.
func cleanupPassLogs(prefix string) {
	matches, _ := filepath.Glob(prefix + "*")
	for _, m := range matches {
		_ = os.Remove(m)
	}
}

// nullOutputName returns the platform-specific null sink for FFmpeg first passes.
func nullOutputName() string {
	if runtime.GOOS == "windows" {
		return "NUL"
	}
	return "/dev/null"
}

// roiRateControl normalizes the ROI rate-control flag.
func roiRateControl(cfg Config) string {
	rc := strings.ToLower(strings.TrimSpace(cfg.ROIRateControl))
	if rc == "" {
		return "abr"
	}
	return rc
}

// roiCandidateKind labels candidates by their encoding mode.
func roiCandidateKind(rateControl string) string {
	if rateControl == "abr" {
		return "roi-target-abr"
	}
	return "roi-preserve-roi-crf"
}

// buildPeripheryFilter builds the scaling and blur chain applied outside the ROI.
func buildPeripheryFilter(info VideoInfo, scale float64, blur int) string {
	var parts []string

	if scale < 0.999 {
		parts = append(
			parts,
			fmt.Sprintf(
				"scale=trunc(iw*%.4f/2)*2:trunc(ih*%.4f/2)*2,scale=%d:%d:flags=bilinear",
				scale,
				scale,
				info.Width,
				info.Height,
			),
		)
	}

	if blur > 0 {
		parts = append(
			parts,
			fmt.Sprintf(
				"boxblur=luma_radius=%d:luma_power=1:chroma_radius=%d:chroma_power=1",
				blur,
				blur,
			),
		)
	}

	parts = append(parts, "format=yuv420p")

	return strings.Join(parts, ",")
}

// peripheryCandidates returns ordered degradation levels from mild to aggressive.
func peripheryCandidates(cfg Config) []peripherySetting {
	raw := []peripherySetting{
		{Scale: 1.00, Blur: 0},
		{Scale: 0.92, Blur: 0},
		{Scale: 0.84, Blur: 0},
		{Scale: 0.76, Blur: 1},
		{Scale: 0.68, Blur: 1},
		{Scale: 0.60, Blur: 1},
		{Scale: 0.52, Blur: 2},
		{Scale: 0.44, Blur: 2},
		{Scale: 0.36, Blur: 3},
		{Scale: 0.30, Blur: 4},
		{Scale: 0.24, Blur: 6},
		{Scale: 0.18, Blur: 8},
		{Scale: 0.12, Blur: 10},
	}

	var out []peripherySetting

	manualIncluded := false

	for _, s := range raw {
		if s.Scale+0.0001 < cfg.ROIMinScale {
			continue
		}
		if s.Blur > cfg.ROIMaxBlur {
			continue
		}

		out = append(out, s)

		if nearlyEqual(s.Scale, cfg.ManualPeripheryScale) && s.Blur == cfg.ManualBlurRadius {
			manualIncluded = true
		}
	}

	if !manualIncluded && cfg.ManualPeripheryScale >= cfg.ROIMinScale && cfg.ManualBlurRadius <= cfg.ROIMaxBlur {
		out = append(out, peripherySetting{Scale: cfg.ManualPeripheryScale, Blur: cfg.ManualBlurRadius})
		sort.Slice(out, func(i, j int) bool {
			if !nearlyEqual(out[i].Scale, out[j].Scale) {
				return out[i].Scale > out[j].Scale
			}
			return out[i].Blur < out[j].Blur
		})
	}

	if len(out) == 0 {
		out = append(out, peripherySetting{Scale: cfg.ROIMinScale, Blur: cfg.ROIMaxBlur})
	}

	return out
}

// attachROIPSNRMetric records ROI-crop PSNR for a candidate when metric fitting is enabled.
func attachROIPSNRMetric(cfg Config, roi ROI, c *Candidate, logPath string) error {
	m, err := computeROIPSNR(cfg.Input, c.Path, roi, logPath, "candidate", "roi-crop")
	if err != nil {
		return err
	}

	if m.AverageYText == "+Inf" {
		c.ROIYPSNR = 99.0
		return nil
	}
	if m.AverageYDB > 0 {
		c.ROIYPSNR = m.AverageYDB
	}

	return nil
}

// chooseBestROIPerceptualCandidate balances target bitrate, ROI PSNR, and visible periphery loss.
func chooseBestROIPerceptualCandidate(candidates []Candidate, targetKbps float64, tolerance float64, tieDB float64, preferredScale float64, preferredBlur int) Candidate {
	if len(candidates) == 0 {
		return Candidate{}
	}

	pool := candidatesWithinTolerance(candidates, targetKbps, tolerance)
	if len(pool) == 0 {
		pool = candidatesWithinTolerance(candidates, targetKbps, math.Max(tolerance*2.0, 0.15))
	}
	if len(pool) == 0 {
		closest := chooseClosestByBitrate(candidates, targetKbps)
		pool = []Candidate{closest}
	}

	if degraded := peripheryDegradedCandidates(pool); len(degraded) > 0 {
		pool = degraded
	}

	if !hasROIPSNR(pool) {
		return choosePreferredPeripheryCandidate(pool, preferredScale, preferredBlur)
	}

	bestPSNR := math.Inf(-1)
	for _, c := range pool {
		if c.ROIYPSNR > 0 && c.ROIYPSNR > bestPSNR {
			bestPSNR = c.ROIYPSNR
		}
	}

	threshold := bestPSNR - tieDB
	var nearBest []Candidate
	for _, c := range pool {
		if c.ROIYPSNR > 0 && c.ROIYPSNR >= threshold {
			nearBest = append(nearBest, c)
		}
	}
	if len(nearBest) == 0 {
		return chooseHighestROIPSNR(pool)
	}

	return chooseLeastDegraded(nearBest)
}

// peripheryDegradedCandidates keeps candidates that visibly change the area outside the ROI.
func peripheryDegradedCandidates(candidates []Candidate) []Candidate {
	var out []Candidate
	for _, c := range candidates {
		if isPeripheryDegraded(c) {
			out = append(out, c)
		}
	}
	return out
}

// isPeripheryDegraded reports whether a candidate changes the background before encoding.
func isPeripheryDegraded(c Candidate) bool {
	return c.Blur > 0 || c.Scale < 0.999
}

// choosePreferredPeripheryCandidate chooses the candidate closest to the manual degradation knobs.
func choosePreferredPeripheryCandidate(candidates []Candidate, preferredScale float64, preferredBlur int) Candidate {
	if len(candidates) == 0 {
		return Candidate{}
	}

	best := candidates[0]
	bestScore := peripheryPreferenceScore(best, preferredScale, preferredBlur)

	for _, c := range candidates[1:] {
		score := peripheryPreferenceScore(c, preferredScale, preferredBlur)
		if score < bestScore {
			best = c
			bestScore = score
		}
	}

	return best
}

// peripheryPreferenceScore ranks candidates by proximity to the configured manual baseline.
func peripheryPreferenceScore(c Candidate, preferredScale float64, preferredBlur int) float64 {
	scaleScore := math.Abs(c.Scale-preferredScale) * 10
	blurScore := math.Abs(float64(c.Blur-preferredBlur)) * 0.25
	return scaleScore + blurScore
}

// candidatesWithinTolerance filters candidates by relative bitrate error.
func candidatesWithinTolerance(candidates []Candidate, targetKbps float64, tolerance float64) []Candidate {
	var out []Candidate
	for _, c := range candidates {
		if withinTolerance(c.Kbps, targetKbps, tolerance) {
			out = append(out, c)
		}
	}
	return out
}

// hasROIPSNR reports whether any candidate has a usable ROI PSNR score.
func hasROIPSNR(candidates []Candidate) bool {
	for _, c := range candidates {
		if c.ROIYPSNR > 0 {
			return true
		}
	}
	return false
}

// chooseHighestROIPSNR returns the candidate with the best ROI metric.
func chooseHighestROIPSNR(candidates []Candidate) Candidate {
	if len(candidates) == 0 {
		return Candidate{}
	}

	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.ROIYPSNR > best.ROIYPSNR {
			best = c
		}
	}
	return best
}

// chooseLeastDegraded prefers the largest periphery scale and then the smallest blur.
func chooseLeastDegraded(candidates []Candidate) Candidate {
	if len(candidates) == 0 {
		return Candidate{}
	}

	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.Scale > best.Scale+0.0001 {
			best = c
			continue
		}
		if nearlyEqual(c.Scale, best.Scale) && c.Blur < best.Blur {
			best = c
			continue
		}
		if nearlyEqual(c.Scale, best.Scale) && c.Blur == best.Blur && c.ROIYPSNR > best.ROIYPSNR {
			best = c
		}
	}
	return best
}

// measuredAverageBitrateKbps estimates file bitrate from size and probed duration.
func measuredAverageBitrateKbps(path string) (float64, error) {
	info, err := probeVideo(path)
	if err != nil {
		return 0, err
	}

	st, err := os.Stat(path)
	if err != nil {
		return 0, err
	}

	if info.Duration <= 0 {
		return 0, fmt.Errorf("cannot measure bitrate for %s: invalid duration", path)
	}

	return float64(st.Size()*8) / info.Duration / 1000.0, nil
}

// parseBitrateKbps parses CLI bitrate strings and returns kilobits per second.
func parseBitrateKbps(value string) (float64, bool) {
	s := strings.TrimSpace(strings.ToLower(value))
	s = strings.ReplaceAll(s, " ", "")
	if s == "" {
		return 0, false
	}

	s = strings.TrimSuffix(s, "bps")

	multiplier := 1.0

	switch {
	case strings.HasSuffix(s, "kb"):
		s = strings.TrimSuffix(s, "kb")
		multiplier = 1.0
	case strings.HasSuffix(s, "k"):
		s = strings.TrimSuffix(s, "k")
		multiplier = 1.0
	case strings.HasSuffix(s, "mb"):
		s = strings.TrimSuffix(s, "mb")
		multiplier = 1000.0
	case strings.HasSuffix(s, "m"):
		s = strings.TrimSuffix(s, "m")
		multiplier = 1000.0
	default:
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, false
		}
		return v / 1000.0, true
	}

	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, false
	}

	return v * multiplier, true
}

// withinTolerance checks whether a measured bitrate is close enough to target.
func withinTolerance(actual float64, target float64, tolerance float64) bool {
	if target <= 0 {
		return false
	}
	return math.Abs(actual-target)/target <= tolerance
}

// chooseClosestByBitrate returns the candidate nearest to the requested bitrate.
func chooseClosestByBitrate(candidates []Candidate, targetKbps float64) Candidate {
	if len(candidates) == 0 {
		return Candidate{}
	}

	best := candidates[0]
	bestScore := math.Abs(candidates[0].Kbps - targetKbps)

	for _, c := range candidates[1:] {
		score := math.Abs(c.Kbps - targetKbps)
		if score < bestScore {
			best = c
			bestScore = score
		}
	}

	return best
}

// chooseHighestUnderTargetOrMinimum maximizes bitrate under the upper bound, or falls back low.
func chooseHighestUnderTargetOrMinimum(candidates []Candidate, targetKbps float64, tolerance float64) Candidate {
	if len(candidates) == 0 {
		return Candidate{}
	}

	upperLimit := targetKbps * (1 + tolerance)

	var bestUnder Candidate
	hasUnder := false

	for _, c := range candidates {
		if c.Kbps <= upperLimit {
			if !hasUnder || c.Kbps > bestUnder.Kbps {
				bestUnder = c
				hasUnder = true
			}
		}
	}

	if hasUnder {
		return bestUnder
	}

	return chooseLowestBitrateCandidate(candidates)
}

// chooseLowestBitrateCandidate returns the smallest candidate by measured bitrate.
func chooseLowestBitrateCandidate(candidates []Candidate) Candidate {
	if len(candidates) == 0 {
		return Candidate{}
	}

	best := candidates[0]

	for _, c := range candidates[1:] {
		if c.Kbps < best.Kbps {
			best = c
		}
	}

	return best
}

// appendUniqueCRF avoids duplicate emergency-search candidates.
func appendUniqueCRF(candidates []Candidate, c Candidate) []Candidate {
	for _, existing := range candidates {
		if existing.CRF == c.CRF && nearlyEqual(existing.Scale, c.Scale) && existing.Blur == c.Blur {
			return candidates
		}
	}
	return append(candidates, c)
}

// candidateSummary removes transient file paths before writing JSON reports.
func candidateSummary(c Candidate) CandidateSummary {
	return CandidateSummary{
		Kind:        c.Kind,
		CRF:         c.CRF,
		RateControl: c.RateControl,
		Scale:       c.Scale,
		Blur:        c.Blur,
		Kbps:        c.Kbps,
		ROIYPSNR:    c.ROIYPSNR,
		Note:        c.Note,
	}
}

// candidateSummaries converts and sorts candidates for stable reports.
func candidateSummaries(candidates []Candidate) []CandidateSummary {
	out := make([]CandidateSummary, 0, len(candidates))

	for _, c := range candidates {
		out = append(out, candidateSummary(c))
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if out[i].CRF != out[j].CRF {
			return out[i].CRF < out[j].CRF
		}
		if !nearlyEqual(out[i].Scale, out[j].Scale) {
			return out[i].Scale > out[j].Scale
		}
		return out[i].Blur < out[j].Blur
	})

	return out
}

// nearlyEqual compares small floating-point settings such as periphery scale.
func nearlyEqual(a float64, b float64) bool {
	return math.Abs(a-b) < 0.0001
}

// computeBitrateWindows groups ffprobe video packets into time-based bitrate samples.
func computeBitrateWindows(path string, windowSeconds float64, duration float64) ([]BitrateSample, error) {
	if windowSeconds <= 0 {
		return nil, errors.New("windowSeconds must be greater than zero")
	}

	args := []string{
		"-v", "error",
		"-select_streams", "v:0",
		"-show_packets",
		"-show_entries", "packet=pts_time,dts_time,size",
		"-of", "json",
		path,
	}

	out, err := commandOutput("ffprobe", args...)
	if err != nil {
		return nil, fmt.Errorf("ffprobe packets for %s: %w", path, err)
	}

	var pj ffprobePacketsJSON
	if err := json.Unmarshal(out, &pj); err != nil {
		return nil, fmt.Errorf("parse packet JSON for %s: %w", path, err)
	}

	if duration <= 0 {
		info, err := probeVideo(path)
		if err == nil {
			duration = info.Duration
		}
	}

	maxTime := duration
	for _, p := range pj.Packets {
		t := packetTime(p.PTS, p.DTS)
		if !math.IsNaN(t) && t > maxTime {
			maxTime = t
		}
	}

	if maxTime <= 0 {
		maxTime = windowSeconds
	}

	n := int(math.Ceil(maxTime / windowSeconds))
	if n < 1 {
		n = 1
	}

	bytesPerWindow := make([]int64, n)

	for _, p := range pj.Packets {
		t := packetTime(p.PTS, p.DTS)
		if math.IsNaN(t) || t < 0 {
			continue
		}

		sizeBytes, err := strconv.ParseInt(strings.TrimSpace(p.Size), 10, 64)
		if err != nil || sizeBytes < 0 {
			continue
		}

		idx := int(math.Floor(t / windowSeconds))
		if idx < 0 {
			continue
		}
		if idx >= n {
			idx = n - 1
		}

		bytesPerWindow[idx] += sizeBytes
	}

	samples := make([]BitrateSample, 0, n)

	for i, sizeBytes := range bytesPerWindow {
		start := float64(i) * windowSeconds
		end := start + windowSeconds

		if duration > 0 && end > duration {
			end = duration
		}
		if end <= start {
			end = start + windowSeconds
		}

		seconds := end - start
		kbps := float64(sizeBytes*8) / seconds / 1000.0

		samples = append(samples, BitrateSample{
			Start: start,
			End:   end,
			Kbps:  kbps,
		})
	}

	return samples, nil
}

// packetTime prefers DTS and falls back to PTS for window placement.
func packetTime(pts string, dts string) float64 {
	t := parseFloatOrNaN(dts)
	if !math.IsNaN(t) {
		return t
	}
	return parseFloatOrNaN(pts)
}

// summarizeBitrate calculates average and percentile bitrate values.
func summarizeBitrate(samples []BitrateSample) BitrateSummary {
	if len(samples) == 0 {
		return BitrateSummary{}
	}

	values := make([]float64, 0, len(samples))
	var bits float64
	var seconds float64
	minVal := math.Inf(1)
	maxVal := math.Inf(-1)

	for _, s := range samples {
		d := s.End - s.Start
		if d <= 0 {
			continue
		}

		values = append(values, s.Kbps)
		bits += s.Kbps * 1000.0 * d
		seconds += d

		if s.Kbps < minVal {
			minVal = s.Kbps
		}
		if s.Kbps > maxVal {
			maxVal = s.Kbps
		}
	}

	if len(values) == 0 || seconds <= 0 {
		return BitrateSummary{}
	}

	sort.Float64s(values)

	return BitrateSummary{
		AverageKbps: bits / seconds / 1000.0,
		P50Kbps:     percentileSorted(values, 0.50),
		P95Kbps:     percentileSorted(values, 0.95),
		MinKbps:     minVal,
		MaxKbps:     maxVal,
	}
}

// inputBaselineDecision wraps the original input as the comparison baseline.
func inputBaselineDecision(summary BitrateSummary, artifact Artifact) EncodeDecision {
	actual := summary.AverageKbps
	if actual <= 0 {
		actual = artifact.BitrateKbps
	}

	return EncodeDecision{
		Name:            "input-baseline",
		ActualKbps:      actual,
		WithinTolerance: true,
		RateControl:     "source",
		SizeBytes:       artifact.SizeBytes,
		Note:            "original input video; not re-encoded",
	}
}

// percentileSorted returns an interpolated percentile from sorted values.
func percentileSorted(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	if len(values) == 1 {
		return values[0]
	}
	if p <= 0 {
		return values[0]
	}
	if p >= 1 {
		return values[len(values)-1]
	}

	pos := p * float64(len(values)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))

	if lo == hi {
		return values[lo]
	}

	weight := pos - float64(lo)

	return values[lo]*(1-weight) + values[hi]*weight
}

// renderComparison creates the side-by-side input-vs-ROI video with text overlays and ROI boxes.
func renderComparison(
	cfg Config,
	baseline string,
	roiVideo string,
	output string,
	baselineSamples []BitrateSample,
	roiSamples []BitrateSample,
	info VideoInfo,
	roi ROI,
	baselineDecision EncodeDecision,
	roiDecision EncodeDecision,
) error {
	var prefix string

	if cfg.OverlayBitrate {
		if len(baselineSamples) > cfg.MaxBitrateOverlays || len(roiSamples) > cfg.MaxBitrateOverlays {
			return fmt.Errorf(
				"too many bitrate overlay windows: baseline=%d roi=%d cap=%d; increase --bitrate-window or --max-bitrate-overlays",
				len(baselineSamples),
				len(roiSamples),
				cfg.MaxBitrateOverlays,
			)
		}

		leftChain := drawPanelTextChain(
			"[0:v]",
			"INPUT baseline",
			baselineDecision,
			baselineSamples,
			false,
			"[left]",
		)

		rightChain := drawPanelTextChain(
			"[1:v]",
			"ROI output",
			roiDecision,
			roiSamples,
			true,
			"[right]",
		)

		prefix = leftChain + ";" + rightChain + ";"
	} else {
		leftChain := drawStaticPanelTextChain("[0:v]", "INPUT baseline", baselineDecision, false, "[left]")
		rightChain := drawStaticPanelTextChain("[1:v]", "ROI output", roiDecision, true, "[right]")
		prefix = leftChain + ";" + rightChain + ";"
	}

	leftBox := fmt.Sprintf(
		"drawbox=x=%d:y=%d:w=%d:h=%d:color=red@0.90:t=4",
		roi.X,
		roi.Y,
		roi.W,
		roi.H,
	)

	rightBox := fmt.Sprintf(
		"drawbox=x=%d:y=%d:w=%d:h=%d:color=lime@0.90:t=4",
		info.Width+roi.X,
		roi.Y,
		roi.W,
		roi.H,
	)

	filter := prefix + fmt.Sprintf(
		"[left][right]hstack=inputs=2,%s,%s,format=yuv420p[v]",
		leftBox,
		rightBox,
	)

	args := []string{
		"-hide_banner",
		"-y",
		"-i", baseline,
		"-i", roiVideo,
		"-filter_complex", filter,
		"-map", "[v]",
		"-an",
		"-c:v", "libx264",
		"-preset", cfg.Preset,
		"-crf", "18",
		"-pix_fmt", "yuv420p",
		"-movflags", "+faststart",
		output,
	}

	return runCommand("ffmpeg", args...)
}

// drawPanelTextChain builds drawtext filters with per-window current bitrate labels.
func drawPanelTextChain(
	inputLabel string,
	title string,
	decision EncodeDecision,
	samples []BitrateSample,
	isROI bool,
	outputLabel string,
) string {
	filters := panelBaseFilters(title, decision, isROI)

	for _, s := range samples {
		txt := fmt.Sprintf("current %.0f kbps", s.Kbps)
		enable := fmt.Sprintf("between(t\\,%.3f\\,%.3f)", s.Start, s.End)

		filters = append(
			filters,
			drawTextFilter(txt, 24, 168, 24, "yellow", "black@0.70", enable),
		)
	}

	return inputLabel + strings.Join(filters, ",") + outputLabel
}

// drawStaticPanelTextChain builds drawtext filters without dynamic bitrate labels.
func drawStaticPanelTextChain(inputLabel string, title string, decision EncodeDecision, isROI bool, outputLabel string) string {
	filters := panelBaseFilters(title, decision, isROI)
	return inputLabel + strings.Join(filters, ",") + outputLabel
}

// panelBaseFilters returns the static overlay text shared by both comparison panels.
func panelBaseFilters(title string, decision EncodeDecision, isROI bool) []string {
	line2 := fmt.Sprintf("target %.0f kbps | actual %.0f kbps", decision.TargetKbps, decision.ActualKbps)
	if decision.SizeBytes > 0 {
		line2 += " | " + formatBytes(decision.SizeBytes)
	}

	if decision.RateControl == "source" {
		line2 = fmt.Sprintf("source avg %.0f kbps", decision.ActualKbps)
		if decision.SizeBytes > 0 {
			line2 += " | " + formatBytes(decision.SizeBytes)
		}

		return []string{
			drawTextFilter(title, 24, 24, 28, "white", "black@0.65", ""),
			drawTextFilter(line2, 24, 64, 22, "white", "black@0.65", ""),
			drawTextFilter("original input, not re-encoded", 24, 98, 21, "white", "black@0.65", ""),
			drawTextFilter("reference subjective quality", 24, 132, 21, "white", "black@0.65", ""),
		}
	}

	status := "within tolerance"
	if !decision.WithinTolerance {
		status = "closest possible"
	}

	line3 := fmt.Sprintf("%s | CRF %d", status, decision.CRF)

	line4 := ""
	if isROI {
		rateControl := strings.ToUpper(strings.TrimSpace(decision.RateControl))
		if rateControl == "" || rateControl == "CRF" {
			line4 = fmt.Sprintf("ROI CRF %d | bg scale %.2f blur %d", decision.CRF, decision.Scale, decision.Blur)
		} else {
			line3 = fmt.Sprintf("%s | %s target-rate", status, rateControl)
			line4 = fmt.Sprintf("ROI biased | bg scale %.2f blur %d", decision.Scale, decision.Blur)
		}
	} else {
		line4 = fmt.Sprintf("uniform full-frame CRF %d", decision.CRF)
	}

	return []string{
		drawTextFilter(title, 24, 24, 28, "white", "black@0.65", ""),
		drawTextFilter(line2, 24, 64, 22, "white", "black@0.65", ""),
		drawTextFilter(line3, 24, 98, 21, "white", "black@0.65", ""),
		drawTextFilter(line4, 24, 132, 21, "white", "black@0.65", ""),
	}
}

// drawTextFilter formats one FFmpeg drawtext filter with optional time gating.
func drawTextFilter(text string, x int, y int, fontSize int, fontColor string, boxColor string, enableExpr string) string {
	parts := []string{
		"drawtext=text='" + escapeDrawText(text) + "'",
		fmt.Sprintf("x=%d", x),
		fmt.Sprintf("y=%d", y),
		fmt.Sprintf("fontsize=%d", fontSize),
		"fontcolor=" + fontColor,
		"box=1",
		"boxcolor=" + boxColor,
		"boxborderw=8",
	}

	if enableExpr != "" {
		parts = append(parts, "enable='"+enableExpr+"'")
	}

	return strings.Join(parts, ":")
}

// escapeDrawText escapes characters that are special inside drawtext arguments.
func escapeDrawText(s string) string {
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		"'", "\\'",
		":", "\\:",
		"%", "\\%",
		",", "\\,",
	)
	return replacer.Replace(s)
}

// renderPreview writes one still frame with the selected ROI box.
func renderPreview(cfg Config, info VideoInfo, roi ROI, output string) error {
	t := 0.0
	if info.Duration > 0 {
		t = math.Min(info.Duration*0.25, math.Max(0.0, info.Duration-0.1))
	}

	filter := fmt.Sprintf(
		"drawbox=x=%d:y=%d:w=%d:h=%d:color=lime@0.95:t=6,format=rgb24",
		roi.X,
		roi.Y,
		roi.W,
		roi.H,
	)

	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-ss", fmt.Sprintf("%.3f", t),
		"-i", cfg.Input,
		"-frames:v", "1",
		"-vf", filter,
		output,
	}

	return runCommand("ffmpeg", args...)
}

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

// runCommand executes a command while streaming its output to the terminal.
func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %w", name, err)
	}

	return nil
}

// commandOutput executes a command and returns stdout with stderr in errors.
func commandOutput(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s failed: %w\n%s", name, err, strings.TrimSpace(stderr.String()))
	}

	return stdout.Bytes(), nil
}

// commandCombinedOutput executes a command and returns stdout and stderr together.
func commandCombinedOutput(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	return cmd.CombinedOutput()
}

// copyFile copies the selected candidate to the stable output path.
func copyFile(src string, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	out, err := os.Create(dst)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}

	if err := out.Close(); err != nil {
		return err
	}

	return nil
}

// artifactFor collects file size and container-level bitrate for reports.
func artifactFor(path string) Artifact {
	st, err := os.Stat(path)
	if err != nil {
		return Artifact{Path: path}
	}

	artifact := Artifact{
		Path:      path,
		SizeBytes: st.Size(),
	}

	if strings.HasSuffix(strings.ToLower(path), ".mp4") {
		info, err := probeVideo(path)
		if err == nil && info.Duration > 0 {
			artifact.BitrateKbps = float64(st.Size()*8) / info.Duration / 1000.0
		}
	}

	return artifact
}

// formatBytes renders file sizes for overlays and logs.
func formatBytes(size int64) string {
	if size <= 0 {
		return "size n/a"
	}
	if size < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(size)/1024.0)
	}
	if size < 1024*1024*1024 {
		return fmt.Sprintf("%.1f MB", float64(size)/(1024.0*1024.0))
	}

	return fmt.Sprintf("%.2f GB", float64(size)/(1024.0*1024.0*1024.0))
}

// serve exposes the output directory through a local HTTP file server.
func serve(dir string, addr string) error {
	fs := http.FileServer(http.Dir(dir))
	http.Handle("/", fs)
	return http.ListenAndServe(addr, nil)
}

// minInt returns the smaller integer.
func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

// absInt returns the absolute integer value.
func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
