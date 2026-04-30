package mapui

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Manabreaker/ROI_based_video_streaming/internal/roi"
	"gopkg.in/yaml.v3"
)

// Palette is the fixed qoffset palette used by the painter.
var Palette = []PaletteEntry{
	{Name: "Green", Color: "#22c55e", QOffset: -0.40},
	{Name: "Orange", Color: "#f97316", QOffset: -0.25},
	{Name: "Yellow", Color: "#eab308", QOffset: -0.10},
	{Name: "Red", Color: "#ef4444", QOffset: 0.15},
}

// GroupCells groups painted cells into stable non-overlapping block rectangles.
func GroupCells(cells []PaintCell) ([]roi.QPMapBlock, error) {
	if len(cells) == 0 {
		return nil, nil
	}

	cellMap := make(map[[2]int]float64, len(cells))
	for i, c := range cells {
		if c.Col < 0 || c.Row < 0 {
			return nil, fmt.Errorf("cells[%d] col and row must be non-negative", i)
		}
		if !isPaletteQOffset(c.QOffset) {
			return nil, fmt.Errorf("cells[%d] qoffset %.4f is not in the palette", i, c.QOffset)
		}

		key := [2]int{c.Col, c.Row}
		if _, ok := cellMap[key]; ok {
			return nil, fmt.Errorf("duplicate cell col=%d row=%d", c.Col, c.Row)
		}
		cellMap[key] = normalizedQOffset(c.QOffset)
	}

	keys := make([][2]int, 0, len(cellMap))
	for key := range cellMap {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][1] != keys[j][1] {
			return keys[i][1] < keys[j][1]
		}
		return keys[i][0] < keys[j][0]
	})

	visited := make(map[[2]int]bool, len(cellMap))
	blocks := make([]roi.QPMapBlock, 0, len(cellMap))

	for _, start := range keys {
		if visited[start] {
			continue
		}

		qoffset := cellMap[start]
		width := 1
		for {
			next := [2]int{start[0] + width, start[1]}
			value, ok := cellMap[next]
			if !ok || visited[next] || !sameQOffset(value, qoffset) {
				break
			}
			width++
		}

		height := 1
		for {
			row := start[1] + height
			canExtend := true
			for col := start[0]; col < start[0]+width; col++ {
				key := [2]int{col, row}
				value, ok := cellMap[key]
				if !ok || visited[key] || !sameQOffset(value, qoffset) {
					canExtend = false
					break
				}
			}
			if !canExtend {
				break
			}
			height++
		}

		for row := start[1]; row < start[1]+height; row++ {
			for col := start[0]; col < start[0]+width; col++ {
				visited[[2]int{col, row}] = true
			}
		}

		blocks = append(blocks, roi.QPMapBlock{
			Col:     start[0],
			Row:     start[1],
			W:       width,
			H:       height,
			QOffset: qoffset,
		})
	}

	return blocks, nil
}

// WriteConfig writes an encoder-ready YAML config.
func WriteConfig(path string, opts Options, req ConfigRequest, blocks []roi.QPMapBlock) error {
	if len(blocks) == 0 {
		return fmt.Errorf("at least one ROI block is required")
	}

	cfg := generatedConfig{
		Input:          opts.Input,
		OutDir:         strings.TrimSpace(req.OutDir),
		Mode:           "blocks",
		ROIControl:     "qp-map",
		ROIBlockSize:   req.ROIBlockSize,
		ROIBlocks:      blocks,
		TargetBitrate:  strings.TrimSpace(req.TargetBitrate),
		ROIRateControl: "abr",
		ROITwoPass:     false,
		Encoder:        strings.TrimSpace(req.Encoder),
		Preset:         opts.Preset,
		NVENCPreset:    opts.NVENCPreset,
		BitrateWindow:  req.BitrateWindow,
		OverlayBitrate: true,
		Metrics:        false,
		Serve:          false,
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	return os.WriteFile(path, data, 0o644)
}

func validateConfigRequest(req ConfigRequest) error {
	if strings.TrimSpace(req.OutDir) == "" {
		return fmt.Errorf("out must not be empty")
	}
	if strings.TrimSpace(req.TargetBitrate) == "" {
		return fmt.Errorf("targetBitrate must not be empty")
	}
	if err := validateEncoder(req.Encoder); err != nil {
		return err
	}
	if req.BitrateWindow <= 0 {
		return fmt.Errorf("bitrateWindow must be greater than zero")
	}
	if err := validateBlockSize(req.ROIBlockSize); err != nil {
		return err
	}
	if len(req.Cells) == 0 {
		return fmt.Errorf("paint at least one ROI block")
	}
	return nil
}

func isPaletteQOffset(value float64) bool {
	for _, entry := range Palette {
		if sameQOffset(entry.QOffset, value) {
			return true
		}
	}
	return false
}

func normalizedQOffset(value float64) float64 {
	return math.Round(value*100) / 100
}

func sameQOffset(a float64, b float64) bool {
	return math.Abs(a-b) < 0.0001
}
