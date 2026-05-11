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

func classifyEcosiaRawHTML(body []byte) error {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return err
	}
	if strings.Contains(strings.ToLower(doc.Text()), "captcha") {
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
		href, ok := s.Find(Selectors.ImageLink).Attr("href")
		if !ok || strings.TrimSpace(href) == "" {
			return
		}
		title, _ := s.Find(Selectors.ImageLink).Find("img").Attr("alt")
		title = strings.TrimSpace(title)
		var (
			source = strings.TrimSpace(s.Find(Selectors.ImageSource).Text())
			dims   = strings.TrimSpace(s.Find(Selectors.ImageDims).Text())
			desc   = source
		)
		if dims != "" {
			if source != "" {
				desc = source + " (" + dims + ")"
			} else {
				desc = dims
			}
		}
		results = append(results, core.SearchResult{
			Rank:        rank,
			URL:         href,
			Title:       title,
			Description: desc,
		})
		rank++
	})
	return results, nil
}

func Search(ctx context.Context, query core.Query) (results []core.SearchResult, err error) {
	ctx = core.PrepareEngineContext(ctx, query, "ecosia", false)
	defer func() {
		if recovered := recover(); recovered != nil {
			err = core.RecoverEnginePanicWithContext(ctx, "ecosia", recovered, nil)
			results = nil
		}
	}()

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
	// Skip ads (rank<0) so organic ranks stay sequential from startRank.
	organicIdx := 0
	for i := range parsedResults {
		if parsedResults[i].Ad {
			continue
		}
		parsedResults[i].Rank = startRank + organicIdx
		organicIdx++
	}

	core.WithRequest(ctx).WithField("results_count", len(parsedResults)).Debug(
		fmt.Sprintf("Ecosia Raw results : %v", parsedResults),
	)

	return core.DeduplicateResults(parsedResults), nil
}
