package extract

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

type Extractor struct {
	RawFetch      RawFetcher
	RenderedFetch RenderedFetcher
	Cfg           Config
}

func (e *Extractor) Extract(ctx context.Context, req ExtractRequest) (*ExtractResult, error) {
	startedAt := time.Now()
	cfg := e.Cfg.Normalized()
	req = normalizeRequest(req, cfg)
	if req.URL == "" {
		return nil, errors.New("url is required")
	}
	if _, err := url.ParseRequestURI(req.URL); err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}
	if req.Mode == "" {
		req.Mode = Mode(cfg.DefaultMode)
	}
	if req.Mode != ModeAuto && req.Mode != ModeFast && req.Mode != ModeRendered {
		return nil, fmt.Errorf("invalid mode %q", req.Mode)
	}

	// A site's llms.txt is purpose-built markdown — when the caller opts in and
	// the URL is a root, it beats anything we can scrape. Falls through silently
	// to normal extraction when absent.
	if req.UseLLMSTxt {
		if result, ok := e.tryLLMSTxt(ctx, req, startedAt); ok {
			return result, nil
		}
	}

	var rawResult *ExtractResult
	var rawErr error
	if req.Mode != ModeRendered && e.RawFetch != nil {
		rawResult, rawErr = e.extractFast(ctx, req, startedAt)
		if req.Mode == ModeFast || goodEnough(rawResult, rawErr, req.MinRunes) {
			return rawResult, rawErr
		}
	}

	if e.RenderedFetch == nil {
		return rawResult, rawErr
	}
	renderedResult, renderedErr := e.extractRendered(ctx, req, startedAt)
	if renderedErr == nil && renderedResult != nil {
		// In auto mode the raw pass already produced something usable but below
		// the quality threshold. Only prefer the rendered pass when it actually
		// recovered more content — a bot wall or consent page can render shorter
		// than the raw HTML, and falling back to it would be a regression.
		if rawErr == nil && rawResult != nil && textLength(rawResult) > textLength(renderedResult) {
			return rawResult, nil
		}
		return renderedResult, nil
	}
	if rawResult != nil || rawErr != nil {
		return rawResult, rawErr
	}
	return nil, renderedErr
}

func (e *Extractor) extractFast(ctx context.Context, req ExtractRequest, startedAt time.Time) (*ExtractResult, error) {
	fetchCtx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()
	resp, err := e.RawFetch(fetchCtx, req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("empty raw response")
	}
	return buildResult(req, resp, string(ModeFast), startedAt)
}

func (e *Extractor) extractRendered(ctx context.Context, req ExtractRequest, startedAt time.Time) (*ExtractResult, error) {
	fetchCtx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()
	resp, err := e.RenderedFetch(fetchCtx, req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("empty rendered response")
	}
	return buildResult(req, resp, string(ModeRendered), startedAt)
}

func normalizeRequest(req ExtractRequest, cfg Config) ExtractRequest {
	req.URL = normalizeURL(strings.TrimSpace(req.URL))
	if req.Timeout <= 0 {
		req.Timeout = cfg.Timeout
	}
	if req.MaxBytes <= 0 {
		req.MaxBytes = cfg.MaxBytes
	}
	if req.MaxBytes > 0 && req.MaxBytes < 64*1024 {
		req.MaxBytes = 64 * 1024
	}
	req.Mode = Mode(strings.ToLower(strings.TrimSpace(string(req.Mode))))
	return req
}

// normalizeURL defaults a missing scheme to https so callers can pass a bare host
// (e.g. "kamaloff.ru"). URLs that already carry a scheme are left untouched.
func normalizeURL(raw string) string {
	if raw == "" || strings.Contains(raw, "://") || strings.HasPrefix(raw, "//") {
		return raw
	}
	return "https://" + raw
}

func buildResult(req ExtractRequest, resp *FetchResponse, mode string, startedAt time.Time) (*ExtractResult, error) {
	if err := classifyStatus(resp.StatusCode); err != nil {
		return nil, err
	}
	body := resp.Body
	if req.MaxBytes > 0 && len(body) > req.MaxBytes {
		body = body[:req.MaxBytes]
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	metadata := parseMetadata(doc, req.URL)
	content, contentErr := extractContent(body, req.URL, !req.FullPage)
	result := &ExtractResult{
		URL:         req.URL,
		Title:       firstNonEmpty(content.Title, metadata.Title),
		Description: firstNonEmpty(content.Description, metadata.Description),
		Markdown:    content.Markdown,
		Text:        content.Text,
		Headings:    metadata.Headings,
		Links:       metadata.Links,
		Canonical:   metadata.Canonical,
		Lang:        firstNonEmpty(content.Lang, metadata.Lang),
		SchemaOrg:   metadata.SchemaOrg,
		OGTags:      metadata.OGTags,
		Meta: ExtractMeta{
			ModeUsed:  mode,
			FetchedAt: time.Now().UTC().Format(time.RFC3339),
			Bytes:     len(body),
			TookMs:    time.Since(startedAt).Milliseconds(),
		},
	}
	if contentErr != nil && strings.TrimSpace(result.Text) == "" {
		return result, contentErr
	}
	return result, nil
}

func classifyStatus(status int) error {
	if status == 0 {
		return nil
	}
	if status == http.StatusForbidden || status == http.StatusUnauthorized {
		return fmt.Errorf("blocked: HTTP %d", status)
	}
	if status == http.StatusTooManyRequests {
		return fmt.Errorf("rate limited: HTTP %d", status)
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("unexpected HTTP status %d", status)
	}
	return nil
}

// defaultMinRunes is the built-in auto-mode escalation floor.
const defaultMinRunes = 200

// goodEnough reports whether the raw pass is worth keeping instead of escalating
// to a render. minRunes overrides the floor (<= 0 uses defaultMinRunes); the
// headings shortcut accepts shorter structured pages, never below 120 runes.
func goodEnough(result *ExtractResult, err error, minRunes int) bool {
	if err != nil || result == nil {
		return false
	}
	if minRunes <= 0 {
		minRunes = defaultMinRunes
	}
	textLen := textLength(result)
	if textLen >= minRunes {
		return true
	}
	return textLen >= 120 && textLen >= minRunes-80 && len(result.Headings) > 0
}

// textLength returns the rune count of the trimmed extracted text, used both to
// gate auto-mode escalation and to compare raw vs rendered output quality.
func textLength(result *ExtractResult) int {
	if result == nil {
		return 0
	}
	return len([]rune(strings.TrimSpace(result.Text)))
}
