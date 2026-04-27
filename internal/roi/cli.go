package roi

import (
	"flag"
)

// ParseFlags builds Config from CLI flags.
func ParseFlags() Config {
	var cfg Config

	flag.StringVar(&cfg.Input, "input", "", "input video file, camera URL, RTSP URL, or any FFmpeg-readable source")
	flag.StringVar(&cfg.OutDir, "out", "out", "output directory")
	flag.StringVar(&cfg.Mode, "mode", "static", "ROI mode: static or motion")
	flag.StringVar(&cfg.ROIString, "roi", "", "static ROI as x,y,w,h; pixels or fractions 0..1; if empty, center ROI is used")

	flag.StringVar(&cfg.TargetBitrate, "target-bitrate", "1000k", "target actual bitrate, e.g. 300k, 1000k, 1.5M")
	flag.Float64Var(&cfg.Tolerance, "tolerance", 0.07, "acceptable relative bitrate error, e.g. 0.07 means +-7%")

	flag.BoolVar(&cfg.FitROI, "fit-roi", true, "fit ROI output by changing periphery degradation")
	flag.IntVar(&cfg.ROIHighQualityCRF, "roi-crf", 16, "CRF used for final ROI output; lower means closer to original ROI")
	flag.IntVar(&cfg.ROIMinCRF, "roi-min-crf", 10, "minimum CRF used when the video is too simple and target bitrate is higher than full-detail output")
	flag.IntVar(&cfg.ROIMaxCRFIfNeeded, "roi-max-crf-if-needed", 36, "maximum CRF only when --allow-roi-quality-loss=true and target cannot be reached otherwise")
	flag.BoolVar(&cfg.AllowROIQualityLoss, "allow-roi-quality-loss", false, "if true, may increase ROI CRF when target is impossible while preserving high-quality ROI")

	flag.Float64Var(&cfg.ManualPeripheryScale, "periphery-scale", 0.35, "manual periphery scale when --fit-roi=false")
	flag.IntVar(&cfg.ManualBlurRadius, "blur", 2, "manual periphery blur when --fit-roi=false")
	flag.Float64Var(&cfg.MiddleMargin, "middle-margin", 0.35, "middle-quality ring expansion around ROI as fraction of ROI size")
	flag.Float64Var(&cfg.MiddleScale, "middle-scale", 0.67, "middle-quality ring scale before re-upscaling; roughly 720p from a 1080p source")
	flag.IntVar(&cfg.MiddleBlurRadius, "middle-blur", 1, "middle-quality ring blur radius")
	flag.Float64Var(&cfg.ROIMinScale, "roi-min-scale", 0.12, "minimum periphery scale candidate for ROI fitting")
	flag.IntVar(&cfg.ROIMaxBlur, "roi-max-blur", 10, "maximum periphery blur candidate for ROI fitting")

	flag.StringVar(&cfg.ROIRateControl, "roi-rate-control", "abr", "ROI encoder rate control: abr keeps ROI output near --target-bitrate; crf preserves old fixed-CRF behavior")
	flag.BoolVar(&cfg.ROITwoPass, "roi-two-pass", true, "use x264 two-pass ABR for ROI output when --roi-rate-control=abr and --encoder=libx264")
	flag.BoolVar(&cfg.ROIFitMetric, "roi-fit-metric", true, "during ROI fitting, measure ROI-crop PSNR for each candidate and pick the least degraded periphery near the best ROI score")
	flag.Float64Var(&cfg.ROIPSNRTieDB, "roi-psnr-tie-db", 0.25, "when fitting ROI by metric, prefer milder periphery if ROI PSNR is within this many dB of the best candidate")
	flag.Float64Var(&cfg.ROIMaxrateMultiplier, "roi-maxrate-multiplier", 1.15, "ABR maxrate as a multiplier of --target-bitrate for ROI output")
	flag.Float64Var(&cfg.ROIBufsizeSeconds, "roi-bufsize-seconds", 2.0, "ABR VBV buffer size in target-bitrate seconds for ROI output")

	flag.StringVar(&cfg.VideoEncoder, "encoder", "auto", "video encoder: auto, libx264, or h264_nvenc")
	flag.StringVar(&cfg.Preset, "preset", "veryfast", "x264 preset")
	flag.StringVar(&cfg.NVENCPreset, "nvenc-preset", "p4", "NVENC preset used when --encoder resolves to h264_nvenc")
	flag.IntVar(&cfg.FitIterations, "fit-iterations", 9, "maximum CRF search iterations for emergency ROI fitting")

	flag.Float64Var(&cfg.MotionWindow, "motion-window", 0.6, "time gap in seconds between frames used for simple motion ROI detection")
	flag.IntVar(&cfg.MotionThresh, "motion-threshold", 34, "grayscale difference threshold for motion ROI detection")
	flag.Float64Var(&cfg.ROIMargin, "roi-margin", 0.18, "ROI expansion margin as fraction of detected bbox size")

	flag.BoolVar(&cfg.OverlayBitrate, "overlay-bitrate", true, "draw dynamic bitrate overlay on comparison video")
	flag.Float64Var(&cfg.BitrateWindow, "bitrate-window", 1.0, "window size in seconds for dynamic bitrate calculation")
	flag.IntVar(&cfg.MaxBitrateOverlays, "max-bitrate-overlays", 300, "safety cap for drawtext overlays; increase --bitrate-window for long videos")

	flag.BoolVar(&cfg.Metrics, "metrics", true, "calculate ROI PSNR against original for input baseline and ROI output")

	flag.BoolVar(&cfg.Serve, "serve", false, "start local HTTP server after processing")
	flag.StringVar(&cfg.HTTPAddr, "http", ":8080", "HTTP address for --serve")
	flag.BoolVar(&cfg.KeepTemp, "keep-temp", false, "keep temporary candidate files")

	flag.Parse()

	return cfg
}
