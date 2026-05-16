package roi

// Config holds CLI options for input/output, ROI selection, encoding, metrics, and serving.
type Config struct {
	// Input/output.
	Input  string `yaml:"input"`
	OutDir string `yaml:"out"`

	// ROI selection.
	Mode         string       `yaml:"mode"`
	ROIString    string       `yaml:"roi"`
	ROIBlockSize int          `yaml:"roi-block-size"`
	ROIBlocks    []QPMapBlock `yaml:"roi-blocks"`

	// Bitrate target for the generated ROI output.
	TargetBitrate string  `yaml:"target-bitrate"`
	Tolerance     float64 `yaml:"tolerance"`

	// ROI quality policy.
	ROIControl          string  `yaml:"roi-control"`
	ROIQOffset          float64 `yaml:"roi-qoffset"`
	ROIMiddleQOffset    float64 `yaml:"roi-middle-qoffset"`
	FitROI              bool    `yaml:"fit-roi"`
	ROIHighQualityCRF   int     `yaml:"roi-crf"`
	ROIMinCRF           int     `yaml:"roi-min-crf"`
	ROIMaxCRFIfNeeded   int     `yaml:"roi-max-crf-if-needed"`
	AllowROIQualityLoss bool    `yaml:"allow-roi-quality-loss"`

	// Periphery degradation settings.
	ManualPeripheryScale float64 `yaml:"periphery-scale"`
	ManualBlurRadius     int     `yaml:"blur"`
	MiddleMargin         float64 `yaml:"middle-margin"`
	MiddleScale          float64 `yaml:"middle-scale"`
	MiddleBlurRadius     int     `yaml:"middle-blur"`
	ROIMinScale          float64 `yaml:"roi-min-scale"`
	ROIMaxBlur           int     `yaml:"roi-max-blur"`

	// ROI encoder rate control and candidate scoring.
	ROIRateControl       string  `yaml:"roi-rate-control"`
	ROITwoPass           bool    `yaml:"roi-two-pass"`
	ROIFitMetric         bool    `yaml:"roi-fit-metric"`
	ROIPSNRTieDB         float64 `yaml:"roi-psnr-tie-db"`
	ROIMaxrateMultiplier float64 `yaml:"roi-maxrate-multiplier"`
	ROIBufsizeSeconds    float64 `yaml:"roi-bufsize-seconds"`

	// Encoder tuning.
	VideoEncoder  string `yaml:"encoder"`
	Preset        string `yaml:"preset"`
	NVENCPreset   string `yaml:"nvenc-preset"`
	FitIterations int    `yaml:"fit-iterations"`

	// Motion-based ROI detection.
	MotionWindow float64 `yaml:"motion-window"`
	MotionThresh int     `yaml:"motion-threshold"`
	ROIMargin    float64 `yaml:"roi-margin"`

	// Model-based CV ROI detection.
	CVModel       string  `yaml:"cv-model"`
	CVMinScore    float64 `yaml:"cv-min-score"`
	CVSampleCount int     `yaml:"cv-samples"`
	CVFrameWidth  int     `yaml:"cv-frame-width"`

	// Dynamic bitrate overlay.
	OverlayBitrate     bool    `yaml:"overlay-bitrate"`
	BitrateWindow      float64 `yaml:"bitrate-window"`
	MaxBitrateOverlays int     `yaml:"max-bitrate-overlays"`

	// Reports and local preview server.
	Metrics bool `yaml:"metrics"`

	Serve    bool   `yaml:"serve"`
	HTTPAddr string `yaml:"http"`
	KeepTemp bool   `yaml:"keep-temp"`
}

// VideoInfo describes the probed primary video stream.
type VideoInfo struct {
	Width    int     `json:"width"`
	Height   int     `json:"height"`
	Duration float64 `json:"duration_seconds"`
	FPS      float64 `json:"fps"`
}

// ROI defines the rectangular region that should remain visually important.
type ROI struct {
	X      int    `json:"x"`
	Y      int    `json:"y"`
	W      int    `json:"w"`
	H      int    `json:"h"`
	Source string `json:"source"`
}

