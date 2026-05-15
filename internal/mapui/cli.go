package mapui

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strings"

	"github.com/Manabreaker/ROI_based_video_encoding/internal/roi"
)

const defaultROIBlockSize = 64

// ParseArgs builds Options from command-line arguments.
func ParseArgs(args []string, output io.Writer) (Options, error) {
	opts := DefaultOptions()

	fs := flag.NewFlagSet("roi-map-ui", flag.ContinueOnError)
	fs.SetOutput(output)

	fs.StringVar(&opts.Input, "input", opts.Input, "input video path")
	fs.StringVar(&opts.ConfigOut, "config-out", opts.ConfigOut, "output YAML config path")
	fs.StringVar(&opts.Addr, "addr", opts.Addr, "localhost listen address")
	fs.BoolVar(&opts.OpenBrowser, "open", opts.OpenBrowser, "open the browser automatically")
	noOpen := fs.Bool("no-open", false, "do not open the browser automatically")
	fs.StringVar(&opts.OutDir, "out", opts.OutDir, "default encoder output directory written to YAML")
	fs.StringVar(&opts.TargetBitrate, "target-bitrate", opts.TargetBitrate, "default target bitrate written to YAML")
	fs.StringVar(&opts.Encoder, "encoder", opts.Encoder, "default encoder written to YAML: "+roi.SupportedVideoEncoderList())
	fs.Float64Var(&opts.BitrateWindow, "bitrate-window", opts.BitrateWindow, "default bitrate overlay window written to YAML")
	fs.IntVar(&opts.ROIBlockSize, "roi-block-size", opts.ROIBlockSize, "QP-map block size in pixels")
	fs.StringVar(&opts.Preset, "preset", opts.Preset, "x264 preset written to YAML")
	fs.StringVar(&opts.NVENCPreset, "nvenc-preset", opts.NVENCPreset, "NVENC preset written to YAML")

	if err := fs.Parse(args); err != nil {
		return Options{}, err
	}
	if *noOpen {
		opts.OpenBrowser = false
	}

	if err := ValidateOptions(opts); err != nil {
		return Options{}, err
	}
	return opts, nil
}

// DefaultOptions returns conservative defaults for the local UI.
func DefaultOptions() Options {
	return Options{
		ConfigOut:     "config/roi_blocks_generated.yaml",
		Addr:          "127.0.0.1:8090",
		OpenBrowser:   true,
		OutDir:        "out/roi_blocks_generated",
		TargetBitrate: "500k",
		Encoder:       "auto",
		Preset:        "veryfast",
		NVENCPreset:   "p4",
		BitrateWindow: 2,
		ROIBlockSize:  defaultROIBlockSize,
	}
}

// ValidateOptions checks CLI-level invariants before the server starts.
func ValidateOptions(opts Options) error {
	if strings.TrimSpace(opts.Input) == "" {
		return errors.New("missing --input")
	}
	if strings.TrimSpace(opts.ConfigOut) == "" {
		return errors.New("--config-out must not be empty")
	}
	if strings.TrimSpace(opts.OutDir) == "" {
		return errors.New("--out must not be empty")
	}
	if strings.TrimSpace(opts.TargetBitrate) == "" {
		return errors.New("--target-bitrate must not be empty")
	}
	if err := validateEncoder(opts.Encoder); err != nil {
		return err
	}
	if opts.BitrateWindow <= 0 {
		return errors.New("--bitrate-window must be greater than zero")
	}
	if err := validateBlockSize(opts.ROIBlockSize); err != nil {
		return err
	}
	if !isLocalAddr(opts.Addr) {
		return errors.New("--addr must listen on localhost, 127.0.0.1, or ::1")
	}
	if _, err := os.Stat(opts.Input); err != nil {
		return fmt.Errorf("stat --input %q: %w", opts.Input, err)
	}
	return nil
}

func validateEncoder(value string) error {
	if roi.IsSupportedVideoEncoder(value) {
		return nil
	}
	return fmt.Errorf("--encoder must be %s", roi.SupportedVideoEncoderList())
}

func validateBlockSize(value int) error {
	if value <= 0 || value%2 != 0 {
		return errors.New("--roi-block-size must be a positive even integer")
	}
	return nil
}

func isLocalAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	host = strings.Trim(host, "[]")
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}
