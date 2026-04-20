package google

import (
	"context"
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/corpix/uarand"
	"github.com/karust/openserp/core"
	"github.com/sirupsen/logrus"
)

func googleRequest(ctx context.Context, searchURL string, query core.Query) (*http.Response, error) {
	baseClient, err := core.NewRawHTTPClient(query)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
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
		if !linkTag.Is("a") {
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
	return core.DeduplicateResults(results), err
}

func Search(ctx context.Context, query core.Query) ([]core.SearchResult, error) {
	ctx = core.EnsureContext(ctx)

	googleURL, err := BuildURL(query)
	if err != nil {
		return nil, err
	}
	logrus.Debugf("Google URL built: %s", googleURL)

	res, err := googleRequest(ctx, googleURL, query)
	if err != nil {
		return nil, err
	}
	defer core.DrainAndCloseResponse(res)
	logrus.Debugf("Google Raw response: code=%d", res.StatusCode)

	results, err := googleResultParser(res)
	if err != nil {
		return nil, err
	}

	if query.Start > 0 {
		for i := range results {
			results[i].Rank = query.Start + i + 1
		}
	}
	logrus.Debugf("Google Raw results : %v", results)

	return results, nil
}
