package ecosia

import (
	"io"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/karust/openserp/core"
)

// ParseHTML parses an Ecosia SERP HTML document and returns search results.
// No network I/O.
func ParseHTML(r io.Reader) ([]core.SearchResult, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, err
	}
	return parseEcosiaDocument(doc), nil
}

func parseEcosiaDocument(doc *goquery.Document) []core.SearchResult {
	var results []core.SearchResult
	rank := 1
	adRank := 1

	doc.Find(Selectors.Result).Each(func(_ int, item *goquery.Selection) {
		res, ok := parseEcosiaItem(item, rank, false)
		if !ok {
			return
		}
		results = append(results, res)
		rank++
	})

	doc.Find(Selectors.Ad).Each(func(_ int, item *goquery.Selection) {
		res, ok := parseEcosiaItem(item, adRank, true)
		if !ok {
			return
		}
		results = append(results, res)
		adRank++
	})

	setSeparatedAdAbsoluteRanks(results, 0)
	return core.AttachFeaturesToFirstResult(core.DeduplicateResults(results), extractEcosiaFeatures(doc))
}

func parseEcosiaItem(item *goquery.Selection, rank int, ad bool) (core.SearchResult, bool) {
	linkTag := item.Find(Selectors.ResultLink).First()
	if linkTag.Length() == 0 {
		linkTag = item.Find("a[href]").First()
	}
	if linkTag.Length() == 0 {
		return core.SearchResult{}, false
	}

	href, exists := linkTag.Attr("href")
	if !exists {
		return core.SearchResult{}, false
	}
	href = strings.TrimSpace(href)
	if href == "" || strings.HasPrefix(href, "javascript:") {
		return core.SearchResult{}, false
	}

	title := ""
	if t := item.Find(Selectors.Title).First(); t.Length() > 0 {
		title = strings.TrimSpace(t.Text())
	}
	if title == "" {
		if t := item.Find("h2, h3").First(); t.Length() > 0 {
			title = strings.TrimSpace(t.Text())
		}
	}

	desc := ""
	if d := item.Find(Selectors.Desc).First(); d.Length() > 0 {
		desc = strings.TrimSpace(d.Text())
	}

	return core.SearchResult{
		Rank:        rank,
		URL:         href,
		Title:       title,
		Description: desc,
		Ad:          ad,
	}, true
}

func setSeparatedAdAbsoluteRanks(results []core.SearchResult, start int) {
	adCount := 0
	for i := range results {
		if results[i].Ad {
			adCount++
			results[i].AbsoluteRank = start + results[i].Rank
		}
	}
	organicAbsoluteRank := start + adCount + 1
	for i := range results {
		if results[i].Ad {
			continue
		}
		results[i].AbsoluteRank = organicAbsoluteRank
		organicAbsoluteRank++
	}
}
