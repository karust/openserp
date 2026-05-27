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
	features := extractBaiduFeatures(doc)
	// Match all result-card variants in one pass so DOM order is preserved and
	// every card type (organic www_index, baike/encyclopedia, op cards) is
	// collected. Selecting one variant at a time and returning on the first hit
	// dropped baike and other result-op cards that interleave with organic rows.
	results := parseBaiduSelection(doc.Find(baiduResultSelector()))
	return core.AttachFeaturesToFirstResult(results, features)
}

func baiduResultSelectors() []string {
	selectors := make([]string, 0, len(Selectors.ResultsAlt)+1)
	selectors = append(selectors, Selectors.Results)
	selectors = append(selectors, Selectors.ResultsAlt...)
	return selectors
}

func baiduResultSelector() string {
	return strings.Join(baiduResultSelectors(), ", ")
}

func parseBaiduSelection(sel *goquery.Selection) []core.SearchResult {
	var results []core.SearchResult
	rank := 1
	adRank := 1
	absoluteRank := 1

	sel.Each(func(_ int, item *goquery.Selection) {
		isAd := baiduSelectionHasAdMarker(item)

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
		// Organic Baidu results link out through an absolute redirect
		// (http://www.baidu.com/link?url=...). Op cards like "People also search"
		// (tpl=recommend_list) instead carry relative on-site search links
		// (/s?wd=...); treat those as related-search modules, not organic rows.
		if strings.HasPrefix(href, "/") {
			return
		}
		// Baidu result cards carry the canonical destination in the mu= attribute
		// (e.g. baike.baidu.com, britannica.com), while the visible link is an
		// opaque www.baidu.com/link?url= redirect. Prefer mu= so callers get the
		// real URL, which also enables domain-based classification (encyclopedia,
		// news, etc.) downstream.
		if mu := canonicalBaiduURL(item); mu != "" {
			href = mu
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

	// Re-rank sequentially after dedup so callers get a clean 1..N sequence
	// (dedup may drop intermediate ranks when the same URL appears in
	// multiple Baidu result-card variants on the same SERP).
	deduped := core.DeduplicateResults(results)
	organicIdx := 0
	for i := range deduped {
		if deduped[i].Ad {
			continue
		}
		organicIdx++
		deduped[i].Rank = organicIdx
	}
	return deduped
}

// canonicalBaiduURL returns the card's mu= destination when it is an absolute
// http(s) URL. The attribute lives on the result-card container; when the title
// link is nested, walk up to the nearest ancestor that carries it.
func canonicalBaiduURL(item *goquery.Selection) string {
	mu := strings.TrimSpace(firstAttrValue(item, "mu"))
	if mu == "" {
		if host := item.Closest("[mu]"); host.Length() > 0 {
			mu = strings.TrimSpace(firstAttrValue(host, "mu"))
		}
	}
	if strings.HasPrefix(mu, "http://") || strings.HasPrefix(mu, "https://") {
		return mu
	}
	return ""
}

func firstAttrValue(item *goquery.Selection, name string) string {
	if value, ok := item.Attr(name); ok {
		return value
	}
	return ""
}

func baiduSelectionHasAdMarker(item *goquery.Selection) bool {
	for _, selector := range Selectors.AdMarkers {
		if item.Is(selector) || item.Find(selector).Length() > 0 {
			return true
		}
	}

	isAd := false
	item.Find("span, i, em").EachWithBreak(func(_ int, marker *goquery.Selection) bool {
		text := strings.TrimSpace(marker.Text())
		if text == "广告" || text == "推广" || text == "商业推广" {
			isAd = true
			return false
		}
		return true
	})
	return isAd
}
