package roi

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// fitROIToTarget tries periphery settings and writes the ROI output that best matches the target.
func fitROIToTarget(cfg Config, info VideoInfo, selection ROISelection, targetKbps float64, output string, workDir string) (EncodeDecision, error) {
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return EncodeDecision{}, err
	}

	roi := selection.ROI
	rateControl := roiRateControl(cfg)
	if roiControl(cfg) == "qp-map" {
		return renderROIQPMapToTarget(cfg, info, selection, targetKbps, output, workDir, rateControl)
	}

	if !cfg.FitROI {
		path := filepath.Join(workDir, fmt.Sprintf("roi_manual_%s.mp4", rateControl))
		if err := renderROICandidate(cfg, info, selection, path, cfg.ROIHighQualityCRF, cfg.ManualPeripheryScale, cfg.ManualBlurRadius, targetKbps); err != nil {
			return EncodeDecision{}, err
		}

		actual, err := measuredAverageBitrateKbps(path)
		if err != nil {
			return EncodeDecision{}, err
		}

		c := Candidate{
			Kind:        roiCandidateKind(rateControl),
			Encoder:     cfg.VideoEncoder,
			CRF:         cfg.ROIHighQualityCRF,
			RateControl: rateControl,
			Scale:       cfg.ManualPeripheryScale,
			Blur:        cfg.ManualBlurRadius,
			Kbps:        actual,
			Path:        path,
		}
		c.MiddleScale, c.MiddleBlur = middleQualitySettings(cfg, c.Scale, c.Blur)
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
			Encoder:         cfg.VideoEncoder,
			TargetKbps:      targetKbps,
			ActualKbps:      actual,
			WithinTolerance: withinTolerance(actual, targetKbps, cfg.Tolerance),
			CRF:             cfg.ROIHighQualityCRF,
			RateControl:     rateControl,
			Scale:           cfg.ManualPeripheryScale,
			Blur:            cfg.ManualBlurRadius,
			MiddleScale:     c.MiddleScale,
			MiddleBlur:      c.MiddleBlur,
			MiddleMargin:    cfg.MiddleMargin,
			ROIYPSNR:        c.ROIYPSNR,
			Note:            fmt.Sprintf("manual ROI periphery settings; %s rate control", rateControl),
			Candidates:      []CandidateSummary{candidateSummary(c)},
		}, nil
	}

	settings := peripheryCandidates(cfg)

	candidates, err := fitPeripheryCandidatesInterpolated(cfg, info, selection, targetKbps, workDir, settings, rateControl)
	if err != nil {
		return EncodeDecision{}, err
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

				if err := renderROICandidateCRFForSelection(cfg, info, selection, path, crf, 1.0, 0); err != nil {
					return EncodeDecision{}, err
				}

				kbps, err := measuredAverageBitrateKbps(path)
				if err != nil {
					return EncodeDecision{}, err
				}

				c := Candidate{
					Kind:        "roi-full-detail-lower-crf",
					Encoder:     cfg.VideoEncoder,
					CRF:         crf,
					RateControl: "crf",
					Scale:       1.0,
					Blur:        0,
					Kbps:        kbps,
					Path:        path,
					Note:        "content is simple; tried lower CRF to use more of the requested budget without degrading periphery",
				}
				c.MiddleScale, c.MiddleBlur = middleQualitySettings(cfg, c.Scale, c.Blur)
				candidates = append(candidates, c)

				fmt.Printf("      ROI full-detail candidate CRF %2d, scale 1.00, blur 0 -> %.1f kbps\n", crf, kbps)
			}

			best = chooseHighestUnderTargetOrMinimum(candidates, targetKbps, cfg.Tolerance)
		}

		if best.Kbps > targetKbps*(1+cfg.Tolerance) && cfg.AllowROIQualityLoss {
			worst := chooseLowestBitrateCandidate(candidates)

			emergencyCandidates, err := fitROIEmergencyCRF(cfg, info, selection, targetKbps, workDir, worst.Scale, worst.Blur)
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
		if len(selection.Timeline) > 1 {
			note += fmt.Sprintf("; moving ROI is applied as %d sampled time segments", len(selection.Timeline))
		}
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
		Encoder:         best.Encoder,
		TargetKbps:      targetKbps,
		ActualKbps:      best.Kbps,
		WithinTolerance: withinTolerance(best.Kbps, targetKbps, cfg.Tolerance),
		CRF:             best.CRF,
		RateControl:     best.RateControl,
		Scale:           best.Scale,
		Blur:            best.Blur,
		MiddleScale:     best.MiddleScale,
		MiddleBlur:      best.MiddleBlur,
		MiddleMargin:    cfg.MiddleMargin,
		ROIYPSNR:        best.ROIYPSNR,
		Note:            note,
		Candidates:      candidateSummaries(candidates),
	}, nil
}

