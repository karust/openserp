package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	extractpkg "github.com/karust/openserp/extract"
)

type extractPayload struct {
	URL  string `json:"url"`
	Mode string `json:"mode"`
	// Clean defaults to true (article-only). Pointer so we can tell "omitted"
	// (use default) from an explicit false (full-page extraction).
	Clean      *bool `json:"clean"`
	UseLLMSTxt bool  `json:"use_llms_txt"`
	MinRunes   int   `json:"min_runes"`
}

func (s *Server) handleExtract(c *fiber.Ctx) error {
	startedAt := time.Now()
	requestCtx := withRequestUsage(c.UserContext(), "extract")
	c.SetUserContext(requestCtx)
	defer setNetworkBytesHeader(c, requestCtx)
	defer setBrowserProfileHeader(c, requestCtx)

	cfg := s.opts.Extract.Normalized()
	if !cfg.Enabled {
		return &APIError{HTTPStatus: fiber.StatusNotFound, ErrorCode: "not_found", Message: "Extraction is disabled"}
	}
	format, err := resolveFormat(c)
	if err != nil {
		return err
	}
	req, err := s.extractRequestFromFiber(c, cfg)
	if err != nil {
		return err
	}
	extractor := s.newExtractor()
	result, err := extractor.Extract(requestCtx, req)
	if err != nil {
		WithRequest(requestCtx).WithError(err).Warn("Extract failed")
		if errors.Is(err, ErrTargetNotAllowed) {
			return &APIError{HTTPStatus: fiber.StatusBadRequest, ErrorCode: "invalid_extract_url", Message: err.Error()}
		}
		return &APIError{HTTPStatus: fiber.StatusBadGateway, ErrorCode: "extract_failed", Message: "Failed to extract URL content"}
	}
	result.Meta.TookMs = time.Since(startedAt).Milliseconds()
	return sendExtractResult(c, format, result)
}

func (s *Server) extractRequestFromFiber(c *fiber.Ctx, cfg extractpkg.Config) (extractpkg.ExtractRequest, error) {
	var body extractPayload
	if len(c.Body()) > 0 {
		_ = c.BodyParser(&body)
	}
	proxyOverride, err := NormalizeProxyRequestOverride(c.Get("X-Use-Proxy"))
	if err != nil {
		return extractpkg.ExtractRequest{}, errInvalidParam(fmt.Sprintf("X-Use-Proxy: %v", err))
	}
	proxyURL := strings.TrimSpace(c.Get("X-Proxy-URL"))
	if proxyURL != "" {
		normalized, err := NormalizeProxyURL(proxyURL)
		if err != nil {
			return extractpkg.ExtractRequest{}, errInvalidParam(fmt.Sprintf("X-Proxy-URL: %v", err))
		}
		proxyURL = normalized
	}
	q := Query{ProxyURL: proxyURL, ProxyOverride: proxyOverride}
	if err := s.validateRequestProxyURL(&q); err != nil {
		return extractpkg.ExtractRequest{}, err
	}
	mode := firstNonEmpty(c.Query("mode"), body.Mode, cfg.DefaultMode)
	// Default clean=true (article-only). FullPage is the inverse: full-readable-body
	// extraction, opted in via clean=false on the query string or body.
	bodyClean := true
	if body.Clean != nil {
		bodyClean = *body.Clean
	}
	clean := parseBoolDefault(c.Query("clean"), bodyClean)
	minRunes, err := parseNonNegativeIntQuery(c.Query("min_runes"), body.MinRunes)
	if err != nil {
		return extractpkg.ExtractRequest{}, errInvalidParam("min_runes must be a non-negative integer")
	}
	targetURL := extractpkg.NormalizeURL(strings.TrimSpace(firstNonEmpty(c.Query("url"), body.URL)))
	if err := validateExtractTargetURL(c.UserContext(), targetURL, cfg.AllowPrivateNetworks); err != nil {
		return extractpkg.ExtractRequest{}, errInvalidParam(err.Error())
	}
	return extractpkg.ExtractRequest{
		URL:        targetURL,
		Mode:       extractpkg.Mode(mode),
		ProxyURL:   proxyURL,
		LangCode:   strings.TrimSpace(c.Query("lang")),
		Timeout:    cfg.Timeout,
		MaxBytes:   cfg.MaxBytes,
		FullPage:   !clean,
		UseLLMSTxt: parseBoolDefault(c.Query("use_llms_txt"), body.UseLLMSTxt),
		MinRunes:   minRunes,
	}, nil
}

