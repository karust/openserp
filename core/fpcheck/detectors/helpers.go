package detectors

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/karust/openserp/core/fpcheck"
)

var (
	reNonAlphanumUnderscore = regexp.MustCompile(`[^a-z0-9_]+`)
	reMultiUnderscore       = regexp.MustCompile(`_+`)
	reExtractScore          = regexp.MustCompile(`(?i)(score|overall|risk)[^\d]{0,20}(\d+(?:\.\d+)?)`)
)

type detectorRow struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

func waitFor(ctx context.Context, timeout time.Duration, poll time.Duration, probe func() (bool, error)) error {
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	if poll <= 0 {
		poll = 250 * time.Millisecond
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}

		ok, err := probe()
		if err != nil {
			return err
		}
		if ok {
			return nil
		}

		timer := time.NewTimer(poll)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}
	}

	return fmt.Errorf("ready condition not met after %s", timeout)
}

func normalizeKey(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return "unknown"
	}

	replacer := strings.NewReplacer(
		" ", "_",
		"-", "_",
		"/", "_",
		"\\", "_",
		":", "_",
		".", "_",
	)
	name = replacer.Replace(name)
	name = reNonAlphanumUnderscore.ReplaceAllString(name, "")
	name = reMultiUnderscore.ReplaceAllString(name, "_")
	name = strings.Trim(name, "_")
	if name == "" {
		return "unknown"
	}
	return name
}

func classifyStatus(status string) bool {
	value := strings.ToLower(strings.TrimSpace(status))
	if value == "" {
		return false
	}

	if strings.Contains(value, "🔴") {
		return true
	}
	if strings.Contains(value, "🟢") || strings.Contains(value, "⚪") {
		return false
	}

	notDetected := []string{"not detected", "not found", "clean", "clear", "pass", "passed", "ok", "safe", "green", "false", "no"}
	for _, marker := range notDetected {
		if strings.Contains(value, marker) {
			return false
		}
	}

	detected := []string{"detected", "fail", "failed", "bot", "leak", "warning", "critical", "red", "true", "yes"}
	for _, marker := range detected {
		if strings.Contains(value, marker) {
			return true
		}
	}

	return false
}

func parseRows(page *rod.Page) ([]detectorRow, error) {
	res, err := page.Eval(`() => {
		const normalize = (value) => (value || "").replace(/\s+/g, " ").trim();
		const rows = [];
		const seen = new Set();

		const tableRows = Array.from(document.querySelectorAll("table tr"));
		for (const row of tableRows) {
			const cells = Array.from(row.querySelectorAll("th, td"));
			if (cells.length < 2) continue;
			const name = normalize(cells[0].innerText || cells[0].textContent || "");
			if (!name) continue;
			if (/^(test(\s+name)?|property|status|result|check)$/i.test(name)) continue;

			const valueCell = cells[cells.length - 1];
			const status = normalize(valueCell.innerText || valueCell.textContent || "");
			if (!status) continue;

			const key = name.toLowerCase();
			if (seen.has(key)) continue;
			seen.add(key);

			rows.push({
				name,
				status,
				detail: normalize(row.innerText || row.textContent || ""),
			});
		}

		const candidates = Array.from(document.querySelectorAll("[data-test], [data-testid], [data-check], [data-name], .check, .result, li"));
		for (const node of candidates) {
			const text = normalize(node.innerText || node.textContent || "");
			if (!text || text.length > 260) continue;
			if (!/(pass|fail|detected|not detected|warning|critical|true|false|yes|no|leak|bot)/i.test(text)) continue;

			let name = normalize(node.getAttribute("data-check") || node.getAttribute("data-name") || node.getAttribute("data-testid") || "");
			let status = "";

			if (!name) {
				const parts = text.split(/[:\-|]/).map(normalize).filter(Boolean);
				if (parts.length >= 2) {
					name = parts[0];
					status = parts.slice(1).join(" ");
				}
			}

			if (!name) {
				const lines = text.split(/\n+/).map(normalize).filter(Boolean);
				if (lines.length >= 2) {
					name = lines[0];
					status = lines.slice(1).join(" ");
				}
			}

			if (!name) continue;
			if (!status) {
				status = text;
			}

			const key = name.toLowerCase();
			if (seen.has(key)) continue;
			seen.add(key);

			rows.push({ name, status, detail: text });
		}

		return rows;
	}`)
	if err != nil {
		return nil, err
	}

	var rows []detectorRow
	if err := res.Value.Unmarshal(&rows); err != nil {
		return nil, fmt.Errorf("decode detector rows: %w", err)
	}
	return rows, nil
}

func rowsToDetections(rows []detectorRow, criticalKeywords []string) map[string]fpcheck.Detection {
	out := make(map[string]fpcheck.Detection, len(rows))
	for _, row := range rows {
		key := normalizeKey(row.Name)
		if key == "unknown" {
			continue
		}

		detected := classifyStatus(row.Status)
		severity := ""
		if detected && hasKeyword(key+" "+strings.ToLower(row.Detail), criticalKeywords) {
			severity = "critical"
		}

		description := strings.TrimSpace(row.Status)
		if description == "" {
			description = strings.TrimSpace(row.Detail)
		}

		out[key] = fpcheck.Detection{
			Detected:    detected,
			Description: description,
			Severity:    severity,
		}
	}
	return out
}

func hasKeyword(value string, keywords []string) bool {
	if len(keywords) == 0 {
		return false
	}
	for _, keyword := range keywords {
		k := strings.ToLower(strings.TrimSpace(keyword))
		if k == "" {
			continue
		}
		if strings.Contains(value, k) {
			return true
		}
	}
	return false
}

func extractScore(text string) (float64, bool) {
	matches := reExtractScore.FindStringSubmatch(text)
	if len(matches) < 3 {
		return 0, false
	}
	score, err := strconv.ParseFloat(matches[2], 64)
	if err != nil {
		return 0, false
	}
	return score, true
}
