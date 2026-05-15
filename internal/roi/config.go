package roi

import (
	"errors"
	"fmt"
	"strings"
)

// validateConfig rejects flag combinations that would make the FFmpeg pipeline ambiguous or unsafe.
func validateConfig(cfg Config) error {
	if strings.TrimSpace(cfg.Input) == "" {
		return errors.New("missing --input")
	}
	if _, ok := parseBitrateKbps(cfg.TargetBitrate); !ok {
		return fmt.Errorf("cannot parse --target-bitrate %q; examples: 300k, 1000k, 1.5M", cfg.TargetBitrate)
	}
	if cfg.Tolerance <= 0 || cfg.Tolerance > 0.50 {
		return errors.New("--tolerance must be in range (0, 0.5]")
	}
	switch roiMode(cfg) {
	case "static", "motion", "blocks":
	default:
		return errors.New("--mode must be static, motion, or blocks")
	}
	if cfg.ROIHighQualityCRF < 0 || cfg.ROIHighQualityCRF > 51 {
		return errors.New("--roi-crf must be in range 0..51")
	}
	switch roiControl(cfg) {
	case "qp-map", "mask":
	default:
		return errors.New("--roi-control must be either qp-map or mask")
	}
	if cfg.ROIQOffset < -1 || cfg.ROIQOffset > 1 {
		return errors.New("--roi-qoffset must be in range [-1,1]")
	}
	if cfg.ROIMiddleQOffset < -1 || cfg.ROIMiddleQOffset > 1 {
		return errors.New("--roi-middle-qoffset must be in range [-1,1]")
	}
	if cfg.ROIBlockSize <= 0 || cfg.ROIBlockSize%2 != 0 {
		return errors.New("--roi-block-size must be a positive even integer")
	}
	if roiMode(cfg) == "blocks" && len(cfg.ROIBlocks) == 0 {
		return errors.New("--mode blocks requires --roi-blocks")
	}
	if len(cfg.ROIBlocks) > 0 && roiControl(cfg) != "qp-map" {
		return errors.New("--roi-blocks requires --roi-control qp-map")
	}
	for i, b := range cfg.ROIBlocks {
		if b.Col < 0 || b.Row < 0 {
			return fmt.Errorf("roi-blocks[%d] col and row must be non-negative", i)
		}
		if b.W < 0 || b.H < 0 {
			return fmt.Errorf("roi-blocks[%d] w and h must be non-negative; omitted or 0 means one block", i)
		}
		if b.QOffset < -1 || b.QOffset > 1 {
			return fmt.Errorf("roi-blocks[%d] qoffset must be in range [-1,1]", i)
		}
	}
	if cfg.ROIMinCRF < 0 || cfg.ROIMinCRF > cfg.ROIHighQualityCRF {
		return errors.New("--roi-min-crf must be in range 0..roi-crf")
	}
	if cfg.ROIMaxCRFIfNeeded < cfg.ROIHighQualityCRF || cfg.ROIMaxCRFIfNeeded > 51 {
		return errors.New("--roi-max-crf-if-needed must be in range roi-crf..51")
	}
	if cfg.ManualPeripheryScale <= 0 || cfg.ManualPeripheryScale > 1 {
		return errors.New("--periphery-scale must be in range (0,1]")
	}
	if cfg.ManualBlurRadius < 0 || cfg.ManualBlurRadius > 40 {
		return errors.New("--blur must be in range 0..40")
	}
	if cfg.MiddleMargin < 0 || cfg.MiddleMargin > 2 {
		return errors.New("--middle-margin must be in range 0..2")
	}
	if cfg.MiddleScale <= 0 || cfg.MiddleScale > 1 {
		return errors.New("--middle-scale must be in range (0,1]")
	}
	if cfg.MiddleBlurRadius < 0 || cfg.MiddleBlurRadius > 40 {
		return errors.New("--middle-blur must be in range 0..40")
	}
	if cfg.ROIMinScale <= 0 || cfg.ROIMinScale > 1 {
		return errors.New("--roi-min-scale must be in range (0,1]")
	}
	if cfg.ROIMaxBlur < 0 || cfg.ROIMaxBlur > 40 {
		return errors.New("--roi-max-blur must be in range 0..40")
	}
	switch roiRateControl(cfg) {
	case "abr", "crf":
	default:
		return errors.New("--roi-rate-control must be either abr or crf")
	}
	if cfg.ROIPSNRTieDB < 0 || cfg.ROIPSNRTieDB > 5 {
		return errors.New("--roi-psnr-tie-db must be in range 0..5")
	}
	if cfg.ROIMaxrateMultiplier < 1.0 || cfg.ROIMaxrateMultiplier > 5.0 {
		return errors.New("--roi-maxrate-multiplier must be in range 1..5")
	}
	if cfg.ROIBufsizeSeconds <= 0 || cfg.ROIBufsizeSeconds > 30 {
		return errors.New("--roi-bufsize-seconds must be in range (0,30]")
	}
	if !isSupportedVideoEncoder(normalizeVideoEncoder(cfg.VideoEncoder)) {
		return fmt.Errorf("--encoder must be %s", supportedVideoEncoderList())
	}
	if strings.TrimSpace(cfg.NVENCPreset) == "" {
		return errors.New("--nvenc-preset must not be empty")
	}
	if cfg.FitIterations < 1 || cfg.FitIterations > 30 {
		return errors.New("--fit-iterations must be in range 1..30")
	}
	if cfg.BitrateWindow <= 0 {
		return errors.New("--bitrate-window must be greater than zero")
	}
	return nil
}