// TimedROI describes one time segment where the ROI should be applied.
type TimedROI struct {
	StartSeconds float64 `json:"start_seconds"`
	EndSeconds   float64 `json:"end_seconds"`
	ROI          ROI     `json:"roi"`
}

// ROISelection carries both the summary ROI and an optional moving ROI timeline.
type ROISelection struct {
	ROI      ROI
	Timeline []TimedROI
}

// Artifact describes a generated or referenced output file in the report.
type Artifact struct {
	Path        string  `json:"path"`
	SizeBytes   int64   `json:"size_bytes"`
	BitrateKbps float64 `json:"bitrate_kbps,omitempty"`
}

// Candidate records one tried ROI encoding variant before the final choice.
type Candidate struct {
	Kind          string  `json:"kind"`
	Encoder       string  `json:"encoder,omitempty"`
	ROIControl    string  `json:"roi_control,omitempty"`
	CRF           int     `json:"crf"`
	RateControl   string  `json:"rate_control,omitempty"`
	ROIQOffset    float64 `json:"roi_qoffset,omitempty"`
	MiddleQOffset float64 `json:"middle_qoffset,omitempty"`
	Scale         float64 `json:"periphery_scale,omitempty"`
	Blur          int     `json:"periphery_blur,omitempty"`
	MiddleScale   float64 `json:"middle_scale,omitempty"`
	MiddleBlur    int     `json:"middle_blur,omitempty"`
	ROIBlockSize  int     `json:"roi_block_size,omitempty"`
	ROIBlockCount int     `json:"roi_block_count,omitempty"`
	Kbps          float64 `json:"bitrate_kbps"`
	ROIYPSNR      float64 `json:"roi_psnr_y_db,omitempty"`
	Note          string  `json:"note,omitempty"`
	Path          string  `json:"-"`
}

// CandidateSummary is the report-safe view of a Candidate.
type CandidateSummary struct {
	Kind          string  `json:"kind"`
	Encoder       string  `json:"encoder,omitempty"`
	ROIControl    string  `json:"roi_control,omitempty"`
	CRF           int     `json:"crf"`
	RateControl   string  `json:"rate_control,omitempty"`
	ROIQOffset    float64 `json:"roi_qoffset,omitempty"`
	MiddleQOffset float64 `json:"middle_qoffset,omitempty"`
	Scale         float64 `json:"periphery_scale,omitempty"`
	Blur          int     `json:"periphery_blur,omitempty"`
	MiddleScale   float64 `json:"middle_scale,omitempty"`
	MiddleBlur    int     `json:"middle_blur,omitempty"`
	ROIBlockSize  int     `json:"roi_block_size,omitempty"`
	ROIBlockCount int     `json:"roi_block_count,omitempty"`
	Kbps          float64 `json:"bitrate_kbps"`
	ROIYPSNR      float64 `json:"roi_psnr_y_db,omitempty"`
	Note          string  `json:"note,omitempty"`
}

// EncodeDecision explains the chosen settings and measured result for one side of the comparison.
type EncodeDecision struct {
	Name            string             `json:"name"`
	Encoder         string             `json:"encoder,omitempty"`
	ROIControl      string             `json:"roi_control,omitempty"`
	TargetKbps      float64            `json:"target_kbps,omitempty"`
	ActualKbps      float64            `json:"actual_kbps"`
	WithinTolerance bool               `json:"within_tolerance"`
	CRF             int                `json:"crf,omitempty"`
	RateControl     string             `json:"rate_control,omitempty"`
	ROIQOffset      float64            `json:"roi_qoffset,omitempty"`
	MiddleQOffset   float64            `json:"middle_qoffset,omitempty"`
	Scale           float64            `json:"periphery_scale,omitempty"`
	Blur            int                `json:"periphery_blur,omitempty"`
	MiddleScale     float64            `json:"middle_scale,omitempty"`
	MiddleBlur      int                `json:"middle_blur,omitempty"`
	MiddleMargin    float64            `json:"middle_margin,omitempty"`
	ROIBlockSize    int                `json:"roi_block_size,omitempty"`
	ROIBlockCount   int                `json:"roi_block_count,omitempty"`
	ROIYPSNR        float64            `json:"roi_psnr_y_db,omitempty"`
	SizeBytes       int64              `json:"size_bytes,omitempty"`
	Note            string             `json:"note,omitempty"`
	Candidates      []CandidateSummary `json:"candidates,omitempty"`
}

