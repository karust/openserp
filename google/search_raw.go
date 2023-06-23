package google

import (
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/corpix/uarand"
	"github.com/karust/openserp/core"
	"github.com/sirupsen/logrus"
)

func googleRequest(searchURL string) (*http.Response, error) {
	baseClient := &http.Client{}
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

func googleResultParser(response *http.Response) ([]core.SearchResult, error) {
	doc, err := goquery.NewDocumentFromResponse(response)
	if err != nil {
		return nil, err
	}

	results := []core.SearchResult{}
	rank := 1

	// Get individual results
	sel := doc.Find("div.g")

	for i := range sel.Nodes {
		item := sel.Eq(i)

		// Find URL
		linkTag := item.Find("a")
		link, _ := linkTag.Attr("href")
		link = strings.Trim(link, " ")

		// Find title
		titleTag := item.Find("h3")
		title := titleTag.Text()

		// Find description
		descTag := item.Find(`div[data-sncf~="1"]`)
		desc := descTag.Text()

		if link != "" && link != "#" {
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

	logrus.Tracef("Google search document size: %d", len(doc.Text()))
	return results, err
}

func Search(query core.Query) ([]core.SearchResult, error) {
	googleURL, err := BuildURL(query)
	if err != nil {
		return nil, err
	}
	logrus.Debugf("Google URL built: %s", googleURL)

	res, err := googleRequest(googleURL)
	if err != nil {
		return nil, err
	}
	logrus.Debugf("Google Raw response: code=%d", res.StatusCode)

	results, err := googleResultParser(res)
	if err != nil {
		return nil, err
	}
	logrus.Debugf("Google Raw results : %v", results)

	return results, nil
}
