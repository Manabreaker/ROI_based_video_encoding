package roi

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// resolveVideoEncoder picks NVENC in auto mode when FFmpeg advertises h264_nvenc.
func resolveVideoEncoder(requested string) (string, error) {
	encoder := normalizeVideoEncoder(requested)
	if encoder == "" || encoder == "auto" {
		if ffmpegHasEncoder("h264_nvenc") {
			return "h264_nvenc", nil
		}
		return "libx264", nil
	}

	if encoder == "h264_nvenc" && !ffmpegHasEncoder("h264_nvenc") {
		return "", errors.New("--encoder h264_nvenc was requested, but ffmpeg does not list h264_nvenc")
	}

	return encoder, nil
}

// normalizeVideoEncoder canonicalizes CLI encoder names.
func normalizeVideoEncoder(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// ffmpegHasEncoder checks FFmpeg capabilities without shell pipelines or eval.
func ffmpegHasEncoder(name string) bool {
	out, err := commandOutput("ffmpeg", "-hide_banner", "-encoders")
	if err != nil {
		return false
	}
	return encoderListed(string(out), name)
}

// encoderListed reports whether an encoder appears as a standalone token.
func encoderListed(ffmpegEncoders string, name string) bool {
	for _, line := range strings.Split(ffmpegEncoders, "\n") {
		for _, field := range strings.Fields(line) {
			if field == name {
				return true
			}
		}
	}
	return false
}

// qualityEncoderArgs returns encoder-specific arguments for fixed-quality renders.
func qualityEncoderArgs(cfg Config, quality int) []string {
	if isNVENC(cfg) {
		return []string{
			"-c:v", "h264_nvenc",
			"-preset", cfg.NVENCPreset,
			"-rc", "vbr",
			"-cq", strconv.Itoa(quality),
			"-b:v", "0",
		}
	}

	return []string{
		"-c:v", "libx264",
		"-preset", cfg.Preset,
		"-crf", strconv.Itoa(quality),
	}
}

// bitrateEncoderArgs returns encoder-specific arguments for target-bitrate renders.
func bitrateEncoderArgs(cfg Config, bitrate string, maxrate string, bufsize string) []string {
	if isNVENC(cfg) {
		return []string{
			"-c:v", "h264_nvenc",
			"-preset", cfg.NVENCPreset,
			"-rc", "vbr",
			"-b:v", bitrate,
			"-maxrate", maxrate,
			"-bufsize", bufsize,
		}
	}

	return []string{
		"-c:v", "libx264",
		"-preset", cfg.Preset,
		"-b:v", bitrate,
		"-maxrate", maxrate,
		"-bufsize", bufsize,
	}
}

// isNVENC reports whether the resolved encoder uses NVIDIA's hardware encoder.
func isNVENC(cfg Config) bool {
	return normalizeVideoEncoder(cfg.VideoEncoder) == "h264_nvenc"
}

// roiRateArgs derives bitrate, maxrate, and bufsize arguments for x264 ABR.
func roiRateArgs(cfg Config, targetKbps float64) (string, string, string) {
	maxrateKbps := targetKbps * cfg.ROIMaxrateMultiplier
	if maxrateKbps < targetKbps {
		maxrateKbps = targetKbps
	}

	bufsizeKbps := targetKbps * cfg.ROIBufsizeSeconds
	if bufsizeKbps < targetKbps {
		bufsizeKbps = targetKbps
	}

	return bitrateArgKbps(targetKbps), bitrateArgKbps(maxrateKbps), bitrateArgKbps(bufsizeKbps)
}

// bitrateArgKbps formats a kilobit value for FFmpeg arguments.
func bitrateArgKbps(kbps float64) string {
	if kbps < 1 {
		kbps = 1
	}
	return fmt.Sprintf("%dk", int(math.Round(kbps)))
}

// gopSize uses a roughly two-second GOP with conservative bounds.
func gopSize(info VideoInfo) int {
	fps := info.FPS
	if fps <= 0 {
		fps = 30
	}
	gop := int(math.Round(fps * 2.0))
	if gop < 12 {
		return 12
	}
	if gop > 300 {
		return 300
	}
	return gop
}

// cleanupPassLogs removes x264 two-pass side files.
func cleanupPassLogs(prefix string) {
	matches, _ := filepath.Glob(prefix + "*")
	for _, m := range matches {
		_ = os.Remove(m)
	}
}

// nullOutputName returns the platform-specific null sink for FFmpeg first passes.
func nullOutputName() string {
	if runtime.GOOS == "windows" {
		return "NUL"
	}
	return "/dev/null"
}

// roiRateControl normalizes the ROI rate-control flag.
func roiRateControl(cfg Config) string {
	rc := strings.ToLower(strings.TrimSpace(cfg.ROIRateControl))
	if rc == "" {
		return "abr"
	}
	return rc
}

// roiCandidateKind labels candidates by their encoding mode.
func roiCandidateKind(rateControl string) string {
	if rateControl == "abr" {
		return "roi-target-abr"
	}
	return "roi-preserve-roi-crf"
}
