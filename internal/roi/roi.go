package roi

import (
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// selectROI chooses either a static ROI or a simple motion-derived ROI.
func selectROI(cfg Config, info VideoInfo, tmpDir string) (ROI, error) {
	if usesROIBlockMap(cfg) {
		return blockMapROI(cfg, info)
	}

	switch roiMode(cfg) {
	case "static":
		if strings.TrimSpace(cfg.ROIString) == "" {
			r := defaultCenterROI(info)
			r.Source = "static-default-center"
			return clampROI(r, info), nil
		}

		r, err := parseROI(cfg.ROIString, info)
		if err != nil {
			return ROI{}, err
		}

		r.Source = "static-cli"
		return clampROI(r, info), nil

	case "motion":
		r, err := detectMotionROI(cfg, info, tmpDir)
		if err != nil {
			return ROI{}, err
		}

		return clampROI(r, info), nil

	default:
		return ROI{}, fmt.Errorf("unknown --mode %q; use static, motion, or blocks", cfg.Mode)
	}
}

func roiMode(cfg Config) string {
	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))
	switch mode {
	case "", "static":
		return "static"
	case "motion":
		return "motion"
	case "block", "blocks", "qp-blocks", "qp_blocks", "qp-map-blocks", "qp_map_blocks":
		return "blocks"
	default:
		return mode
	}
}

// parseROI accepts x,y,w,h values as pixels or fractions of the frame.
func parseROI(s string, info VideoInfo) (ROI, error) {
	parts := strings.Split(s, ",")
	if len(parts) != 4 {
		return ROI{}, errors.New("--roi must be x,y,w,h")
	}

	vals := make([]float64, 4)
	allFraction := true

	for i, p := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			return ROI{}, fmt.Errorf("invalid ROI component %q: %w", p, err)
		}

		vals[i] = v

		if v < 0 || v > 1 {
			allFraction = false
		}
	}

	if allFraction {
		return ROI{
			X: int(math.Round(vals[0] * float64(info.Width))),
			Y: int(math.Round(vals[1] * float64(info.Height))),
			W: int(math.Round(vals[2] * float64(info.Width))),
			H: int(math.Round(vals[3] * float64(info.Height))),
		}, nil
	}

	return ROI{
		X: int(vals[0]),
		Y: int(vals[1]),
		W: int(vals[2]),
		H: int(vals[3]),
	}, nil
}

// defaultCenterROI returns an even-sized ROI centered in the frame.
func defaultCenterROI(info VideoInfo) ROI {
	w := evenInt(int(float64(info.Width) * 0.38))
	h := evenInt(int(float64(info.Height) * 0.38))
	x := evenInt((info.Width - w) / 2)
	y := evenInt((info.Height - h) / 2)

	return ROI{
		X: x,
		Y: y,
		W: w,
		H: h,
	}
}

// clampROI keeps ROI coordinates inside the frame and compatible with yuv420p.
func clampROI(r ROI, info VideoInfo) ROI {
	const minW = 32
	const minH = 32

	if r.W < minW {
		r.W = minW
	}
	if r.H < minH {
		r.H = minH
	}
	if r.W > info.Width {
		r.W = info.Width
	}
	if r.H > info.Height {
		r.H = info.Height
	}
	if r.X < 0 {
		r.X = 0
	}
	if r.Y < 0 {
		r.Y = 0
	}
	if r.X+r.W > info.Width {
		r.X = info.Width - r.W
	}
	if r.Y+r.H > info.Height {
		r.Y = info.Height - r.H
	}

	r.X = evenInt(r.X)
	r.Y = evenInt(r.Y)
	r.W = evenInt(r.W)
	r.H = evenInt(r.H)

	if r.X+r.W > info.Width {
		r.W = evenInt(info.Width - r.X)
	}
	if r.Y+r.H > info.Height {
		r.H = evenInt(info.Height - r.Y)
	}

	return r
}

// evenInt rounds down to a positive even integer for codec-friendly dimensions.
func evenInt(v int) int {
	if v < 0 {
		v = 0
	}
	if v%2 != 0 {
		v--
	}
	if v < 2 {
		return 2
	}
	return v
}

