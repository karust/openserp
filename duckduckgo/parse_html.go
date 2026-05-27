package duckduckgo

import (
	"io"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/karust/openserp/core"
)

// ParseHTML parses a DuckDuckGo SERP HTML document and returns search results.
// No network I/O.
func ParseHTML(r io.Reader) ([]core.SearchResult, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, err
	}
	return parseDDGDocument(doc), nil
}

func parseDDGDocument(doc *goquery.Document) []core.SearchResult {
	var results []core.SearchResult
	rank := 1
	adRank := 1
	absoluteRank := 1

	resultSel := firstMatchingSelector(doc, Selectors.Results)
	if resultSel == "" {
		return core.AttachFeaturesToFirstResult(results, extractDDGFeatures(doc))
	}

	doc.Find(resultSel).Each(func(_ int, item *goquery.Selection) {
		href := extractFirstAttr(item, Selectors.Link, "href")
		if href == "" || href == "#" || strings.HasPrefix(href, "javascript:") {
			return
		}

		title := extractFirstText(item, Selectors.Title)
		if title == "" {
			return
		}

		desc := extractFirstText(item, Selectors.Desc)

		isAd := duckduckgoSelectionHasAdMarker(item)

		r := core.SearchResult{
			Rank:         rank,
			AbsoluteRank: absoluteRank,
			URL:          href,
			Title:        title,
			Description:  desc,
			Ad:           isAd,
		}
		if !isAd {
			rank++
		} else {
			r.Rank = adRank
			adRank++
		}
		results = append(results, r)
		absoluteRank++
	})

	return core.AttachFeaturesToFirstResult(core.DeduplicateResults(results), extractDDGFeatures(doc))
}

func duckduckgoSelectionHasAdMarker(item *goquery.Selection) bool {
	for _, sel := range Selectors.AdBadge {
		if item.Is(sel) || item.Find(sel).Length() > 0 {
			return true
		}
	}
	return false
}

// firstMatchingSelector returns the first selector from the list that matches
// at least one element in the document.
func firstMatchingSelector(doc *goquery.Document, selectors []string) string {
	for _, sel := range selectors {
		if doc.Find(sel).Length() > 0 {
			return sel
		}
	}
	return ""
}

// extractFirstAttr tries each selector in order and returns the named attribute
// of the first match, or "".
func extractFirstAttr(item *goquery.Selection, selectors []string, attr string) string {
	for _, sel := range selectors {
		tag := item.Find(sel).First()
		if tag.Length() == 0 {
			continue
		}
		val, exists := tag.Attr(attr)
		if exists && val != "" {
			return strings.TrimSpace(val)
		}
	}
	return ""
}

// extractFirstText tries each selector in order and returns the trimmed text of
// the first match, or "".
func extractFirstText(item *goquery.Selection, selectors []string) string {
	for _, sel := range selectors {
		tag := item.Find(sel).First()
		if tag.Length() == 0 {
			continue
		}
		if text := strings.TrimSpace(tag.Text()); text != "" {
			return text
		}
	}
	return ""
}
