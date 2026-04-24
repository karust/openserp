package core

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
)

// EnrichContext carries request-scoped values needed to enrich a raw result.
type EnrichContext struct {
	Engine string
	Query  Query
}

// EnrichResult converts a raw engine result into the v1 Result shape.
func EnrichResult(raw SearchResult, ctx EnrichContext) Result {
	normalizedURL := normalizeURL(raw.URL)
	domain := extractDomain(normalizedURL)
	displayURL := buildDisplayURL(normalizedURL, domain)
	favicon := ""
	if domain != "" {
		favicon = "https://" + domain + "/favicon.ico"
	}

	resultType := ResultTypeOrganic
	if raw.Ad {
		resultType = ResultTypeAd
	}
	// Google answer boxes use negative rank; promote to answer_box type.
	if raw.Rank <= 0 && !raw.Ad {
		resultType = ResultTypeAnswerBox
	}

	limit := ctx.Query.Limit
	if limit <= 0 {
		limit = 25
	}
	onPage := raw.Rank
	if onPage < 0 {
		onPage = 0
	}
	absolute := ctx.Query.Start + onPage
	page := ctx.Query.Start/limit + 1

	result := Result{
		ID:         buildResultID(ctx.Engine, normalizedURL),
		Rank:       raw.Rank,
		Type:       resultType,
		Title:      raw.Title,
		URL:        normalizedURL,
		DisplayURL: displayURL,
		Snippet:    raw.Description,
		Domain:     domain,
		Favicon:    favicon,
		IsAd:       raw.Ad,
		Position: Position{
			Absolute: absolute,
			Page:     page,
			OnPage:   onPage,
		},
		Engine: ctx.Engine,
		Rich:   nil,
		EngineMeta: map[string]any{
			"raw_rank": raw.Rank,
		},
	}

	result.DomainInfo = EnrichDomainInfo(domain)
	result.Classification = ClassifyURL(normalizedURL, domain)

	return result
}

// EnrichImageResult converts a raw engine result into the v1 ImageResult shape.
func EnrichImageResult(raw SearchResult, ctx EnrichContext) ImageResult {
	imageURL := normalizeURL(raw.URL)
	// raw.Description may hold the page URL for image results in some engines.
	pageURL := raw.Description
	if pageURL == "" {
		pageURL = imageURL
	}
	sourceDomain := extractDomain(pageURL)

	return ImageResult{
		ID:    buildImageID(ctx.Engine, imageURL),
		Rank:  raw.Rank,
		Type:  ResultTypeImage,
		Title: raw.Title,
		Image: ImageData{
			URL: imageURL,
		},
		Source: ImageSource{
			PageURL: pageURL,
			Domain:  sourceDomain,
		},
		Engine:     ctx.Engine,
		EngineMeta: map[string]any{"raw_rank": raw.Rank},
	}
}

// buildResultID returns a stable "r_<hex>" ID for web results.
func buildResultID(engine, normalizedURL string) string {
	h := sha256.Sum256([]byte(engine + "|" + normalizedURL))
	return "r_" + hex.EncodeToString(h[:12])
}

// buildImageID returns a stable "i_<hex>" ID for image results.
func buildImageID(engine, imageURL string) string {
	h := sha256.Sum256([]byte(engine + "|" + imageURL))
	return "i_" + hex.EncodeToString(h[:12])
}

// normalizeURL lowercases scheme+host, strips trailing slash, and removes
// common tracking parameters. This is the canonical form used for ID hashing.
func normalizeURL(raw string) string {
	if raw == "" {
		return raw
	}
	// Unwrap Bing redirect URLs: bing.com/ck/a?...&u=a1<base64url>
	if strings.Contains(raw, "bing.com/ck/a") {
		if unwrapped := unwrapBingURL(raw); unwrapped != "" {
			raw = unwrapped
		}
	}

	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}

	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)

	// Strip common tracking parameters.
	q := u.Query()
	trackingParams := []string{
		"utm_source", "utm_medium", "utm_campaign", "utm_term", "utm_content",
		"fbclid", "gclid", "msclkid", "ref", "_ga",
	}
	for _, p := range trackingParams {
		q.Del(p)
	}
	u.RawQuery = q.Encode()

	normalized := u.String()
	// Strip trailing slash from path (but keep root slash for bare domains).
	if u.Path != "/" && strings.HasSuffix(normalized, "/") {
		normalized = strings.TrimRight(normalized, "/")
	}
	return normalized
}

// unwrapBingURL extracts the real destination from a Bing click-tracking URL.
// Bing encodes as: u=a1<url-safe-base64-without-padding>
func unwrapBingURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	uParam := u.Query().Get("u")
	if !strings.HasPrefix(uParam, "a1") {
		return ""
	}
	encoded := uParam[2:]
	// Bing uses URL-safe base64 without padding; add padding.
	switch len(encoded) % 4 {
	case 2:
		encoded += "=="
	case 3:
		encoded += "="
	}
	import64 := strings.NewReplacer("-", "+", "_", "/")
	decoded, err := decodeBase64String(import64.Replace(encoded))
	if err != nil {
		return ""
	}
	candidate := string(decoded)
	if strings.HasPrefix(candidate, "http://") || strings.HasPrefix(candidate, "https://") {
		return candidate
	}
	return ""
}

// extractDomain returns the registrable domain (no www.) from a URL string.
func extractDomain(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	host := u.Hostname()
	host = strings.TrimPrefix(host, "www.")
	return host
}

// buildDisplayURL returns a human-readable URL breadcrumb similar to what
// search engines show on SERPs (e.g. "go.dev › doc › install").
func buildDisplayURL(rawURL, domain string) string {
	if rawURL == "" {
		return domain
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Path == "" || u.Path == "/" {
		return domain
	}

	// Replace path separators with › for breadcrumb style.
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	nonEmpty := parts[:0]
	for _, p := range parts {
		if p != "" {
			nonEmpty = append(nonEmpty, p)
		}
	}
	if len(nonEmpty) == 0 {
		return domain
	}

	breadcrumb := domain + " › " + strings.Join(nonEmpty, " › ")
	// Truncate to ~60 chars.
	const maxLen = 60
	if len(breadcrumb) > maxLen {
		breadcrumb = breadcrumb[:maxLen-1] + "…"
	}
	return breadcrumb
}

// decodeBase64String decodes standard (not URL-safe) base64.
func decodeBase64String(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}

// NormalizeURLForClustering returns a URL suitable for cross-engine grouping
// (same as normalizeURL but exported for use in cluster building).
func NormalizeURLForClustering(rawURL string) string {
	return normalizeURL(rawURL)
}

// ResultID returns the stable ID for a given engine + URL pair.
func ResultID(engine, rawURL string) string {
	return buildResultID(engine, normalizeURL(rawURL))
}

// ValidateResultType returns the input type if it is a known enum value,
// otherwise returns ResultTypeOrganic with a warning message.
func ValidateResultType(t ResultType) (ResultType, string) {
	switch t {
	case ResultTypeOrganic, ResultTypeAd, ResultTypeFeaturedSnippet,
		ResultTypeKnowledgePanel, ResultTypePeopleAlsoAsk, ResultTypeVideo,
		ResultTypeImage, ResultTypeNews, ResultTypeShopping,
		ResultTypeLocal, ResultTypeAnswerBox:
		return t, ""
	}
	return ResultTypeOrganic, fmt.Sprintf("unknown result type %q, defaulting to organic", t)
}
