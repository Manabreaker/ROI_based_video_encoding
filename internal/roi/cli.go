package roi

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseFlags builds Config from CLI flags.
func ParseFlags() Config {
	cfg, err := ParseArgs(os.Args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		_, _ = fmt.Fprintf(os.Stderr, "\n[ERROR] %v\n", err)
		os.Exit(2)
	}
	return cfg
}

// ParseArgs builds Config from command-line args. Defaults are applied first,
// then an optional YAML config file, then explicit flags.
func ParseArgs(args []string) (Config, error) {
	cfg := defaultConfig()

	configPath, flagArgs, err := extractConfigPath(args)
	if err != nil {
		return Config{}, err
	}
	if configPath != "" {
		if err := loadYAMLConfig(configPath, &cfg); err != nil {
			return Config{}, err
		}
	}

	fs := flag.NewFlagSet("roi-poc", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var parsedConfigPath string
	registerConfigFlags(fs, &cfg, &parsedConfigPath)

	if err := fs.Parse(flagArgs); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func defaultConfig() Config {
	return Config{
		OutDir:               "out",
		Mode:                 "static",
		TargetBitrate:        "1000k",
		Tolerance:            0.07,
		ROIControl:           "qp-map",
		ROIQOffset:           -0.30,
		ROIMiddleQOffset:     -0.10,
		FitROI:               true,
		ROIHighQualityCRF:    16,
		ROIMinCRF:            10,
		ROIMaxCRFIfNeeded:    36,
		AllowROIQualityLoss:  false,
		ManualPeripheryScale: 0.35,
		ManualBlurRadius:     2,
		MiddleMargin:         0.35,
		MiddleScale:          0.67,
		MiddleBlurRadius:     1,
		ROIMinScale:          0.12,
		ROIMaxBlur:           10,
		ROIRateControl:       "abr",
		ROITwoPass:           true,
		ROIFitMetric:         true,
		ROIPSNRTieDB:         0.25,
		ROIMaxrateMultiplier: 1.15,
		ROIBufsizeSeconds:    2.0,
		VideoEncoder:         "auto",
		Preset:               "veryfast",
		NVENCPreset:          "p4",
		FitIterations:        9,
		MotionWindow:         0.6,
		MotionThresh:         34,
		ROIMargin:            0.18,
		OverlayBitrate:       true,
		BitrateWindow:        1.0,
		MaxBitrateOverlays:   300,
		Metrics:              true,
		Serve:                false,
		HTTPAddr:             ":8080",
		KeepTemp:             false,
	}
}

func registerConfigFlags(fs *flag.FlagSet, cfg *Config, configPath *string) {
	fs.StringVar(configPath, "config", "", "YAML config file; explicit flags override values loaded from the file")

	fs.StringVar(&cfg.Input, "input", cfg.Input, "input video file, camera URL, RTSP URL, or any FFmpeg-readable source")
	fs.StringVar(&cfg.OutDir, "out", cfg.OutDir, "output directory")
	fs.StringVar(&cfg.Mode, "mode", cfg.Mode, "ROI mode: static or motion")
	fs.StringVar(&cfg.ROIString, "roi", cfg.ROIString, "static ROI as x,y,w,h; pixels or fractions 0..1; if empty, center ROI is used")

	fs.StringVar(&cfg.TargetBitrate, "target-bitrate", cfg.TargetBitrate, "target actual bitrate, e.g. 300k, 1000k, 1.5M")
	fs.Float64Var(&cfg.Tolerance, "tolerance", cfg.Tolerance, "acceptable relative bitrate error, e.g. 0.07 means +-7%")

	fs.StringVar(&cfg.ROIControl, "roi-control", cfg.ROIControl, "ROI control method: qp-map or mask")
	fs.Float64Var(&cfg.ROIQOffset, "roi-qoffset", cfg.ROIQOffset, "QP offset for the main ROI in --roi-control=qp-map; negative values improve quality")
	fs.Float64Var(&cfg.ROIMiddleQOffset, "roi-middle-qoffset", cfg.ROIMiddleQOffset, "QP offset for the middle ROI ring in --roi-control=qp-map; 0 disables the middle ROI side data")
	fs.BoolVar(&cfg.FitROI, "fit-roi", cfg.FitROI, "fit ROI output by changing periphery degradation")
	fs.IntVar(&cfg.ROIHighQualityCRF, "roi-crf", cfg.ROIHighQualityCRF, "CRF used for final ROI output; lower means closer to original ROI")
	fs.IntVar(&cfg.ROIMinCRF, "roi-min-crf", cfg.ROIMinCRF, "minimum CRF used when the video is too simple and target bitrate is higher than full-detail output")
	fs.IntVar(&cfg.ROIMaxCRFIfNeeded, "roi-max-crf-if-needed", cfg.ROIMaxCRFIfNeeded, "maximum CRF only when --allow-roi-quality-loss=true and target cannot be reached otherwise")
	fs.BoolVar(&cfg.AllowROIQualityLoss, "allow-roi-quality-loss", cfg.AllowROIQualityLoss, "if true, may increase ROI CRF when target is impossible while preserving high-quality ROI")

	fs.Float64Var(&cfg.ManualPeripheryScale, "periphery-scale", cfg.ManualPeripheryScale, "manual periphery scale when --fit-roi=false")
	fs.IntVar(&cfg.ManualBlurRadius, "blur", cfg.ManualBlurRadius, "manual periphery blur when --fit-roi=false")
	fs.Float64Var(&cfg.MiddleMargin, "middle-margin", cfg.MiddleMargin, "middle-quality ring expansion around ROI as fraction of ROI size")
	fs.Float64Var(&cfg.MiddleScale, "middle-scale", cfg.MiddleScale, "middle-quality ring scale before re-upscaling; roughly 720p from a 1080p source")
	fs.IntVar(&cfg.MiddleBlurRadius, "middle-blur", cfg.MiddleBlurRadius, "middle-quality ring blur radius")
	fs.Float64Var(&cfg.ROIMinScale, "roi-min-scale", cfg.ROIMinScale, "minimum periphery scale candidate for ROI fitting")
	fs.IntVar(&cfg.ROIMaxBlur, "roi-max-blur", cfg.ROIMaxBlur, "maximum periphery blur candidate for ROI fitting")

	fs.StringVar(&cfg.ROIRateControl, "roi-rate-control", cfg.ROIRateControl, "ROI encoder rate control: abr keeps ROI output near --target-bitrate; crf preserves old fixed-CRF behavior")
	fs.BoolVar(&cfg.ROITwoPass, "roi-two-pass", cfg.ROITwoPass, "use x264 two-pass ABR for ROI output when --roi-rate-control=abr and --encoder=libx264")
	fs.BoolVar(&cfg.ROIFitMetric, "roi-fit-metric", cfg.ROIFitMetric, "during ROI fitting, measure ROI-crop PSNR for each candidate and pick the least degraded periphery near the best ROI score")
	fs.Float64Var(&cfg.ROIPSNRTieDB, "roi-psnr-tie-db", cfg.ROIPSNRTieDB, "when fitting ROI by metric, prefer milder periphery if ROI PSNR is within this many dB of the best candidate")
	fs.Float64Var(&cfg.ROIMaxrateMultiplier, "roi-maxrate-multiplier", cfg.ROIMaxrateMultiplier, "ABR maxrate as a multiplier of --target-bitrate for ROI output")
	fs.Float64Var(&cfg.ROIBufsizeSeconds, "roi-bufsize-seconds", cfg.ROIBufsizeSeconds, "ABR VBV buffer size in target-bitrate seconds for ROI output")

	fs.StringVar(&cfg.VideoEncoder, "encoder", cfg.VideoEncoder, "video encoder: auto, libx264, or h264_nvenc")
	fs.StringVar(&cfg.Preset, "preset", cfg.Preset, "x264 preset")
	fs.StringVar(&cfg.NVENCPreset, "nvenc-preset", cfg.NVENCPreset, "NVENC preset used when --encoder resolves to h264_nvenc")
	fs.IntVar(&cfg.FitIterations, "fit-iterations", cfg.FitIterations, "maximum interpolation probes for ROI fitting and CRF search iterations for emergency ROI fitting")

	fs.Float64Var(&cfg.MotionWindow, "motion-window", cfg.MotionWindow, "time gap in seconds between frames used for simple motion ROI detection")
	fs.IntVar(&cfg.MotionThresh, "motion-threshold", cfg.MotionThresh, "grayscale difference threshold for motion ROI detection")
	fs.Float64Var(&cfg.ROIMargin, "roi-margin", cfg.ROIMargin, "ROI expansion margin as fraction of detected bbox size")

	fs.BoolVar(&cfg.OverlayBitrate, "overlay-bitrate", cfg.OverlayBitrate, "draw dynamic bitrate overlay on comparison video")
	fs.Float64Var(&cfg.BitrateWindow, "bitrate-window", cfg.BitrateWindow, "window size in seconds for dynamic bitrate calculation")
	fs.IntVar(&cfg.MaxBitrateOverlays, "max-bitrate-overlays", cfg.MaxBitrateOverlays, "safety cap for drawtext overlays; increase --bitrate-window for long videos")

	fs.BoolVar(&cfg.Metrics, "metrics", cfg.Metrics, "calculate ROI PSNR against original for input baseline and ROI output")

	fs.BoolVar(&cfg.Serve, "serve", cfg.Serve, "start local HTTP server after processing")
	fs.StringVar(&cfg.HTTPAddr, "http", cfg.HTTPAddr, "HTTP address for --serve")
	fs.BoolVar(&cfg.KeepTemp, "keep-temp", cfg.KeepTemp, "keep temporary candidate files")
}

func extractConfigPath(args []string) (string, []string, error) {
	var path string
	var out []string
	foundPositionalConfig := false

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			out = append(out, args[i:]...)
			break
		}
		if arg == "-config" || arg == "--config" {
			if i+1 >= len(args) {
				return "", nil, errors.New("missing value for --config")
			}
			path = args[i+1]
			out = append(out, arg, args[i+1])
			i++
			continue
		}
		if value, ok := strings.CutPrefix(arg, "-config="); ok {
			path = value
			out = append(out, arg)
			continue
		}
		if value, ok := strings.CutPrefix(arg, "--config="); ok {
			path = value
			out = append(out, arg)
			continue
		}

		if i == 0 && !foundPositionalConfig && !strings.HasPrefix(arg, "-") && isYAMLConfigPath(arg) {
			path = arg
			foundPositionalConfig = true
			continue
		}

		out = append(out, arg)
	}

	return path, out, nil
}

func isYAMLConfigPath(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml")
}

func loadYAMLConfig(path string, cfg *Config) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open YAML config %q: %w", path, err)
	}
	defer func() {
		_ = f.Close()
	}()

	decoder := yaml.NewDecoder(f)
	decoder.KnownFields(true)

	if err := decoder.Decode(cfg); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("parse YAML config %q: %w", path, err)
	}

	return nil
}