// BitrateSample stores measured video-packet bitrate for one time window.
type BitrateSample struct {
	Start float64 `json:"start_seconds"`
	End   float64 `json:"end_seconds"`
	Kbps  float64 `json:"kbps"`
}

// BitrateSummary aggregates bitrate windows for reporting and overlays.
type BitrateSummary struct {
	AverageKbps float64 `json:"average_kbps"`
	P50Kbps     float64 `json:"p50_kbps"`
	P95Kbps     float64 `json:"p95_kbps"`
	MinKbps     float64 `json:"min_kbps"`
	MaxKbps     float64 `json:"max_kbps"`
}

// BitrateReport stores the dynamic bitrate measurements for baseline and ROI output.
type BitrateReport struct {
	WindowSeconds float64         `json:"window_seconds"`
	Baseline      []BitrateSample `json:"baseline"`
	ROI           []BitrateSample `json:"roi"`
	Summary       struct {
		Baseline BitrateSummary `json:"baseline"`
		ROI      BitrateSummary `json:"roi"`
	} `json:"summary"`
}

// QualityMetric stores one ROI-crop PSNR measurement.
type QualityMetric struct {
	Name         string  `json:"name"`
	Scope        string  `json:"scope"`
	Output       string  `json:"output"`
	AverageYDB   float64 `json:"average_y_db,omitempty"`
	AverageYText string  `json:"average_y_text,omitempty"`
	RawLog       string  `json:"raw_log,omitempty"`
}

// QualityReport groups objective ROI quality metrics.
type QualityReport struct {
	ROI  ROI             `json:"roi"`
	PSNR []QualityMetric `json:"psnr"`
}

// Report is the top-level JSON summary written after processing.
type Report struct {
	CreatedAt     string           `json:"created_at"`
	Input         string           `json:"input"`
	Mode          string           `json:"mode"`
	TargetBitrate string           `json:"target_bitrate"`
	TargetKbps    float64          `json:"target_kbps"`
	Video         VideoInfo        `json:"video"`
	ROI           ROI              `json:"roi"`
	ROITimeline   []TimedROI       `json:"roi_timeline,omitempty"`
	ROIBlockSize  int              `json:"roi_block_size,omitempty"`
	ROIBlocks     []QPMapBlock     `json:"roi_blocks,omitempty"`
	Decisions     []EncodeDecision `json:"decisions"`
	Artifacts     []Artifact       `json:"artifacts"`
	Notes         []string         `json:"notes"`
}

// ffprobeVideoJSON mirrors the subset of ffprobe stream metadata this tool reads.
type ffprobeVideoJSON struct {
	Streams []struct {
		Width        int    `json:"width"`
		Height       int    `json:"height"`
		RFrameRate   string `json:"r_frame_rate"`
		AvgFrameRate string `json:"avg_frame_rate"`
		Duration     string `json:"duration"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

// ffprobePacketsJSON mirrors the packet data needed for bitrate windows.
type ffprobePacketsJSON struct {
	Packets []struct {
		PTS  string `json:"pts_time"`
		DTS  string `json:"dts_time"`
		Size string `json:"size"`
	} `json:"packets"`
}

// peripherySetting describes one candidate degradation level outside the ROI.
type peripherySetting struct {
	Scale float64
	Blur  int
}

// QPMapBlock identifies one or more 64x64-style QP-map blocks by grid position.
type QPMapBlock struct {
	Col     int     `yaml:"col" json:"col"`
	Row     int     `yaml:"row" json:"row"`
	W       int     `yaml:"w,omitempty" json:"w,omitempty"`
	H       int     `yaml:"h,omitempty" json:"h,omitempty"`
	QOffset float64 `yaml:"qoffset" json:"qoffset"`
}
