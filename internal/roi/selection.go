package roi

import (
	"math"
	"sort"
)

type peripheryCandidateEvaluator func(index int, setting peripherySetting) (Candidate, error)

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

// searchPeripheryCandidatesInterpolated evaluates a small subset of the ordered periphery ladder.
// It assumes bitrate generally decreases as the periphery becomes more degraded, but falls back to
// midpoint probes when measured candidates do not bracket the target cleanly.
func searchPeripheryCandidatesInterpolated(
	settings []peripherySetting,
	targetKbps float64,
	tolerance float64,
	maxProbes int,
	preferredScale float64,
	preferredBlur int,
	eval peripheryCandidateEvaluator,
) ([]Candidate, error) {
	if len(settings) == 0 || maxProbes <= 0 {
		return nil, nil
	}

	if maxProbes > len(settings) {
		maxProbes = len(settings)
	}

	probed := map[int]Candidate{}

	addProbe := func(idx int) error {
		if idx < 0 || idx >= len(settings) {
			return nil
		}
		if _, ok := probed[idx]; ok {
			return nil
		}
		if len(probed) >= maxProbes {
			return nil
		}

		c, err := eval(idx, settings[idx])
		if err != nil {
			return err
		}
		probed[idx] = c
		return nil
	}

	preferredIdx := preferredPeripheryIndex(settings, preferredScale, preferredBlur)
	for _, idx := range []int{0, len(settings) - 1, preferredIdx} {
		if err := addProbe(idx); err != nil {
			return nil, err
		}
	}

	for len(probed) < maxProbes {
		if hasCandidateWithinTolerance(probed, targetKbps, tolerance) {
			for _, idx := range neighborIndexesForClosestWithinTolerance(probed, len(settings), targetKbps, tolerance) {
				if err := addProbe(idx); err != nil {
					return nil, err
				}
			}
			break
		}

		idx, ok := nextInterpolatedPeripheryIndex(probed, len(settings), targetKbps)
		if !ok {
			break
		}
		if err := addProbe(idx); err != nil {
			return nil, err
		}
	}

	return candidatesBySettingOrder(probed, len(settings)), nil
}

func preferredPeripheryIndex(settings []peripherySetting, preferredScale float64, preferredBlur int) int {
	if len(settings) == 0 {
		return -1
	}

	bestIdx := 0
	bestScore := peripherySettingPreferenceScore(settings[0], preferredScale, preferredBlur)

	for i := 1; i < len(settings); i++ {
		score := peripherySettingPreferenceScore(settings[i], preferredScale, preferredBlur)
		if score < bestScore {
			bestIdx = i
			bestScore = score
		}
	}

	return bestIdx
}

func peripherySettingPreferenceScore(s peripherySetting, preferredScale float64, preferredBlur int) float64 {
	scaleScore := math.Abs(s.Scale-preferredScale) * 10
	blurScore := math.Abs(float64(s.Blur-preferredBlur)) * 0.25
	return scaleScore + blurScore
}

func neighborIndexesForClosestWithinTolerance(probed map[int]Candidate, count int, targetKbps float64, tolerance float64) []int {
	type scoredIndex struct {
		Index int
		Score float64
	}

	var indexes []scoredIndex

	for idx, c := range probed {
		if !withinTolerance(c.Kbps, targetKbps, tolerance) {
			continue
		}

		indexes = append(indexes, scoredIndex{
			Index: idx,
			Score: math.Abs(c.Kbps - targetKbps),
		})
	}

	if len(indexes) == 0 {
		return nil
	}

	sort.Slice(indexes, func(i, j int) bool {
		if !nearlyEqual(indexes[i].Score, indexes[j].Score) {
			return indexes[i].Score < indexes[j].Score
		}
		return indexes[i].Index < indexes[j].Index
	})

	var out []int
	for _, idx := range []int{indexes[0].Index - 1, indexes[0].Index + 1} {
		if idx < 0 || idx >= count {
			continue
		}
		if _, ok := probed[idx]; !ok {
			out = append(out, idx)
		}
	}

	return out
}

func hasCandidateWithinTolerance(probed map[int]Candidate, targetKbps float64, tolerance float64) bool {
	for _, c := range probed {
		if withinTolerance(c.Kbps, targetKbps, tolerance) {
			return true
		}
	}
	return false
}