func renderROIQPMapToTarget(cfg Config, info VideoInfo, selection ROISelection, targetKbps float64, output string, workDir string, rateControl string) (EncodeDecision, error) {
	roi := selection.ROI
	path := filepath.Join(workDir, fmt.Sprintf("roi_qp_map_%s.mp4", rateControl))
	blockCount := countROIBlockCells(cfg.ROIBlocks)
	blockSize := 0
	if blockCount > 0 {
		blockSize = normalizedROIBlockSize(cfg)
	}

	if err := renderROIQPMapCandidate(cfg, info, selection, path, cfg.ROIHighQualityCRF, targetKbps); err != nil {
		return EncodeDecision{}, err
	}

	actual, err := measuredAverageBitrateKbps(path)
	if err != nil {
		return EncodeDecision{}, err
	}

	c := Candidate{
		Kind:          qpMapCandidateKind(rateControl),
		Encoder:       cfg.VideoEncoder,
		ROIControl:    "qp-map",
		CRF:           cfg.ROIHighQualityCRF,
		RateControl:   rateControl,
		ROIQOffset:    cfg.ROIQOffset,
		MiddleQOffset: cfg.ROIMiddleQOffset,
		ROIBlockSize:  blockSize,
		ROIBlockCount: blockCount,
		Kbps:          actual,
		Path:          path,
		Note:          "encoder-level ROI side data via FFmpeg addroi",
	}
	if cfg.ROIFitMetric {
		if err := attachROIPSNRMetric(cfg, roi, &c, filepath.Join(workDir, "roi_qp_map_psnr.log")); err != nil {
			fmt.Printf("      warning: ROI candidate metric failed: %v\n", err)
		}
	}

	if blockCount > 0 && c.ROIYPSNR > 0 {
		fmt.Printf("      ROI QP-map candidate %s/%s, CRF %2d, %d blocks at %dpx grid -> %.1f kbps, ROI PSNR-Y %.2f dB\n",
			c.Encoder,
			c.RateControl,
			c.CRF,
			c.ROIBlockCount,
			c.ROIBlockSize,
			c.Kbps,
			c.ROIYPSNR,
		)
	} else if blockCount > 0 {
		fmt.Printf("      ROI QP-map candidate %s/%s, CRF %2d, %d blocks at %dpx grid -> %.1f kbps\n",
			c.Encoder,
			c.RateControl,
			c.CRF,
			c.ROIBlockCount,
			c.ROIBlockSize,
			c.Kbps,
		)
	} else if c.ROIYPSNR > 0 {
		fmt.Printf("      ROI QP-map candidate %s/%s, CRF %2d, middle qoffset %.3f, ROI qoffset %.3f -> %.1f kbps, ROI PSNR-Y %.2f dB\n",
			c.Encoder,
			c.RateControl,
			c.CRF,
			c.MiddleQOffset,
			c.ROIQOffset,
			c.Kbps,
			c.ROIYPSNR,
		)
	} else {
		fmt.Printf("      ROI QP-map candidate %s/%s, CRF %2d, middle qoffset %.3f, ROI qoffset %.3f -> %.1f kbps\n",
			c.Encoder,
			c.RateControl,
			c.CRF,
			c.MiddleQOffset,
			c.ROIQOffset,
			c.Kbps,
		)
	}

	if err := copyFile(path, output); err != nil {
		return EncodeDecision{}, err
	}

	note := "encoder-level ROI QP map via FFmpeg addroi side data; pixels are not preprocessed before encoding"
	if len(selection.Timeline) > 1 {
		note += fmt.Sprintf("; moving ROI is applied as %d sampled time segments", len(selection.Timeline))
	}
	if blockCount > 0 {
		note += fmt.Sprintf("; block map uses %d configured cells at %d px grid and overrides roi-qoffset/middle-qoffset rectangles", blockCount, blockSize)
	}
	switch normalizeVideoEncoder(cfg.VideoEncoder) {
	case encoderNVENC:
		note += "; spatial AQ is enabled for NVENC ROI handling"
	case encoderX264:
		note += "; x264 AQ is enabled because libx264 requires adaptive quantization for ROI side data"
	default:
		note += fmt.Sprintf("; %s hardware encoding is enabled, but ROI side-data handling is encoder-dependent", cfg.VideoEncoder)
	}
	if !withinTolerance(actual, targetKbps, cfg.Tolerance) {
		note += "; measured bitrate is outside tolerance"
	}

	return EncodeDecision{
		Name:            "roi",
		Encoder:         cfg.VideoEncoder,
		ROIControl:      "qp-map",
		TargetKbps:      targetKbps,
		ActualKbps:      actual,
		WithinTolerance: withinTolerance(actual, targetKbps, cfg.Tolerance),
		CRF:             cfg.ROIHighQualityCRF,
		RateControl:     rateControl,
		ROIQOffset:      cfg.ROIQOffset,
		MiddleQOffset:   cfg.ROIMiddleQOffset,
		ROIBlockSize:    blockSize,
		ROIBlockCount:   blockCount,
		ROIYPSNR:        c.ROIYPSNR,
		Note:            note,
		Candidates:      []CandidateSummary{candidateSummary(c)},
	}, nil
}

