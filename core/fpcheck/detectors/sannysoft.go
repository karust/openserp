package detectors

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/karust/openserp/core/fpcheck"
)

const sannysoftURL = "https://bot.sannysoft.com"

type Sannysoft struct{}

func NewSannysoft() fpcheck.Detector {
	return Sannysoft{}
}

func (Sannysoft) Name() string {
	return "sannysoft"
}

func (Sannysoft) URL() string {
	return sannysoftURL
}

func (Sannysoft) Extract(ctx context.Context, page *rod.Page) (map[string]fpcheck.Detection, string, error) {
	var checks []sannysoftRow
	err := waitFor(ctx, 20*time.Second, 250*time.Millisecond, func() (bool, error) {
		hasRows, _, err := page.Has("table tr")
		if err != nil {
			return false, fmt.Errorf("table probe failed: %w", err)
		}
		if !hasRows {
			return false, nil
		}

		rows, err := extractSannysoftRows(page)
		if err != nil {
			return false, nil
		}
		checks = rows
		return len(checks) >= 5, nil
	})
	if err != nil {
		return nil, "", err
	}
	if len(checks) == 0 {
		return nil, "", fmt.Errorf("sannysoft did not return any fingerprint check rows")
	}

	detections := make(map[string]fpcheck.Detection, len(checks))
	for _, check := range checks {
		key := normalizeKey(check.Name)
		if key == "unknown" {
			continue
		}

		detected := check.Status == "fail"
		severity := ""
		if detected && strings.Contains(key, "webdriver") {
			severity = "critical"
		}

		description := check.Status
		if description == "" {
			description = "unknown"
		}

		detections[key] = fpcheck.Detection{
			Detected:    detected,
			Description: description,
			Severity:    severity,
		}
	}

	return detections, "", nil
}

type sannysoftRow struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

func extractSannysoftRows(page *rod.Page) ([]sannysoftRow, error) {
	res, err := page.Eval(`() => {
		const parseRGB = (value) => {
			const match = (value || "").match(/rgba?\((\d+),\s*(\d+),\s*(\d+)/i);
			if (!match) return null;
			return [parseInt(match[1], 10), parseInt(match[2], 10), parseInt(match[3], 10)];
		};

		const classify = (text, className, bgColor) => {
			const normalizedText = (text || "").toLowerCase();
			const normalizedClass = (className || "").toLowerCase();
			const rgb = parseRGB(bgColor);

			if (/\b(fail(?:ed)?|detected|bot)\b/.test(normalizedText)) return "fail";
			if (/\b(pass(?:ed)?|ok|success)\b/.test(normalizedText)) return "pass";
			if (/\b(fail(?:ed)?|error|danger|bad|red)\b/.test(normalizedClass)) return "fail";
			if (/\b(pass(?:ed)?|success|ok|good|green)\b/.test(normalizedClass)) return "pass";

			if (rgb) {
				const [r, g, b] = rgb;
				if (r > g + 35 && r > b + 35) return "fail";
				if (g > r + 20 && g > b + 20) return "pass";
			}

			return "unknown";
		};

		const rows = Array.from(document.querySelectorAll("table tr"));
		const seen = new Set();
		const checks = [];

		for (const row of rows) {
			const cells = Array.from(row.querySelectorAll("th, td"));
			if (cells.length < 2) continue;

			const nameCell = cells[0];
			const name = (nameCell.innerText || nameCell.textContent || "").replace(/\s+/g, " ").trim();
			if (!name) continue;
			if (/^(test(\s+name)?|property|status|result)$/i.test(name)) continue;

			const resultCells = cells.slice(1);
			const statusCell =
				resultCells.find((cell) => /\b(result|pass|fail|success|ok)\b/i.test(cell.className || "")) ||
				resultCells.find((cell) => {
					const bg = window.getComputedStyle(cell).backgroundColor || "";
					return bg !== "" && bg !== "transparent" && bg !== "rgba(0, 0, 0, 0)";
				}) ||
				resultCells[0];
			if (!statusCell) continue;

			const statusText = (statusCell.innerText || statusCell.textContent || "").replace(/\s+/g, " ").trim();
			const className = (row.className || "") + " " + (statusCell.className || "");
			const bgColor = window.getComputedStyle(statusCell).backgroundColor || "";
			const dedupeKey = name.toLowerCase();
			if (seen.has(dedupeKey)) continue;
			seen.add(dedupeKey);

			checks.push({
				name,
				status: classify(statusText, className, bgColor),
			});
		}

		return checks;
	}`)
	if err != nil {
		return nil, err
	}

	var checks []sannysoftRow
	if err := res.Value.Unmarshal(&checks); err != nil {
		return nil, fmt.Errorf("decode sannysoft results: %w", err)
	}

	return checks, nil
}
