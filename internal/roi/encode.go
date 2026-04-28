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

	candidates, err := fitPeripheryCandidatesInterpolated(cfg, info, roi, targetKbps, workDir, settings, rateControl)
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

				if err := renderROICandidateCRF(cfg, info, roi, path, crf, 1.0, 0); err != nil {
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

// fitPeripheryCandidatesInterpolated probes the ordered periphery ladder using bitrate interpolation.
func fitPeripheryCandidatesInterpolated(
	cfg Config,
	info VideoInfo,
	roi ROI,
	targetKbps float64,
	workDir string,
	settings []peripherySetting,
	rateControl string,
) ([]Candidate, error) {
	eval := func(idx int, s peripherySetting) (Candidate, error) {
		return evaluatePeripheryCandidate(cfg, info, roi, targetKbps, workDir, settings, rateControl, idx, s)
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
	roi ROI,
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

	if err := renderROICandidate(cfg, info, roi, path, cfg.ROIHighQualityCRF, s.Scale, s.Blur, targetKbps); err != nil {
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
		if err := attachROIPSNRMetric(cfg, roi, &c, logPath); err != nil {
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
func renderROICandidate(cfg Config, info VideoInfo, roi ROI, output string, crf int, scale float64, blur int, targetKbps float64) error {
	if roiRateControl(cfg) == "abr" {
		return renderROICandidateABR(cfg, info, roi, output, scale, blur, targetKbps)
	}
	return renderROICandidateCRF(cfg, info, roi, output, crf, scale, blur)
}

// renderROICandidateCRF encodes an ROI candidate with fixed quality settings.
func renderROICandidateCRF(cfg Config, info VideoInfo, roi ROI, output string, crf int, scale float64, blur int) error {
	filter := buildROIFilter(cfg, info, roi, scale, blur)

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
	if targetKbps <= 0 {
		return errors.New("targetKbps must be greater than zero for ROI ABR encoding")
	}

	filter := buildROIFilter(cfg, info, roi, scale, blur)
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

	if !cfg.ROITwoPass || isNVENC(cfg) {
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

// buildROIFilter creates low, middle, and original-ROI layers before final encoding.
func buildROIFilter(cfg Config, info VideoInfo, roi ROI, scale float64, blur int) string {
	middle := middleROI(cfg, roi, info)
	middleScale, middleBlur := middleQualitySettings(cfg, scale, blur)
	lowFilter := buildPeripheryFilter(info, scale, blur)
	middleFilter := buildPeripheryFilter(info, middleScale, middleBlur)

	return fmt.Sprintf(
		"[0:v]split=3[lowsrc][middlesrc][roisrc];"+
			"[lowsrc]%s[low];"+
			"[middlesrc]%s,crop=%d:%d:%d:%d[mid];"+
			"[roisrc]crop=%d:%d:%d:%d,format=yuv420p[roi];"+
			"[low][mid]overlay=%d:%d[withmid];"+
			"[withmid][roi]overlay=%d:%d,format=yuv420p[v]",
		lowFilter,
		middleFilter,
		middle.W,
		middle.H,
		middle.X,
		middle.Y,
		roi.W,
		roi.H,
		roi.X,
		roi.Y,
		middle.X,
		middle.Y,
		roi.X,
		roi.Y,
	)
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
