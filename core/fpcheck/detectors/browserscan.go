package detectors

import (
	"context"
	"fmt"
	"time"

	"github.com/go-rod/rod"
	"github.com/karust/openserp/core/fpcheck"
)

const browserscanURL = "https://www.browserscan.net/bot-detection"

type BrowserScan struct{}

func NewBrowserScan() fpcheck.Detector {
	return BrowserScan{}
}

func (BrowserScan) Name() string {
	return "browserscan"
}

func (BrowserScan) URL() string {
	return browserscanURL
}

func (BrowserScan) Extract(ctx context.Context, page *rod.Page) (map[string]fpcheck.Detection, string, error) {
	err := waitFor(ctx, 25*time.Second, 250*time.Millisecond, func() (bool, error) {
		res, err := page.Eval(`() => {
			const text = (document.body && document.body.innerText ? document.body.innerText : "").toLowerCase();
			return text.includes("bot") && (text.includes("detected") || text.includes("pass") || text.includes("fail"));
		}`)
		if err != nil {
			return false, nil
		}

		var ready bool
		if err := res.Value.Unmarshal(&ready); err != nil {
			return false, nil
		}
		return ready, nil
	})
	if err != nil {
		return nil, "", fmt.Errorf("browserscan readiness: %w", err)
	}

	rows, err := parseRows(page)
	if err != nil {
		return nil, "", err
	}
	if len(rows) == 0 {
		return nil, "", fmt.Errorf("browserscan detector rows not found")
	}

	detections := rowsToDetections(rows, []string{"bot", "webdriver", "automation", "headless", "fingerprint"})
	if len(detections) == 0 {
		return nil, "", fmt.Errorf("browserscan detections are empty")
	}

	return detections, "", nil
}
