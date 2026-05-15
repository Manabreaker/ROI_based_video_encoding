package mapui

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Manabreaker/ROI_based_video_encoding/internal/roi"
	"gopkg.in/yaml.v3"
)

func TestGroupCellsSingleAndRectangles(t *testing.T) {
	got, err := GroupCells([]PaintCell{
		{Col: 1, Row: 1, QOffset: -0.40},
		{Col: 2, Row: 1, QOffset: -0.40},
		{Col: 1, Row: 2, QOffset: -0.40},
		{Col: 2, Row: 2, QOffset: -0.40},
		{Col: 5, Row: 3, QOffset: -0.10},
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []roi.QPMapBlock{
		{Col: 1, Row: 1, W: 2, H: 2, QOffset: -0.40},
		{Col: 5, Row: 3, W: 1, H: 1, QOffset: -0.10},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GroupCells = %+v, want %+v", got, want)
	}
}

func TestGroupCellsKeepsDifferentColorsAndGapsSeparate(t *testing.T) {
	got, err := GroupCells([]PaintCell{
		{Col: 0, Row: 0, QOffset: -0.40},
		{Col: 1, Row: 0, QOffset: -0.25},
		{Col: 3, Row: 0, QOffset: -0.40},
		{Col: 3, Row: 1, QOffset: -0.40},
		{Col: 4, Row: 1, QOffset: -0.40},
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []roi.QPMapBlock{
		{Col: 0, Row: 0, W: 1, H: 1, QOffset: -0.40},
		{Col: 1, Row: 0, W: 1, H: 1, QOffset: -0.25},
		{Col: 3, Row: 0, W: 1, H: 2, QOffset: -0.40},
		{Col: 4, Row: 1, W: 1, H: 1, QOffset: -0.40},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GroupCells = %+v, want %+v", got, want)
	}
}

func TestGroupCellsRejectsInvalidInput(t *testing.T) {
	for _, cells := range [][]PaintCell{
		{{Col: -1, Row: 0, QOffset: -0.40}},
		{{Col: 1, Row: 0, QOffset: -0.40}, {Col: 1, Row: 0, QOffset: -0.25}},
		{{Col: 1, Row: 0, QOffset: -0.33}},
	} {
		if _, err := GroupCells(cells); err == nil {
			t.Fatalf("GroupCells(%+v) returned nil error", cells)
		}
	}
}

func TestWriteConfigWritesEncoderReadyYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "generated.yaml")
	opts := DefaultOptions()
	opts.Input = "examples/ball.mp4"
	req := ConfigRequest{
		OutDir:        "out/painted",
		TargetBitrate: "750k",
		Encoder:       "libx264",
		BitrateWindow: 2.5,
		ROIBlockSize:  64,
	}
	blocks := []roi.QPMapBlock{
		{Col: 1, Row: 2, W: 2, H: 1, QOffset: -0.40},
	}

	if err := WriteConfig(path, opts, req, blocks); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var got generatedConfig
	if err := yaml.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}

	if got.Input != "examples/ball.mp4" || got.OutDir != "out/painted" {
		t.Fatalf("unexpected input/out: %+v", got)
	}
	if got.Mode != "blocks" || got.ROIControl != "qp-map" || got.ROIBlockSize != 64 {
		t.Fatalf("unexpected ROI fields: %+v", got)
	}
	if got.TargetBitrate != "750k" || got.Encoder != "libx264" || got.BitrateWindow != 2.5 {
		t.Fatalf("unexpected encoder fields: %+v", got)
	}
	if !reflect.DeepEqual(got.ROIBlocks, blocks) {
		t.Fatalf("ROIBlocks = %+v, want %+v", got.ROIBlocks, blocks)
	}
	if got.ROIRateControl != "abr" || got.ROITwoPass || !got.OverlayBitrate || got.Metrics || got.Serve {
		t.Fatalf("unexpected defaults: %+v", got)
	}
}
