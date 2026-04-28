package roi

import (
	"fmt"
	"math"
	"strings"
)

const nvencH264MaxWidth = 4096

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
	filter, scaled, err := buildComparisonFilter(cfg, baselineSamples, roiSamples, info, roi, baselineDecision, roiDecision)
	if err != nil {
		return err
	}
	if scaled {
		fmt.Printf("      note: scaling comparison to %d px width to fit h264_nvenc H.264 width limit; ROI output stays %dx%d\n",
			nvencH264MaxWidth,
			info.Width,
			info.Height,
		)
	}

	args := []string{
		"-hide_banner",
		"-y",
		"-i", baseline,
		"-i", roiVideo,
		"-filter_complex", filter,
		"-map", "[v]",
		"-an",
		"-pix_fmt", "yuv420p",
	}
	args = append(args, qualityEncoderArgs(cfg, 18)...)
	args = append(args, "-movflags", "+faststart", output)

	return runCommand("ffmpeg", args...)
}

func buildComparisonFilter(
	cfg Config,
	baselineSamples []BitrateSample,
	roiSamples []BitrateSample,
	info VideoInfo,
	roi ROI,
	baselineDecision EncodeDecision,
	roiDecision EncodeDecision,
) (string, bool, error) {
	var prefix string

	if cfg.OverlayBitrate {
		if len(baselineSamples) > cfg.MaxBitrateOverlays || len(roiSamples) > cfg.MaxBitrateOverlays {
			return "", false, fmt.Errorf(
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

	middle := middleROI(cfg, roi, info)

	leftROIBox := fmt.Sprintf(
		"drawbox=x=%d:y=%d:w=%d:h=%d:color=lime@0.90:t=4",
		roi.X,
		roi.Y,
		roi.W,
		roi.H,
	)

	rightMiddleBox := fmt.Sprintf(
		"drawbox=x=%d:y=%d:w=%d:h=%d:color=orange@0.95:t=5",
		info.Width+middle.X,
		middle.Y,
		middle.W,
		middle.H,
	)

	rightROIBox := fmt.Sprintf(
		"drawbox=x=%d:y=%d:w=%d:h=%d:color=lime@0.90:t=4",
		info.Width+roi.X,
		roi.Y,
		roi.W,
		roi.H,
	)

	boxes := []string{leftROIBox}
	if roiDecision.ROIControl != "qp-map" {
		boxes = append(boxes, fmt.Sprintf(
			"drawbox=x=%d:y=0:w=%d:h=%d:color=red@0.90:t=5",
			info.Width,
			info.Width,
			info.Height,
		))
	}
	boxes = append(boxes, rightMiddleBox, rightROIBox)

	scaled := shouldScaleComparisonForNVENC(cfg, info)
	chain := []string{"[left][right]hstack=inputs=2"}
	chain = append(chain, boxes...)
	if scaled {
		chain = append(chain, fmt.Sprintf("scale=w=%d:h=-2", nvencH264MaxWidth))
	}
	chain = append(chain, "format=yuv420p")

	return prefix + strings.Join(chain, ",") + "[v]", scaled, nil
}

func shouldScaleComparisonForNVENC(cfg Config, info VideoInfo) bool {
	return isNVENC(cfg) && info.Width > 0 && info.Width*2 > nvencH264MaxWidth
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
		if decision.ROIControl == "qp-map" {
			line3 = fmt.Sprintf("%s | %s QP-map", status, strings.ToUpper(strings.TrimSpace(decision.RateControl)))
			line4 = fmt.Sprintf("ROI qoffset %.2f | MID %.2f", decision.ROIQOffset, decision.MiddleQOffset)
			return []string{
				drawTextFilter(title, 24, 24, 28, "white", "black@0.65", ""),
				drawTextFilter(line2, 24, 64, 22, "white", "black@0.65", ""),
				drawTextFilter(line3, 24, 98, 21, "white", "black@0.65", ""),
				drawTextFilter(line4, 24, 132, 21, "white", "black@0.65", ""),
			}
		}

		rateControl := strings.ToUpper(strings.TrimSpace(decision.RateControl))
		middleScale := decision.MiddleScale
		if middleScale <= 0 {
			middleScale = 1
		}
		if rateControl == "" || rateControl == "CRF" {
			line4 = fmt.Sprintf("G ROI | O %.2f b%d | R %.2f b%d", middleScale, decision.MiddleBlur, decision.Scale, decision.Blur)
		} else {
			line3 = fmt.Sprintf("%s | %s target-rate", status, rateControl)
			line4 = fmt.Sprintf("G ROI | O %.2f b%d | R %.2f b%d", middleScale, decision.MiddleBlur, decision.Scale, decision.Blur)
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

	middle := middleROI(cfg, roi, info)
	var boxes []string
	if roiControl(cfg) != "qp-map" {
		boxes = append(boxes, fmt.Sprintf("drawbox=x=0:y=0:w=%d:h=%d:color=red@0.90:t=6", info.Width, info.Height))
	}
	boxes = append(
		boxes,
		fmt.Sprintf("drawbox=x=%d:y=%d:w=%d:h=%d:color=orange@0.95:t=6", middle.X, middle.Y, middle.W, middle.H),
		fmt.Sprintf("drawbox=x=%d:y=%d:w=%d:h=%d:color=lime@0.95:t=6", roi.X, roi.Y, roi.W, roi.H),
		"format=rgb24",
	)

	filter := strings.Join(boxes, ",")

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
