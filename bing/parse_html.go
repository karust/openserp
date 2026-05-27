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
	absoluteRank := 1

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

		link, exists := titleTag.Attr("href")
		if !exists || link == "" || link == "#" {
			return
		}

		title := titleTag.Text()
		if title == "" {
			title = extractFirstText(item, Selectors.TitleFallbacks)
		}
		if title == "" {
			return
		}

		desc := descriptionFromItem(item, title)

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
			URL:          link,
			Title:        title,
			Description:  desc,
			Ad:           isAd,
		})
		absoluteRank++
	})

	return core.AttachFeaturesToFirstResult(core.DeduplicateResults(results), extractBingFeatures(doc))
}

func extractFirstText(item *goquery.Selection, selectors []string) string {
	for _, selector := range selectors {
		if tag := item.Find(selector).First(); tag.Length() > 0 {
			if text := strings.TrimSpace(tag.Text()); text != "" {
				return text
			}
			if label, exists := tag.Attr("aria-label"); exists {
				if label = strings.TrimSpace(label); label != "" {
					return label
				}
			}
		}
	}
	return ""
}

// descriptionFromItem extracts a description using the same 4-step fallback
// chain as the rod-based browser parser. Bing renders snippet text with heavy
// source-indentation whitespace, so each candidate is whitespace-collapsed.
func descriptionFromItem(item *goquery.Selection, title string) string {
	if descTag := item.Find(Selectors.DescPrimary).First(); descTag.Length() > 0 {
		if text := normalizeWhitespace(descTag.Text()); text != "" {
			return text
		}
	}
	if descTag := item.Find(Selectors.DescFallback).First(); descTag.Length() > 0 {
		if text := normalizeWhitespace(descTag.Text()); text != "" {
			return text
		}
	}
	if descTag := item.Find(Selectors.DescAny).First(); descTag.Length() > 0 {
		if text := normalizeWhitespace(descTag.Text()); text != "" {
			return text
		}
	}
	// Structural fallback: strip title from full text
	return normalizeWhitespace(strings.Replace(item.Text(), title, "", 1))
}

// normalizeWhitespace collapses runs of whitespace (including the newlines and
// indentation Bing leaves in snippet markup) into single spaces.
func normalizeWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
