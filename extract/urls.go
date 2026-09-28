package extract

import (
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// effectiveBaseURL applies the first <base href> outside <template>. An unusable
// first href falls back to the document URL, later <base> elements are ignored.
func effectiveBaseURL(doc *goquery.Document, documentURL string) string {
	href, _ := doc.Find("base[href]").Not("template base").First().Attr("href")
	base, err := url.Parse(resolveURL(documentURL, href))
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return documentURL
	}
	return base.String()
}

// resolveContentURLs makes content links absolute before trafilatura sees them:
// it resolves relative hrefs with path.Join (broken for file-like bases) and
// rewrites protocol-relative image srcs to http.
func resolveContentURLs(doc *goquery.Document, baseURL string) {
	doc.Find("a[href], img").Each(func(_ int, sel *goquery.Selection) {
		for _, attr := range []string{"href", "src", "data-src"} {
			if raw, ok := sel.Attr(attr); ok {
				if resolved := resolveURL(baseURL, raw); resolved != "" {
					sel.SetAttr(attr, resolved)
				}
			}
		}
	})
}

// resolveURL returns raw as an absolute URL, or "" for empty, fragment-only,
// javascript: and malformed references.
func resolveURL(baseURL string, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "#") || strings.HasPrefix(strings.ToLower(raw), "javascript:") {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if parsed.IsAbs() {
		return parsed.String()
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	return base.ResolveReference(parsed).String()
}
