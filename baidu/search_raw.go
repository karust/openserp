package baidu

import (
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/corpix/uarand"
	"github.com/karust/openserp/core"
	"github.com/sirupsen/logrus"
)

func baiduRequest(searchURL string, query core.Query) (*http.Response, error) {
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

	logrus.Tracef("Baidu search document size: %d", len(doc.Text()))
	return results, err
}

func Search(query core.Query) ([]core.SearchResult, error) {
	googleURL, err := BuildURL(query)
	if err != nil {
		return nil, err
	}
	logrus.Debugf("Baidu URL built: %s", googleURL)

	res, err := baiduRequest(googleURL, query)
	if err != nil {
		return nil, err
	}
	logrus.Debugf("Baidu Raw response: code=%d", res.StatusCode)

	results, err := baiduResultParser(res)
	if err != nil {
		return nil, err
	}
	if query.Start > 0 {
		for i := range results {
			results[i].Rank = query.Start + i + 1
		}
	}
	logrus.Debugf("Baidu Raw results : %v", results)

	return core.DeduplicateResults(results), nil
}
