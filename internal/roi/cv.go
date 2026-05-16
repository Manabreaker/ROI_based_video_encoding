package roi

import (
	_ "embed"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"strings"

	pigo "github.com/esimov/pigo/core"
)

const defaultCVModelName = "pigo-facefinder"

//go:embed models/pigo_facefinder
var embeddedPigoFacefinder []byte

func detectCVROI(cfg Config, info VideoInfo, tmpDir string) (ROI, error) {
	selection, err := detectCVROISelection(cfg, info, tmpDir)
	if err != nil {
		return ROI{}, err
	}

	return selection.ROI, nil
}

func detectCVROISelection(cfg Config, info VideoInfo, tmpDir string) (ROISelection, error) {
	model, modelName, err := loadCVModel(cfg.CVModel)
	if err != nil {
		return ROISelection{}, err
	}

	classifier, err := pigo.NewPigo().Unpack(model)
	if err != nil {
		return ROISelection{}, fmt.Errorf("load CV model %q: %w", modelName, err)
	}

	times := cvSampleTimes(info.Duration, cfg.CVSampleCount)
	samples := make([]cvROISample, 0, len(times))
	var union ROI
	var previous *ROI
	found := false
	accepted := 0
	bestScore := float32(0)

	for i, sec := range times {
		frame := filepath.Join(tmpDir, fmt.Sprintf("cv_model_%02d.png", i))
		if err := extractCVFrame(cfg.Input, sec, frame, cfg.CVFrameWidth); err != nil {
			return ROISelection{}, err
		}

		img, err := loadImage(frame)
		if err != nil {
			return ROISelection{}, err
		}

		detections := detectPigoFaces(classifier, img, cfg.CVMinScore)
		if d, ok := choosePigoDetection(detections, img.Bounds(), info, previous); ok {
			r := expandROI(pigoDetectionROI(d, img.Bounds(), info), cfg.ROIMargin, info)
			r.Source = fmt.Sprintf("cv-%s-track-sample-%02d-score-%.1f", modelName, i, d.Q)
			if !found {
				union = r
				found = true
			} else {
				union = unionROI(union, r)
			}
			accepted++
			if d.Q > bestScore {
				bestScore = d.Q
			}
			current := r
			previous = &current
			samples = append(samples, cvROISample{Time: sec, ROI: r, Detected: true, Score: d.Q})
			continue
		}

		if previous != nil {
			r := *previous
			r.Source = fmt.Sprintf("cv-%s-track-sample-%02d-hold", modelName, i)
			samples = append(samples, cvROISample{Time: sec, ROI: r})
			continue
		}

		r := defaultCenterROI(info)
		r.Source = fmt.Sprintf("cv-%s-track-sample-%02d-fallback-center", modelName, i)
		samples = append(samples, cvROISample{Time: sec, ROI: r})
	}

	if !found {
		r := defaultCenterROI(info)
		r.Source = fmt.Sprintf("cv-%s-fallback-center", modelName)
		return staticROISelection(r), nil
	}

	union.Source = fmt.Sprintf("cv-%s-track-%d-samples-%d-detections-score-%.1f", modelName, len(samples), accepted, bestScore)
	return ROISelection{
		ROI:      clampROI(union, info),
		Timeline: timedROIFromSamples(samples, info.Duration),
	}, nil
}

type cvROISample struct {
	Time     float64
	ROI      ROI
	Detected bool
	Score    float32
}

func loadCVModel(value string) ([]byte, string, error) {
	model := strings.TrimSpace(value)
	if model == "" || model == defaultCVModelName {
		return embeddedPigoFacefinder, defaultCVModelName, nil
	}

	data, err := os.ReadFile(model)
	if err != nil {
		return nil, "", fmt.Errorf("read --cv-model %q: %w", model, err)
	}
	return data, filepath.Base(model), nil
}

func cvSampleTimes(duration float64, count int) []float64 {
	if count < 1 {
		count = 1
	}
	if duration <= 0 {
		return []float64{0}
	}
	if count == 1 {
		return []float64{clampSampleTime(duration*0.5, duration)}
	}

	padding := math.Min(0.5, duration*0.1)
	start := padding
	end := duration - padding
	if end <= start {
		start = 0
		end = duration
	}

	out := make([]float64, 0, count)
	for i := 0; i < count; i++ {
		frac := float64(i) / float64(count-1)
		out = append(out, clampSampleTime(start+(end-start)*frac, duration))
	}
	return out
}

func clampSampleTime(sec float64, duration float64) float64 {
	if sec < 0 {
		return 0
	}
	if duration > 0 && sec > duration-0.1 {
		return math.Max(0, duration-0.1)
	}
	return sec
}

func extractCVFrame(input string, sec float64, output string, frameWidth int) error {
	filter := "format=rgb24"
	if frameWidth > 0 {
		filter = fmt.Sprintf("scale=w=%d:h=-2:flags=bicubic,format=rgb24", frameWidth)
	}

	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-ss", fmt.Sprintf("%.3f", sec),
		"-i", input,
		"-frames:v", "1",
		"-vf", filter,
		output,
	}
	return runCommand("ffmpeg", args...)
}

