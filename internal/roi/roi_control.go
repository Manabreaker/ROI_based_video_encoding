package roi

import "fmt"

func validateROISideDataSupport(cfg Config) error {
	if roiControl(cfg) != "qp-map" || encoderConsumesROISideData(cfg) {
		return nil
	}
	return fmt.Errorf("--roi-control qp-map uses FFmpeg ROI side data, but encoder %s does not consume it; use --encoder libx264 for encoder-level ROI or --roi-control mask for preprocessing", cfg.VideoEncoder)
}

func encoderConsumesROISideData(cfg Config) bool {
	switch normalizeVideoEncoder(cfg.VideoEncoder) {
	case "", encoderAuto, encoderX264, encoderNVENCSDK:
		return true
	default:
		return false
	}
}
