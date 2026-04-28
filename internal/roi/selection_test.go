package roi

import "testing"

func TestChooseBestROIPerceptualCandidatePrefersDegradedPeriphery(t *testing.T) {
	candidates := []Candidate{
		{Scale: 1.00, Blur: 0, Kbps: 500},
		{Scale: 0.76, Blur: 1, Kbps: 502},
		{Scale: 0.35, Blur: 2, Kbps: 498},
	}

	got := chooseBestROIPerceptualCandidate(candidates, 500, 0.07, 0.25, 0.35, 2)
	if got.Scale != 0.35 || got.Blur != 2 {
		t.Fatalf("selected candidate = %.2f/%d, want 0.35/2", got.Scale, got.Blur)
	}
}

func TestChooseBestROIPerceptualCandidateHonorsPSNRTie(t *testing.T) {
	candidates := []Candidate{
		{Scale: 0.76, Blur: 1, Kbps: 500, ROIYPSNR: 41.0},
		{Scale: 0.35, Blur: 2, Kbps: 500, ROIYPSNR: 41.2},
		{Scale: 0.24, Blur: 6, Kbps: 500, ROIYPSNR: 38.0},
	}

	got := chooseBestROIPerceptualCandidate(candidates, 500, 0.07, 0.25, 0.76, 1)
	if got.Scale != 0.76 || got.Blur != 1 {
		t.Fatalf("selected candidate = %.2f/%d, want 0.76/1", got.Scale, got.Blur)
	}
}

func TestCandidateSummariesAreStableSorted(t *testing.T) {
	got := candidateSummaries([]Candidate{
		{Kind: "b", CRF: 18, Scale: 0.5, Blur: 2, Kbps: 100},
		{Kind: "a", CRF: 18, Scale: 0.3, Blur: 4, Kbps: 100},
		{Kind: "a", CRF: 16, Scale: 0.9, Blur: 0, Kbps: 100},
	})

	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0].Kind != "a" || got[0].CRF != 16 {
		t.Fatalf("first summary = %+v, want kind a CRF 16", got[0])
	}
	if got[1].Kind != "a" || got[1].CRF != 18 {
		t.Fatalf("second summary = %+v, want kind a CRF 18", got[1])
	}
	if got[2].Kind != "b" {
		t.Fatalf("third summary = %+v, want kind b", got[2])
	}
}

func TestSearchPeripheryCandidatesInterpolatedJumpsNearTarget(t *testing.T) {
	settings := []peripherySetting{
		{Scale: 1.0, Blur: 0},
		{Scale: 0.9, Blur: 0},
		{Scale: 0.8, Blur: 1},
		{Scale: 0.7, Blur: 1},
		{Scale: 0.6, Blur: 2},
		{Scale: 0.5, Blur: 3},
		{Scale: 0.4, Blur: 4},
		{Scale: 0.3, Blur: 5},
		{Scale: 0.2, Blur: 6},
		{Scale: 0.1, Blur: 7},
	}

	var seen []int
	candidates, err := searchPeripheryCandidatesInterpolated(
		settings,
		300,
		0.01,
		5,
		0.6,
		2,
		func(idx int, s peripherySetting) (Candidate, error) {
			seen = append(seen, idx)
			return Candidate{
				Scale: s.Scale,
				Blur:  s.Blur,
				Kbps:  1000 - float64(idx)*100,
			}, nil
		},
	)
	if err != nil {
		t.Fatalf("searchPeripheryCandidatesInterpolated returned error: %v", err)
	}

	if len(seen) >= len(settings) {
		t.Fatalf("evaluated %d candidates, want fewer than full linear scan %d", len(seen), len(settings))
	}
	if !containsInt(seen, 7) {
		t.Fatalf("evaluated indexes = %v, want interpolated target index 7", seen)
	}
	if len(candidates) != len(seen) {
		t.Fatalf("candidates len = %d, seen len = %d", len(candidates), len(seen))
	}
	for i := 1; i < len(candidates); i++ {
		if candidates[i-1].Scale < candidates[i].Scale {
			t.Fatalf("candidates are not returned in setting order: %+v", candidates)
		}
	}
}

func TestSearchPeripheryCandidatesInterpolatedRespectsProbeLimit(t *testing.T) {
	settings := []peripherySetting{
		{Scale: 1.0, Blur: 0},
		{Scale: 0.8, Blur: 1},
		{Scale: 0.6, Blur: 2},
		{Scale: 0.4, Blur: 3},
		{Scale: 0.2, Blur: 4},
	}

	var seen []int
	_, err := searchPeripheryCandidatesInterpolated(
		settings,
		500,
		0.01,
		3,
		0.6,
		2,
		func(idx int, s peripherySetting) (Candidate, error) {
			seen = append(seen, idx)
			return Candidate{Scale: s.Scale, Blur: s.Blur, Kbps: 1000 - float64(idx)*100}, nil
		},
	)
	if err != nil {
		t.Fatalf("searchPeripheryCandidatesInterpolated returned error: %v", err)
	}
	if len(seen) != 3 {
		t.Fatalf("evaluated %d candidates, want exactly probe limit 3", len(seen))
	}
}

func containsInt(values []int, want int) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
