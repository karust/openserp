package extract

import (
	"context"
	"net/url"
	"strings"
	"time"
)

// llmsTxtCandidates are the well-known LLM-optimized markdown files, tried in
// order of richness. /llms-full.txt is the concatenated full corpus;
// /llms.txt is the curated index. See https://llmstxt.org/.
var llmsTxtCandidates = []string{"/llms-full.txt", "/llms.txt"}

// minLLMSTxtRunes guards against a site answering an unknown path with its SPA
// index.html (HTTP 200, but HTML, not markdown). Anything shorter than this, or
// that sniffs as HTML, is rejected so we fall through to normal extraction.
const minLLMSTxtRunes = 200

// isSiteRoot reports whether the URL points at a site root, where /llms.txt is
// meaningful. We only probe roots because /llms.txt describes the whole site —
// for a deep page (e.g. /blog/post) it would return the site index and miss the
// content the caller actually asked for.
func isSiteRoot(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	path := strings.Trim(parsed.Path, "/")
	return path == ""
}

// tryLLMSTxt probes the well-known llms.txt files at the site root using the raw
// fetcher. It returns (result, true) on the first usable hit, or (nil, false) to
// signal the caller should fall through to normal HTML extraction. Errors are
// swallowed deliberately: a missing/!200/HTML llms.txt is the common case and
// must never fail the extract.
func (e *Extractor) tryLLMSTxt(ctx context.Context, req ExtractRequest, startedAt time.Time) (*ExtractResult, bool) {
	if e.RawFetch == nil || !isSiteRoot(req.URL) {
		return nil, false
	}
	base, err := url.Parse(req.URL)
	if err != nil {
		return nil, false
	}
	for _, candidate := range llmsTxtCandidates {
		ref, err := url.Parse(candidate)
		if err != nil {
			continue
		}
		probe := req
		probe.URL = base.ResolveReference(ref).String()

		fetchCtx, cancel := context.WithTimeout(ctx, req.Timeout)
		resp, ferr := e.RawFetch(fetchCtx, probe)
		cancel()
		if ferr != nil || resp == nil || resp.StatusCode != 200 {
			continue
		}
		body := resp.Body
		if req.MaxBytes > 0 && len(body) > req.MaxBytes {
			body = body[:req.MaxBytes]
		}
		text := strings.TrimSpace(string(body))
		if len([]rune(text)) < minLLMSTxtRunes || looksLikeHTML(text) {
			continue
		}
		return &ExtractResult{
			URL:      req.URL,
			Title:    firstNonEmpty(llmsTxtTitle(text), req.URL),
			Markdown: normalizeMarkdown(text),
			Text:     text,
			Meta: ExtractMeta{
				ModeUsed:  "llms_txt",
				FetchedAt: time.Now().UTC().Format(time.RFC3339),
				Bytes:     len(body),
				TookMs:    time.Since(startedAt).Milliseconds(),
			},
		}, true
	}
	return nil, false
}

// looksLikeHTML rejects bodies that are actually HTML (common when a site serves
// its SPA shell for unknown paths) rather than the markdown we want.
func looksLikeHTML(text string) bool {
	head := strings.ToLower(strings.TrimSpace(text))
	if len(head) > 256 {
		head = head[:256]
	}
	return strings.HasPrefix(head, "<!doctype html") ||
		strings.HasPrefix(head, "<html") ||
		strings.Contains(head, "<head>") ||
		strings.Contains(head, "<body")
}

// llmsTxtTitle pulls a title from the first markdown H1 ("# Title"), if present.
func llmsTxtTitle(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
		if line != "" {
			break
		}
	}
	return ""
}
