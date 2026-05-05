package baidu

import (
	"io"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/karust/openserp/core"
)

// ParseHTML parses a Baidu SERP HTML document and returns search results.
// No network I/O.
func ParseHTML(r io.Reader) ([]core.SearchResult, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, err
	}
	return parseBaiduDocument(doc), nil
}

func parseBaiduDocument(doc *goquery.Document) []core.SearchResult {
	var results []core.SearchResult
	rank := 1

	doc.Find(Selectors.Results).Each(func(_ int, item *goquery.Selection) {
		linkTag := item.Find(Selectors.Link).First()
		if linkTag.Length() == 0 {
			return
		}

		href, exists := linkTag.Attr("href")
		if !exists || href == "" || href == "#" || !strings.HasPrefix(href, "http") {
			return
		}

		title := strings.TrimSpace(linkTag.Text())
		if title == "" {
			return
		}

		desc := ""
		if descTag := item.Find(Selectors.Desc).First(); descTag.Length() > 0 {
			desc = strings.TrimSpace(descTag.Text())
		}
		if desc == "" {
			full := strings.TrimSpace(item.Text())
			desc = strings.TrimSpace(strings.Replace(full, title, "", 1))
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
