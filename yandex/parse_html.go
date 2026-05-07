package yandex

import (
	"io"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/karust/openserp/core"
)

// ParseHTML parses a Yandex SERP HTML document and returns search results.
// No network I/O.
func ParseHTML(r io.Reader) ([]core.SearchResult, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, err
	}
	return parseYandexDocument(doc), nil
}

func parseYandexDocument(doc *goquery.Document) []core.SearchResult {
	var results []core.SearchResult
	rank := 1

	doc.Find(Selectors.Results).Each(func(_ int, item *goquery.Selection) {
		// Skip blocks without a result heading (filters out non-organic blocks
		// that share the result-row container).
		titleTag := item.Find(Selectors.Title).First()
		if titleTag.Length() == 0 {
			return
		}

		// Prefer the canonical organic-title link, then the closest <a> wrapping
		// the title, then any <a> in the block.
		linkTag := item.Find(Selectors.LinkPrimary).First()
		if linkTag.Length() == 0 {
			linkTag = titleTag.Closest("a")
		}
		if linkTag.Length() == 0 {
			linkTag = item.Find(Selectors.Link).First()
		}
		if linkTag.Length() == 0 {
			return
		}

		href, exists := linkTag.Attr("href")
		if !exists {
			return
		}
		href = strings.TrimSpace(href)
		if href == "" || href == "#" || strings.HasPrefix(href, "javascript:") {
			return
		}

		title := strings.TrimSpace(titleTag.Text())
		if title == "" {
			return
		}

		desc := ""
		if descTag := item.Find(Selectors.Desc).First(); descTag.Length() > 0 {
			desc = strings.TrimSpace(descTag.Text())
		}
		if desc == "" {
			if descTag := item.Find(Selectors.DescFallback).First(); descTag.Length() > 0 {
				desc = strings.TrimSpace(descTag.Text())
			}
		}

		results = append(results, core.SearchResult{
			Rank:        rank,
			URL:         href,
			Title:       title,
			Description: desc,
		})
		rank++
	})

	return core.DeduplicateResults(results)
}
