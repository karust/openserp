package baidu

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/corpix/uarand"
	"github.com/karust/openserp/core"
	"github.com/sirupsen/logrus"
)

func baiduRequest(searchURL string) (*http.Response, error) {
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

func baiduResultParser(response *http.Response) ([]core.SearchResult, error) {
	doc, err := goquery.NewDocumentFromResponse(response)
	if err != nil {
		return nil, err
	}

	results := []core.SearchResult{}
	rank := 1

	// Get individual results
	sel := doc.Find("div.c-container.new-pmd")

	fmt.Println(sel.Length())

	for i := range sel.Nodes {
		item := sel.Eq(i)

		// Find URL
		linkTag := item.Find("a")
		link, _ := linkTag.Attr("href")
		link = strings.Trim(link, " ")

		// Find title
		title := linkTag.Text()

		// Find description
		desc := item.Text()
		desc = strings.ReplaceAll(desc, title, "")

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

	logrus.Tracef("Baidu search document size: %d", len(doc.Text()))
	return results, err
}

func Search(query core.Query) ([]core.SearchResult, error) {
	googleURL, err := BuildURL(query)
	if err != nil {
		return nil, err
	}
	logrus.Debugf("Baidu URL built: %s", googleURL)

	res, err := baiduRequest(googleURL)
	if err != nil {
		return nil, err
	}
	logrus.Debugf("Baidu Raw response: code=%d", res.StatusCode)

	results, err := baiduResultParser(res)
	if err != nil {
		return nil, err
	}
	logrus.Debugf("Baidu Raw results : %v", results)

	return results, nil
}
