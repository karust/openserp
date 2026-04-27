package yandex

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/corpix/uarand"
	"github.com/karust/openserp/core"
	"github.com/sirupsen/logrus"
)

func yandexRequest(ctx context.Context, searchURL string, query core.Query) (*http.Response, error) {
	baseClient, err := core.NewRawHTTPClient(query)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", uarand.GetRandom())
	core.SetAcceptLanguageHeader(req, query.LangCode)

	res, err := baseClient.Do(req)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func yandexResultParser(response *http.Response) ([]core.SearchResult, error) {
	doc, err := goquery.NewDocumentFromReader(response.Body)
	if err != nil {
		return nil, err
	}

	results := []core.SearchResult{}
	rank := 1

	// Prefer stable container + attributes and keep legacy fallback.
	sel := doc.Find("#search-result > li[data-fast], li.serp-item")

	for i := range sel.Nodes {
		item := sel.Eq(i)

		// Skip blocks without a result heading.
		titleTag := item.Find("h2").First()
		if titleTag.Length() == 0 {
			continue
		}

		// Find URL
		linkTag := item.Find("a.OrganicTitle-Link").First()
		if linkTag.Length() == 0 {
			linkTag = titleTag.Closest("a")
		}
		if linkTag.Length() == 0 {
			linkTag = item.Find("a").First()
		}
		link, _ := linkTag.Attr("href")
		link = strings.Trim(link, " ")

		// Find title
		title := strings.TrimSpace(titleTag.Text())

		// Find description
		descTag := item.Find(`span.OrganicTextContentSpan`).First()
		if descTag.Length() == 0 {
			descTag = item.Find("div.OrganicText").First()
		}
		desc := strings.TrimSpace(descTag.Text())

		if link != "" && link != "#" && title != "" {
			result := core.SearchResult{
				Rank:        rank,
				URL:         link,
				Title:       title,
				Description: desc,
			}

			results = append(results, result)
			rank++
		}
	}

	logrus.WithField("document_size", len(doc.Text())).Trace(
		fmt.Sprintf("Yandex search document size: %d", len(doc.Text())),
	)
	return core.DeduplicateResults(results), err
}

func Search(ctx context.Context, query core.Query) (results []core.SearchResult, err error) {
	ctx = core.EnsureContext(ctx)
	ctx = core.WithEngine(ctx, "yandex")
	ctx = core.WithQueryHash(ctx, core.QueryHashFromQuery(query))
	defer func() {
		if recovered := recover(); recovered != nil {
			err = core.RecoverEnginePanicWithContext(ctx, "yandex", recovered, nil)
			results = nil
		}
	}()

	startPage, skipOnFirstPage, err := core.ComputePagination(query.Start, 10)
	if err != nil {
		return nil, err
	}

	googleURL, err := BuildURL(query, startPage)
	if err != nil {
		return nil, err
	}
	core.WithRequest(ctx).WithField("url", googleURL).Debug(fmt.Sprintf("Yandex URL built: %s", googleURL))

	res, err := yandexRequest(ctx, googleURL, query)
	if err != nil {
		return nil, err
	}
	defer core.DrainAndCloseResponse(res)
	core.WithRequest(ctx).WithField("status_code", res.StatusCode).Debug(
		fmt.Sprintf("Yandex Raw response: code=%d", res.StatusCode),
	)

	parsedResults, err := yandexResultParser(res)
	if err != nil {
		return nil, err
	}

	if skipOnFirstPage > 0 {
		if skipOnFirstPage >= len(parsedResults) {
			parsedResults = []core.SearchResult{}
		} else {
			parsedResults = parsedResults[skipOnFirstPage:]
		}
	}
	if query.Start > 0 {
		for i := range parsedResults {
			parsedResults[i].Rank = query.Start + i + 1
		}
	}
	core.WithRequest(ctx).WithField("results_count", len(parsedResults)).Debug(
		fmt.Sprintf("Yandex Raw results : %v", parsedResults),
	)

	return parsedResults, nil
}
