package bing

import (
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
	return parseBingDocument(doc), nil
}

func parseBingDocument(doc *goquery.Document) []core.SearchResult {
	var results []core.SearchResult
	rank := 1
	adRank := 1

	doc.Find(Selectors.Results).Each(func(_ int, item *goquery.Selection) {
		titleTag := item.Find(Selectors.Title).First()
		if titleTag.Length() == 0 {
			return
		}

		link, exists := titleTag.Attr("href")
		if !exists || link == "" || link == "#" {
			return
		}

		title := titleTag.Text()
		if title == "" {
			return
		}

		desc := descriptionFromItem(item, title)

		results = append(results, core.SearchResult{
			Rank:        rank,
			URL:         link,
			Title:       title,
			Description: desc,
			Ad:          false,
		})
		rank++
	})

	doc.Find(Selectors.Ads).Each(func(_ int, item *goquery.Selection) {
		titleTag := item.Find(Selectors.AdTitle).First()
		if titleTag.Length() == 0 {
			return
		}

		link, exists := titleTag.Attr("href")
		if !exists || link == "" {
			return
		}

		title := titleTag.Text()
		desc := ""
		if descTag := item.Find("p").First(); descTag.Length() > 0 {
			desc = descTag.Text()
		}

		results = append(results, core.SearchResult{
			Rank:        adRank,
			URL:         link,
			Title:       title,
			Description: desc,
			Ad:          true,
		})
		adRank++
	})

	setSeparatedAdAbsoluteRanks(results, 0)
	return core.DeduplicateResults(results)
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

// descriptionFromItem extracts a description using the same 4-step fallback
// chain as the rod-based browser parser.
func descriptionFromItem(item *goquery.Selection, title string) string {
	if descTag := item.Find(Selectors.DescPrimary).First(); descTag.Length() > 0 {
		if text := strings.TrimSpace(descTag.Text()); text != "" {
			return text
		}
	}
	if descTag := item.Find(Selectors.DescFallback).First(); descTag.Length() > 0 {
		if text := strings.TrimSpace(descTag.Text()); text != "" {
			return text
		}
	}
	if descTag := item.Find("p").First(); descTag.Length() > 0 {
		if text := strings.TrimSpace(descTag.Text()); text != "" {
			return text
		}
	}
	// Structural fallback: strip title from full text
	return strings.TrimSpace(strings.Replace(item.Text(), title, "", 1))
}
