package roi

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

type commandSpec struct {
	Name string
	Args []string
}

type nvencSDKEncodePlan struct {
	decode           commandSpec
	helper           commandSpec
	mux              commandSpec
	elementaryOutput string
	output           string
}

func usesNVENCSDKQPMap(cfg Config) bool {
	return normalizeVideoEncoder(cfg.VideoEncoder) == encoderNVENCSDK && roiControl(cfg) == "qp-map"
}

func buildNVENCSDKEncodePlan(cfg Config, info VideoInfo, targetKbps float64, output string, workDir string) (nvencSDKEncodePlan, error) {
	if len(cfg.ROIBlocks) == 0 {
		return nvencSDKEncodePlan{}, errors.New("h264_nvenc_sdk requires at least one roi-block")
	}
	if info.Width <= 0 || info.Height <= 0 {
		return nvencSDKEncodePlan{}, fmt.Errorf("h264_nvenc_sdk requires valid video dimensions, got %dx%d", info.Width, info.Height)
	}
	if targetKbps <= 0 {
		return nvencSDKEncodePlan{}, errors.New("h264_nvenc_sdk requires a positive target bitrate")
	}

	roiBlocks, err := serializeNVENCSDKROIBlocks(cfg.ROIBlocks)
	if err != nil {
		return nvencSDKEncodePlan{}, err
	}

	elementaryOutput := filepath.Join(workDir, "nvenc_sdk", "roi.h264")
	fps := formatNVENCSDKFPS(info.FPS)
	bitrateKbps := strconv.Itoa(int(math.Round(targetKbps)))

	return nvencSDKEncodePlan{
		decode: commandSpec{
			Name: "ffmpeg",
			Args: []string{
				"-hide_banner",
				"-loglevel", "error",
				"-i", cfg.Input,
				"-an",
				"-pix_fmt", "nv12",
				"-f", "rawvideo",
				"pipe:1",
			},
		},
		helper: commandSpec{
			Name: nvencSDKHelperPath(),
			Args: []string{
				"--width", strconv.Itoa(info.Width),
				"--height", strconv.Itoa(info.Height),
				"--fps", fps,
				"--bitrate-kbps", bitrateKbps,
				"--block-size", strconv.Itoa(normalizedROIBlockSize(cfg)),
				"--roi-blocks", roiBlocks,
				"--input-format", "nv12",
				"--codec", "h264",
				"--output", elementaryOutput,
			},
		},
		mux: commandSpec{
			Name: "ffmpeg",
			Args: []string{
				"-hide_banner",
				"-loglevel", "error",
				"-y",
				"-fflags", "+genpts",
				"-r", fps,
				"-i", elementaryOutput,
				"-c:v", "copy",
				"-movflags", "+faststart",
				output,
			},
		},
		elementaryOutput: elementaryOutput,
		output:           output,
	}, nil
}

func nvencSDKHelperPath() string {
	if path := strings.TrimSpace(os.Getenv("ROI_NVENC_BIN")); path != "" {
		return path
	}
	if runtime.GOOS == "windows" {
		return filepath.FromSlash("native/roi-nvenc/roi-nvenc.exe")
	}
	return filepath.FromSlash("native/roi-nvenc/roi-nvenc")
}

func serializeNVENCSDKROIBlocks(blocks []QPMapBlock) (string, error) {
	if len(blocks) == 0 {
		return "", errors.New("h264_nvenc_sdk requires at least one roi-block")
	}

	parts := make([]string, 0, len(blocks))
	for i, b := range blocks {
		if b.Col < 0 || b.Row < 0 {
			return "", fmt.Errorf("roi-blocks[%d] col and row must be non-negative", i)
		}
		if b.QOffset < -1 || b.QOffset > 1 {
			return "", fmt.Errorf("roi-blocks[%d] qoffset must be in range [-1,1]", i)
		}
		parts = append(parts, fmt.Sprintf(
			"%d,%d,%d,%d,%.4f",
			b.Col,
			b.Row,
			normalizedROIBlockSpan(b.W),
			normalizedROIBlockSpan(b.H),
			b.QOffset,
		))
	}

	return strings.Join(parts, ";"), nil
}

func formatNVENCSDKFPS(fps float64) string {
	if fps <= 0 {
		fps = 30
	}
	return fmt.Sprintf("%.3f", fps)
}

