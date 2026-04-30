package detectors

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/karust/openserp/core/fpcheck"
)

const customDetectorName = "custom"
const defaultCustomSelector = "body"

type Custom struct {
	targetURL string
	selector  string
}

type customPayload struct {
	Found        bool   `json:"found"`
	Error        string `json:"error"`
	Title        string `json:"title"`
	URL          string `json:"url"`
	ReadyState   string `json:"readyState"`
	Selector     string `json:"selector"`
	SelectedText string `json:"selectedText"`
	SelectedHTML string `json:"selectedHTML"`
}

func NewCustom(rawURL string) (fpcheck.Detector, error) {
	return NewCustomWithSelector(rawURL, "")
}

func NewCustomWithSelector(rawURL string, selector string) (fpcheck.Detector, error) {
	normalized, err := normalizeCustomURL(rawURL)
	if err != nil {
		return nil, err
	}
	return Custom{
		targetURL: normalized,
		selector:  normalizeCustomSelector(selector),
	}, nil
}

func (c Custom) Name() string {
	return customDetectorName
}

func (c Custom) URL() string {
	return c.targetURL
}

func (c Custom) Selector() string {
	return normalizeCustomSelector(c.selector)
}

func (c Custom) Extract(ctx context.Context, page *rod.Page) (map[string]fpcheck.Detection, string, error) {
	selector := c.Selector()
	var payload customPayload
	err := waitFor(ctx, 15*time.Second, 200*time.Millisecond, func() (bool, error) {
		current, err := extractCustomPayload(page, selector)
		if err != nil {
			return false, err
		}
		if !current.Found {
			return false, nil
		}
		payload = current
		return true, nil
	})
	if err != nil {
		return nil, "", fmt.Errorf("custom page readiness for selector %q: %w", selector, err)
	}

	rawOut := strings.TrimSpace(payload.SelectedText)
	if rawOut == "" {
		rawOut = strings.TrimSpace(payload.SelectedHTML)
	}

	return map[string]fpcheck.Detection{
		"selected_page_output": {
			Detected:    false,
			Description: fmt.Sprintf("captured selector %q", payload.Selector),
		},
	}, rawOut, nil
}

func extractCustomPayload(page *rod.Page, selector string) (customPayload, error) {
	res, err := page.Timeout(2*time.Second).Eval(`(selector) => {
		const normalize = (value) => (value || "").replace(/\s+/g, " ").trim();
		let selected = null;
		try {
			selected = document.querySelector(selector);
		 } catch (err) {
			return {
				found: false,
				error: err && err.message ? err.message : String(err),
				selector,
			};
		}
		return {
			found: !!selected,
			title: document.title || "",
			url: location.href || "",
			readyState: document.readyState || "",
			selector,
			selectedText: normalize(selected ? selected.innerText || selected.textContent || "" : ""),
			selectedHTML: selected ? selected.innerHTML || "" : "",
		};
	}`, selector)
	if err != nil {
		return customPayload{}, err
	}

	var payload customPayload
	if err := res.Value.Unmarshal(&payload); err != nil {
		return customPayload{}, fmt.Errorf("decode custom detector payload: %w", err)
	}
	if strings.TrimSpace(payload.Error) != "" {
		return customPayload{}, fmt.Errorf("query selector %q: %s", selector, payload.Error)
	}
	return payload, nil
}

func normalizeCustomURL(rawURL string) (string, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return "", fmt.Errorf("custom detector requires non-empty url query parameter")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid custom detector URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("invalid custom detector URL scheme %q: use http or https", parsed.Scheme)
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return "", fmt.Errorf("invalid custom detector URL: host is required")
	}

	return parsed.String(), nil
}

func normalizeCustomSelector(selector string) string {
	trimmed := strings.TrimSpace(selector)
	if trimmed == "" {
		return defaultCustomSelector
	}
	return trimmed
}
