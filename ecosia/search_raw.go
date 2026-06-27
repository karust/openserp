package ecosia

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/karust/openserp/core"
)

// isCaptchaDoc reports whether a parsed Ecosia document is a Cloudflare
// challenge rather than a SERP. Prefers the hidden Turnstile input, then falls
// back to the shared challenge-page body phrases (see cfBodyMarkers).
func isCaptchaDoc(doc *goquery.Document) bool {
	if doc.Find(Selectors.Captcha).Length() > 0 {
		return true
	}
	text := strings.ToLower(doc.Text())
	for _, m := range cfBodyMarkers {
		if strings.Contains(text, m) {
			return true
		}
	}
	return false
}

func classifyEcosiaRawHTML(body []byte) error {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return err
	}
	if isCaptchaDoc(doc) {
		return core.ErrCaptcha
	}
	if doc.Find("[data-test-id='web-no-results']").Length() > 0 ||
		(doc.Find(Selectors.Mainline).Length() > 0 &&
			doc.Find(Selectors.Result).Length() == 0 &&
			doc.Find(Selectors.Ad).Length() == 0) {
		return core.ErrEmptyResult
	}
	return nil
}

// imageResultParser parses an Ecosia image SERP HTML response into search
// results using goquery (no browser required).
func imageResultParser(response *http.Response) ([]core.SearchResult, error) {
	doc, err := goquery.NewDocumentFromReader(response.Body)
	if err != nil {
		return nil, err
	}
	var (
		results []core.SearchResult
		rank    = 1
	)
	doc.Find(Selectors.ImageResult).Each(func(_ int, s *goquery.Selection) {
		link := s.Find(Selectors.ImageLink)
		href, _ := link.Attr("href")
		title, _ := link.Find("img").Attr("alt")
		source := strings.TrimSpace(s.Find(Selectors.ImageSource).Text())
		dims := strings.TrimSpace(s.Find(Selectors.ImageDims).Text())
		if res, ok := assembleEcosiaImageRow(href, strings.TrimSpace(title), source, dims, rank); ok {
			results = append(results, res)
			rank++
		}
	})
	return results, nil
}

func Search(ctx context.Context, query core.Query) (results []core.SearchResult, err error) {
	ctx = core.PrepareEngineContext(ctx, query, "ecosia", false)

	pageNum, startRank, err := startPage(query.Start)
	if err != nil {
		return nil, err
	}

	ecosiaURL, err := BuildURL(query, pageNum)
	if err != nil {
		return nil, err
	}
	core.WithRequest(ctx).WithField("url", ecosiaURL).Debug(fmt.Sprintf("Ecosia URL built: %s", ecosiaURL))

	res, err := core.RawSearchRequest(ctx, ecosiaURL, query)
	if err != nil {
		return nil, err
	}
	defer core.DrainAndCloseResponse(res)
	core.WithRequest(ctx).WithField("status_code", res.StatusCode).Debug(
		fmt.Sprintf("Ecosia Raw response: code=%d", res.StatusCode),
	)

	body, err := core.ReadRawSearchBody(res)
	if err != nil {
		return nil, err
	}
	htmlStatus := classifyEcosiaRawHTML(body)
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
		return nil, fmt.Errorf("%w: ecosia raw search returned no parseable results", core.ErrParser)
	}

	// Ecosia paginates by page index, not result offset, so re-rank from
	// the page boundary rather than query.Start (off-grid offsets round down).
	// Skip ads so organic ranks stay sequential from startRank.
	organicIdx := 0
	for i := range parsedResults {
		if parsedResults[i].Ad {
			continue
		}
		parsedResults[i].Rank = startRank + organicIdx
		organicIdx++
	}
	core.SetSeparatedAdAbsoluteRanks(parsedResults, pageNum*10)

	core.WithRequest(ctx).WithField("results_count", len(parsedResults)).Debug(
		fmt.Sprintf("Ecosia Raw results : %v", parsedResults),
	)

	return core.DeduplicateResults(parsedResults), nil
}