// fitPeripheryCandidatesInterpolated probes the ordered periphery ladder using bitrate interpolation.
func fitPeripheryCandidatesInterpolated(
	cfg Config,
	info VideoInfo,
	selection ROISelection,
	targetKbps float64,
	workDir string,
	settings []peripherySetting,
	rateControl string,
) ([]Candidate, error) {
	eval := func(idx int, s peripherySetting) (Candidate, error) {
		return evaluatePeripheryCandidate(cfg, info, selection, targetKbps, workDir, settings, rateControl, idx, s)
	}

	return searchPeripheryCandidatesInterpolated(
		settings,
		targetKbps,
		cfg.Tolerance,
		cfg.FitIterations,
		cfg.ManualPeripheryScale,
		cfg.ManualBlurRadius,
		eval,
	)
}

func evaluatePeripheryCandidate(
	cfg Config,
	info VideoInfo,
	selection ROISelection,
	targetKbps float64,
	workDir string,
	settings []peripherySetting,
	rateControl string,
	idx int,
	s peripherySetting,
) (Candidate, error) {
	path := filepath.Join(workDir, fmt.Sprintf("roi_%s_crf_%02d_candidate_%02d_scale_%.2f_blur_%02d.mp4",
		rateControl,
		cfg.ROIHighQualityCRF,
		idx,
		s.Scale,
		s.Blur,
	))

	if err := renderROICandidate(cfg, info, selection, path, cfg.ROIHighQualityCRF, s.Scale, s.Blur, targetKbps); err != nil {
		return Candidate{}, err
	}

	kbps, err := measuredAverageBitrateKbps(path)
	if err != nil {
		return Candidate{}, err
	}

	c := Candidate{
		Kind:        roiCandidateKind(rateControl),
		Encoder:     cfg.VideoEncoder,
		CRF:         cfg.ROIHighQualityCRF,
		RateControl: rateControl,
		Scale:       s.Scale,
		Blur:        s.Blur,
		Kbps:        kbps,
		Path:        path,
	}
	c.MiddleScale, c.MiddleBlur = middleQualitySettings(cfg, c.Scale, c.Blur)

	if cfg.ROIFitMetric {
		logPath := filepath.Join(workDir, fmt.Sprintf("roi_candidate_%02d_psnr.log", idx))
		if err := attachROIPSNRMetric(cfg, selection.ROI, &c, logPath); err != nil {
			fmt.Printf("      warning: ROI candidate metric failed: %v\n", err)
		}
	}

	logROICandidate(c, idx, len(settings))

	return c, nil
}

