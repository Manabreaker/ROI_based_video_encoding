package roi

// Config holds CLI options for input/output, ROI selection, encoding, metrics, and serving.
type Config struct {
	// Input/output.
	Input  string
	OutDir string

	// ROI selection.
	Mode      string
	ROIString string

	// Bitrate target for the generated ROI output.
	TargetBitrate string
	Tolerance     float64

	// ROI quality policy.
	FitROI              bool
	ROIHighQualityCRF   int
	ROIMinCRF           int
	ROIMaxCRFIfNeeded   int
	AllowROIQualityLoss bool

	// Periphery degradation settings.
	ManualPeripheryScale float64
	ManualBlurRadius     int
	MiddleMargin         float64
	MiddleScale          float64
	MiddleBlurRadius     int
	ROIMinScale          float64
	ROIMaxBlur           int

	// ROI encoder rate control and candidate scoring.
	ROIRateControl       string
	ROITwoPass           bool
	ROIFitMetric         bool
	ROIPSNRTieDB         float64
	ROIMaxrateMultiplier float64
	ROIBufsizeSeconds    float64

	// Encoder tuning.
	VideoEncoder  string
	Preset        string
	NVENCPreset   string
	FitIterations int

	// Motion-based ROI detection.
	MotionWindow float64
	MotionThresh int
	ROIMargin    float64

	// Dynamic bitrate overlay.
	OverlayBitrate     bool
	BitrateWindow      float64
	MaxBitrateOverlays int

	// Reports and local preview server.
	Metrics bool

	Serve    bool
	HTTPAddr string
	KeepTemp bool
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

// Artifact describes a generated or referenced output file in the report.
type Artifact struct {
	Path        string  `json:"path"`
	SizeBytes   int64   `json:"size_bytes"`
	BitrateKbps float64 `json:"bitrate_kbps,omitempty"`
}

// Candidate records one tried ROI encoding variant before the final choice.
type Candidate struct {
	Kind        string  `json:"kind"`
	Encoder     string  `json:"encoder,omitempty"`
	CRF         int     `json:"crf"`
	RateControl string  `json:"rate_control,omitempty"`
	Scale       float64 `json:"periphery_scale,omitempty"`
	Blur        int     `json:"periphery_blur,omitempty"`
	MiddleScale float64 `json:"middle_scale,omitempty"`
	MiddleBlur  int     `json:"middle_blur,omitempty"`
	Kbps        float64 `json:"bitrate_kbps"`
	ROIYPSNR    float64 `json:"roi_psnr_y_db,omitempty"`
	Note        string  `json:"note,omitempty"`
	Path        string  `json:"-"`
}

// CandidateSummary is the report-safe view of a Candidate.
type CandidateSummary struct {
	Kind        string  `json:"kind"`
	Encoder     string  `json:"encoder,omitempty"`
	CRF         int     `json:"crf"`
	RateControl string  `json:"rate_control,omitempty"`
	Scale       float64 `json:"periphery_scale,omitempty"`
	Blur        int     `json:"periphery_blur,omitempty"`
	MiddleScale float64 `json:"middle_scale,omitempty"`
	MiddleBlur  int     `json:"middle_blur,omitempty"`
	Kbps        float64 `json:"bitrate_kbps"`
	ROIYPSNR    float64 `json:"roi_psnr_y_db,omitempty"`
	Note        string  `json:"note,omitempty"`
}

// EncodeDecision explains the chosen settings and measured result for one side of the comparison.
type EncodeDecision struct {
	Name            string             `json:"name"`
	Encoder         string             `json:"encoder,omitempty"`
	TargetKbps      float64            `json:"target_kbps,omitempty"`
	ActualKbps      float64            `json:"actual_kbps"`
	WithinTolerance bool               `json:"within_tolerance"`
	CRF             int                `json:"crf,omitempty"`
	RateControl     string             `json:"rate_control,omitempty"`
	Scale           float64            `json:"periphery_scale,omitempty"`
	Blur            int                `json:"periphery_blur,omitempty"`
	MiddleScale     float64            `json:"middle_scale,omitempty"`
	MiddleBlur      int                `json:"middle_blur,omitempty"`
	MiddleMargin    float64            `json:"middle_margin,omitempty"`
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
