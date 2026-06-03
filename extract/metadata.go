package extract

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

type pageMetadata struct {
	Title       string
	Description string
	Canonical   string
	Lang        string
	OGTags      map[string]string
	SchemaOrg   []json.RawMessage
	Headings    []Heading
	Links       []Link
}

func parseMetadata(doc *goquery.Document, baseURL string) pageMetadata {
	var meta pageMetadata
	if doc == nil {
		return meta
	}
	meta.Title = firstNonEmpty(
		strings.TrimSpace(doc.Find("title").First().Text()),
		metaContent(doc, `meta[property="og:title"]`),
		metaContent(doc, `meta[name="twitter:title"]`),
	)
	meta.Description = firstNonEmpty(
		metaContent(doc, `meta[name="description"]`),
		metaContent(doc, `meta[property="og:description"]`),
		metaContent(doc, `meta[name="twitter:description"]`),
	)
	if canonical, ok := doc.Find(`link[rel="canonical"]`).First().Attr("href"); ok {
		meta.Canonical = resolveURL(baseURL, canonical)
	}
	if lang, ok := doc.Find("html").First().Attr("lang"); ok {
		meta.Lang = strings.TrimSpace(lang)
	}

	meta.OGTags = map[string]string{}
	doc.Find(`meta[property^="og:"], meta[name^="twitter:"]`).Each(func(_ int, sel *goquery.Selection) {
		key, _ := sel.Attr("property")
		if key == "" {
			key, _ = sel.Attr("name")
		}
		value, _ := sel.Attr("content")
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			meta.OGTags[key] = value
		}
	})
	if len(meta.OGTags) == 0 {
		meta.OGTags = nil
	}

	doc.Find(`script[type="application/ld+json"]`).Each(func(_ int, sel *goquery.Selection) {
		appendJSONLD(&meta.SchemaOrg, strings.TrimSpace(sel.Text()))
	})

	doc.Find("h1,h2,h3,h4,h5,h6").Each(func(_ int, sel *goquery.Selection) {
		level, _ := strconv.Atoi(strings.TrimPrefix(strings.ToLower(goquery.NodeName(sel)), "h"))
		text := strings.TrimSpace(sel.Text())
		if level >= 1 && level <= 6 && text != "" {
			meta.Headings = append(meta.Headings, Heading{Level: level, Text: collapseWhitespace(text)})
		}
	})

	doc.Find("a[href]").Each(func(_ int, sel *goquery.Selection) {
		if len(meta.Links) >= 100 {
			return
		}
		href, _ := sel.Attr("href")
		resolved := resolveURL(baseURL, href)
		if resolved == "" {
			return
		}
		text := collapseWhitespace(strings.TrimSpace(sel.Text()))
		meta.Links = append(meta.Links, Link{Text: text, URL: resolved})
	})
	return meta
}

func metaContent(doc *goquery.Document, selector string) string {
	value, _ := doc.Find(selector).First().Attr("content")
	return strings.TrimSpace(value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func appendJSONLD(dst *[]json.RawMessage, raw string) {
	if raw == "" {
		return
	}
	var decoded any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return
	}
	switch value := decoded.(type) {
	case map[string]any:
		if graph, ok := value["@graph"].([]any); ok {
			for _, item := range graph {
				if data, err := json.Marshal(item); err == nil {
					*dst = append(*dst, data)
				}
			}
			return
		}
	case []any:
		for _, item := range value {
			if data, err := json.Marshal(item); err == nil {
				*dst = append(*dst, data)
			}
		}
		return
	}
	if data, err := json.Marshal(decoded); err == nil {
		*dst = append(*dst, data)
	}
}

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

func collapseWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
