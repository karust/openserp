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
		linkTag := item.Find("a").First()
		if linkTag.Length() == 0 {
			return
		}

		href, exists := linkTag.Attr("href")
		if !exists || href == "" || href == "#" || strings.HasPrefix(href, "javascript:") {
			return
		}

		titleTag := item.Find(Selectors.Title).First()
		if titleTag.Length() == 0 {
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
