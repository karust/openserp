package baidu

import (
	"strings"

	"github.com/go-rod/rod"
	"github.com/karust/openserp/core"
	"github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
)

type Baidu struct {
	core.Browser
	core.SearchEngineOptions
}

func New(browser core.Browser, opts core.SearchEngineOptions) *Baidu {
	baid := Baidu{Browser: browser}
	opts.Init()
	baid.SearchEngineOptions = opts
	return &baid
}

func (baid *Baidu) Name() string {
	return "baidu"
}

func (baid *Baidu) GetRateLimiter() *rate.Limiter {
	ratelimit := rate.Every(baid.GetRatelimit())
	return rate.NewLimiter(ratelimit, baid.RateBurst)
}

func (baid *Baidu) isCaptcha(page *rod.Page) bool {
	_, err := page.Timeout(baid.GetSelectorTimeout()).Search("div.passMod_dialog-body")
	if err != nil {
		return false
	}
	return true
}

func (baid *Baidu) isTimeout(page *rod.Page) bool {
	_, err := page.Timeout(baid.GetSelectorTimeout()).Search("button.timeout-button")
	if err != nil {
		return false
	}
	return true
}

func (baid *Baidu) Search(query core.Query) ([]core.SearchResult, error) {
	logrus.Tracef("Start Baidu search, query: %+v", query)

	searchResults := []core.SearchResult{}

	// Build URL from query struct to open in browser
	url, err := BuildURL(query)
	if err != nil {
		return nil, err
	}

	page := baid.Navigate(url)

	results, err := page.Timeout(baid.Timeout).Search("div.c-container.new-pmd")
	if err != nil {
		defer page.Close()
		logrus.Errorf("Cannot parse search results: %s", err)
		return nil, core.ErrSearchTimeout
	}

	// Check why no results, maybe captcha?
	if results == nil {
		defer page.Close()

		if baid.isCaptcha(page) {
			logrus.Errorf("Baidu captcha occurred during: %s", url)
			return nil, core.ErrCaptcha
		} else if baid.isTimeout(page) {
			logrus.Errorf("Baidu timeout occurred during: %s", url)
			return nil, core.ErrCaptcha
		}
		return nil, nil
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
		if err != nil {
			logrus.Error("No `href` tag found")
		}

		// Get title
		title, err := link.Text()
		if err != nil {
			logrus.Error("Cannot extract text from title")
			title = "No title"
		}

		// Get description
		desc, err := r.Text()
		if err != nil {
			desc = ""
		}
		desc = strings.ReplaceAll(desc, title, "")

		gR := core.SearchResult{Rank: i + 1, URL: linkText.String(), Title: title, Description: desc}
		searchResults = append(searchResults, gR)
	}

	if !baid.LeavePageOpen {
		err = page.Close()
		if err != nil {
			logrus.Error(err)
		}
	}

	return searchResults, nil
}
