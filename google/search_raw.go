package google

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

func googleRequest(searchURL string, query core.Query) (*http.Response, error) {
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

func googleResultParser(response *http.Response) ([]core.SearchResult, error) {
	doc, err := goquery.NewDocumentFromReader(response.Body)
	if err != nil {
		return nil, err
	}

	results := []core.SearchResult{}
	rank := 1

	// Use data attributes instead of class names to find results
	// Both old and new DOM have data-hveid and data-ved attributes
	sel := doc.Find("div[data-hveid][data-ved]")

	for i := range sel.Nodes {
		item := sel.Eq(i)

		// Skip items without an h3 element (which indicates a search result)
		if item.Find("h3").Length() == 0 {
			continue
		}

		// Find URL - look for the anchor that contains the h3 title
		linkTag := item.Find("h3").Parent()
		if linkTag.Is("a") == false {
			linkTag = item.Find("h3").Closest("a")
		}

		link, exists := linkTag.Attr("href")
		if !exists || link == "" || link == "#" {
			continue
		}
		link = strings.Trim(link, " ")

		// Find title - this is inside the h3 element
		titleTag := item.Find("h3")
		title := titleTag.Text()

		// Find description - find div with text content after the heading
		// Using attribute selectors that match the description container
		descTag := item.Find("div[data-sncf='1']").Find("div").First()
		if descTag.Length() == 0 {
			// Try another selector approach if the first one fails
			descTag = item.Find("div.VwiC3b")
			if descTag.Length() == 0 {
				// As a last resort, look for any div after the title that might contain description
				titleParent := titleTag.Parent()
				if titleParent.Is("a") {
					titleParent = titleParent.Parent().Parent()
				}
				descTag = titleParent.NextAll().First().Find("div").First()
			}
		}
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

	res, err := googleRequest(googleURL, query)
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