func runNVENCSDKEncodePlan(plan nvencSDKEncodePlan) error {
	if err := os.MkdirAll(filepath.Dir(plan.elementaryOutput), 0o755); err != nil {
		return fmt.Errorf("create NVENC SDK work directory: %w", err)
	}

	if err := runNVENCSDKRawPipe(plan.decode, plan.helper); err != nil {
		return err
	}
	if err := runCommandSpec(plan.mux); err != nil {
		return err
	}

	return nil
}

func runNVENCSDKRawPipe(decode commandSpec, helper commandSpec) error {
	decodeCmd := exec.Command(decode.Name, decode.Args...)
	helperCmd := exec.Command(helper.Name, helper.Args...)

	decodeStdout, err := decodeCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("connect %s stdout: %w", decode.Name, err)
	}
	helperCmd.Stdin = decodeStdout

	var decodeStderr bytes.Buffer
	var helperStderr bytes.Buffer
	decodeCmd.Stderr = &decodeStderr
	helperCmd.Stderr = &helperStderr

	if err := helperCmd.Start(); err != nil {
		return fmt.Errorf("%s failed to start: %w", helper.Name, err)
	}
	if err := decodeCmd.Start(); err != nil {
		_ = decodeStdout.Close()
		_ = helperCmd.Process.Kill()
		_ = helperCmd.Wait()
		return fmt.Errorf("%s failed to start: %w", decode.Name, err)
	}

	decodeErr := decodeCmd.Wait()
	helperErr := helperCmd.Wait()

	if helperErr != nil {
		return fmt.Errorf("%s failed: %w\n%s", helper.Name, helperErr, strings.TrimSpace(helperStderr.String()))
	}
	if decodeErr != nil {
		return fmt.Errorf("%s failed: %w\n%s", decode.Name, decodeErr, strings.TrimSpace(decodeStderr.String()))
	}

	return nil
}

func runCommandSpec(spec commandSpec) error {
	cmd := exec.Command(spec.Name, spec.Args...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %w\n%s", spec.Name, err, strings.TrimSpace(stderr.String()))
	}

	return nil
}

func renderROINVENCSDK(cfg Config, info VideoInfo, selection ROISelection, targetKbps float64, output string, workDir string, rateControl string) (EncodeDecision, error) {
	_ = selection
	plan, err := buildNVENCSDKEncodePlan(cfg, info, targetKbps, output, workDir)
	if err != nil {
		return EncodeDecision{}, err
	}
	if err := runNVENCSDKEncodePlan(plan); err != nil {
		return EncodeDecision{}, err
	}

	actual, err := measuredAverageBitrateKbps(output)
	if err != nil {
		return EncodeDecision{}, err
	}

	return nvencSDKEncodeDecision(cfg, targetKbps, actual, rateControl), nil
}

func nvencSDKEncodeDecision(cfg Config, targetKbps float64, actualKbps float64, rateControl string) EncodeDecision {
	blockSize := normalizedROIBlockSize(cfg)
	blockCount := countROIBlockCells(cfg.ROIBlocks)
	note := fmt.Sprintf(
		"encoder-level ROI QP map via NVIDIA Video Codec SDK Emphasis MAP; static block map applies %d configured cells at %d px grid; single SDK encode path",
		blockCount,
		blockSize,
	)
	if !withinTolerance(actualKbps, targetKbps, cfg.Tolerance) {
		note += "; measured bitrate is outside tolerance"
	}

	candidate := Candidate{
		Kind:          "roi-nvenc-sdk-emphasis-map",
		Encoder:       cfg.VideoEncoder,
		ROIControl:    "qp-map",
		RateControl:   rateControl,
		ROIBlockSize:  blockSize,
		ROIBlockCount: blockCount,
		Kbps:          actualKbps,
		Note:          note,
	}

	return EncodeDecision{
		Name:            "roi",
		Encoder:         cfg.VideoEncoder,
		ROIControl:      "qp-map",
		TargetKbps:      targetKbps,
		ActualKbps:      actualKbps,
		WithinTolerance: withinTolerance(actualKbps, targetKbps, cfg.Tolerance),
		RateControl:     rateControl,
		ROIBlockSize:    blockSize,
		ROIBlockCount:   blockCount,
		Note:            note,
		Candidates:      []CandidateSummary{candidateSummary(candidate)},
	}
}
