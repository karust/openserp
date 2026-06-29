package yandex

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/PuerkitoBio/goquery"
	"github.com/karust/openserp/core"
)

func classifyYandexRawHTML(body []byte) error {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return err
	}
	if doc.Find(Selectors.Captcha).Length() > 0 {
		return core.ErrCaptcha
	}
	if doc.Find(Selectors.NoResults).Length() > 0 {
		return core.ErrEmptyResult
	}
	return nil
}

func Search(ctx context.Context, query core.Query) (results []core.SearchResult, err error) {
	ctx = core.PrepareEngineContext(ctx, query, "yandex", false)

	startPage, skipOnFirstPage, err := core.ComputePagination(query.Start, 10)
	if err != nil {
		return nil, err
	}

	searchURL, err := BuildURL(query, startPage)
	if err != nil {
		return nil, err
	}
	core.WithRequest(ctx).WithField("url", searchURL).Debug(fmt.Sprintf("Yandex URL built: %s", searchURL))

	res, err := core.RawSearchRequest(ctx, searchURL, query)
	if err != nil {
		return nil, err
	}
	defer core.DrainAndCloseResponse(res)
	core.WithRequest(ctx).WithField("status_code", res.StatusCode).Debug(
		fmt.Sprintf("Yandex Raw response: code=%d", res.StatusCode),
	)

	body, err := core.ReadRawSearchBody(res)
	if err != nil {
		return nil, err
	}
	htmlStatus := classifyYandexRawHTML(body)
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
		return nil, fmt.Errorf("%w: yandex raw search returned no parseable results", core.ErrParser)
	}

	if skipOnFirstPage > 0 {
		parsedResults = skipOrganicResults(parsedResults, skipOnFirstPage)
	}
	rebaseOrganicRanks(parsedResults, query.Start)
	offsetAbsoluteRanks(parsedResults, startPage*10)
	core.WithRequest(ctx).WithField("results_count", len(parsedResults)).Debug(
		fmt.Sprintf("Yandex Raw results : %v", parsedResults),
	)

	return core.StripResultFeatures(parsedResults, query.Features), nil
}