func logROICandidate(c Candidate, idx int, total int) {
	if c.ROIYPSNR > 0 {
		fmt.Printf("      ROI interpolation candidate %02d/%02d %s/%s, CRF %2d, middle %.2f/%d, low %.2f/%d -> %.1f kbps, ROI PSNR-Y %.2f dB\n",
			idx+1,
			total,
			c.Encoder,
			c.RateControl,
			c.CRF,
			c.MiddleScale,
			c.MiddleBlur,
			c.Scale,
			c.Blur,
			c.Kbps,
			c.ROIYPSNR,
		)
		return
	}

	fmt.Printf("      ROI interpolation candidate %02d/%02d %s/%s, CRF %2d, middle %.2f/%d, low %.2f/%d -> %.1f kbps\n",
		idx+1,
		total,
		c.Encoder,
		c.RateControl,
		c.CRF,
		c.MiddleScale,
		c.MiddleBlur,
		c.Scale,
		c.Blur,
		c.Kbps,
	)
}

// fitROIEmergencyCRF raises ROI CRF only when preserving high-quality ROI cannot reach the target.
func fitROIEmergencyCRF(cfg Config, info VideoInfo, selection ROISelection, targetKbps float64, workDir string, scale float64, blur int) ([]Candidate, error) {
	cache := map[int]Candidate{}

	eval := func(crf int) (Candidate, error) {
		if c, ok := cache[crf]; ok {
			return c, nil
		}

		path := filepath.Join(workDir, fmt.Sprintf("roi_emergency_crf_%02d_scale_%.2f_blur_%02d.mp4", crf, scale, blur))

		if err := renderROICandidateCRFForSelection(cfg, info, selection, path, crf, scale, blur); err != nil {
			return Candidate{}, err
		}

		kbps, err := measuredAverageBitrateKbps(path)
		if err != nil {
			return Candidate{}, err
		}

		c := Candidate{
			Kind:        "roi-emergency-quality-loss",
			Encoder:     cfg.VideoEncoder,
			CRF:         crf,
			RateControl: "crf",
			Scale:       scale,
			Blur:        blur,
			Kbps:        kbps,
			Path:        path,
			Note:        "ROI CRF increased to hit target; high-quality ROI no longer strictly preserved",
		}
		c.MiddleScale, c.MiddleBlur = middleQualitySettings(cfg, c.Scale, c.Blur)
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
func renderROICandidate(cfg Config, info VideoInfo, selection ROISelection, output string, crf int, scale float64, blur int, targetKbps float64) error {
	if roiRateControl(cfg) == "abr" {
		return renderROICandidateABRForSelection(cfg, info, selection, output, scale, blur, targetKbps)
	}
	return renderROICandidateCRFForSelection(cfg, info, selection, output, crf, scale, blur)
}

func renderROIQPMapCandidate(cfg Config, info VideoInfo, selection ROISelection, output string, crf int, targetKbps float64) error {
	if roiRateControl(cfg) == "abr" {
		return renderROIQPMapCandidateABRForSelection(cfg, info, selection, output, targetKbps)
	}
	return renderROIQPMapCandidateCRFForSelection(cfg, info, selection, output, crf)
}

func renderROIQPMapCandidateCRF(cfg Config, info VideoInfo, roi ROI, output string, crf int) error {
	return renderROIQPMapCandidateCRFForSelection(cfg, info, staticROISelection(roi), output, crf)
}

func renderROIQPMapCandidateCRFForSelection(cfg Config, info VideoInfo, selection ROISelection, output string, crf int) error {
	filter, err := buildROIQPMapFilterForSelection(cfg, info, selection)
	if err != nil {
		return err
	}

	args := []string{
		"-hide_banner",
		"-y",
		"-i", cfg.Input,
		"-filter_complex", filter,
		"-map", "[v]",
		"-an",
		"-pix_fmt", "yuv420p",
	}
	args = append(args, qpMapQualityEncoderArgs(cfg, crf)...)
	args = append(args, "-movflags", "+faststart", output)

	return runCommand("ffmpeg", args...)
}

func renderROIQPMapCandidateABR(cfg Config, info VideoInfo, roi ROI, output string, targetKbps float64) error {
	return renderROIQPMapCandidateABRForSelection(cfg, info, staticROISelection(roi), output, targetKbps)
}

func renderROIQPMapCandidateABRForSelection(cfg Config, info VideoInfo, selection ROISelection, output string, targetKbps float64) error {
	if targetKbps <= 0 {
		return errors.New("targetKbps must be greater than zero for ROI QP-map ABR encoding")
	}

	filter, err := buildROIQPMapFilterForSelection(cfg, info, selection)
	if err != nil {
		return err
	}
	bitrate, maxrate, bufsize := roiRateArgs(cfg, targetKbps)

	baseArgs := []string{
		"-hide_banner",
		"-y",
		"-i", cfg.Input,
		"-filter_complex", filter,
		"-map", "[v]",
		"-an",
		"-pix_fmt", "yuv420p",
	}
	baseArgs = append(baseArgs, qpMapBitrateEncoderArgs(cfg, bitrate, maxrate, bufsize)...)

	if gop := gopSize(info); gop > 0 {
		baseArgs = append(baseArgs, "-g", strconv.Itoa(gop))
	}

	if !cfg.ROITwoPass || isHardwareVideoEncoder(cfg) {
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

// renderROICandidateCRF encodes an ROI candidate with fixed quality settings.
func renderROICandidateCRF(cfg Config, info VideoInfo, roi ROI, output string, crf int, scale float64, blur int) error {
	return renderROICandidateCRFForSelection(cfg, info, staticROISelection(roi), output, crf, scale, blur)
}

func renderROICandidateCRFForSelection(cfg Config, info VideoInfo, selection ROISelection, output string, crf int, scale float64, blur int) error {
	filter := buildROIFilterForSelection(cfg, info, selection, scale, blur)

	args := []string{
		"-hide_banner",
		"-y",
		"-i", cfg.Input,
		"-filter_complex", filter,
		"-map", "[v]",
		"-an",
		"-pix_fmt", "yuv420p",
	}
	args = append(args, qualityEncoderArgs(cfg, crf)...)
	args = append(args, "-movflags", "+faststart", output)

	return runCommand("ffmpeg", args...)
}

// renderROICandidateABR encodes an ROI candidate around the requested average bitrate.
func renderROICandidateABR(cfg Config, info VideoInfo, roi ROI, output string, scale float64, blur int, targetKbps float64) error {
	return renderROICandidateABRForSelection(cfg, info, staticROISelection(roi), output, scale, blur, targetKbps)
}

func renderROICandidateABRForSelection(cfg Config, info VideoInfo, selection ROISelection, output string, scale float64, blur int, targetKbps float64) error {
	if targetKbps <= 0 {
		return errors.New("targetKbps must be greater than zero for ROI ABR encoding")
	}

	filter := buildROIFilterForSelection(cfg, info, selection, scale, blur)
	bitrate, maxrate, bufsize := roiRateArgs(cfg, targetKbps)

	baseArgs := []string{
		"-hide_banner",
		"-y",
		"-i", cfg.Input,
		"-filter_complex", filter,
		"-map", "[v]",
		"-an",
		"-pix_fmt", "yuv420p",
	}
	baseArgs = append(baseArgs, bitrateEncoderArgs(cfg, bitrate, maxrate, bufsize)...)

	if gop := gopSize(info); gop > 0 {
		baseArgs = append(baseArgs, "-g", strconv.Itoa(gop))
	}

	if !cfg.ROITwoPass || isHardwareVideoEncoder(cfg) {
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

// buildROIQPMapFilter attaches encoder-level ROI side data without changing pixels.
func buildROIQPMapFilter(cfg Config, info VideoInfo, roi ROI) (string, error) {
	return buildROIQPMapFilterForSelection(cfg, info, staticROISelection(roi))
}

func buildROIQPMapFilterForSelection(cfg Config, info VideoInfo, selection ROISelection) (string, error) {
	if usesROIBlockMap(cfg) {
		return buildROIBlockQPMapFilter(cfg, info)
	}

	timeline := normalizedROITimeline(selection, info)
	if len(timeline) > 1 {
		return buildDynamicROIQPMapFilter(cfg, info, timeline), nil
	}

	parts := roiQPMapFilters(cfg, info, selection.ROI)
	parts = append(parts, "format=yuv420p[v]")

	return strings.Join(parts, ","), nil
}

func roiQPMapFilters(cfg Config, info VideoInfo, roi ROI) []string {
	middle := middleROI(cfg, roi, info)

	var parts []string
	if cfg.ROIMiddleQOffset != 0 {
		parts = append(parts, fmt.Sprintf(
			"addroi=x=%d:y=%d:w=%d:h=%d:qoffset=%.4f:clear=1",
			middle.X,
			middle.Y,
			middle.W,
			middle.H,
			cfg.ROIMiddleQOffset,
		))
		parts = append(parts, fmt.Sprintf(
			"addroi=x=%d:y=%d:w=%d:h=%d:qoffset=%.4f",
			roi.X,
			roi.Y,
			roi.W,
			roi.H,
			cfg.ROIQOffset,
		))
	} else {
		parts = append(parts, fmt.Sprintf(
			"addroi=x=%d:y=%d:w=%d:h=%d:qoffset=%.4f:clear=1",
			roi.X,
			roi.Y,
			roi.W,
			roi.H,
			cfg.ROIQOffset,
		))
	}

	return parts
}

func buildDynamicROIQPMapFilter(cfg Config, info VideoInfo, timeline []TimedROI) string {
	chains := make([]string, 0, len(timeline)*2+1)
	labels := make([]string, 0, len(timeline))

	for i, item := range timeline {
		sourceLabel := fmt.Sprintf("[track_qp_src_%d]", i)
		outputLabel := fmt.Sprintf("[track_qp_seg_%d]", i)

		chains = append(chains, trimSegmentChain(item, sourceLabel))

		parts := roiQPMapFilters(cfg, info, item.ROI)
		parts = append(parts, "format=yuv420p")
		chains = append(chains, sourceLabel+strings.Join(parts, ",")+outputLabel)
		labels = append(labels, outputLabel)
	}

	chains = append(chains, strings.Join(labels, "")+fmt.Sprintf("concat=n=%d:v=1:a=0,format=yuv420p[v]", len(labels)))
	return strings.Join(chains, ";")
}

func buildROIBlockQPMapFilter(cfg Config, info VideoInfo) (string, error) {
	rects, err := qpMapBlockRects(cfg, info)
	if err != nil {
		return "", err
	}
	if len(rects) == 0 {
		return "", errors.New("roi-blocks requires at least one block")
	}

	parts := make([]string, 0, len(rects)+1)
	for i, r := range rects {
		clear := ""
		if i == 0 {
			clear = ":clear=1"
		}
		parts = append(parts, fmt.Sprintf(
			"addroi=x=%d:y=%d:w=%d:h=%d:qoffset=%.4f%s",
			r.X,
			r.Y,
			r.W,
			r.H,
			r.QOffset,
			clear,
		))
	}
	parts = append(parts, "format=yuv420p[v]")

	return strings.Join(parts, ","), nil
}

// buildROIFilter creates low, middle, and original-ROI layers before final encoding.
func buildROIFilter(cfg Config, info VideoInfo, roi ROI, scale float64, blur int) string {
	return buildROIFilterForSelection(cfg, info, staticROISelection(roi), scale, blur)
}

func buildROIFilterForSelection(cfg Config, info VideoInfo, selection ROISelection, scale float64, blur int) string {
	timeline := normalizedROITimeline(selection, info)
	if len(timeline) > 1 {
		return buildDynamicROIFilter(cfg, info, timeline, scale, blur)
	}

	return buildStaticROIFilterChain("[0:v]", "v", "", cfg, info, selection.ROI, scale, blur)
}

func buildDynamicROIFilter(cfg Config, info VideoInfo, timeline []TimedROI, scale float64, blur int) string {
	chains := make([]string, 0, len(timeline)*2+1)
	labels := make([]string, 0, len(timeline))

	for i, item := range timeline {
		sourceLabel := fmt.Sprintf("[track_mask_src_%d]", i)
		outputName := fmt.Sprintf("track_mask_seg_%d", i)
		outputLabel := "[" + outputName + "]"

		chains = append(chains, trimSegmentChain(item, sourceLabel))
		chains = append(chains, buildStaticROIFilterChain(sourceLabel, outputName, fmt.Sprintf("track_mask_%d_", i), cfg, info, item.ROI, scale, blur))
		labels = append(labels, outputLabel)
	}

	chains = append(chains, strings.Join(labels, "")+fmt.Sprintf("concat=n=%d:v=1:a=0,format=yuv420p[v]", len(labels)))
	return strings.Join(chains, ";")
}

func trimSegmentChain(item TimedROI, outputLabel string) string {
	return fmt.Sprintf(
		"[0:v]trim=start=%.3f:end=%.3f,setpts=PTS-STARTPTS%s",
		item.StartSeconds,
		item.EndSeconds,
		outputLabel,
	)
}

func buildStaticROIFilterChain(inputLabel string, outputName string, prefix string, cfg Config, info VideoInfo, roi ROI, scale float64, blur int) string {
	middle := middleROI(cfg, roi, info)
	middleScale, middleBlur := middleQualitySettings(cfg, scale, blur)
	lowFilter := buildPeripheryFilter(info, scale, blur)
	middleFilter := buildPeripheryFilter(info, middleScale, middleBlur)
	lowSrc := filterLabel(prefix + "lowsrc")
	middleSrc := filterLabel(prefix + "middlesrc")
	roiSrc := filterLabel(prefix + "roisrc")
	low := filterLabel(prefix + "low")
	mid := filterLabel(prefix + "mid")
	roiLabel := filterLabel(prefix + "roi")
	withMid := filterLabel(prefix + "withmid")
	outputLabel := filterLabel(outputName)

	return fmt.Sprintf(
		"%ssplit=3%s%s%s;"+
			"%s%s%s;"+
			"%s%s,crop=%d:%d:%d:%d%s;"+
			"%scrop=%d:%d:%d:%d,format=yuv420p%s;"+
			"%s%soverlay=%d:%d%s;"+
			"%s%soverlay=%d:%d,format=yuv420p%s",
		inputLabel,
		lowSrc,
		middleSrc,
		roiSrc,
		lowSrc,
		lowFilter,
		low,
		middleSrc,
		middleFilter,
		middle.W,
		middle.H,
		middle.X,
		middle.Y,
		mid,
		roiSrc,
		roi.W,
		roi.H,
		roi.X,
		roi.Y,
		roiLabel,
		low,
		mid,
		middle.X,
		middle.Y,
		withMid,
		withMid,
		roiLabel,
		roi.X,
		roi.Y,
		outputLabel,
	)
}

func filterLabel(name string) string {
	return "[" + name + "]"
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