func (s *Server) newExtractor() extractpkg.Extractor {
	return extractpkg.Extractor{
		RawFetch:      s.rawExtractFetch,
		RenderedFetch: s.renderedExtractFetch,
		Cfg:           s.opts.Extract,
	}
}

func (s *Server) rawExtractFetch(ctx context.Context, req extractpkg.ExtractRequest) (*extractpkg.FetchResponse, error) {
	cfg := s.opts.Extract.Normalized()
	if err := validateExtractTargetURL(ctx, req.URL, cfg.AllowPrivateNetworks); err != nil {
		return nil, err
	}
	resp, err := RawSearchRequest(ctx, req.URL, Query{
		ProxyURL:             req.ProxyURL,
		LangCode:             req.LangCode,
		Insecure:             s.opts.FingerprintBrowserOpts.Insecure,
		GuardPrivateNetworks: !cfg.AllowPrivateNetworks,
	})
	if err != nil {
		return nil, err
	}
	defer DrainAndCloseResponse(resp)
	if err := ClassifySearchHTTPStatus(resp.StatusCode); err != nil {
		return nil, err
	}
	limit := int64(req.MaxBytes)
	if limit <= 0 {
		limit = int64(s.opts.Extract.Normalized().MaxBytes)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		body = body[:limit]
	}
	return &extractpkg.FetchResponse{StatusCode: resp.StatusCode, Body: body}, nil
}

func (s *Server) renderedExtractFetch(ctx context.Context, req extractpkg.ExtractRequest) (*extractpkg.FetchResponse, error) {
	if s.opts.BrowserResolver == nil {
		return nil, fmt.Errorf("rendered extraction is unavailable")
	}
	cfg := s.opts.Extract.Normalized()
	if err := s.validateRenderedExtractNavigation(ctx, req, cfg); err != nil {
		return nil, err
	}
	browser, err := s.opts.BrowserResolver(req.ProxyURL)
	if err != nil {
		return nil, err
	}
	page, err := browser.Navigate(WithRequestProxyURL(ctx, req.ProxyURL), req.URL)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = browser.ClosePage(ctx, page, time.Second)
	}()
	html, err := page.HTML()
	if err != nil {
		return nil, err
	}
	body := []byte(html)
	if req.MaxBytes > 0 && len(body) > req.MaxBytes {
		body = body[:req.MaxBytes]
	}
	return &extractpkg.FetchResponse{StatusCode: http.StatusOK, Body: body}, nil
}