type indexedBitrateProbe struct {
	Index int
	Kbps  float64
}

func nextInterpolatedPeripheryIndex(probed map[int]Candidate, count int, targetKbps float64) (int, bool) {
	if len(probed) == 0 || len(probed) >= count {
		return 0, false
	}

	probes := make([]indexedBitrateProbe, 0, len(probed))
	for idx, c := range probed {
		probes = append(probes, indexedBitrateProbe{Index: idx, Kbps: c.Kbps})
	}
	sort.Slice(probes, func(i, j int) bool {
		return probes[i].Index < probes[j].Index
	})

	fallbackIdx := -1
	fallbackGap := 0

	for i := 0; i < len(probes)-1; i++ {
		left := probes[i]
		right := probes[i+1]
		gap := right.Index - left.Index
		if gap <= 1 {
			continue
		}

		if gap > fallbackGap {
			fallbackGap = gap
			fallbackIdx = left.Index + gap/2
		}

		if !probesBracketTarget(left.Kbps, right.Kbps, targetKbps) {
			continue
		}

		idx := interpolatedProbeIndex(left, right, targetKbps)
		return nearestUnprobedInRange(idx, left.Index+1, right.Index-1, probed)
	}

	if fallbackIdx >= 0 {
		return nearestUnprobedInRange(fallbackIdx, 0, count-1, probed)
	}

	return 0, false
}

func probesBracketTarget(leftKbps float64, rightKbps float64, targetKbps float64) bool {
	return (leftKbps >= targetKbps && rightKbps <= targetKbps) ||
		(leftKbps <= targetKbps && rightKbps >= targetKbps)
}

func interpolatedProbeIndex(left indexedBitrateProbe, right indexedBitrateProbe, targetKbps float64) int {
	if nearlyEqual(left.Kbps, right.Kbps) {
		return left.Index + (right.Index-left.Index)/2
	}

	ratio := (targetKbps - left.Kbps) / (right.Kbps - left.Kbps)
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}

	idx := left.Index + int(math.Round(ratio*float64(right.Index-left.Index)))
	if idx <= left.Index {
		idx = left.Index + 1
	}
	if idx >= right.Index {
		idx = right.Index - 1
	}

	return idx
}

func nearestUnprobedInRange(want int, minIdx int, maxIdx int, probed map[int]Candidate) (int, bool) {
	if minIdx > maxIdx {
		return 0, false
	}
	if want < minIdx {
		want = minIdx
	}
	if want > maxIdx {
		want = maxIdx
	}

	if _, ok := probed[want]; !ok {
		return want, true
	}

	for offset := 1; want-offset >= minIdx || want+offset <= maxIdx; offset++ {
		left := want - offset
		if left >= minIdx {
			if _, ok := probed[left]; !ok {
				return left, true
			}
		}

		right := want + offset
		if right <= maxIdx {
			if _, ok := probed[right]; !ok {
				return right, true
			}
		}
	}

	return 0, false
}

func candidatesBySettingOrder(probed map[int]Candidate, count int) []Candidate {
	out := make([]Candidate, 0, len(probed))
	for i := 0; i < count; i++ {
		if c, ok := probed[i]; ok {
			out = append(out, c)
		}
	}
	return out
}

// candidateSummary removes transient file paths before writing JSON reports.
func candidateSummary(c Candidate) CandidateSummary {
	return CandidateSummary{
		Kind:          c.Kind,
		Encoder:       c.Encoder,
		ROIControl:    c.ROIControl,
		CRF:           c.CRF,
		RateControl:   c.RateControl,
		ROIQOffset:    c.ROIQOffset,
		MiddleQOffset: c.MiddleQOffset,
		Scale:         c.Scale,
		Blur:          c.Blur,
		MiddleScale:   c.MiddleScale,
		MiddleBlur:    c.MiddleBlur,
		ROIBlockSize:  c.ROIBlockSize,
		ROIBlockCount: c.ROIBlockCount,
		Kbps:          c.Kbps,
		ROIYPSNR:      c.ROIYPSNR,
		Note:          c.Note,
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
