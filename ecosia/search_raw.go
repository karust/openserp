package ecosia

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/corpix/uarand"
	"github.com/karust/openserp/core"
)

func ecosiaRequest(ctx context.Context, searchURL string, query core.Query) (*http.Response, error) {
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

// resultParser parses an Ecosia SERP HTML response into search results
// using goquery (no browser required).
func resultParser(response *http.Response) ([]core.SearchResult, error) {
	doc, err := goquery.NewDocumentFromReader(response.Body)
	if err != nil {
		return nil, err
	}
	var (
		results []core.SearchResult
		rank    = 1
	)
	doc.Find(sel.Result).Each(func(_ int, s *goquery.Selection) {
		href, ok := s.Find(sel.ResultLink).Attr("href")
		if !ok || strings.TrimSpace(href) == "" {
			return
		}
		var (
			title = strings.TrimSpace(s.Find(sel.Title).Text())
			desc  = strings.TrimSpace(s.Find(sel.Desc).Text())
		)
		results = append(results, core.SearchResult{
			Rank:        rank,
			URL:         href,
			Title:       title,
			Description: desc,
		})
		rank++
	})
	adRank := -1
	doc.Find(sel.Ad).Each(func(_ int, s *goquery.Selection) {
		href, ok := s.Find(sel.ResultLink).Attr("href")
		if !ok || strings.TrimSpace(href) == "" {
			return
		}
		var (
			title = strings.TrimSpace(s.Find(sel.Title).Text())
			desc  = strings.TrimSpace(s.Find(sel.Desc).Text())
		)
		results = append(results, core.SearchResult{
			Rank:        adRank,
			URL:         href,
			Title:       title,
			Description: desc,
			Ad:          true,
		})
		adRank--
	})
	return results, nil
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
	doc.Find(sel.ImageResult).Each(func(_ int, s *goquery.Selection) {
		href, ok := s.Find(sel.ImageLink).Attr("href")
		if !ok || strings.TrimSpace(href) == "" {
			return
		}
		title, _ := s.Find(sel.ImageLink).Find("img").Attr("alt")
		title = strings.TrimSpace(title)
		var (
			source = strings.TrimSpace(s.Find(sel.ImageSource).Text())
			dims   = strings.TrimSpace(s.Find(sel.ImageDims).Text())
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
	ctx = core.EnsureContext(ctx)
	ctx = core.WithEngine(ctx, "ecosia")
	ctx = core.WithQueryHash(ctx, core.QueryHashFromQuery(query))
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

	res, err := ecosiaRequest(ctx, ecosiaURL, query)
	if err != nil {
		return nil, err
	}
	defer core.DrainAndCloseResponse(res)
	core.WithRequest(ctx).WithField("status_code", res.StatusCode).Debug(
		fmt.Sprintf("Ecosia Raw response: code=%d", res.StatusCode),
	)

	parsedResults, err := resultParser(res)
	if err != nil {
		return nil, err
	}

	// Ecosia paginates by page index, not result offset, so re-rank from
	// the page boundary rather than query.Start (off-grid offsets round down).
	for i := range parsedResults {
		parsedResults[i].Rank = startRank + i
	}

	core.WithRequest(ctx).WithField("results_count", len(parsedResults)).Debug(
		fmt.Sprintf("Ecosia Raw results : %v", parsedResults),
	)

	return core.DeduplicateResults(parsedResults), nil
}
