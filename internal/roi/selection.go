package roi

import (
	"math"
	"sort"
)

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

	return choosePreferredPeripheryCandidate(nearBest, preferredScale, preferredBlur)
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
		Encoder:     c.Encoder,
		CRF:         c.CRF,
		RateControl: c.RateControl,
		Scale:       c.Scale,
		Blur:        c.Blur,
		MiddleScale: c.MiddleScale,
		MiddleBlur:  c.MiddleBlur,
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
