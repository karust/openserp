package core

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const responseIDBytes = 8

var imageDimensionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)height:\s*(\d+),\s*width:\s*(\d+)`),
	regexp.MustCompile(`(?i)\b(\d+)x(\d+)\b`),
}

// EnrichContext carries request-scoped values needed to enrich a raw result.
type EnrichContext struct {
	Engine string
	Query  Query
}

// EnrichResult converts a raw engine result into the v2 Result shape.
func EnrichResult(raw SearchResult, ctx EnrichContext) Result {
	normalizedURL := normalizeURL(raw.URL)
	domain := extractDomain(normalizedURL)
	displayURL := buildDisplayURL(normalizedURL, domain)
	favicon := ""
	if domain != "" {
		favicon = "https://" + domain + "/favicon.ico"
	}

	resultType := ResultTypeOrganic
	if raw.Type != "" {
		if validType, warning := ValidateResultType(raw.Type); warning == "" {
			resultType = validType
		}
	}
	if raw.Ad {
		resultType = ResultTypeAd
	}
	// Backward compatibility for older parsers: Google answer boxes used
	// negative rank before SearchResult carried an explicit type hint.
	if raw.Rank <= 0 && !raw.Ad && raw.Type == "" {
		resultType = ResultTypeAnswerBox
	}
	rank := raw.Rank
	if rank < 0 {
		if raw.Ad {
			rank = -rank
		} else {
			rank = 0
		}
	}

	absolute := computeResultPosition(raw, ctx.Query.Start)
	if absolute <= 0 {
		absolute = rank
	}

	result := Result{
		ID:         buildResultID(ctx.Engine, normalizedURL),
		Rank:       rank,
		Type:       resultType,
		Title:      raw.Title,
		URL:        normalizedURL,
		DisplayURL: displayURL,
		Snippet:    raw.Description,
		Domain:     domain,
		Favicon:    favicon,
		Engine:     ctx.Engine,
	}
	if absolute > 0 {
		result.Position = &Position{Absolute: absolute}
	}

	result.DomainInfo = EnrichDomainInfo(domain)
	result.Classification = ClassifyURL(normalizedURL, domain)

	return result
}

// AppendEnrichedSearchResult preserves the legacy results[] surface while
// copying any extracted SERP features onto the top-level feature surface.
func AppendEnrichedSearchResult(env *Envelope, raw SearchResult, ctx EnrichContext, extractedAt time.Time) {
	var sourceResultID string
	if raw.URL != "" || raw.Title != "" || raw.Description != "" || raw.Rank != 0 {
		result := EnrichResult(raw, ctx)
		env.Results = append(env.Results, result)
		sourceResultID = result.ID
	}

	for _, rawFeature := range raw.Features {
		env.SerpFeatures = append(env.SerpFeatures, EnrichSerpFeature(rawFeature, ctx.Engine, sourceResultID, extractedAt))
	}
	if len(raw.Features) == 0 && sourceResultID != "" && shouldMirrorResultAsFeature(raw.Type) {
		result := env.Results[len(env.Results)-1]
		env.SerpFeatures = append(env.SerpFeatures, EnrichSerpFeature(SerpFeature{
			Type:     result.Type,
			Title:    result.Title,
			Text:     result.Snippet,
			Position: result.Position,
			Links: []FeatureLink{{
				Title: result.Title,
				URL:   result.URL,
			}},
		}, ctx.Engine, sourceResultID, extractedAt))
	}
}

// EnrichSerpFeature stamps a raw feature with stable public fields.
func EnrichSerpFeature(raw SerpFeature, engine string, sourceResultID string, extractedAt time.Time) SerpFeature {
	feature := raw
	feature.Engine = engine
	if feature.SourceResultIDs == nil {
		feature.SourceResultIDs = []string{}
	}
	if sourceResultID != "" && !containsString(feature.SourceResultIDs, sourceResultID) {
		feature.SourceResultIDs = append(feature.SourceResultIDs, sourceResultID)
	}
	for i := range feature.Links {
		feature.Links[i].URL = normalizeURL(feature.Links[i].URL)
	}
	for i := range feature.Items {
		feature.Items[i].Link = normalizeURL(feature.Items[i].Link)
	}
	if feature.ID == "" {
		feature.ID = buildFeatureID(feature)
	}
	if feature.ExtractedAt == "" {
		feature.ExtractedAt = extractedAt.UTC().Format(time.RFC3339)
	}
	return feature
}

func buildFeatureID(feature SerpFeature) string {
	primaryLink := ""
	if len(feature.Links) > 0 {
		primaryLink = feature.Links[0].URL
	}
	if primaryLink == "" && len(feature.Items) > 0 {
		primaryLink = feature.Items[0].Link
	}
	key := strings.Join([]string{
		feature.Engine,
		string(feature.Type),
		strings.ToLower(strings.TrimSpace(feature.Title)),
		strings.ToLower(strings.TrimSpace(feature.Text)),
		primaryLink,
	}, "|")
	return "f_" + shortMD5(key)
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func shouldMirrorResultAsFeature(t ResultType) bool {
	switch t {
	case ResultTypeAnswerBox, ResultTypeFeaturedSnippet, ResultTypeKnowledgePanel,
		ResultTypePeopleAlsoAsk, ResultTypeLocal:
		return true
	default:
		return false
	}
}

// EnrichImageResult converts a raw engine result into the v2 ImageResult shape.
func EnrichImageResult(raw SearchResult, ctx EnrichContext) ImageResult {
	imageURL := normalizeURL(raw.URL)
	meta := parseImageDescription(raw.Description)

	pageURL := meta.PageURL
	if pageURL == "" {
		pageURL = imageURL
	}
	pageURL = normalizeURL(pageURL)
	sourceDomain := extractDomain(pageURL)
	imageWidth, imageHeight := meta.Width, meta.Height

	return ImageResult{
		ID:    buildImageID(ctx.Engine, imageURL),
		Rank:  raw.Rank,
		Type:  ResultTypeImage,
		Title: raw.Title,
		Image: ImageData{
			URL:       imageURL,
			Thumbnail: meta.ThumbnailURL,
			Width:     imageWidth,
			Height:    imageHeight,
		},
		Source: ImageSource{
			PageURL: pageURL,
			Domain:  sourceDomain,
		},
		Engine: ctx.Engine,
	}
}

// buildResultID returns a stable "s_<hex>" ID for web results.
func buildResultID(engine, normalizedURL string) string {
	return "s_" + shortMD5(engine+"|"+normalizedURL)
}

// buildImageID returns a stable "i_<hex>" ID for image results.
func buildImageID(engine, imageURL string) string {
	return "i_" + shortMD5(engine+"|"+imageURL)
}

func shortMD5(value string) string {
	h := md5.Sum([]byte(value))
	return hex.EncodeToString(h[:responseIDBytes])
}

func computeResultPosition(raw SearchResult, start int) int {
	if raw.AbsoluteRank > 0 {
		return raw.AbsoluteRank
	}

	rank := raw.Rank
	if rank < 0 {
		rank = -rank
	}
	if rank == 0 {
		return 0
	}
	if start > 0 && rank > start {
		return rank
	}
	return start + rank
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

type imageDescriptionMeta struct {
	PageURL      string
	ThumbnailURL string
	Width        int
	Height       int
}

func parseImageDescription(desc string) imageDescriptionMeta {
	meta := imageDescriptionMeta{}
	trimmed := strings.TrimSpace(desc)
	if trimmed == "" {
		return meta
	}

	lower := strings.ToLower(trimmed)
	if idx := strings.Index(lower, "source page:"); idx >= 0 {
		meta.PageURL = strings.TrimSpace(trimmed[idx+len("source page:"):])
		if comma := strings.Index(meta.PageURL, ","); comma >= 0 {
			meta.PageURL = strings.TrimSpace(meta.PageURL[:comma])
		}
	} else if strings.HasPrefix(lower, "source:") {
		meta.PageURL = strings.TrimSpace(trimmed[len("source:"):])
	}

	if idx := strings.Index(lower, "thumb_url:"); idx >= 0 {
		meta.ThumbnailURL = strings.TrimSpace(trimmed[idx+len("thumb_url:"):])
		if comma := strings.Index(meta.ThumbnailURL, ","); comma >= 0 {
			meta.ThumbnailURL = strings.TrimSpace(meta.ThumbnailURL[:comma])
		}
	}

	for _, pattern := range imageDimensionPatterns {
		match := pattern.FindStringSubmatch(trimmed)
		if len(match) != 3 {
			continue
		}
		first, firstErr := strconv.Atoi(match[1])
		second, secondErr := strconv.Atoi(match[2])
		if firstErr != nil || secondErr != nil {
			continue
		}
		if strings.Contains(strings.ToLower(match[0]), "height") {
			meta.Height = first
			meta.Width = second
		} else {
			meta.Width = first
			meta.Height = second
		}
		break
	}

	if !isHTTPURL(meta.PageURL) {
		meta.PageURL = ""
	}
	if !isHTTPURL(meta.ThumbnailURL) {
		meta.ThumbnailURL = ""
	}
	return meta
}

func isHTTPURL(value string) bool {
	return strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://")
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