func (s *Server) validateRenderedExtractNavigation(ctx context.Context, req extractpkg.ExtractRequest, cfg extractpkg.Config) error {
	if err := validateExtractTargetURL(ctx, req.URL, cfg.AllowPrivateNetworks); err != nil {
		return err
	}
	if cfg.AllowPrivateNetworks {
		return nil
	}

	client, err := NewRawHTTPClient(Query{
		ProxyURL:             req.ProxyURL,
		LangCode:             req.LangCode,
		Insecure:             s.opts.FingerprintBrowserOpts.Insecure,
		GuardPrivateNetworks: true,
	})
	if err != nil {
		return err
	}
	preflight, err := http.NewRequestWithContext(ctx, http.MethodHead, req.URL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(preflight)
	if err != nil {
		if errors.Is(err, ErrTargetNotAllowed) {
			return err
		}
		WithRequest(ctx).WithError(err).Debug("Rendered extract redirect preflight failed; continuing after initial target validation")
		return nil
	}
	_ = resp.Body.Close()
	return nil
}

func validateExtractTargetURL(ctx context.Context, rawURL string, allowPrivateNetworks bool) error {
	rawURL = extractpkg.NormalizeURL(strings.TrimSpace(rawURL))
	if allowPrivateNetworks {
		_, err := validateHTTPURL(rawURL)
		return err
	}
	return ValidatePublicHTTPURL(ctx, rawURL)
}

func (s *Server) enrichEnvelopeWithExtraction(ctx context.Context, env *Envelope, q Query, format string) {
	cfg := s.opts.Extract.Normalized()
	if env == nil || !q.Extract || !cfg.Enabled {
		return
	}
	// One representation per result, chosen by the response format: plain text for
	// format=text, markdown for everything else (json/ndjson/markdown). This keeps
	// the format-specific renderers fed without serializing two near-identical blobs.
	contentFormat := "markdown"
	if format == "text" {
		contentFormat = "text"
	}
	extractor := s.newExtractor()
	limit := q.ExtractTop
	if limit <= 0 || limit > 5 {
		limit = 3
	}
	if limit > len(env.Results) {
		limit = len(env.Results)
	}
	candidateLimit := limit + 3
	if candidateLimit > len(env.Results) {
		candidateLimit = len(env.Results)
	}

	// Per-fetch timeouts bound a single URL; this aggregate deadline bounds the
	// whole batch so a few slow/hanging targets can't stretch the search request
	// open-endedly. The ceiling is derived from the per-URL budget (see
	// Config.BatchTimeout) rather than a separate knob. When it fires, in-flight
	// fetches are cancelled and any not yet started record a timeout error instead
	// of a result — never a 500.
	ctx, cancel := context.WithTimeout(ctx, cfg.BatchTimeout(candidateLimit))
	defer cancel()

	extractOne := func(idx int) {
		// Skip the fetch entirely if the batch budget is already spent.
		if err := ctx.Err(); err != nil {
			env.Results[idx].Extracted = &ExtractedContent{Error: sanitizeExtractError(err)}
			return
		}
		req := extractpkg.ExtractRequest{
			URL:      env.Results[idx].URL,
			Mode:     extractpkg.Mode(q.ExtractMode),
			ProxyURL: q.ProxyURL,
			LangCode: q.LangCode,
			Timeout:  cfg.Timeout,
			MaxBytes: cfg.MaxBytes,
			MinRunes: q.ExtractMinRunes,
		}
		result, err := extractor.Extract(ctx, req)
		if err != nil {
			env.Results[idx].Extracted = &ExtractedContent{Error: sanitizeExtractError(err)}
			return
		}
		content := result.Markdown
		if contentFormat == "text" {
			content = result.Text
		}
		if !extractedContentLooksUseful(content) {
			env.Results[idx].Extracted = &ExtractedContent{Error: "empty extracted content"}
			return
		}
		env.Results[idx].Extracted = &ExtractedContent{
			Title:     result.Title,
			Format:    contentFormat,
			Content:   content,
			ModeUsed:  result.Meta.ModeUsed,
			FetchedAt: result.Meta.FetchedAt,
		}
	}

	sem := make(chan struct{}, cfg.MaxConcurrent)
	var wg sync.WaitGroup
	for i := 0; i < limit; i++ {
		if strings.TrimSpace(env.Results[i].URL) == "" {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int) {
			defer wg.Done()
			defer func() { <-sem }()
			extractOne(idx)
		}(i)
	}
	wg.Wait()

	successes := extractedSuccessCount(env.Results[:limit])
	for i := limit; successes < limit && i < candidateLimit; i++ {
		if strings.TrimSpace(env.Results[i].URL) == "" {
			continue
		}
		extractOne(i)
		if extractedResultSucceeded(env.Results[i]) {
			successes++
		}
	}
}

const minUsefulExtractRunes = 80

func extractedContentLooksUseful(content string) bool {
	return len([]rune(strings.TrimSpace(content))) >= minUsefulExtractRunes
}

func extractedSuccessCount(results []Result) int {
	count := 0
	for _, result := range results {
		if extractedResultSucceeded(result) {
			count++
		}
	}
	return count
}

func extractedResultSucceeded(result Result) bool {
	return result.Extracted != nil &&
		result.Extracted.Error == "" &&
		extractedContentLooksUseful(result.Extracted.Content)
}

func sendExtractResult(c *fiber.Ctx, format string, result *extractpkg.ExtractResult) error {
	switch format {
	case "json":
		return c.JSON(result)
	case "markdown":
		c.Set("Content-Type", "text/markdown; charset=utf-8")
		var b strings.Builder
		if result.Title != "" {
			fmt.Fprintf(&b, "# %s\n\n", result.Title)
		}
		if result.URL != "" {
			fmt.Fprintf(&b, "<%s>\n\n", result.URL)
		}
		b.WriteString(result.Markdown)
		b.WriteString("\n")
		return c.SendString(b.String())
	case "text":
		c.Set("Content-Type", "text/plain; charset=utf-8")
		return c.SendString(result.Text + "\n")
	case "ndjson":
		c.Set("Content-Type", "application/x-ndjson; charset=utf-8")
		data, err := json.Marshal(map[string]any{"kind": "extract", "result": result})
		if err != nil {
			return err
		}
		return c.Send(append(data, '\n'))
	default:
		return errInvalidParam("format must be one of json, markdown, text, ndjson")
	}
}

func parseBoolDefault(raw string, fallback bool) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	return raw == "1" || strings.EqualFold(raw, "true") || strings.EqualFold(raw, "yes")
}

func sanitizeExtractError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.TrimSpace(err.Error())
	if msg == "" {
		return "extract failed"
	}
	if len(msg) > 180 {
		msg = msg[:180]
	}
	return msg
}
