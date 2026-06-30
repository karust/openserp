package baidu

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/PuerkitoBio/goquery"
	"github.com/karust/openserp/core"
)

func classifyBaiduRawHTML(body []byte) error {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return err
	}
	return classifyBaiduDocument(doc)
}

func Search(ctx context.Context, query core.Query) (results []core.SearchResult, err error) {
	ctx = core.PrepareEngineContext(ctx, query, "baidu", false)

	searchURL, err := BuildURL(query)
	if err != nil {
		return nil, err
	}
	core.WithRequest(ctx).WithField("url", searchURL).Debug(fmt.Sprintf("Baidu URL built: %s", searchURL))

	res, err := core.RawSearchRequest(ctx, searchURL, query)
	if err != nil {
		return nil, err
	}
	defer core.DrainAndCloseResponse(res)
	core.WithRequest(ctx).WithField("status_code", res.StatusCode).Debug(
		fmt.Sprintf("Baidu Raw response: code=%d", res.StatusCode),
	)

	body, err := core.ReadRawSearchBody(res)
	if err != nil {
		return nil, err
	}
	htmlStatus := classifyBaiduRawHTML(body)
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
		return nil, fmt.Errorf("%w: baidu raw search returned no parseable results", core.ErrParser)
	}
	if query.Start > 0 {
		for i := range parsedResults {
			if parsedResults[i].AbsoluteRank > 0 {
				parsedResults[i].AbsoluteRank += query.Start
			}
			if parsedResults[i].Ad {
				continue
			}
			parsedResults[i].Rank = query.Start + parsedResults[i].Rank
		}
	}
	core.WithRequest(ctx).WithField("results_count", len(parsedResults)).Debug(
		fmt.Sprintf("Baidu Raw results : %v", parsedResults),
	)

	deduped := core.StripResultFeatures(core.DeduplicateResults(parsedResults), query.Features)
	return deduped, nil
}
