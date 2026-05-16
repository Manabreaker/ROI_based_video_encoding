package roi

import (
	"fmt"
	"math"
	"strings"
)

const hardwareH264ComparisonMaxWidth = 4096

// renderComparison creates the side-by-side input-vs-ROI video with text overlays and ROI boxes.
func renderComparison(
	cfg Config,
	baseline string,
	roiVideo string,
	output string,
	baselineSamples []BitrateSample,
	roiSamples []BitrateSample,
	info VideoInfo,
	selection ROISelection,
	baselineDecision EncodeDecision,
	roiDecision EncodeDecision,
) error {
	filter, scaled, err := buildComparisonFilterForSelection(cfg, baselineSamples, roiSamples, info, selection, baselineDecision, roiDecision)
	if err != nil {
		return err
	}
	if scaled {
		fmt.Printf("      note: scaling comparison to %d px width to fit hardware H.264 encoder limits; ROI output stays %dx%d\n",
			hardwareH264ComparisonMaxWidth,
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
	return buildComparisonFilterForSelection(
		cfg,
		baselineSamples,
		roiSamples,
		info,
		staticROISelection(roi),
		baselineDecision,
		roiDecision,
	)
}

func buildComparisonFilterForSelection(
	cfg Config,
	baselineSamples []BitrateSample,
	roiSamples []BitrateSample,
	info VideoInfo,
	selection ROISelection,
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

	boxes, err := comparisonDrawBoxesForSelection(cfg, info, selection, roiDecision)
	if err != nil {
		return "", false, err
	}

	scaled := shouldScaleComparisonForHardwareH264(cfg, info)
	chain := []string{"[left][right]hstack=inputs=2"}
	chain = append(chain, boxes...)
	if scaled {
		chain = append(chain, fmt.Sprintf("scale=w=%d:h=-2", hardwareH264ComparisonMaxWidth))
	}
	chain = append(chain, "format=yuv420p")

	return prefix + strings.Join(chain, ",") + "[v]", scaled, nil
}

func shouldScaleComparisonForHardwareH264(cfg Config, info VideoInfo) bool {
	return isHardwareVideoEncoder(cfg) && info.Width > 0 && info.Width*2 > hardwareH264ComparisonMaxWidth
}

func comparisonDrawBoxes(cfg Config, info VideoInfo, roi ROI, roiDecision EncodeDecision) ([]string, error) {
	return comparisonDrawBoxesForSelection(cfg, info, staticROISelection(roi), roiDecision)
}

func comparisonDrawBoxesForSelection(cfg Config, info VideoInfo, selection ROISelection, roiDecision EncodeDecision) ([]string, error) {
	if usesROIBlockMap(cfg) {
		left, err := roiBlockDrawBoxes(cfg, info, 0)
		if err != nil {
			return nil, err
		}
		right, err := roiBlockDrawBoxes(cfg, info, info.Width)
		if err != nil {
			return nil, err
		}
		return append(left, right...), nil
	}

	timeline := normalizedROITimeline(selection, info)
	if len(timeline) > 1 {
		boxes := make([]string, 0, len(timeline)*6)
		for _, item := range timeline {
			enable := timedROIEnable(item)
			boxes = append(boxes, zoneDrawBoxes(cfg, info, item.ROI, 0, 5, 5, 4, enable)...)
			boxes = append(boxes, zoneDrawBoxes(cfg, info, item.ROI, info.Width, 5, 5, 4, enable)...)
		}
		return boxes, nil
	}

	left := staticZoneDrawBoxes(cfg, info, selection.ROI, 0, 5, 5, 4)
	right := staticZoneDrawBoxes(cfg, info, selection.ROI, info.Width, 5, 5, 4)
	return append(left, right...), nil
}

func roiBlockDrawBoxes(cfg Config, info VideoInfo, xOffset int) ([]string, error) {
	rects, err := qpMapBlockRects(cfg, info)
	if err != nil {
		return nil, err
	}
	rects = mergeQPMapBlockRectsForDisplay(rects)

	boxes := make([]string, 0, len(rects))
	for _, r := range rects {
		boxes = append(boxes, fmt.Sprintf(
			"drawbox=x=%d:y=%d:w=%d:h=%d:color=%s:t=3",
			xOffset+r.X,
			r.Y,
			r.W,
			r.H,
			roiBlockBoxColor(r.QOffset),
		))
	}
	return boxes, nil
}

func roiBlockBoxColor(qoffset float64) string {
	switch {
	case qOffsetColorMatch(qoffset, -0.40):
		return "lime@0.95"
	case qOffsetColorMatch(qoffset, -0.25):
		return "orange@0.95"
	case qOffsetColorMatch(qoffset, -0.10):
		return "yellow@0.95"
	case qOffsetColorMatch(qoffset, 0.15):
		return "red@0.95"
	case qoffset <= -0.35:
		return "lime@0.95"
	case qoffset <= -0.20:
		return "orange@0.95"
	case qoffset < 0:
		return "yellow@0.95"
	case qoffset > 0:
		return "red@0.95"
	default:
		return "white@0.70"
	}
}

func qOffsetColorMatch(got float64, want float64) bool {
	return math.Abs(got-want) < 0.0000001
}

func staticZoneDrawBoxes(cfg Config, info VideoInfo, roi ROI, xOffset int, redThickness int, middleThickness int, roiThickness int) []string {
	return zoneDrawBoxes(cfg, info, roi, xOffset, redThickness, middleThickness, roiThickness, "")
}

func zoneDrawBoxes(cfg Config, info VideoInfo, roi ROI, xOffset int, redThickness int, middleThickness int, roiThickness int, enableExpr string) []string {
	boxes := make([]string, 0, 3)
	if roiControl(cfg) != "qp-map" {
		boxes = append(boxes, drawBoxFilter(
			xOffset,
			0,
			info.Width,
			info.Height,
			"red@0.90",
			redThickness,
			enableExpr,
		))
	}
	if shouldDrawMiddleZone(cfg) {
		middle := middleROI(cfg, roi, info)
		boxes = append(boxes, drawBoxFilter(
			xOffset+middle.X,
			middle.Y,
			middle.W,
			middle.H,
			"orange@0.95",
			middleThickness,
			enableExpr,
		))
	}
	boxes = append(boxes, drawBoxFilter(
		xOffset+roi.X,
		roi.Y,
		roi.W,
		roi.H,
		"lime@0.90",
		roiThickness,
		enableExpr,
	))
	return boxes
}

func drawBoxFilter(x int, y int, w int, h int, color string, thickness int, enableExpr string) string {
	filter := fmt.Sprintf(
		"drawbox=x=%d:y=%d:w=%d:h=%d:color=%s:t=%d",
		x,
		y,
		w,
		h,
		color,
		thickness,
	)
	if enableExpr != "" {
		filter += ":enable='" + enableExpr + "'"
	}
	return filter
}

func timedROIEnable(item TimedROI) string {
	return fmt.Sprintf("between(t\\,%.3f\\,%.3f)", item.StartSeconds, item.EndSeconds)
}

func shouldDrawMiddleZone(cfg Config) bool {
	return roiControl(cfg) != "qp-map" || cfg.ROIMiddleQOffset != 0
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
			if decision.ROIBlockCount > 0 {
				line4 = fmt.Sprintf("QP blocks %d | %d px", decision.ROIBlockCount, decision.ROIBlockSize)
			} else {
				line4 = fmt.Sprintf("ROI qoffset %.2f | MID %.2f", decision.ROIQOffset, decision.MiddleQOffset)
			}
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
	return renderPreviewForSelection(cfg, info, staticROISelection(roi), output)
}

func renderPreviewForSelection(cfg Config, info VideoInfo, selection ROISelection, output string) error {
	t := 0.0
	if info.Duration > 0 {
		t = math.Min(info.Duration*0.25, math.Max(0.0, info.Duration-0.1))
	}

	var boxes []string
	if usesROIBlockMap(cfg) {
		blockBoxes, err := roiBlockDrawBoxes(cfg, info, 0)
		if err != nil {
			return err
		}
		boxes = append(boxes, blockBoxes...)
	} else {
		boxes = append(boxes, staticZoneDrawBoxes(cfg, info, roiAtTime(selection, t, info), 0, 6, 6, 6)...)
	}
	boxes = append(boxes, "format=rgb24")

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