func detectPigoFaces(classifier *pigo.Pigo, img image.Image, minScore float64) []pigo.Detection {
	src := pigo.ImgToNRGBA(img)
	bounds := src.Bounds()
	cols := bounds.Dx()
	rows := bounds.Dy()
	if cols <= 0 || rows <= 0 {
		return nil
	}

	params := pigo.CascadeParams{
		MinSize:     cvMinFaceSize(cols, rows),
		MaxSize:     minInt(cols, rows),
		ShiftFactor: 0.10,
		ScaleFactor: 1.10,
		ImageParams: pigo.ImageParams{
			Pixels: pigo.RgbToGrayscale(src),
			Rows:   rows,
			Cols:   cols,
			Dim:    cols,
		},
	}

	detections := classifier.RunCascade(params, 0)
	detections = classifier.ClusterDetections(detections, 0.20)

	accepted := detections[:0]
	for _, d := range detections {
		if float64(d.Q) >= minScore {
			accepted = append(accepted, d)
		}
	}
	return accepted
}

func cvMinFaceSize(cols int, rows int) int {
	size := minInt(cols, rows) / 25
	if size < 20 {
		return 20
	}
	if size > 120 {
		return 120
	}
	return size
}

func pigoDetectionROI(d pigo.Detection, bounds image.Rectangle, info VideoInfo) ROI {
	cols := bounds.Dx()
	rows := bounds.Dy()
	if cols <= 0 || rows <= 0 {
		return ROI{}
	}

	scaleX := float64(info.Width) / float64(cols)
	scaleY := float64(info.Height) / float64(rows)
	x := float64(d.Col-d.Scale/2) * scaleX
	y := float64(d.Row-d.Scale/2) * scaleY
	w := float64(d.Scale) * scaleX
	h := float64(d.Scale) * scaleY

	return clampROI(ROI{
		X: int(math.Round(x)),
		Y: int(math.Round(y)),
		W: int(math.Round(w)),
		H: int(math.Round(h)),
	}, info)
}

func choosePigoDetection(detections []pigo.Detection, bounds image.Rectangle, info VideoInfo, previous *ROI) (pigo.Detection, bool) {
	if len(detections) == 0 {
		return pigo.Detection{}, false
	}

	best := detections[0]
	if previous == nil {
		for _, d := range detections[1:] {
			if d.Q > best.Q {
				best = d
			}
		}
		return best, true
	}

	prevX, prevY := roiCenter(*previous)
	bestMetric := math.Inf(1)
	for _, d := range detections {
		r := pigoDetectionROI(d, bounds, info)
		x, y := roiCenter(r)
		distance := math.Hypot(x-prevX, y-prevY)
		metric := distance - float64(d.Q)*4
		if metric < bestMetric {
			bestMetric = metric
			best = d
		}
	}

	return best, true
}

func timedROIFromSamples(samples []cvROISample, duration float64) []TimedROI {
	if len(samples) == 0 {
		return nil
	}

	timeline := make([]TimedROI, 0, len(samples))
	for i, sample := range samples {
		start := 0.0
		if i > 0 {
			start = midpoint(samples[i-1].Time, sample.Time)
		}

		end := duration
		if i < len(samples)-1 {
			end = midpoint(sample.Time, samples[i+1].Time)
		}
		if duration <= 0 && end <= start {
			end = start + 1.0
		}
		if end <= start {
			end = start + 0.001
		}

		timeline = appendOrExtendTimedROI(timeline, TimedROI{
			StartSeconds: start,
			EndSeconds:   end,
			ROI:          sample.ROI,
		})
	}

	return timeline
}

func appendOrExtendTimedROI(timeline []TimedROI, next TimedROI) []TimedROI {
	if len(timeline) == 0 {
		return append(timeline, next)
	}

	last := &timeline[len(timeline)-1]
	if sameROIGeometry(last.ROI, next.ROI) {
		last.EndSeconds = next.EndSeconds
		return timeline
	}

	return append(timeline, next)
}

func midpoint(a float64, b float64) float64 {
	return (a + b) / 2
}

func roiCenter(r ROI) (float64, float64) {
	return float64(r.X) + float64(r.W)/2, float64(r.Y) + float64(r.H)/2
}

func sameROIGeometry(a ROI, b ROI) bool {
	return a.X == b.X && a.Y == b.Y && a.W == b.W && a.H == b.H
}

func unionROI(a ROI, b ROI) ROI {
	minX := a.X
	if b.X < minX {
		minX = b.X
	}
	minY := a.Y
	if b.Y < minY {
		minY = b.Y
	}
	maxX := a.X + a.W
	if b.X+b.W > maxX {
		maxX = b.X + b.W
	}
	maxY := a.Y + a.H
	if b.Y+b.H > maxY {
		maxY = b.Y + b.H
	}

	return ROI{
		X: minX,
		Y: minY,
		W: maxX - minX,
		H: maxY - minY,
	}
}
