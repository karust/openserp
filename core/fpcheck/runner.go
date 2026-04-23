package fpcheck

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// RunOptions controls detector run behavior.
type RunOptions struct {
	ArtifactDir     string
	WaitBeforeClose time.Duration
}

// Run navigates the given browser to detector URL, extracts verdicts,
// captures a screenshot artifact, and returns a normalized report.
func Run(ctx context.Context, browser BrowserNavigator, detector Detector, artifactDir string) (Report, error) {
	return RunWithOptions(ctx, browser, detector, RunOptions{
		ArtifactDir: artifactDir,
	})
}

// RunWithOptions navigates the given browser to detector URL, extracts
// verdicts, captures a screenshot artifact, optionally waits, and returns a
// normalized report.
func RunWithOptions(ctx context.Context, browser BrowserNavigator, detector Detector, options RunOptions) (Report, error) {
	report := Report{
		DetectorName: detector.Name(),
		URL:          detector.URL(),
		Detections:   map[string]Detection{},
	}

	artifactDir := strings.TrimSpace(options.ArtifactDir)
	if artifactDir == "" {
		artifactDir = "testdata"
	}

	screenshotPath := filepath.Join(artifactDir, fmt.Sprintf("fpcheck_%s.png", sanitizeFilePart(detector.Name())))
	report.Screenshot = filepath.ToSlash(screenshotPath)

	page, err := browser.Navigate(ctx, detector.URL())
	if err != nil {
		return report, fmt.Errorf("navigate %s: %w", detector.Name(), err)
	}
	defer func() {
		if options.WaitBeforeClose > 0 {
			_ = sleepWithContext(ctx, options.WaitBeforeClose)
		}
		closePageWithTimeout(context.Background(), page, time.Second)
	}()

	detections, rawNotes, err := detector.Extract(ctx, page)
	if err != nil {
		_ = saveScreenshot(page, screenshotPath)
		return report, fmt.Errorf("extract %s: %w", detector.Name(), err)
	}

	if err := saveScreenshot(page, screenshotPath); err != nil {
		return report, fmt.Errorf("capture screenshot %s: %w", detector.Name(), err)
	}

	report.CapturedAtUTC = time.Now().UTC().Format(time.RFC3339)
	report.Detections = detections
	report.Summary = summarize(detections)
	report.RawNotes = strings.TrimSpace(rawNotes)

	return report, nil
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func summarize(detections map[string]Detection) Summary {
	summary := Summary{}
	for key, detection := range detections {
		if detection.Detected {
			summary.Failed++
			if strings.EqualFold(strings.TrimSpace(detection.Severity), "critical") {
				summary.Critical = append(summary.Critical, key)
			}
			continue
		}
		summary.Passed++
	}
	sort.Strings(summary.Critical)
	return summary
}

func saveScreenshot(page *rod.Page, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create screenshot directory: %w", err)
	}

	bytes, err := page.Screenshot(true, nil)
	if err != nil {
		return fmt.Errorf("capture screenshot: %w", err)
	}
	if err := os.WriteFile(path, bytes, 0o644); err != nil {
		return fmt.Errorf("write screenshot file %s: %w", path, err)
	}
	return nil
}

func closePageWithTimeout(ctx context.Context, page *rod.Page, timeout time.Duration) {
	if page == nil {
		return
	}
	if timeout <= 0 {
		timeout = time.Second
	}
	closeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	pageWithTimeout := page.Context(closeCtx)
	info, _ := pageWithTimeout.Info()

	_ = pageWithTimeout.Close()

	if info != nil && info.BrowserContextID != "" {
		_ = (proto.TargetDisposeBrowserContext{BrowserContextID: info.BrowserContextID}).Call(page.Browser().Context(closeCtx))
	}
}

func sanitizeFilePart(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "detector"
	}

	parts := strings.FieldsFunc(value, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	})
	if len(parts) == 0 {
		return "detector"
	}

	return strings.Join(parts, "_")
}
