package yandex

import (
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
	return parseYandexDocument(doc), nil
}

func parseYandexDocument(doc *goquery.Document) []core.SearchResult {
	var results []core.SearchResult
	rank := 1
	adRank := 1
	absoluteRank := 1

	doc.Find(Selectors.Results).Each(func(_ int, item *goquery.Selection) {
		// The neuro/AI answer renders as a serp-item li too, so it would be
		// caught here as an organic row. It is surfaced separately as an
		// ai_summary serp_feature; skip it from the rankable result stream.
		if isYandexNeuroAnswer(item) {
			return
		}

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
		isAd := yandexSelectionHasAdMarker(item) || yandexURLLooksAd(href)

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

		resultRank := rank
		if isAd {
			resultRank = adRank
			adRank++
		} else {
			rank++
		}

		results = append(results, core.SearchResult{
			Rank:         resultRank,
			AbsoluteRank: absoluteRank,
			URL:          href,
			Title:        title,
			Description:  desc,
			Ad:           isAd,
		})
		absoluteRank++
	})

	return core.AttachFeaturesToFirstResult(core.DeduplicateResults(results), extractYandexFeatures(doc))
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
