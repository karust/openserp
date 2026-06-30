package bing

import (
	"errors"
	"io"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/karust/openserp/core"
)

// ParseHTML parses a Bing SERP HTML document and returns search results.
// Mirrors the rod-based parser in search.go but operates on a goquery doc.
// No network I/O.
func ParseHTML(r io.Reader) ([]core.SearchResult, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, err
	}
	pageStatus := classifyBingDocument(doc)
	if errors.Is(pageStatus, core.ErrEmptyResult) {
		return []core.SearchResult{}, nil
	}
	if pageStatus != nil {
		return nil, pageStatus
	}
	return parseBingDocument(doc), nil
}

func classifyBingDocument(doc *goquery.Document) error {
	return core.ClassifyChallengeDocument(doc, core.DocSignals{
		CaptchaSelectors: Selectors.Captcha,
		CaptchaMarkers:   Selectors.CaptchaMarkers,
		EmptyMarkers:     Selectors.NoResultsMarkers,
	})
}

func parseBingDocument(doc *goquery.Document) []core.SearchResult {
	var results []core.SearchResult
	rank := core.NewRankState(0)

	doc.Find(Selectors.ResultItems).Each(func(_ int, item *goquery.Selection) {
		isAd := item.Is(Selectors.Ads)
		isOrganic := item.Is(Selectors.Results)
		if !isAd && !isOrganic {
			return
		}

		titleSelector := Selectors.Title
		if isAd {
			titleSelector = Selectors.AdTitle
		}

		titleTag := item.Find(titleSelector).First()
		if titleTag.Length() == 0 {
			return
		}
		href, _ := titleTag.Attr("href")
		title := bingDocumentTitle(item, titleTag)
		desc := bingDocumentDescription(item, title)

		if res, ok := assembleBingRow(href, title, desc, isAd, rank); ok {
			results = append(results, res)
		}
	})

	return core.AttachFeaturesToFirstResult(core.DeduplicateResults(results), extractBingFeatures(doc))
}

// assembleBingRow validates an already-extracted Bing row and assigns ranks.
// Shared by the rod (browser) and goquery (raw / parse) parsers, which differ
// only in how they pull title/href/desc out of the DOM.
func assembleBingRow(href, title, desc string, isAd bool, rank *core.RankState) (core.SearchResult, bool) {
	url := strings.TrimSpace(href)
	if url == "" || url == "#" || strings.HasPrefix(url, "javascript:") {
		return core.SearchResult{}, false
	}
	if title == "" {
		return core.SearchResult{}, false
	}

	resultRank, absoluteRank := rank.Next(isAd)
	return core.SearchResult{
		Rank:         resultRank,
		AbsoluteRank: absoluteRank,
		URL:          url,
		Title:        title,
		Description:  desc,
		Ad:           isAd,
	}, true
}

// bingDocumentTitle reproduces the rod path's title fallback for goquery: the
// title anchor's aria-label/title attribute, then its text, then any fallback
// selector's text or aria-label.
func bingDocumentTitle(item, titleTag *goquery.Selection) string {
	if title := firstNonEmptyAttr(titleTag, "aria-label", "title"); title != "" {
		return title
	}
	if title := core.NormalizeWhitespace(titleTag.Text()); title != "" {
		return title
	}
	for _, selector := range Selectors.TitleFallbacks {
		tag := item.Find(selector).First()
		if tag.Length() == 0 {
			continue
		}
		if text := core.NormalizeWhitespace(tag.Text()); text != "" {
			return text
		}
		if label := firstNonEmptyAttr(tag, "aria-label", "title"); label != "" {
			return label
		}
	}
	return ""
}

// bingDocumentDescription reproduces the rod path's 3-selector description
// fallback plus the strip-title structural fallback. Bing renders snippet text
// with heavy source-indentation whitespace, so each candidate is collapsed.
func bingDocumentDescription(item *goquery.Selection, title string) string {
	for _, selector := range []string{Selectors.DescPrimary, Selectors.DescFallback, Selectors.DescAny} {
		if tag := item.Find(selector).First(); tag.Length() > 0 {
			if text := core.NormalizeWhitespace(tag.Text()); text != "" {
				return text
			}
		}
	}
	return core.NormalizeWhitespace(strings.Replace(item.Text(), title, "", 1))
}

func firstNonEmptyAttr(item *goquery.Selection, attrs ...string) string {
	for _, attr := range attrs {
		value, exists := item.Attr(attr)
		if !exists {
			continue
		}
		if value = core.NormalizeWhitespace(value); value != "" {
			return value
		}
	}
	return ""
}
