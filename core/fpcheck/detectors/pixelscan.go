package detectors

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/karust/openserp/core/fpcheck"
)

const (
	pixelscanURL = "https://pixelscan.net/bot-check"
)

type PixelScan struct{}

func NewPixelScan() fpcheck.Detector {
	return PixelScan{}
}

func (PixelScan) Name() string {
	return "pixelscan"
}

func (PixelScan) URL() string {
	return pixelscanURL
}

func (PixelScan) Extract(ctx context.Context, page *rod.Page) (map[string]fpcheck.Detection, string, error) {
	err := waitFor(ctx, 25*time.Second, 250*time.Millisecond, func() (bool, error) {
		res, err := page.Eval(`() => {
			const hasSummary = !!document.querySelector(".bot-check-summary, .bot-check__summary, .bot-check-accordion__row");
			const hasState = !!document.querySelector(".state-success, .state-error");
			const body = (document.body && document.body.innerText ? document.body.innerText : "").toLowerCase();
			return hasSummary || hasState || body.includes("running bot detection") || body.includes("definitely a human") || body.includes("bot behavior detected");
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
		return nil, "", fmt.Errorf("pixelscan readiness: %w", err)
	}

	res, err := page.Eval(`() => {
		const normalize = (value) => (value || "").replace(/\s+/g, " ").trim();
		const isVisible = (node) => !!node && !!(node.offsetParent || node.getClientRects().length) &&
			window.getComputedStyle(node).display !== "none" &&
			window.getComputedStyle(node).visibility !== "hidden" &&
			window.getComputedStyle(node).opacity !== "0";

		const successNode = document.querySelector(".state-success");
		const errorNode = document.querySelector(".state-error");
		let state = "unknown";
		if (isVisible(successNode)) state = "human";
		if (isVisible(errorNode)) state = "bot";

		if (state === "unknown") {
			const text = normalize(document.body && document.body.innerText ? document.body.innerText : "").toLowerCase();
			if (text.includes("you're definitely a human")) state = "human";
			if (text.includes("bot behavior detected")) state = "bot";
		}

		const summary = [];
		for (const section of Array.from(document.querySelectorAll(".summary-section"))) {
			const full = normalize(section.innerText || section.textContent || "");
			if (!full) continue;

			const statusNode = section.querySelector(".summary-section__status");
			const status = normalize(statusNode ? (statusNode.innerText || statusNode.textContent || "") : "");
			let name = full;
			if (status) {
				name = normalize(full.replace(new RegExp("\\\\s*" + status + "\\\\s*\\\\d*\\\\s*parameters?$", "i"), ""));
			}
			if (!name || !status) continue;
			summary.push({name, status});
		}

		const rows = [];
		for (const row of Array.from(document.querySelectorAll(".bot-check-accordion__row"))) {
			const statusNode = row.querySelector(".bot-check-accordion__status");
			const labelNode = row.querySelector(".bot-check-accordion__label");
			const status = normalize(statusNode ? (statusNode.innerText || statusNode.textContent || "") : "");
			if (!status) continue;

			let name = normalize(labelNode ? (labelNode.innerText || labelNode.textContent || "") : "");
			if (!name) {
				const full = normalize(row.innerText || row.textContent || "");
				name = normalize(full.replace(new RegExp("\\\\s*" + status + "\\\\s*$", "i"), ""));
			}
			if (!name) continue;

			rows.push({name, status});
		}

		const body = normalize(document.body && document.body.innerText ? document.body.innerText : "");
		return {
			state,
			summary,
			rows,
			body: body.slice(0, 6000),
		};
	}`)
	if err != nil {
		return nil, "", err
	}

	var payload struct {
		State   string `json:"state"`
		Summary []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"summary"`
		Rows []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"rows"`
		Body string `json:"body"`
	}
	if err := res.Value.Unmarshal(&payload); err != nil {
		return nil, "", fmt.Errorf("decode pixelscan payload: %w", err)
	}
	payload.Body = strings.TrimSpace(payload.Body)
	if payload.Body == "" {
		return nil, "", fmt.Errorf("pixelscan body is empty")
	}

	detections := make(map[string]fpcheck.Detection)
	if payload.State != "" {
		overallDetected := strings.EqualFold(strings.TrimSpace(payload.State), "bot")
		overallStatus := strings.TrimSpace(payload.State)
		if overallStatus == "" {
			overallStatus = "unknown"
		}
		overallSeverity := ""
		if overallDetected {
			overallSeverity = "critical"
		}
		detections["overall_verdict"] = fpcheck.Detection{
			Detected:    overallDetected,
			Description: overallStatus,
			Severity:    overallSeverity,
		}
	}

	for _, item := range payload.Summary {
		key := "summary_" + normalizeKey(item.Name)
		if key == "summary_unknown" {
			continue
		}
		detected := classifyStatus(item.Status)
		severity := ""
		if detected && hasKeyword(strings.ToLower(item.Name), []string{"webdriver", "cdp", "bot"}) {
			severity = "critical"
		}
		detections[key] = fpcheck.Detection{
			Detected:    detected,
			Description: strings.TrimSpace(item.Status),
			Severity:    severity,
		}
	}

	for _, row := range payload.Rows {
		key := normalizeKey(row.Name)
		if key == "unknown" {
			continue
		}
		detected := classifyStatus(row.Status)
		severity := ""
		if detected && hasKeyword(strings.ToLower(row.Name), []string{"webdriver", "cdp", "headless", "automation"}) {
			severity = "critical"
		}
		detections[key] = fpcheck.Detection{
			Detected:    detected,
			Description: strings.TrimSpace(row.Status),
			Severity:    severity,
		}
	}

	if len(detections) == 0 {
		if score, ok := extractScore(payload.Body); ok {
			scoreValue := score
			detections["overall_score"] = fpcheck.Detection{
				Detected:    false,
				Description: fmt.Sprintf("score %.2f", score),
				Numeric:     &scoreValue,
			}
		}
	}

	if len(detections) == 0 {
		return nil, payload.Body, fmt.Errorf("pixelscan detections not found")
	}

	return detections, payload.Body, nil
}
