package detectors

import (
	"context"
	"fmt"
	"time"

	"github.com/go-rod/rod"
	"github.com/karust/openserp/core/fpcheck"
)

const rebrowserURL = "https://bot-detector.rebrowser.net/"

type Rebrowser struct{}

func NewRebrowser() fpcheck.Detector {
	return Rebrowser{}
}

func (Rebrowser) Name() string {
	return "rebrowser"
}

func (Rebrowser) URL() string {
	return rebrowserURL
}

func (Rebrowser) Extract(ctx context.Context, page *rod.Page) (map[string]fpcheck.Detection, string, error) {
	err := waitFor(ctx, 20*time.Second, 250*time.Millisecond, func() (bool, error) {
		hasBody, _, err := page.Has("body")
		if err != nil {
			return false, err
		}
		if !hasBody {
			return false, nil
		}

		res, err := page.Eval(`() => {
			const text = (document.body && document.body.innerText ? document.body.innerText : "").toLowerCase();
			return text.includes("runtimeenableleak") || text.includes("sourceurlleak") || text.includes("mainworldexecution") || text.includes("webdriver");
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
		return nil, "", fmt.Errorf("rebrowser readiness: %w", err)
	}

	rows, err := parseRows(page)
	if err != nil {
		return nil, "", err
	}
	if len(rows) == 0 {
		return nil, "", fmt.Errorf("rebrowser detector rows not found")
	}

	detections := rowsToDetections(rows, []string{
		"runtimeenableleak",
		"sourceurlleak",
		"mainworldexecution",
		"webdriver",
		"automation",
	})

	if len(detections) == 0 {
		return nil, "", fmt.Errorf("rebrowser detections are empty")
	}

	return detections, "", nil
}
