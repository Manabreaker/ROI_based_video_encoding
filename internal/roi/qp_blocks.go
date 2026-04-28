package roi

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

const defaultROIBlockSize = 64

type qpMapBlockRect struct {
	X       int
	Y       int
	W       int
	H       int
	Col     int
	Row     int
	QOffset float64
}

func usesROIBlockMap(cfg Config) bool {
	return len(cfg.ROIBlocks) > 0
}

func normalizedROIBlockSize(cfg Config) int {
	if cfg.ROIBlockSize <= 0 {
		return defaultROIBlockSize
	}
	return cfg.ROIBlockSize
}

func normalizedROIBlockSpan(v int) int {
	if v <= 0 {
		return 1
	}
	return v
}

func countROIBlockCells(blocks []QPMapBlock) int {
	count := 0
	for _, b := range blocks {
		count += normalizedROIBlockSpan(b.W) * normalizedROIBlockSpan(b.H)
	}
	return count
}

func qpMapBlockRects(cfg Config, info VideoInfo) ([]qpMapBlockRect, error) {
	if !usesROIBlockMap(cfg) {
		return nil, nil
	}
	if info.Width <= 0 || info.Height <= 0 {
		return nil, fmt.Errorf("cannot build ROI block map for invalid video size %dx%d", info.Width, info.Height)
	}

	blockSize := normalizedROIBlockSize(cfg)
	cols := int(math.Ceil(float64(info.Width) / float64(blockSize)))
	rows := int(math.Ceil(float64(info.Height) / float64(blockSize)))
	seen := make(map[[2]int]int)
	rects := make([]qpMapBlockRect, 0, countROIBlockCells(cfg.ROIBlocks))

	for i, b := range cfg.ROIBlocks {
		wBlocks := normalizedROIBlockSpan(b.W)
		hBlocks := normalizedROIBlockSpan(b.H)

		if b.Col < 0 || b.Row < 0 {
			return nil, fmt.Errorf("roi-blocks[%d] must have non-negative col and row", i)
		}
		if b.Col >= cols || b.Row >= rows {
			return nil, fmt.Errorf("roi-blocks[%d] starts outside the %dx%d block grid", i, cols, rows)
		}
		if b.Col+wBlocks > cols || b.Row+hBlocks > rows {
			return nil, fmt.Errorf("roi-blocks[%d] extends outside the %dx%d block grid", i, cols, rows)
		}

		for row := b.Row; row < b.Row+hBlocks; row++ {
			for col := b.Col; col < b.Col+wBlocks; col++ {
				key := [2]int{col, row}
				if prev, ok := seen[key]; ok {
					return nil, fmt.Errorf("roi-blocks[%d] overlaps roi-blocks[%d] at col=%d row=%d", i, prev, col, row)
				}
				seen[key] = i

				x := col * blockSize
				y := row * blockSize
				w := blockSize
				h := blockSize
				if x+w > info.Width {
					w = info.Width - x
				}
				if y+h > info.Height {
					h = info.Height - y
				}
				w = evenBlockExtent(w)
				h = evenBlockExtent(h)
				if w <= 0 || h <= 0 {
					return nil, fmt.Errorf("roi-blocks[%d] produces an empty block at col=%d row=%d", i, col, row)
				}

				rects = append(rects, qpMapBlockRect{
					X:       x,
					Y:       y,
					W:       w,
					H:       h,
					Col:     col,
					Row:     row,
					QOffset: b.QOffset,
				})
			}
		}
	}

	return rects, nil
}

func evenBlockExtent(v int) int {
	if v%2 != 0 {
		v--
	}
	return v
}

func blockMapROI(cfg Config, info VideoInfo) (ROI, error) {
	rects, err := qpMapBlockRects(cfg, info)
	if err != nil {
		return ROI{}, err
	}
	if len(rects) == 0 {
		return ROI{}, fmt.Errorf("mode blocks requires at least one roi-block")
	}

	minX := rects[0].X
	minY := rects[0].Y
	maxX := rects[0].X + rects[0].W
	maxY := rects[0].Y + rects[0].H

	for _, r := range rects[1:] {
		if r.X < minX {
			minX = r.X
		}
		if r.Y < minY {
			minY = r.Y
		}
		if r.X+r.W > maxX {
			maxX = r.X + r.W
		}
		if r.Y+r.H > maxY {
			maxY = r.Y + r.H
		}
	}

	return clampROI(ROI{
		X:      minX,
		Y:      minY,
		W:      maxX - minX,
		H:      maxY - minY,
		Source: fmt.Sprintf("qp-blocks-%dpx", normalizedROIBlockSize(cfg)),
	}, info), nil
}

type roiBlocksFlag struct {
	target *[]QPMapBlock
}

func (f roiBlocksFlag) String() string {
	if f.target == nil || len(*f.target) == 0 {
		return ""
	}

	parts := make([]string, 0, len(*f.target))
	for _, b := range *f.target {
		w := normalizedROIBlockSpan(b.W)
		h := normalizedROIBlockSpan(b.H)
		parts = append(parts, fmt.Sprintf("%d,%d,%d,%d,%.4f", b.Col, b.Row, w, h, b.QOffset))
	}
	return strings.Join(parts, ";")
}

func (f roiBlocksFlag) Set(value string) error {
	if f.target == nil {
		return fmt.Errorf("roi-blocks flag is not configured")
	}

	blocks, err := parseROIBlocksFlag(value)
	if err != nil {
		return err
	}

	*f.target = blocks
	return nil
}

func parseROIBlocksFlag(value string) ([]QPMapBlock, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}

	entries := strings.Split(value, ";")
	blocks := make([]QPMapBlock, 0, len(entries))

	for i, entry := range entries {
		fields := strings.Split(entry, ",")
		for j := range fields {
			fields[j] = strings.TrimSpace(fields[j])
		}
		if len(fields) != 3 && len(fields) != 5 {
			return nil, fmt.Errorf("roi-blocks entry %d must be col,row,qoffset or col,row,w,h,qoffset", i)
		}

		col, err := strconv.Atoi(fields[0])
		if err != nil {
			return nil, fmt.Errorf("invalid roi-blocks entry %d col: %w", i, err)
		}
		row, err := strconv.Atoi(fields[1])
		if err != nil {
			return nil, fmt.Errorf("invalid roi-blocks entry %d row: %w", i, err)
		}

		w := 1
		h := 1
		qoffsetField := 2
		if len(fields) == 5 {
			w, err = strconv.Atoi(fields[2])
			if err != nil {
				return nil, fmt.Errorf("invalid roi-blocks entry %d w: %w", i, err)
			}
			h, err = strconv.Atoi(fields[3])
			if err != nil {
				return nil, fmt.Errorf("invalid roi-blocks entry %d h: %w", i, err)
			}
			qoffsetField = 4
		}

		qoffset, err := strconv.ParseFloat(fields[qoffsetField], 64)
		if err != nil {
			return nil, fmt.Errorf("invalid roi-blocks entry %d qoffset: %w", i, err)
		}

		blocks = append(blocks, QPMapBlock{
			Col:     col,
			Row:     row,
			W:       w,
			H:       h,
			QOffset: qoffset,
		})
	}

	return blocks, nil
}
