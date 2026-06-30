package duckduckgo

import (
	"errors"
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
	pageStatus := classifyDDGDocument(doc)
	if errors.Is(pageStatus, core.ErrEmptyResult) {
		return []core.SearchResult{}, nil
	}
	if pageStatus != nil {
		return nil, pageStatus
	}
	return parseDDGDocument(doc), nil
}

func classifyDDGDocument(doc *goquery.Document) error {
	return core.ClassifyChallengeDocument(doc, core.DocSignals{
		CaptchaSelectors: Selectors.CaptchaSelectors,
		CaptchaMarkers:   Selectors.CaptchaMarkers,
		EmptySelectors:   Selectors.NoResults,
	})
}

func parseDDGDocument(doc *goquery.Document) []core.SearchResult {
	var results []core.SearchResult
	rank := core.NewRankState(0)

	resultSel := firstMatchingSelector(doc, Selectors.Results)
	if resultSel == "" {
		return core.AttachFeaturesToFirstResult(results, extractDDGFeatures(doc))
	}

	doc.Find(resultSel).Each(func(_ int, item *goquery.Selection) {
		href := ddgDocumentHref(item)
		if href == "" || href == "#" || strings.HasPrefix(href, "javascript:") {
			return
		}

		title := firstText(item, Selectors.Title...)
		if title == "" {
			return
		}

		desc := firstText(item, Selectors.Desc...)
		isAd := ddgSelectionHasAdMarker(item)

		resultRank, absoluteRank := rank.Next(isAd)
		results = append(results, core.SearchResult{
			Rank:         resultRank,
			AbsoluteRank: absoluteRank,
			URL:          href,
			Title:        title,
			Description:  desc,
			Ad:           isAd,
		})
	})

	return core.AttachFeaturesToFirstResult(core.DeduplicateResults(results), extractDDGFeatures(doc))
}

// firstText returns the normalized text of the first selector that matches a
// non-empty element.
func firstText(item *goquery.Selection, selectors ...string) string {
	for _, sel := range selectors {
		if tag := item.Find(sel).First(); tag.Length() > 0 {
			if text := core.NormalizeWhitespace(tag.Text()); text != "" {
				return text
			}
		}
	}
	return ""
}

// ddgSelectionHasAdMarker reports whether the row self-or-descendant matches any
// DuckDuckGo ad-badge selector.
func ddgSelectionHasAdMarker(item *goquery.Selection) bool {
	for _, sel := range Selectors.AdBadge {
		if item.Is(sel) || item.Find(sel).Length() > 0 {
			return true
		}
	}
	return false
}

// ddgDocumentHref returns the first non-empty href among the link selectors,
// trimmed. It skips a selector whose anchor has an absent/empty href and tries
// the next (the pre-refactor extractFirstAttr behavior).
func ddgDocumentHref(item *goquery.Selection) string {
	for _, sel := range Selectors.Link {
		tag := item.Find(sel).First()
		if tag.Length() == 0 {
			continue
		}
		if val, exists := tag.Attr("href"); exists && val != "" {
			return strings.TrimSpace(val)
		}
	}
	return ""
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
