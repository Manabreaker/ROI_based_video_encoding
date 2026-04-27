package roi

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

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
