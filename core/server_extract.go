package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
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
	Clean      *bool  `json:"clean"`
	UseLLMSTxt bool   `json:"use_llms_txt"`
	MinRunes   int    `json:"min_runes"`
	Lang       string `json:"lang"`
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

// baseExtractRequest builds the URL-independent part of an extract request
// from proxy headers, query params, and the parsed body. Shared by /extract
// and /extract/batch so both accept the same knobs.
func (s *Server) baseExtractRequest(c *fiber.Ctx, body extractPayload, cfg extractpkg.Config) (extractpkg.ExtractRequest, error) {
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
	mode := extractpkg.Mode(strings.ToLower(firstNonEmpty(c.Query("mode"), body.Mode, cfg.DefaultMode)))
	switch mode {
	case extractpkg.ModeAuto, extractpkg.ModeFast, extractpkg.ModeRendered:
	default:
		return extractpkg.ExtractRequest{}, errInvalidParam("mode must be one of auto, fast, rendered")
	}
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
	return extractpkg.ExtractRequest{
		Mode:       mode,
		ProxyURL:   proxyURL,
		LangCode:   firstNonEmpty(body.Lang, c.Query("lang")),
		Timeout:    cfg.Timeout,
		MaxBytes:   cfg.MaxBytes,
		FullPage:   !clean,
		UseLLMSTxt: parseBoolDefault(c.Query("use_llms_txt"), body.UseLLMSTxt),
		MinRunes:   minRunes,
	}, nil
}

func (s *Server) extractRequestFromFiber(c *fiber.Ctx, cfg extractpkg.Config) (extractpkg.ExtractRequest, error) {
	var body extractPayload
	if len(c.Body()) > 0 {
		_ = c.BodyParser(&body)
	}
	req, err := s.baseExtractRequest(c, body, cfg)
	if err != nil {
		return extractpkg.ExtractRequest{}, err
	}
	req.URL = extractpkg.NormalizeURL(strings.TrimSpace(firstNonEmpty(c.Query("url"), body.URL)))
	if err := validateExtractTargetURL(c.UserContext(), req.URL, cfg.AllowPrivateNetworks); err != nil {
		return extractpkg.ExtractRequest{}, errInvalidParam(err.Error())
	}
	return req, nil
}

func (s *Server) newExtractor() extractpkg.Extractor {
	return extractpkg.Extractor{
		RawFetch:      s.rawExtractFetch,
		RenderedFetch: s.renderedExtractFetch,
		Cfg:           s.opts.Extract,
	}
}

func (s *Server) rawExtractFetch(ctx context.Context, req extractpkg.ExtractRequest) (*extractpkg.FetchResponse, error) {
	return RawExtractFetch(ctx, req, s.opts.Extract, s.opts.FingerprintBrowserOpts.Insecure)
}

