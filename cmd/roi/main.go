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
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	// Input is any FFmpeg-readable video source: local file, URL, or stream.
	Input string
	// OutDir stores all generated videos, preview image, report, and temporary frames.
	OutDir string
	// Mode selects ROI source: "static" uses CLI/default ROI, "motion" detects it from frame differences.
	Mode string
	// ROIString is x,y,w,h in pixels or fractions of the frame size.
	ROIString string
	// PeripheryScale controls how much the non-ROI area is downscaled before being restored.
	PeripheryScale float64
	// BlurRadius adds visible degradation to the non-ROI area after downscale/upscale.
	BlurRadius int
	// CRF and Preset are passed to libx264 for all generated demo videos.
	CRF    int
	Preset string
	// MotionWindow is the time gap between two sampled frames for motion ROI detection.
	MotionWindow float64
	// MotionThresh is the per-pixel grayscale difference needed to mark motion.
	MotionThresh int
	// ROIMargin expands detected motion boxes so the ROI is not clipped too tightly.
	ROIMargin float64
	// Serve starts a small HTTP server for browsing generated artifacts.
	Serve bool
	// HTTPAddr is the listen address used only when Serve is enabled.
	HTTPAddr string
	// KeepTemp preserves extracted frames for debugging motion detection.
	KeepTemp bool
}

// VideoInfo contains the input stream properties used to size filters and reports.
type VideoInfo struct {
	Width    int     `json:"width"`
	Height   int     `json:"height"`
	Duration float64 `json:"duration_seconds"`
	FPS      float64 `json:"fps"`
}

// ROI describes a rectangular region of interest in source-frame pixels.
type ROI struct {
	X      int    `json:"x"`
	Y      int    `json:"y"`
	W      int    `json:"w"`
	H      int    `json:"h"`
	Source string `json:"source"`
}

// Artifact records a generated output file and its approximate video bitrate.
type Artifact struct {
	Path       string  `json:"path"`
	SizeBytes  int64   `json:"size_bytes"`
	BitrateKbs float64 `json:"bitrate_kbps,omitempty"`
}

// Report is the machine-readable summary written next to generated artifacts.
type Report struct {
	CreatedAt string     `json:"created_at"`
	Input     string     `json:"input"`
	Mode      string     `json:"mode"`
	Video     VideoInfo  `json:"video"`
	ROI       ROI        `json:"roi"`
	Artifacts []Artifact `json:"artifacts"`
	Notes     []string   `json:"notes"`
}

// ffprobeJSON mirrors only the ffprobe fields this program reads.
type ffprobeJSON struct {
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

// main is the CLI entrypoint.
func main() {
	cfg := parseFlags()
	if err := run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "\n[ERROR] %v\n", err)
		os.Exit(1)
	}
}

// parseFlags reads CLI options into Config and leaves validation to run.
func parseFlags() Config {
	var cfg Config

	flag.StringVar(&cfg.Input, "input", "", "path/URL to input video file, RTSP URL, or another FFmpeg-readable source")
	flag.StringVar(&cfg.OutDir, "out", "out", "output directory")
	flag.StringVar(&cfg.Mode, "mode", "static", "ROI mode: static or motion")
	flag.StringVar(&cfg.ROIString, "roi", "", "static ROI as x,y,w,h; pixels or fractions 0..1; if empty, center ROI is used")
	flag.Float64Var(&cfg.PeripheryScale, "periphery-scale", 0.42, "scale factor for degraded periphery, e.g. 0.35..0.60")
	flag.IntVar(&cfg.BlurRadius, "blur", 2, "boxblur radius for degraded periphery")
	flag.IntVar(&cfg.CRF, "crf", 23, "H.264 CRF for generated demo videos")
	flag.StringVar(&cfg.Preset, "preset", "veryfast", "x264 preset for generated demo videos")
	flag.Float64Var(&cfg.MotionWindow, "motion-window", 0.6, "time gap in seconds between frames used for simple motion ROI detection")
	flag.IntVar(&cfg.MotionThresh, "motion-threshold", 34, "grayscale difference threshold for motion ROI detection")
	flag.Float64Var(&cfg.ROIMargin, "roi-margin", 0.18, "ROI expansion margin as fraction of detected bbox size")
	flag.BoolVar(&cfg.Serve, "serve", false, "start local HTTP server with output artifacts after processing")
	flag.StringVar(&cfg.HTTPAddr, "http", ":8080", "HTTP address for --serve")
	flag.BoolVar(&cfg.KeepTemp, "keep-temp", false, "keep temporary extracted frames")

	flag.Parse()
	return cfg
}

