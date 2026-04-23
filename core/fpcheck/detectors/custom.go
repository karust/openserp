package detectors

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/karust/openserp/core/fpcheck"
)

const customDetectorName = "custom"

type Custom struct {
	targetURL string
}

func NewCustom(rawURL string) (fpcheck.Detector, error) {
	normalized, err := normalizeCustomURL(rawURL)
	if err != nil {
		return nil, err
	}
	return Custom{targetURL: normalized}, nil
}

func (c Custom) Name() string {
	return customDetectorName
}

func (c Custom) URL() string {
	return c.targetURL
}

func (c Custom) Extract(ctx context.Context, page *rod.Page) (map[string]fpcheck.Detection, string, error) {
	err := waitFor(ctx, 15*time.Second, 200*time.Millisecond, func() (bool, error) {
		hasBody, _, err := page.Has("pre")
		if err != nil {
			return false, err
		}
		return hasBody, nil
	})
	if err != nil {
		return nil, "", fmt.Errorf("custom page readiness: %w", err)
	}

	res, err := page.Eval(`() => {
		const normalize = (value) => (value || "").replace(/\s+/g, " ").trim();
		return {
			title: document.title || "",
			url: location.href || "",
			readyState: document.readyState || "",
			bodyText: normalize(document.body ? document.body.innerText || document.body.textContent || "" : ""),
			html: document.documentElement ? document.documentElement.outerHTML || "" : "",
		};
	}`)
	if err != nil {
		return nil, "", err
	}

	var payload struct {
		Title      string `json:"title"`
		URL        string `json:"url"`
		ReadyState string `json:"readyState"`
		BodyText   string `json:"bodyText"`
		HTML       string `json:"html"`
	}
	if err := res.Value.Unmarshal(&payload); err != nil {
		return nil, "", fmt.Errorf("decode custom detector payload: %w", err)
	}

	payload.BodyText = strings.TrimSpace(payload.BodyText)
	payload.HTML = strings.TrimSpace(payload.HTML)

	rawOut, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, "", fmt.Errorf("encode custom detector payload: %w", err)
	}

	return map[string]fpcheck.Detection{
		"raw_page_output": {
			Detected:    false,
			Description: "raw page payload captured",
		},
	}, string(rawOut), nil
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
