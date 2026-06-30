package google

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/karust/openserp/core"
	"github.com/sirupsen/logrus"
)

// Matches a literal zero result count, e.g. "about 0 results". Requires "0"
// as its own word so it doesn't match large counts like "10,500,000 results".
var zeroResultsPattern = regexp.MustCompile(`\b0 results\b`)

// ParseHTML parses a Google SERP HTML document and returns search results.
// It is the pure parser used by both raw HTTP search and parse endpoints.
func ParseHTML(r io.Reader) ([]core.SearchResult, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, err
	}
	pageStatus := classifyGoogleDocument(doc)
	if errors.Is(pageStatus, core.ErrEmptyResult) {
		return []core.SearchResult{}, nil
	}
	if pageStatus != nil {
		return nil, pageStatus
	}
	return parseGoogleDocument(doc), nil
}

func parseGoogleDocument(doc *goquery.Document) []core.SearchResult {
	results := []core.SearchResult{}
	rank := core.NewRankState(0)

	// Prefer the canonical organic result block (div.tF2Cxc, innermost). Fall
	// back to the broad attribute selector only when no tF2Cxc blocks exist, so
	// older/alternate SERP layouts still parse.
	sel := doc.Find(Selectors.Results)
	if sel.Length() == 0 {
		sel = doc.Find(Selectors.ResultsBroad)
	}

	for i := range sel.Nodes {
		item := sel.Eq(i)

		// Skip items without an h3 element (which indicates a search result)
		if item.Find(Selectors.Title).Length() == 0 {
			continue
		}

		// Find URL - look for the anchor that contains the h3 title
		linkTag := item.Find(Selectors.Title).Parent()
		if !linkTag.Is("a") {
			linkTag = item.Find(Selectors.Title).Closest("a")
		}

		link, exists := linkTag.Attr("href")
		if !exists || link == "" || link == "#" {
			continue
		}
		link = strings.Trim(link, " ")

		// Find title - this is inside the h3 element
		titleTag := item.Find(Selectors.Title)
		title := titleTag.Text()

		isAd := item.Is(Selectors.Ad) || item.Find(Selectors.Ad).Length() > 0

		// Find description - find div with text content after the heading
		// Using attribute selectors that match the description container
		descTag := item.Find(Selectors.DescPrimary).First()
		if descTag.Length() == 0 {
			// Try another selector approach if the first one fails
			descTag = item.Find(Selectors.DescFallback)
			if descTag.Length() == 0 {
				// As a last resort, look for any div after the title that might contain description
				titleParent := titleTag.Parent()
				if titleParent.Is("a") {
					titleParent = titleParent.Parent().Parent()
				}
				descTag = titleParent.NextAll().First().Find("div").First()
			}
		}
		desc := descTag.Text()

		if link != "" && link != "#" {
			resultRank, absoluteRank := rank.Next(isAd)
			result := core.SearchResult{
				Rank:         resultRank,
				AbsoluteRank: absoluteRank,
				URL:          link,
				Title:        title,
				Description:  desc,
				Ad:           isAd,
			}

			results = append(results, result)
		}
	}

	logrus.WithField("document_size", len(doc.Text())).Trace(
		fmt.Sprintf("Google search document size: %d", len(doc.Text())),
	)
	return core.AttachFeaturesToFirstResult(core.DeduplicateResults(results), extractGoogleFeatures(doc))
}

func classifyGoogleRawHTML(body []byte) error {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return err
	}
	return classifyGoogleDocument(doc)
}

func classifyGoogleDocument(doc *goquery.Document) error {
	if isGoogleCaptchaDocument(doc) {
		return core.ErrCaptcha
	}
	if isGoogleSoftBlockDocument(doc) {
		return core.ErrBlocked
	}
	if isGoogleNoResultsDocument(doc) {
		return core.ErrEmptyResult
	}
	return nil
}

func isGoogleCaptchaDocument(doc *goquery.Document) bool {
	err := core.ClassifyChallengeDocument(doc, core.DocSignals{
		CaptchaSelectors: []string{Selectors.CaptchaPage},
		CaptchaMarkers:   Selectors.CaptchaMarkers,
	})
	return errors.Is(err, core.ErrCaptcha)
}

// isGoogleSoftBlockDocument reports the JS-retry interstitial Google serves
// instead of a SERP.
func isGoogleSoftBlockDocument(doc *goquery.Document) bool {
	if doc.Find(Selectors.Results).Length() > 0 || doc.Find(Selectors.ResultsBroad).Length() > 0 {
		return false
	}
	return strings.Contains(strings.ToLower(doc.Find(Selectors.SoftBlock).Text()), "/httpservice/retry/enablejs")
}

func isGoogleNoResultsDocument(doc *goquery.Document) bool {
	stats := strings.ToLower(doc.Find(Selectors.ResultStats).Text())
	if isZeroResultStats(stats) {
		return true
	}
	text := strings.ToLower(doc.Find(Selectors.NoResults).Text())
	if text == "" {
		text = strings.ToLower(doc.Text())
	}
	return strings.Contains(text, "did not match any documents") || isZeroResultStats(text)
}

// isZeroResultStats reports whether s states a literal zero result count,
// e.g. "about 0 results". A plain substring check on "0 results" also matches
// large counts like "10,500,000 results", so require "0" as its own word.
func isZeroResultStats(s string) bool {
	return zeroResultsPattern.MatchString(s)
}

func Search(ctx context.Context, query core.Query) (results []core.SearchResult, err error) {
	ctx = core.PrepareEngineContext(ctx, query, "google", false)

	googleURL, err := BuildURL(query)
	if err != nil {
		return nil, err
	}
	core.WithRequest(ctx).WithField("url", googleURL).Debug(fmt.Sprintf("Google URL built: %s", googleURL))

	res, err := core.RawSearchRequest(ctx, googleURL, query)
	if err != nil {
		return nil, err
	}
	defer core.DrainAndCloseResponse(res)
	core.WithRequest(ctx).WithField("status_code", res.StatusCode).Debug(
		fmt.Sprintf("Google Raw response: code=%d", res.StatusCode),
	)

	body, err := core.ReadRawSearchBody(res)
	if err != nil {
		return nil, err
	}
	htmlStatus := classifyGoogleRawHTML(body)
	if htmlStatus != nil && !errors.Is(htmlStatus, core.ErrEmptyResult) {
		return nil, htmlStatus
	}

	parsedResults, err := ParseHTML(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if len(parsedResults) == 0 {
		if errors.Is(htmlStatus, core.ErrEmptyResult) {
			return []core.SearchResult{}, nil
		}
		return nil, fmt.Errorf("%w: google raw search returned no parseable results", core.ErrParser)
	}

	if query.Start > 0 {
		organicIdx := 0
		for i := range parsedResults {
			if parsedResults[i].Ad {
				continue
			}
			organicIdx++
			parsedResults[i].Rank = query.Start + organicIdx
		}
		for i := range parsedResults {
			if parsedResults[i].AbsoluteRank > 0 {
				parsedResults[i].AbsoluteRank += query.Start
			}
		}
	}
	core.WithRequest(ctx).WithField("results_count", len(parsedResults)).Debug(
		fmt.Sprintf("Google Raw results : %v", parsedResults),
	)

	return core.StripResultFeatures(parsedResults, query.Features), nil
}
