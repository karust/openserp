package yandex

import (
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/corpix/uarand"
	"github.com/karust/openserp/core"
	"github.com/sirupsen/logrus"
)

func yandexRequest(searchURL string, query core.Query) (*http.Response, error) {
	baseClient, err := core.NewRawHTTPClient(query)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("GET", searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", uarand.GetRandom())

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

	logrus.Tracef("Yandex search document size: %d", len(doc.Text()))
	return core.DeduplicateResults(results), err
}

func Search(query core.Query) ([]core.SearchResult, error) {
	startPage, skipOnFirstPage, err := core.ComputePagination(query.Start, 10)
	if err != nil {
		return nil, err
	}

	googleURL, err := BuildURL(query, startPage)
	if err != nil {
		return nil, err
	}
	logrus.Debugf("Yandex URL built: %s", googleURL)

	res, err := yandexRequest(googleURL, query)
	if err != nil {
		return nil, err
	}
	logrus.Debugf("Yandex Raw response: code=%d", res.StatusCode)

	results, err := yandexResultParser(res)
	if err != nil {
		return nil, err
	}

	if skipOnFirstPage > 0 {
		if skipOnFirstPage >= len(results) {
			results = []core.SearchResult{}
		} else {
			results = results[skipOnFirstPage:]
		}
	}
	if query.Start > 0 {
		for i := range results {
			results[i].Rank = query.Start + i + 1
		}
	}
	logrus.Debugf("Yandex Raw results : %v", results)

	return results, nil
}