// run validates configuration, creates all artifacts, writes the report, and optionally serves the output directory.
func run(cfg Config) error {
	if strings.TrimSpace(cfg.Input) == "" {
		return errors.New("missing --input")
	}
	if cfg.PeripheryScale <= 0 || cfg.PeripheryScale >= 1 {
		return errors.New("--periphery-scale must be in range (0,1)")
	}
	if cfg.BlurRadius < 0 || cfg.BlurRadius > 30 {
		return errors.New("--blur must be in range 0..30")
	}
	if cfg.CRF < 0 || cfg.CRF > 51 {
		return errors.New("--crf must be in range 0..51")
	}

	if err := ensureTool("ffmpeg"); err != nil {
		return err
	}
	if err := ensureTool("ffprobe"); err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.OutDir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	fmt.Println("[1/6] Probing input video...")

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

	fmt.Printf("      %dx%d, duration %.2fs, fps %.2f\n", info.Width, info.Height, info.Duration, info.FPS)

	tmpDir := filepath.Join(cfg.OutDir, "_tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return err
	}
	if !cfg.KeepTemp {
		defer os.RemoveAll(tmpDir)
	}

	fmt.Println("[2/6] Selecting ROI...")

	roi, err := selectROI(cfg, info, tmpDir)
	if err != nil {
		return err
	}

	fmt.Printf("      ROI %s: x=%d y=%d w=%d h=%d\n", roi.Source, roi.X, roi.Y, roi.W, roi.H)

	baseline := filepath.Join(cfg.OutDir, "baseline_uniform_low_quality.mp4")
	roiVideo := filepath.Join(cfg.OutDir, "roi_high_quality_region.mp4")
	comparison := filepath.Join(cfg.OutDir, "comparison_baseline_vs_roi.mp4")
	preview := filepath.Join(cfg.OutDir, "roi_preview.png")
	reportPath := filepath.Join(cfg.OutDir, "report.json")

	fmt.Println("[3/6] Rendering baseline video: full frame is degraded uniformly...")

	if err := renderBaseline(cfg, info, roi, baseline); err != nil {
		return err
	}

	fmt.Println("[4/6] Rendering ROI video: periphery is degraded, ROI is kept from original...")

	if err := renderROIVideo(cfg, info, roi, roiVideo); err != nil {
		return err
	}

	baselineArtifact := artifactFor(baseline, info.Duration)
	roiArtifact := artifactFor(roiVideo, info.Duration)

	fmt.Println("[5/6] Rendering side-by-side comparison and preview frame...")

	if err := renderComparison(cfg, baseline, roiVideo, baselineArtifact, roiArtifact, comparison); err != nil {
		return err
	}
	if err := renderPreview(cfg, info, roi, preview); err != nil {
		return err
	}

	fmt.Println("[6/6] Writing report...")

	report := Report{
		CreatedAt: time.Now().Format(time.RFC3339),
		Input:     cfg.Input,
		Mode:      cfg.Mode,
		Video:     info,
		ROI:       roi,
		Notes: []string{
			"PoC uses mask-based spatial quality redistribution: the ROI crop is overlaid from the original stream onto a degraded periphery.",
			"This demonstrates Stage 3 feasibility before moving to encoder-level QP/ROI maps in the prototype stage.",
		},
	}

	report.Artifacts = append(report.Artifacts,
		baselineArtifact,
		roiArtifact,
		artifactFor(comparison, info.Duration),
		artifactFor(preview, info.Duration),
	)

	b, _ := json.MarshalIndent(report, "", "  ")
	if err := os.WriteFile(reportPath, b, 0o644); err != nil {
		return fmt.Errorf("write report: %w", err)
	}

	fmt.Println("\nDone. Artifacts:")
	fmt.Printf("  - %s\n", baseline)
	fmt.Printf("  - %s\n", roiVideo)
	fmt.Printf("  - %s\n", comparison)
	fmt.Printf("  - %s\n", preview)
	fmt.Printf("  - %s\n", reportPath)

	if cfg.Serve {
		fmt.Printf("\nServing %s at http://localhost%s/\n", cfg.OutDir, cfg.HTTPAddr)
		return serve(cfg.OutDir, cfg.HTTPAddr)
	}

	return nil
}

// ensureTool verifies that an external binary needed by the pipeline is available.
func ensureTool(name string) error {
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("required tool %q not found in PATH", name)
	}
	return nil
}

