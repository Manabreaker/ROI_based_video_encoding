package roi

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const (
	encoderAuto             = "auto"
	encoderX264             = "libx264"
	encoderNVENC            = "h264_nvenc"
	encoderAMF              = "h264_amf"
	encoderVideoToolbox     = "h264_videotoolbox"
	videoToolboxQualityKbps = "8000k"
)

var supportedVideoEncoders = []string{
	encoderAuto,
	encoderX264,
	encoderNVENC,
	encoderAMF,
	encoderVideoToolbox,
}

// resolveVideoEncoder picks a hardware encoder in auto mode when FFmpeg advertises one.
func resolveVideoEncoder(requested string) (string, error) {
	encoder := normalizeVideoEncoder(requested)
	if encoder == "" || encoder == encoderAuto {
		for _, candidate := range autoVideoEncoderCandidates() {
			if ffmpegHasEncoder(candidate) {
				return candidate, nil
			}
		}
		return encoderX264, nil
	}

	if !isSupportedVideoEncoder(encoder) {
		return "", fmt.Errorf("--encoder must be %s", supportedVideoEncoderList())
	}
	if isHardwareVideoEncoderName(encoder) && !ffmpegHasEncoder(encoder) {
		return "", fmt.Errorf("--encoder %s was requested, but ffmpeg does not list %s", encoder, encoder)
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

func autoVideoEncoderCandidates() []string {
	if runtime.GOOS == "darwin" {
		return []string{encoderVideoToolbox, encoderNVENC, encoderAMF}
	}
	return []string{encoderNVENC, encoderAMF, encoderVideoToolbox}
}

func isSupportedVideoEncoder(value string) bool {
	for _, encoder := range supportedVideoEncoders {
		if value == encoder {
			return true
		}
	}
	return false
}

func IsSupportedVideoEncoder(value string) bool {
	return isSupportedVideoEncoder(normalizeVideoEncoder(value))
}

func supportedVideoEncoderList() string {
	return strings.Join(supportedVideoEncoders, ", ")
}

func SupportedVideoEncoderList() string {
	return supportedVideoEncoderList()
}

// qualityEncoderArgs returns encoder-specific arguments for fixed-quality renders.
func qualityEncoderArgs(cfg Config, quality int) []string {
	qualityArg := strconv.Itoa(quality)

	switch normalizeVideoEncoder(cfg.VideoEncoder) {
	case encoderNVENC:
		return []string{
			"-c:v", encoderNVENC,
			"-preset", cfg.NVENCPreset,
			"-rc", "vbr",
			"-cq", qualityArg,
			"-b:v", "0",
		}
	case encoderAMF:
		return []string{
			"-c:v", encoderAMF,
			"-usage", "transcoding",
			"-quality", "balanced",
			"-rc", "cqp",
			"-qp_i", qualityArg,
			"-qp_p", qualityArg,
			"-qp_b", qualityArg,
		}
	case encoderVideoToolbox:
		return videoToolboxQualityEncoderArgs(quality)
	}

	return []string{
		"-c:v", encoderX264,
		"-preset", cfg.Preset,
		"-crf", qualityArg,
	}
}

// bitrateEncoderArgs returns encoder-specific arguments for target-bitrate renders.
func bitrateEncoderArgs(cfg Config, bitrate string, maxrate string, bufsize string) []string {
	switch normalizeVideoEncoder(cfg.VideoEncoder) {
	case encoderNVENC:
		return []string{
			"-c:v", encoderNVENC,
			"-preset", cfg.NVENCPreset,
			"-rc", "vbr",
			"-b:v", bitrate,
			"-maxrate", maxrate,
			"-bufsize", bufsize,
		}
	case encoderAMF:
		return []string{
			"-c:v", encoderAMF,
			"-usage", "transcoding",
			"-quality", "balanced",
			"-rc", "vbr_peak",
			"-b:v", bitrate,
			"-maxrate", maxrate,
			"-bufsize", bufsize,
		}
	case encoderVideoToolbox:
		return []string{
			"-c:v", encoderVideoToolbox,
			"-b:v", bitrate,
			"-maxrate", maxrate,
			"-bufsize", bufsize,
		}
	}

	return []string{
		"-c:v", encoderX264,
		"-preset", cfg.Preset,
		"-b:v", bitrate,
		"-maxrate", maxrate,
		"-bufsize", bufsize,
	}
}

func qpMapQualityEncoderArgs(cfg Config, quality int) []string {
	args := qualityEncoderArgs(cfg, quality)
	return appendROIEncoderArgs(cfg, args)
}

func qpMapBitrateEncoderArgs(cfg Config, bitrate string, maxrate string, bufsize string) []string {
	args := bitrateEncoderArgs(cfg, bitrate, maxrate, bufsize)
	return appendROIEncoderArgs(cfg, args)
}

func appendROIEncoderArgs(cfg Config, args []string) []string {
	switch normalizeVideoEncoder(cfg.VideoEncoder) {
	case encoderNVENC:
		return append(args, "-spatial-aq", "1")
	case encoderX264:
		return append(args, "-aq-mode", "1")
	}
	return args
}

func videoToolboxQualityEncoderArgs(quality int) []string {
	args := []string{"-c:v", encoderVideoToolbox}
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		return append(args, "-q:v", strconv.Itoa(videoToolboxQualityValue(quality)))
	}
	return append(args, "-b:v", videoToolboxQualityKbps)
}

func videoToolboxQualityValue(quality int) int {
	value := 100 - quality
	if value < 1 {
		return 1
	}
	if value > 100 {
		return 100
	}
	return value
}

// isNVENC reports whether the resolved encoder uses NVIDIA's hardware encoder.
func isNVENC(cfg Config) bool {
	return normalizeVideoEncoder(cfg.VideoEncoder) == encoderNVENC
}

func isHardwareVideoEncoder(cfg Config) bool {
	return isHardwareVideoEncoderName(normalizeVideoEncoder(cfg.VideoEncoder))
}

func isHardwareVideoEncoderName(encoder string) bool {
	switch encoder {
	case encoderNVENC, encoderAMF, encoderVideoToolbox:
		return true
	default:
		return false
	}
}

// roiRateArgs derives bitrate, maxrate, and bufsize arguments for ABR encoding.
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

func roiControl(cfg Config) string {
	control := strings.ToLower(strings.TrimSpace(cfg.ROIControl))
	switch control {
	case "", "qpmap", "qp_map", "qp-map":
		return "qp-map"
	case "mask", "preprocess", "preprocessing":
		return "mask"
	default:
		return control
	}
}

// roiCandidateKind labels candidates by their encoding mode.
func roiCandidateKind(rateControl string) string {
	if rateControl == "abr" {
		return "roi-target-abr"
	}
	return "roi-preserve-roi-crf"
}

func qpMapCandidateKind(rateControl string) string {
	if rateControl == "abr" {
		return "roi-qp-map-abr"
	}
	return "roi-qp-map-crf"
}
