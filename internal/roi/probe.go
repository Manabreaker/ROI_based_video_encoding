package roi

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

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
