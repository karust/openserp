package yandex

import (
	"crypto/tls"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/corpix/uarand"
	"github.com/karust/openserp/core"
	"github.com/sirupsen/logrus"
)

func yandexRequest(searchURL string, query core.Query) (*http.Response, error) {
	// Create HTTP transport with proxy
	transport := &http.Transport{}
	if query.ProxyURL != "" {
		proxyUrl, err := url.Parse(query.ProxyURL)
		if err != nil {
			return nil, err
		}
		transport.Proxy = http.ProxyURL(proxyUrl)
	}

	// Set insecure TLS
	if query.Insecure {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	baseClient := &http.Client{
		Transport: transport,
		Timeout:   time.Second * 10,
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

	// Get individual results
	sel := doc.Find("li.serp-item")

	for i := range sel.Nodes {
		item := sel.Eq(i)

		// Find URL
		linkTag := item.Find("a")
		link, _ := linkTag.Attr("href")
		link = strings.Trim(link, " ")

		// Find title
		titleTag := item.Find("h2")
		title := titleTag.Text()

		// Find description
		descTag := item.Find(`span.OrganicTextContentSpan`)
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

	logrus.Tracef("Yandex search document size: %d", len(doc.Text()))
	return core.DeduplicateResults(results), err
}

func Search(query core.Query) ([]core.SearchResult, error) {
	googleURL, err := BuildURL(query, 1)
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
	logrus.Debugf("Yandex Raw results : %v", results)

	return results, nil
}
