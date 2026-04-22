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

const deviceAndBrowserURL = "https://deviceandbrowserinfo.com/are_you_a_bot"

type DeviceAndBrowser struct{}

func NewDeviceAndBrowser() fpcheck.Detector {
	return DeviceAndBrowser{}
}

func (DeviceAndBrowser) Name() string {
	return "deviceandbrowser"
}

func (DeviceAndBrowser) URL() string {
	return deviceAndBrowserURL
}

func (DeviceAndBrowser) Extract(ctx context.Context, page *rod.Page) (map[string]fpcheck.Detection, string, error) {
	err := waitFor(ctx, 25*time.Second, 250*time.Millisecond, func() (bool, error) {
		res, err := page.Eval(`() => {
			const hasJson = !!document.querySelector("#jsonResult");
			const hasCard = !!document.querySelector("#resultsBotTest");
			const text = (document.body && document.body.innerText ? document.body.innerText : "").toLowerCase();
			return hasJson || hasCard || text.includes("are you a bot");
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
		return nil, "", fmt.Errorf("deviceandbrowser readiness: %w", err)
	}

	res, err := page.Eval(`() => {
		const normalize = (value) => (value || "").replace(/\s+/g, " ").trim();
		const decodeHTML = (value) => {
			const textarea = document.createElement("textarea");
			textarea.innerHTML = value;
			return textarea.value;
		};

		const out = { isBot: null, details: {}, rawJson: "", cardText: "", body: "" };

		const card = document.querySelector("#resultsBotTest");
		if (card) {
			out.cardText = normalize(card.innerText || card.textContent || "");
			const low = out.cardText.toLowerCase();
			if (low.includes("you are a bot")) out.isBot = true;
			if (low.includes("not a bot")) out.isBot = false;
		}

		const jsonNode = document.querySelector("#jsonResult");
		if (jsonNode) {
			let raw = (jsonNode.textContent || jsonNode.innerText || "").replace(/\u00a0/g, " ").trim();
			if (!raw && jsonNode.innerHTML) {
				raw = decodeHTML(jsonNode.innerHTML.replace(/<br\s*\/?>/gi, "\n")).replace(/\u00a0/g, " ").trim();
			}
			out.rawJson = raw;
			try {
				const parsed = JSON.parse(raw);
				if (typeof parsed.isBot === "boolean") out.isBot = parsed.isBot;
				if (parsed.details && typeof parsed.details === "object") {
					for (const [k, v] of Object.entries(parsed.details)) {
						if (typeof v === "boolean") out.details[k] = v;
					}
				}
			} catch (_) {}
		}

		out.body = normalize(document.body && document.body.innerText ? document.body.innerText : "").slice(0, 6000);
		return out;
	}`)
	if err != nil {
		return nil, "", err
	}

	var payload struct {
		IsBot    *bool           `json:"isBot"`
		Details  map[string]bool `json:"details"`
		RawJSON  string          `json:"rawJson"`
		CardText string          `json:"cardText"`
		Body     string          `json:"body"`
	}
	if err := res.Value.Unmarshal(&payload); err != nil {
		return nil, "", fmt.Errorf("decode deviceandbrowser payload: %w", err)
	}

	detections := make(map[string]fpcheck.Detection)
	if payload.IsBot != nil {
		severity := ""
		if *payload.IsBot {
			severity = "critical"
		}
		detections["overall_is_bot"] = fpcheck.Detection{
			Detected:    *payload.IsBot,
			Description: strings.TrimSpace(payload.CardText),
			Severity:    severity,
		}
	}

	for key, value := range payload.Details {
		norm := normalizeKey(key)
		if norm == "unknown" {
			continue
		}

		severity := ""
		if value && hasKeyword(norm, []string{"webdriver", "cdp", "headless", "bot", "playwright", "selenium"}) {
			severity = "critical"
		}
		detections[norm] = fpcheck.Detection{
			Detected:    value,
			Description: fmt.Sprintf("%t", value),
			Severity:    severity,
		}
	}

	if len(detections) == 0 && strings.TrimSpace(payload.RawJSON) != "" {
		// Fallback parse when JSON was extracted but JS-side parser missed fields.
		var decoded struct {
			IsBot   *bool           `json:"isBot"`
			Details map[string]bool `json:"details"`
		}
		if err := json.Unmarshal([]byte(payload.RawJSON), &decoded); err == nil {
			if decoded.IsBot != nil {
				detections["overall_is_bot"] = fpcheck.Detection{
					Detected:    *decoded.IsBot,
					Description: strings.TrimSpace(payload.CardText),
				}
			}
			for key, value := range decoded.Details {
				norm := normalizeKey(key)
				if norm == "unknown" {
					continue
				}
				detections[norm] = fpcheck.Detection{
					Detected:    value,
					Description: fmt.Sprintf("%t", value),
				}
			}
		}
	}

	if len(detections) == 0 {
		return nil, payload.Body, fmt.Errorf("deviceandbrowser detections are empty")
	}

	rawNotes := strings.TrimSpace(payload.RawJSON)
	if rawNotes == "" {
		rawNotes = strings.TrimSpace(payload.Body)
	}
	return detections, rawNotes, nil
}
