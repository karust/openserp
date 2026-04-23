package detectors

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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

type rebrowserCheck struct {
	Type   string  `json:"type"`
	Icon   string  `json:"icon"`
	Rating float64 `json:"rating"`
	Note   string  `json:"note"`
	Debug  string  `json:"debug"`
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
			const output = document.querySelector('#detections-json');
			if (!output || !output.value) return false;
			try {
				const parsed = JSON.parse(output.value);
				return Array.isArray(parsed) && parsed.length > 0;
			} catch (_) {
				return false;
			}
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

	res, err := page.Eval(`() => {
		const normalize = (value) => (value || "").replace(/\s+/g, " ").trim();
		const stripHTML = (value) => {
			const div = document.createElement('div');
			div.innerHTML = value || '';
			return normalize(div.textContent || div.innerText || '');
		};
		const checks = new Map();

		const put = (item) => {
			const type = normalize(item.type);
			if (!type) return;

			const current = checks.get(type) || { type, icon: "", rating: 0, note: "", debug: "" };
			const next = {
				type,
				icon: normalize(item.icon || current.icon),
				rating: Number.isFinite(item.rating) ? item.rating : current.rating,
				note: normalize(item.note || current.note),
				debug: normalize(item.debug || current.debug),
			};
			checks.set(type, next);
		};

		try {
			const raw = document.querySelector('#detections-json')?.value || '[]';
			const parsed = JSON.parse(raw);
			if (Array.isArray(parsed)) {
				for (const item of parsed) {
					put({
						type: item.type || '',
						rating: Number(item.rating),
						note: stripHTML(item.note || ''),
						debug: typeof item.debug === 'string' ? normalize(item.debug) : normalize(JSON.stringify(item.debug || {})),
					});
				}
			}
		} catch (_) {}

		for (const row of Array.from(document.querySelectorAll('#detections-table tbody tr'))) {
			const cells = Array.from(row.querySelectorAll('td'));
			if (cells.length < 1) continue;
			const rawName = normalize(cells[0].innerText || cells[0].textContent || "");
			if (!rawName) continue;
			const chars = Array.from(rawName);
			const icon = chars.length > 0 ? chars[0] : "";
			const type = normalize(rawName.replace(icon, ""));
			const note = cells.length > 2 ? normalize(cells[2].innerText || cells[2].textContent || "") : "";
			put({
				type,
				icon,
				note,
			});
		}

		return Array.from(checks.values());
	}`)
	if err != nil {
		return nil, "", fmt.Errorf("rebrowser extraction failed: %w", err)
	}

	var checks []rebrowserCheck
	if err := res.Value.Unmarshal(&checks); err != nil {
		return nil, "", fmt.Errorf("decode rebrowser checks: %w", err)
	}
	if len(checks) == 0 {
		return nil, "", fmt.Errorf("rebrowser detector checks not found")
	}

	detections := rebrowserChecksToDetections(checks)
	if len(detections) == 0 {
		return nil, "", fmt.Errorf("rebrowser detections are empty")
	}

	raw, _ := json.MarshalIndent(checks, "", "  ")
	return detections, string(raw), nil
}

func rebrowserChecksToDetections(checks []rebrowserCheck) map[string]fpcheck.Detection {
	detections := make(map[string]fpcheck.Detection, len(checks))
	for _, check := range checks {
		key := normalizeKey(check.Type)
		if key == "unknown" {
			continue
		}

		detected := false
		switch check.Icon {
		case "🔴":
			detected = true
		case "🟢", "🟡", "⚪️", "⚪":
			detected = false
		default:
			detected = check.Rating >= 1
		}

		description := strings.TrimSpace(check.Note)
		if strings.TrimSpace(check.Debug) != "" {
			if description != "" {
				description = description + " | " + strings.TrimSpace(check.Debug)
			} else {
				description = strings.TrimSpace(check.Debug)
			}
		}
		if description == "" {
			description = fmt.Sprintf("rating=%.2f", check.Rating)
		}

		severity := ""
		if detected {
			severity = "critical"
		}

		detections[key] = fpcheck.Detection{
			Detected:    detected,
			Description: description,
			Severity:    severity,
		}
	}
	return detections
}