// detectMotionROI estimates ROI from the changed pixels between two sampled frames.
func detectMotionROI(cfg Config, info VideoInfo, tmpDir string) (ROI, error) {
	first := filepath.Join(tmpDir, "motion_a.png")
	second := filepath.Join(tmpDir, "motion_b.png")

	t1 := 0.0
	t2 := cfg.MotionWindow

	if info.Duration > 2*cfg.MotionWindow+0.2 {
		t1 = math.Max(0.0, info.Duration*0.25)
		t2 = math.Min(info.Duration-0.1, t1+cfg.MotionWindow)
	}

	if err := extractFrame(cfg.Input, t1, first); err != nil {
		return ROI{}, err
	}
	if err := extractFrame(cfg.Input, t2, second); err != nil {
		return ROI{}, err
	}

	imgA, err := loadImage(first)
	if err != nil {
		return ROI{}, err
	}
	imgB, err := loadImage(second)
	if err != nil {
		return ROI{}, err
	}

	r, changed := motionBBox(imgA, imgB, cfg.MotionThresh)
	if !changed {
		d := defaultCenterROI(info)
		d.Source = "motion-fallback-center"
		return d, nil
	}

	r = expandROI(r, cfg.ROIMargin, info)
	r.Source = "motion-diff"

	return r, nil
}

// extractFrame writes one RGB frame from the input at the requested timestamp.
func extractFrame(input string, sec float64, output string) error {
	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-ss", fmt.Sprintf("%.3f", sec),
		"-i", input,
		"-frames:v", "1",
		"-vf", "format=rgb24",
		output,
	}
	return runCommand("ffmpeg", args...)
}

// loadImage decodes a PNG or JPEG frame from disk.
func loadImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	return img, err
}

// motionBBox returns the bounding box of pixels whose luma changed enough.
func motionBBox(a, b image.Image, thresh int) (ROI, bool) {
	ba := a.Bounds()
	bb := b.Bounds()

	w := minInt(ba.Dx(), bb.Dx())
	h := minInt(ba.Dy(), bb.Dy())

	if w <= 0 || h <= 0 {
		return ROI{}, false
	}

	minX := w
	minY := h
	maxX := 0
	maxY := 0
	changed := 0
	step := 4

	for y := 0; y < h; y += step {
		for x := 0; x < w; x += step {
			ga := grayAt(a, ba.Min.X+x, ba.Min.Y+y)
			gb := grayAt(b, bb.Min.X+x, bb.Min.Y+y)

			if absInt(ga-gb) >= thresh {
				if x < minX {
					minX = x
				}
				if y < minY {
					minY = y
				}
				if x > maxX {
					maxX = x
				}
				if y > maxY {
					maxY = y
				}
				changed++
			}
		}
	}

	if changed < 50 {
		return ROI{}, false
	}

	return ROI{
		X: minX,
		Y: minY,
		W: maxX - minX + step,
		H: maxY - minY + step,
	}, true
}

// grayAt approximates luma for quick motion comparison.
func grayAt(img image.Image, x, y int) int {
	r, g, b, _ := img.At(x, y).RGBA()

	rr := int(r >> 8)
	gg := int(g >> 8)
	bb := int(b >> 8)

	return (299*rr + 587*gg + 114*bb) / 1000
}

// expandROI adds margin around a detected box and clamps it to the frame.
func expandROI(r ROI, margin float64, info VideoInfo) ROI {
	if margin < 0 {
		margin = 0
	}

	dx := int(float64(r.W) * margin)
	dy := int(float64(r.H) * margin)

	r.X -= dx
	r.Y -= dy
	r.W += 2 * dx
	r.H += 2 * dy

	return clampROI(r, info)
}

// middleROI expands the ROI to create the orange medium-quality ring.
func middleROI(cfg Config, roi ROI, info VideoInfo) ROI {
	return expandROI(roi, cfg.MiddleMargin, info)
}

// middleQualitySettings keeps the middle ring no worse than the outer low layer.
func middleQualitySettings(cfg Config, lowScale float64, lowBlur int) (float64, int) {
	scale := cfg.MiddleScale
	if scale < lowScale {
		scale = lowScale
	}

	blur := cfg.MiddleBlurRadius
	if blur > lowBlur {
		blur = lowBlur
	}

	return scale, blur
}

// minInt returns the smaller integer.
func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

// absInt returns the absolute integer value.
func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
