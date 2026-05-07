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
	for _, selector := range baiduResultSelectors() {
		if results := parseBaiduSelection(doc.Find(selector)); len(results) > 0 {
			return results
		}
	}
	return nil
}

func baiduResultSelectors() []string {
	selectors := make([]string, 0, len(Selectors.ResultsAlt)+1)
	selectors = append(selectors, Selectors.Results)
	selectors = append(selectors, Selectors.ResultsAlt...)
	return selectors
}

func parseBaiduSelection(sel *goquery.Selection) []core.SearchResult {
	var results []core.SearchResult
	rank := 1

	sel.Each(func(_ int, item *goquery.Selection) {
		// h3-first: organic results always carry a heading; this filters out
		// non-result blocks that may share the wrapper class.
		titleTag := item.Find("h3").First()
		var (
			title   string
			linkTag *goquery.Selection
		)
		if titleTag.Length() > 0 {
			title = strings.TrimSpace(titleTag.Text())
			if child := titleTag.Find("a[href]").First(); child.Length() > 0 {
				linkTag = child
			} else if closest := titleTag.Closest("a[href]"); closest.Length() > 0 {
				linkTag = closest
			}
		}
		if linkTag == nil || linkTag.Length() == 0 {
			first := item.Find(Selectors.Link).First()
			if first.Length() == 0 {
				return
			}
			linkTag = first
		}
		if title == "" {
			title = strings.TrimSpace(linkTag.Text())
		}
		if title == "" {
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

		desc := ""
		if descTag := item.Find(Selectors.Desc).First(); descTag.Length() > 0 {
			desc = strings.TrimSpace(descTag.Text())
		}
		if desc == "" {
			for _, alt := range Selectors.DescAlt {
				if descTag := item.Find(alt).First(); descTag.Length() > 0 {
					if t := strings.TrimSpace(descTag.Text()); t != "" {
						desc = t
						break
					}
				}
			}
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

	// Re-rank sequentially after dedup so callers get a clean 1..N sequence
	// (dedup may drop intermediate ranks when the same URL appears in
	// multiple Baidu result-card variants on the same SERP).
	deduped := core.DeduplicateResults(results)
	for i := range deduped {
		deduped[i].Rank = i + 1
	}
	return deduped
}
