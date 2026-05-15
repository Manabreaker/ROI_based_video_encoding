package mapui

import "github.com/Manabreaker/ROI_based_video_encoding/internal/roi"

// Options contains the local UI server configuration.
type Options struct {
	Input         string
	ConfigOut     string
	Addr          string
	OpenBrowser   bool
	OutDir        string
	TargetBitrate string
	Encoder       string
	Preset        string
	NVENCPreset   string
	BitrateWindow float64
	ROIBlockSize  int
}

// PaletteEntry describes one selectable block quality level in the UI.
type PaletteEntry struct {
	Name    string  `json:"name"`
	Color   string  `json:"color"`
	QOffset float64 `json:"qoffset"`
}

// PaintCell is one painted grid cell from the browser UI.
type PaintCell struct {
	Col     int     `json:"col"`
	Row     int     `json:"row"`
	QOffset float64 `json:"qoffset"`
}

// ConfigRequest is posted by the browser when the user confirms the map.
type ConfigRequest struct {
	OutDir        string      `json:"out"`
	TargetBitrate string      `json:"targetBitrate"`
	Encoder       string      `json:"encoder"`
	BitrateWindow float64     `json:"bitrateWindow"`
	ROIBlockSize  int         `json:"roiBlockSize"`
	Cells         []PaintCell `json:"cells"`
}

type configResponse struct {
	Path       string `json:"path"`
	BlockCount int    `json:"blockCount"`
	RectCount  int    `json:"rectCount"`
	Command    string `json:"command"`
}

type metaResponse struct {
	Input         string         `json:"input"`
	ConfigOut     string         `json:"configOut"`
	OutDir        string         `json:"out"`
	TargetBitrate string         `json:"targetBitrate"`
	Encoder       string         `json:"encoder"`
	Preset        string         `json:"preset"`
	NVENCPreset   string         `json:"nvencPreset"`
	BitrateWindow float64        `json:"bitrateWindow"`
	ROIBlockSize  int            `json:"roiBlockSize"`
	Palette       []PaletteEntry `json:"palette"`
}

type generatedConfig struct {
	Input          string           `yaml:"input"`
	OutDir         string           `yaml:"out"`
	Mode           string           `yaml:"mode"`
	ROIControl     string           `yaml:"roi-control"`
	ROIBlockSize   int              `yaml:"roi-block-size"`
	ROIBlocks      []roi.QPMapBlock `yaml:"roi-blocks"`
	TargetBitrate  string           `yaml:"target-bitrate"`
	ROIRateControl string           `yaml:"roi-rate-control"`
	ROITwoPass     bool             `yaml:"roi-two-pass"`
	Encoder        string           `yaml:"encoder"`
	Preset         string           `yaml:"preset"`
	NVENCPreset    string           `yaml:"nvenc-preset"`
	BitrateWindow  float64          `yaml:"bitrate-window"`
	OverlayBitrate bool             `yaml:"overlay-bitrate"`
	Metrics        bool             `yaml:"metrics"`
	Serve          bool             `yaml:"serve"`
}
