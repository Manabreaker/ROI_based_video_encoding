package roi

import (
	"strings"
	"testing"
)

func TestEscapeDrawText(t *testing.T) {
	got := escapeDrawText("a:b,c%'\\")
	want := `a\:b\,c\%\'\\`

	if got != want {
		t.Fatalf("escapeDrawText = %q, want %q", got, want)
	}
}

func TestPanelBaseFiltersForSourceBaseline(t *testing.T) {
	filters := panelBaseFilters("INPUT baseline", EncodeDecision{
		ActualKbps:  855,
		RateControl: "source",
		SizeBytes:   1024,
	}, false)
	joined := strings.Join(filters, ",")

	for _, part := range []string{
		"INPUT baseline",
		"source avg 855 kbps",
		"1.0 KB",
		`original input\, not re-encoded`,
	} {
		if !strings.Contains(joined, part) {
			t.Fatalf("source panel does not contain %q:\n%s", part, joined)
		}
	}
}

func TestPanelBaseFiltersForThreeZoneROI(t *testing.T) {
	filters := panelBaseFilters("ROI output", EncodeDecision{
		TargetKbps:      500,
		ActualKbps:      405,
		WithinTolerance: false,
		RateControl:     "abr",
		Scale:           0.44,
		Blur:            2,
		MiddleScale:     0.67,
		MiddleBlur:      1,
	}, true)
	joined := strings.Join(filters, ",")

	for _, part := range []string{
		"ROI output",
		"closest possible | ABR target-rate",
		"G ROI | O 0.67 b1 | R 0.44 b2",
	} {
		if !strings.Contains(joined, part) {
			t.Fatalf("ROI panel does not contain %q:\n%s", part, joined)
		}
	}
}
