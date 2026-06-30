package yandex

import (
	"errors"
	"io"
	"net/url"
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
	pageStatus := classifyYandexDocument(doc)
	if errors.Is(pageStatus, core.ErrEmptyResult) {
		return []core.SearchResult{}, nil
	}
	if pageStatus != nil {
		return nil, pageStatus
	}
	return parseYandexDocument(doc), nil
}

func classifyYandexDocument(doc *goquery.Document) error {
	return core.ClassifyChallengeDocument(doc, core.DocSignals{
		CaptchaSelectors: []string{Selectors.Captcha},
		EmptySelectors:   []string{Selectors.NoResults},
	})
}

func parseYandexDocument(doc *goquery.Document) []core.SearchResult {
	var results []core.SearchResult
	rank := core.NewRankState(0)

	doc.Find(Selectors.Results).Each(func(_ int, item *goquery.Selection) {
		// The neuro/AI answer renders as a serp-item li too, so it would be
		// caught here as an organic row. It is surfaced separately as an
		// ai_summary serp_feature; skip it from the rankable result stream.
		if isYandexNeuroAnswer(item) {
			return
		}

		href, ok := yandexDocumentHref(item)
		if !ok {
			return
		}
		title := core.NormalizeWhitespace(item.Find(Selectors.Title).First().Text())
		desc := firstNonEmptyText(item, Selectors.Desc, Selectors.DescFallback)
		isAd := yandexSelectionHasAdMarker(item) || yandexURLLooksAd(href)

		if res, ok := assembleYandexRow(href, title, desc, isAd, rank); ok {
			results = append(results, res)
		}
	})

	return core.AttachFeaturesToFirstResult(core.DeduplicateResults(results), extractYandexFeatures(doc))
}

// assembleYandexRow validates an already-extracted Yandex row and assigns ranks.
// Shared by the rod (browser) and goquery (raw / parse) parsers, which differ
// only in how they pull title/href/desc out of the DOM.
func assembleYandexRow(href, title, desc string, isAd bool, rank *core.RankState) (core.SearchResult, bool) {
	href = strings.TrimSpace(href)
	if href == "" || href == "#" || strings.HasPrefix(href, "javascript:") {
		return core.SearchResult{}, false
	}
	if title == "" {
		return core.SearchResult{}, false
	}

	resultRank, absoluteRank := rank.Next(isAd)
	return core.SearchResult{
		Rank:         resultRank,
		AbsoluteRank: absoluteRank,
		URL:          href,
		Title:        title,
		Description:  desc,
		Ad:           isAd,
	}, true
}

// yandexDocumentHref resolves a result row's link href in the goquery path:
// the canonical organic-title link, then the <a> wrapping the title, then any
// <a> in the block. ok=false when no anchor with an href attribute is found.
func yandexDocumentHref(item *goquery.Selection) (string, bool) {
	linkTag := item.Find(Selectors.LinkPrimary).First()
	if linkTag.Length() == 0 {
		linkTag = item.Find(Selectors.Title).First().Closest("a")
	}
	if linkTag.Length() == 0 {
		linkTag = item.Find(Selectors.Link).First()
	}
	if linkTag.Length() == 0 {
		return "", false
	}
	href, exists := linkTag.Attr("href")
	if !exists {
		return "", false
	}
	return href, true
}

// firstNonEmptyText returns the normalized text of the first selector that
// matches a non-empty element, the goquery counterpart of core.FirstNonEmptyText.
func firstNonEmptyText(item *goquery.Selection, selectors ...string) string {
	for _, selector := range selectors {
		if tag := item.Find(selector).First(); tag.Length() > 0 {
			if text := core.NormalizeWhitespace(tag.Text()); text != "" {
				return text
			}
		}
	}
	return ""
}

// isYandexNeuroAnswer reports whether a serp-item is the AI/neuro answer card.
func isYandexNeuroAnswer(item *goquery.Selection) bool {
	if name, ok := item.Attr("data-fast-name"); ok && name == "neuro_answer" {
		return true
	}
	return item.Find("[data-fast-name='neuro_answer']").Length() > 0
}

func yandexSelectionHasAdMarker(item *goquery.Selection) bool {
	for _, selector := range Selectors.AdMarkers {
		if item.Is(selector) || item.Find(selector).Length() > 0 {
			return true
		}
	}
	return false
}

func yandexURLLooksAd(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	return strings.HasPrefix(host, "yabs.yandex.") ||
		host == "an.yandex.ru" ||
		strings.HasSuffix(host, ".yandexadexchange.net")
}

func skipOrganicResults(results []core.SearchResult, skip int) []core.SearchResult {
	if skip <= 0 {
		return results
	}
	out := results[:0]
	for _, result := range results {
		if !result.Ad && skip > 0 {
			skip--
			continue
		}
		out = append(out, result)
	}
	return out
}

func rebaseOrganicRanks(results []core.SearchResult, start int) {
	if start <= 0 {
		return
	}
	organicIdx := 0
	for i := range results {
		if results[i].Ad {
			continue
		}
		organicIdx++
		results[i].Rank = start + organicIdx
	}
}

func offsetAbsoluteRanks(results []core.SearchResult, start int) {
	if start <= 0 {
		return
	}
	for i := range results {
		if results[i].AbsoluteRank > 0 {
			results[i].AbsoluteRank += start
		}
	}
}
