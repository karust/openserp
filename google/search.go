package google

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/karust/openserp/core"
	"github.com/sirupsen/logrus"
)

type Google struct {
	core.Browser
	findNumRgxp  *regexp.Regexp
	checkTimeout time.Duration
}

func New(browser core.Browser) *Google {
	gogl := Google{Browser: browser}
	gogl.checkTimeout = time.Second * 2
	gogl.findNumRgxp = regexp.MustCompile("\\d")
	return &gogl
}
func (gogl *Google) Name() string {
	return "google"
}

func (gogl *Google) FindTotalResults(page *rod.Page) (int, error) {
	resultsStats, err := page.Timeout(gogl.checkTimeout).Search("div#result-stats")
	if err != nil {
		return 0, errors.New("Result stats not found: " + err.Error())
	}
	stats, err := resultsStats.First.Text()
	if err != nil {
		return 0, errors.New("Cannot extract result stats text: " + err.Error())
	}

	// Escape moment with `seconds` and extract digits
	allNums := gogl.findNumRgxp.FindAllString(stats[:len(stats)-15], -1)
	stats = strings.Join(allNums, "")

	total, err := strconv.Atoi(stats)
	if err != nil {
		return 0, err
	}
	return total, nil
}

func (gogl *Google) Search(query core.Query) ([]core.SearchResult, error) {
	logrus.Tracef("Start Google search, query: %+v", query)

	searchResults := []core.SearchResult{}

	// Build URL from query struct to open in browser
	url, err := BuildURL(query)
	if err != nil {
		return nil, err
	}
	page := gogl.Navigate(url)

	totalResults, err := gogl.FindTotalResults(page)
	if err != nil {
		return nil, err
	}
	if totalResults == 0 {
		return searchResults, nil
	}

	results, err := page.Search("div>div.g")
	if err != nil {
		return nil, err
	}

	resultElements, err := results.All()
	if err != nil {
		return nil, err
	}

	for i, r := range resultElements {
		// Get URL
		link, err := r.Element("a")
		if err != nil {
			continue
		}
		linkText, err := link.Property("href")

		// Get title
		titleTag, err := link.Element("h3")
		if err != nil {
			logrus.Error(err)
			continue
		}

		title, err := titleTag.Text()
		if err != nil {
			title = "No title"
			logrus.Error(err)
		}

		// Get description
		descTag, err := r.Element(`div[data-sncf~="1"]`)
		desc := "No description found"
		if err == nil {
			desc = descTag.MustText()
		}

		gR := core.SearchResult{Rank: i, URL: linkText.String(), Title: title, Description: desc}
		searchResults = append(searchResults, gR)
	}

	err = page.Close()
	if err != nil {
		logrus.Error(err)
	}

	return searchResults, nil
}
