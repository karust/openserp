package baidu

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

func baiduRequest(ctx context.Context, searchURL string, query core.Query) (*http.Response, error) {
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

func baiduResultParser(response *http.Response) ([]core.SearchResult, error) {
	doc, err := goquery.NewDocumentFromReader(response.Body)
	if err != nil {
		return nil, err
	}

	results := []core.SearchResult{}
	rank := 1

	// Prefer organic result blocks from the main result column.
	sel := doc.Find("#content_left .result.c-container")
	if sel.Length() == 0 {
		sel = doc.Find("div.c-container.new-pmd")
	}

	for i := range sel.Nodes {
		item := sel.Eq(i)

		// Find URL
		titleTag := item.Find("h3").First()
		if titleTag.Length() == 0 {
			continue
		}

		linkTag := titleTag.Closest("a")
		if linkTag.Length() == 0 {
			linkTag = item.Find("a").First()
		}
		link, _ := linkTag.Attr("href")
		link = strings.TrimSpace(link)

		// Find title
		title := strings.TrimSpace(titleTag.Text())

		// Find description
		descTag := item.Find(".c-abstract, .content-right_8Zs40, .summary-gap_3Jb4I").First()
		desc := strings.TrimSpace(descTag.Text())
		if desc == "" {
			desc = strings.TrimSpace(item.Text())
		}
		desc = strings.ReplaceAll(desc, title, "")
		desc = strings.TrimSpace(desc)

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
		fmt.Sprintf("Baidu search document size: %d", len(doc.Text())),
	)
	return results, err
}

func Search(ctx context.Context, query core.Query) (results []core.SearchResult, err error) {
	ctx = core.EnsureContext(ctx)
	ctx = core.WithEngine(ctx, "baidu")
	ctx = core.WithQueryHash(ctx, core.QueryHashFromQuery(query))
	defer func() {
		if recovered := recover(); recovered != nil {
			err = core.RecoverEnginePanicWithContext(ctx, "baidu", recovered, nil)
			results = nil
		}
	}()

	googleURL, err := BuildURL(query)
	if err != nil {
		return nil, err
	}
	core.WithRequest(ctx).WithField("url", googleURL).Debug(fmt.Sprintf("Baidu URL built: %s", googleURL))

	res, err := baiduRequest(ctx, googleURL, query)
	if err != nil {
		return nil, err
	}
	defer core.DrainAndCloseResponse(res)
	core.WithRequest(ctx).WithField("status_code", res.StatusCode).Debug(
		fmt.Sprintf("Baidu Raw response: code=%d", res.StatusCode),
	)

	parsedResults, err := baiduResultParser(res)
	if err != nil {
		return nil, err
	}
	if query.Start > 0 {
		for i := range parsedResults {
			parsedResults[i].Rank = query.Start + i + 1
		}
	}
	core.WithRequest(ctx).WithField("results_count", len(parsedResults)).Debug(
		fmt.Sprintf("Baidu Raw results : %v", parsedResults),
	)

	return core.DeduplicateResults(parsedResults), nil
}