// probeVideo reads the first video stream metadata through ffprobe.
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

	var pj ffprobeJSON
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

// parseFloatOrZero parses ffprobe numeric fields, treating missing values as zero.
func parseFloatOrZero(s string) float64 {
	if s == "" || s == "N/A" {
		return 0
	}

	v, _ := strconv.ParseFloat(s, 64)
	return v
}

// parseRate converts ffprobe frame-rate values like "30000/1001" to fps.
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

// selectROI chooses the ROI from static settings or motion detection.
func selectROI(cfg Config, info VideoInfo, tmpDir string) (ROI, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Mode)) {
	case "static":
		if cfg.ROIString == "" {
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

// parseROI accepts x,y,w,h either as pixels or as 0..1 fractions of video size.
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

// defaultCenterROI returns a centered fallback ROI sized to show the effect clearly.
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

// clampROI keeps the ROI inside the frame and aligns values for H.264-friendly even dimensions.
func clampROI(r ROI, info VideoInfo) ROI {
	minW, minH := 32, 32

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

// evenInt rounds an integer down to the nearest valid even value.
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

// detectMotionROI extracts two frames and builds an ROI around changed pixels.
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

// extractFrame saves one RGB frame from the input at the requested timestamp.
func extractFrame(input string, sec float64, output string) error {
	args := []string{
		"-y",
		"-ss", fmt.Sprintf("%.3f", sec),
		"-i", input,
		"-frames:v", "1",
		"-vf", "format=rgb24",
		"-update", "1",
		output,
	}

	return runCommand("ffmpeg", args...)
}

// loadImage decodes a frame image written by FFmpeg.
func loadImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	return img, err
}

// motionBBox finds the bounding box of pixels whose grayscale difference exceeds thresh.
func motionBBox(a, b image.Image, thresh int) (ROI, bool) {
	ba, bb := a.Bounds(), b.Bounds()

	w := minInt(ba.Dx(), bb.Dx())
	h := minInt(ba.Dy(), bb.Dy())

	if w <= 0 || h <= 0 {
		return ROI{}, false
	}

	minX, minY := w, h
	maxX, maxY := 0, 0
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

// grayAt returns luma-like grayscale in the 0..255 range.
func grayAt(img image.Image, x, y int) int {
	r, g, b, _ := img.At(x, y).RGBA()

	rr := int(r >> 8)
	gg := int(g >> 8)
	bb := int(b >> 8)

	return (299*rr + 587*gg + 114*bb) / 1000
}

// expandROI grows an ROI by a margin fraction and clamps it back to the frame.
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

// renderBaseline creates a uniformly degraded reference video with the ROI outlined.
func renderBaseline(cfg Config, info VideoInfo, roi ROI, output string) error {
	filter := fmt.Sprintf(
		"scale=trunc(iw*%.4f/2)*2:trunc(ih*%.4f/2)*2,"+
			"scale=%d:%d:flags=bilinear,"+
			"boxblur=luma_radius=%d:luma_power=1:chroma_radius=%d:chroma_power=1,"+
			"drawbox=x=%d:y=%d:w=%d:h=%d:color=red@0.85:t=4,"+
			"format=yuv420p",
		cfg.PeripheryScale,
		cfg.PeripheryScale,
		info.Width,
		info.Height,
		cfg.BlurRadius,
		cfg.BlurRadius,
		roi.X,
		roi.Y,
		roi.W,
		roi.H,
	)

	args := []string{
		"-y",
		"-i", cfg.Input,
		"-vf", filter,
		"-an",
		"-c:v", "libx264",
		"-preset", cfg.Preset,
		"-crf", strconv.Itoa(cfg.CRF),
		"-movflags", "+faststart",
		output,
	}

	return runCommand("ffmpeg", args...)
}

// renderROIVideo degrades the whole frame, then overlays the original ROI crop.
func renderROIVideo(cfg Config, info VideoInfo, roi ROI, output string) error {
	filter := fmt.Sprintf(
		"[0:v]split=2[bgsrc][roisrc];"+
			"[bgsrc]scale=trunc(iw*%.4f/2)*2:trunc(ih*%.4f/2)*2,"+
			"scale=%d:%d:flags=bilinear,"+
			"boxblur=luma_radius=%d:luma_power=1:chroma_radius=%d:chroma_power=1[bg];"+
			"[roisrc]crop=%d:%d:%d:%d[roi];"+
			"[bg][roi]overlay=%d:%d,"+
			"drawbox=x=%d:y=%d:w=%d:h=%d:color=lime@0.95:t=4,"+
			"format=yuv420p[v]",
		cfg.PeripheryScale,
		cfg.PeripheryScale,
		info.Width,
		info.Height,
		cfg.BlurRadius,
		cfg.BlurRadius,
		roi.W,
		roi.H,
		roi.X,
		roi.Y,
		roi.X,
		roi.Y,
		roi.X,
		roi.Y,
		roi.W,
		roi.H,
	)

	args := []string{
		"-y",
		"-i", cfg.Input,
		"-filter_complex", filter,
		"-map", "[v]",
		"-an",
		"-c:v", "libx264",
		"-preset", cfg.Preset,
		"-crf", strconv.Itoa(cfg.CRF),
		"-movflags", "+faststart",
		output,
	}

	return runCommand("ffmpeg", args...)
}

// renderComparison stacks baseline and ROI videos side by side and overlays their size metrics.
func renderComparison(cfg Config, baseline, roiVideo string, baselineArtifact, roiArtifact Artifact, output string) error {
	leftLabel := drawTextEscape(comparisonLabel("Baseline", baselineArtifact))
	rightLabel := drawTextEscape(comparisonLabel("ROI", roiArtifact))
	filter := fmt.Sprintf(
		"[0:v][1:v]hstack=inputs=2,"+
			"drawtext=text='%s':x=32:y=32:fontsize=36:fontcolor=white:box=1:boxcolor=black@0.58:boxborderw=14,"+
			"drawtext=text='%s':x=(main_w/2)+32:y=32:fontsize=36:fontcolor=white:box=1:boxcolor=black@0.58:boxborderw=14,"+
			"format=yuv420p[v]",
		leftLabel,
		rightLabel,
	)

	args := []string{
		"-y",
		"-i", baseline,
		"-i", roiVideo,
		"-filter_complex", filter,
		"-map", "[v]",
		"-an",
		"-c:v", "libx264",
		"-preset", cfg.Preset,
		"-crf", strconv.Itoa(cfg.CRF),
		"-movflags", "+faststart",
		output,
	}

	return runCommand("ffmpeg", args...)
}

// comparisonLabel formats approximate output size metrics for the comparison overlay.
func comparisonLabel(name string, art Artifact) string {
	bitrate := "n/a"
	if art.BitrateKbs > 0 {
		bitrate = fmt.Sprintf("avg %.0f kbps", art.BitrateKbs)
	}

	return fmt.Sprintf("%s | %s | %s", name, bitrate, formatBytes(art.SizeBytes))
}

// drawTextEscape escapes generated text for FFmpeg drawtext filter arguments.
func drawTextEscape(s string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`:`, `\:`,
		`'`, `\'`,
		`%`, `\%`,
	)

	return replacer.Replace(s)
}

// formatBytes prints a compact approximate file size for on-video labels.
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

// renderPreview saves one frame with the selected ROI rectangle drawn over it.
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
		"-y",
		"-ss", fmt.Sprintf("%.3f", t),
		"-i", cfg.Input,
		"-frames:v", "1",
		"-vf", filter,
		"-update", "1",
		output,
	}

	return runCommand("ffmpeg", args...)
}

// runCommand streams an external command's output directly to the terminal.
func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %w", name, err)
	}

	return nil
}

// commandOutput runs an external command and returns stdout, preserving stderr for errors.
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

// serve exposes generated artifacts from dir over a local HTTP file server.
func serve(dir, addr string) error {
	fs := http.FileServer(http.Dir(dir))
	http.Handle("/", fs)

	fmt.Printf("Open: http://localhost%s/comparison_baseline_vs_roi.mp4\n", addr)

	return http.ListenAndServe(addr, nil)
}

// artifactFor builds report metadata for a generated file.
func artifactFor(path string, duration float64) Artifact {
	st, err := os.Stat(path)
	if err != nil {
		return Artifact{Path: path}
	}

	art := Artifact{
		Path:      path,
		SizeBytes: st.Size(),
	}

	if duration > 0 && strings.HasSuffix(strings.ToLower(path), ".mp4") {
		art.BitrateKbs = float64(st.Size()*8) / duration / 1000.0
	}

	return art
}

// minInt returns the smaller of two ints.
func minInt(a, b int) int {
	if a < b {
		return a
	}

	return b
}

// absInt returns the absolute value of an int.
func absInt(v int) int {
	if v < 0 {
		return -v
	}

	return v
}
