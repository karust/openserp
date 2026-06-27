package ecosia

import (
	"fmt"
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
		if res, ok := parseEcosiaSelectionRow(item, rank, false); ok {
			results = append(results, res)
			rank++
		}
	})

	doc.Find(Selectors.Ad).Each(func(_ int, item *goquery.Selection) {
		if res, ok := parseEcosiaSelectionRow(item, adRank, true); ok {
			results = append(results, res)
			adRank++
		}
	})

	core.SetSeparatedAdAbsoluteRanks(results, 0)
	return core.AttachFeaturesToFirstResult(core.DeduplicateResults(results), extractEcosiaFeatures(doc))
}

// parseEcosiaSelectionRow extracts a web row from a goquery selection.
func parseEcosiaSelectionRow(item *goquery.Selection, rank int, ad bool) (core.SearchResult, bool) {
	link := item.Find(Selectors.ResultLink).First()
	if link.Length() == 0 {
		// Fall back to the first anchor when the test-id selector is absent.
		link = item.Find("a[href]").First()
	}
	href, _ := link.Attr("href")

	title := selectionText(item, Selectors.Title)
	if title == "" {
		title = selectionText(item, "h2, h3")
	}
	desc := selectionText(item, Selectors.Desc)
	return assembleEcosiaRow(href, title, desc, rank, ad)
}

// assembleEcosiaRow validates an already-extracted web row and builds the
// result. Shared by the rod (browser) and goquery (raw / parse) parsers.
func assembleEcosiaRow(href, title, desc string, rank int, ad bool) (core.SearchResult, bool) {
	href = strings.TrimSpace(href)
	if href == "" || strings.HasPrefix(href, "javascript:") {
		return core.SearchResult{}, false
	}
	return core.SearchResult{
		Rank:        rank,
		URL:         href,
		Title:       strings.TrimSpace(title),
		Description: strings.TrimSpace(desc),
		Ad:          ad,
	}, true
}

// assembleEcosiaImageRow validates an already-extracted image card and builds
// the result (formatting the source/dimensions description). Shared by both
// parser backends.
func assembleEcosiaImageRow(href, title, source, dims string, rank int) (core.SearchResult, bool) {
	href = strings.TrimSpace(href)
	if href == "" {
		return core.SearchResult{}, false
	}
	desc := source
	if dims != "" {
		if source != "" {
			desc = fmt.Sprintf("%s (%s)", source, dims)
		} else {
			desc = dims
		}
	}
	return core.SearchResult{
		Rank:        rank,
		URL:         href,
		Title:       title,
		Description: desc,
	}, true
}

// selectionText returns the trimmed text of the first selector that matches,
// the goquery counterpart of the rod element's selector-fallback text lookup.
func selectionText(item *goquery.Selection, selectors ...string) string {
	for _, selector := range selectors {
		if tag := item.Find(selector).First(); tag.Length() > 0 {
			return strings.TrimSpace(tag.Text())
		}
	}
	return ""
}
