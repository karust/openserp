package fpcheck

import (
	"context"

	"github.com/go-rod/rod"
)

// Detection represents a single anti-bot signal verdict from a detector page.
type Detection struct {
	Detected    bool     `json:"detected"`
	Description string   `json:"description"`
	Severity    string   `json:"severity,omitempty"`
	Numeric     *float64 `json:"numeric,omitempty"`
}

// Summary contains aggregate counters for a detector report.
type Summary struct {
	Passed   int      `json:"passed"`
	Failed   int      `json:"failed"`
	Critical []string `json:"critical,omitempty"`
}

// Report is the normalized output for one detector run.
type Report struct {
	DetectorName  string               `json:"detector_name"`
	URL           string               `json:"url"`
	UseStealth    bool                 `json:"use_stealth"`
	CapturedAtUTC string               `json:"captured_at_utc"`
	Screenshot    string               `json:"screenshot_path"`
	Detections    map[string]Detection `json:"detections"`
	Summary       Summary              `json:"summary"`
	RawNotes      string               `json:"raw_notes,omitempty"`
}

// Detector knows how to extract normalized anti-bot verdicts from one site.
type Detector interface {
	Name() string
	URL() string
	Extract(ctx context.Context, page *rod.Page) (map[string]Detection, string, error)
}

// BrowserNavigator is the minimal browser contract used by fpcheck runner.
type BrowserNavigator interface {
	Navigate(ctx context.Context, URL string) (*rod.Page, error)
}