// RawExtractFetch performs the browserless extraction fetch: validate the
// target, issue a guarded HTTP GET, classify the status, and return the body
// capped to the byte budget. Shared by the HTTP server and the CLI.
func RawExtractFetch(ctx context.Context, req extractpkg.ExtractRequest, cfg extractpkg.Config, insecure bool) (*extractpkg.FetchResponse, error) {
	cfg = cfg.Normalized()
	if err := validateExtractTargetURL(ctx, req.URL, cfg.AllowPrivateNetworks); err != nil {
		return nil, err
	}
	resp, err := RawSearchRequest(ctx, req.URL, Query{
		ProxyURL:             req.ProxyURL,
		LangCode:             req.LangCode,
		Insecure:             insecure,
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
		limit = int64(cfg.MaxBytes)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		body = body[:limit]
	}
	out := &extractpkg.FetchResponse{StatusCode: resp.StatusCode, Body: body}
	if resp.Request != nil && resp.Request.URL != nil {
		out.FinalURL = resp.Request.URL.String()
	}
	return out, nil
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
	return RenderExtractHTML(WithRequestProxyURL(ctx, req.ProxyURL), browser, req)
}

// RenderExtractHTML navigates an already-resolved browser to the target,
// returns its rendered HTML capped to the byte budget, and always closes the
// page. Shared by the HTTP server's BrowserResolver path and the CLI's
// one-shot browser. Callers own target validation and proxy gating.
func RenderExtractHTML(ctx context.Context, browser *Browser, req extractpkg.ExtractRequest) (*extractpkg.FetchResponse, error) {
	page, err := browser.Navigate(ctx, req.URL)
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
	out := &extractpkg.FetchResponse{StatusCode: http.StatusOK, Body: body}
	// Best effort - without it links resolve against the requested URL.
	if info, err := page.Info(); err == nil {
		out.FinalURL = info.URL
	}
	return out, nil
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
	EnrichEnvelopeWithExtraction(ctx, env, q, format, s.newExtractor(), s.opts.Extract)
}

// EnrichEnvelopeWithExtraction fills env.Results[*].Extracted by running the
// extractor over the top organic results, with candidate fill-in when a top
// result fails. It is shared by the HTTP search handler and the CLI so both
// apply the same depth bounds, batch deadline, and result selection. The
// extractor and cfg are supplied by the caller (the server reuses its
// long-lived browser pool; the CLI builds a one-shot browser).
func EnrichEnvelopeWithExtraction(ctx context.Context, env *Envelope, q Query, format string, extractor extractpkg.Extractor, cfg extractpkg.Config) {
	cfg = cfg.Normalized()
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
	limit := clampExtractTop(q.ExtractTop)
	if limit > len(env.Results) {
		limit = len(env.Results)
	}
	candidateLimit := limit + 3
	if candidateLimit > len(env.Results) {
		candidateLimit = len(env.Results)
	}

	// Per-fetch timeouts bound a single URL; this aggregate deadline bounds the
	// whole batch so a few slow/hanging targets can't stretch the request
	// open-endedly. The ceiling is derived from the per-URL budget (see
	// Config.BatchTimeout) rather than a separate knob. When it fires, in-flight
	// fetches are cancelled and any not yet started record a timeout error instead
	// of a result — never a 500.
	ctx, cancel := context.WithTimeout(ctx, cfg.BatchTimeout(candidateLimit))
	defer cancel()

	extractOne := func(idx int) {
		// Skip the fetch entirely if the batch budget is already spent.
		if err := ctx.Err(); err != nil {
			env.Results[idx].Extracted = &ExtractedContent{Error: SanitizeExtractError(err)}
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
			env.Results[idx].Extracted = &ExtractedContent{Error: SanitizeExtractError(err)}
			return
		}
		content := result.Markdown
		if contentFormat == "text" {
			content = result.Text
		}
		if !ExtractedContentLooksUseful(content) {
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

// ExtractedContentLooksUseful reports whether extracted page content is long
// enough to keep, rather than an empty/boilerplate shell. Shared by the HTTP
// server and the CLI so both apply the same threshold.
func ExtractedContentLooksUseful(content string) bool {
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
		ExtractedContentLooksUseful(result.Extracted.Content)
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

// SanitizeExtractError trims and length-bounds an extraction error for safe
// inclusion in a response payload. Shared by the HTTP server and the CLI.
func SanitizeExtractError(err error) string {
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

const maxBatchExtractURLs = 20

type batchExtractPayload struct {
	extractPayload
	URLs []string `json:"urls"`
}

// batchExtractItem is one entry of the bare-array /extract/batch response.
// The {page_content, metadata} shape is the Open WebUI ExternalWebLoader
// contract - do not wrap it in the Envelope.
type batchExtractItem struct {
	PageContent string            `json:"page_content"`
	Metadata    map[string]string `json:"metadata"`
}

func (s *Server) handleBatchExtract(c *fiber.Ctx) error {
	requestCtx := withRequestUsage(c.UserContext(), "extract-batch")
	c.SetUserContext(requestCtx)
	defer setNetworkBytesHeader(c, requestCtx)
	defer setBrowserProfileHeader(c, requestCtx)

	cfg := s.opts.Extract.Normalized()
	if !cfg.Enabled {
		return &APIError{HTTPStatus: fiber.StatusNotFound, ErrorCode: "not_found", Message: "Extraction is disabled"}
	}

	if len(c.Body()) == 0 {
		return errInvalidParam("request body is required")
	}
	var body batchExtractPayload
	if err := c.BodyParser(&body); err != nil {
		return errInvalidParam("invalid JSON body")
	}
	urls := dedupeBatchURLs(body.URLs)
	if len(urls) == 0 {
		return errInvalidParam("urls array is required and must contain at least one valid URL")
	}
	if len(urls) > maxBatchExtractURLs {
		return errInvalidParam(fmt.Sprintf("urls array exceeds maximum of %d", maxBatchExtractURLs))
	}
	baseReq, err := s.baseExtractRequest(c, body.extractPayload, cfg)
	if err != nil {
		return err
	}

	// Target URLs are validated in the fetch path, inside the workers - a bad
	// URL becomes an error item instead of failing the whole batch (Open WebUI
	// drops every doc on a non-2xx). 400 is reserved for malformed requests.
	extractor := s.newExtractor()
	results := make([]batchExtractItem, len(urls))

	// Bounded parallelism plus an aggregate deadline, same pattern as
	// EnrichEnvelopeWithExtraction.
	ctx, cancel := context.WithTimeout(requestCtx, cfg.BatchTimeout(len(urls)))
	defer cancel()

	sem := make(chan struct{}, cfg.MaxConcurrent)
	var wg sync.WaitGroup
	for i, u := range urls {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, url string) {
			defer wg.Done()
			defer func() { <-sem }()
			results[idx] = batchExtractOne(ctx, extractor, baseReq, url)
		}(i, u)
	}
	wg.Wait()

	return c.JSON(results)
}

// dedupeBatchURLs normalizes, drops empties, and keeps first occurrence order.
func dedupeBatchURLs(raw []string) []string {
	seen := make(map[string]struct{}, len(raw))
	var urls []string
	for _, r := range raw {
		u := extractpkg.NormalizeURL(strings.TrimSpace(r))
		if u == "" {
			continue
		}
		if _, dup := seen[u]; dup {
			continue
		}
		seen[u] = struct{}{}
		urls = append(urls, u)
	}
	return urls
}

// batchExtractOne extracts a single URL, folding failures into the item.
func batchExtractOne(ctx context.Context, extractor extractpkg.Extractor, req extractpkg.ExtractRequest, url string) batchExtractItem {
	fail := func(err error) batchExtractItem {
		WithRequest(ctx).WithError(err).WithField("url", url).Warn("Batch extract failed")
		return batchExtractItem{Metadata: map[string]string{"source": url, "error": SanitizeExtractError(err)}}
	}
	// Skip the fetch once the batch budget is spent.
	if ctx.Err() != nil {
		return fail(errors.New("batch timeout"))
	}
	req.URL = url
	result, err := extractor.Extract(ctx, req)
	if err != nil {
		return fail(err)
	}
	return batchExtractItem{
		PageContent: result.Markdown,
		Metadata: map[string]string{
			"source":      url,
			"title":       result.Title,
			"description": result.Description,
			"lang":        result.Lang,
			"canonical":   result.Canonical,
			"mode_used":   result.Meta.ModeUsed,
			"fetched_at":  result.Meta.FetchedAt,
			"took_ms":     strconv.FormatInt(result.Meta.TookMs, 10),
		},
	}
}
