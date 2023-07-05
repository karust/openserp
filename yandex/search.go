package yandex

import (
	"encoding/json"
	"time"

	"github.com/go-rod/rod"
	"github.com/karust/openserp/core"
	"github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
)

type YandexImageData struct {
	SerpItem struct {
		Freshness string
		Snippet   struct {
			Title     string
			Text      string
			URL       string
			Domain    string
			ShopScore int
		}
		ImgHref string `json:"img_href"`
		Pos     int
	} `json:"serp-item"`
}

type Yandex struct {
	core.Browser
	core.SearchEngineOptions
	pageSleep time.Duration // Sleep between pages
}

func New(browser core.Browser, opts core.SearchEngineOptions) *Yandex {
	yand := Yandex{Browser: browser}
	opts.Init()
	yand.SearchEngineOptions = opts

	yand.pageSleep = time.Second * 1
	return &yand
}

func (yand *Yandex) Name() string {
	return "yandex"
}

func (yand *Yandex) GetRateLimiter() *rate.Limiter {
	ratelimit := rate.Every(yand.GetRatelimit())
	return rate.NewLimiter(ratelimit, yand.RateBurst)
}

func (yand *Yandex) isCaptcha(page *rod.Page) bool {
	_, err := page.Timeout(yand.GetSelectorTimeout()).Search("form#checkbox-captcha-form")
	if err != nil {
		return false
	}
	return true
}

// Check if nothig is found
func (yand *Yandex) isNoResults(page *rod.Page) bool {
	noResFound := false

	_, err := page.Timeout(yand.GetSelectorTimeout()).Search("div.EmptySearchResults-Title")
	if err == nil {
		noResFound = true
	}

	_, err = page.Timeout(yand.GetSelectorTimeout()).Search("div>div.RequestMeta-Message")
	if err == nil {
		noResFound = true
	}

	return noResFound
}

func (yand *Yandex) parseResults(results rod.Elements, pageNum int) []core.SearchResult {
	searchResults := []core.SearchResult{}

	for i, r := range results {
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
		titleTag, err := link.Element("h2")
		if err != nil {
			logrus.Error("No title `h2` tag found")
			continue
		}

		title, err := titleTag.Text()
		if err != nil {
			logrus.Error("Cannot extract text from title")
			title = "No title"
		}

		// Get description
		descTag, err := r.Element(`span.OrganicTextContentSpan`)
		desc := ""
		if err != nil {
			logrus.Trace("No description `span.OrganicTextContentSpan` tag found")
		} else {
			desc = descTag.MustText()
		}

		r := core.SearchResult{Rank: (pageNum * 10) + (i + 1), URL: linkText.String(), Title: title, Description: desc}
		searchResults = append(searchResults, r)
	}

	return searchResults
}

func (yand *Yandex) Search(query core.Query) ([]core.SearchResult, error) {
	logrus.Tracef("Start Yandex search, query: %+v", query)

	allResults := []core.SearchResult{}
	searchPage := 0

	for len(allResults) < query.Limit {
		url, err := BuildURL(query, searchPage)
		if err != nil {
			return nil, err
		}

		page := yand.Navigate(url)

		// Get all search results in page
		searchRes, err := page.Timeout(yand.Timeout).Search("li.serp-item")
		if err != nil {
			defer page.Close()
			logrus.Errorf("Cannot parse search results: %s", err)
			return nil, core.ErrSearchTimeout
		}

		// Check why no results, maybe captcha?
		if searchRes == nil {
			defer page.Close()

			if yand.isNoResults(page) {
				logrus.Errorf("No results found")
			} else if yand.isCaptcha(page) {
				logrus.Errorf("Yandex captcha occurred during: %s", url)
				return nil, core.ErrCaptcha
			}
			break
		}

		elements, err := searchRes.All()
		if err != nil {
			logrus.Errorf("Cannot get all elements from search results: %s", err)
			break
		}

		r := yand.parseResults(elements, searchPage)
		allResults = append(allResults, r...)

		searchPage++

		if !yand.Browser.LeavePageOpen {
			// Close tab before opening new one during the cycle
			err = page.Close()
			if err != nil {
				logrus.Error(err)
			}
		}

		time.Sleep(yand.pageSleep)
	}

	return allResults, nil
}

func (yand *Yandex) parseImageResults(results rod.Elements, pageNum int) []core.SearchResult {
	searchResults := []core.SearchResult{}

	for i, r := range results {

		dataAttr, err := r.Attribute("data-bem")
		if err != nil {
			continue
		}

		var data YandexImageData

		err = json.Unmarshal([]byte(*dataAttr), &data)
		if err != nil {
			logrus.Errorf("Cannot unmarshal yandex image: %v\nData: %v", err, *dataAttr)
			continue
		}

		linkText := data.SerpItem.ImgHref
		title := data.SerpItem.Snippet.Title
		description := data.SerpItem.Snippet.Text

		r := core.SearchResult{Rank: (pageNum * 10) + (i + 1), URL: linkText, Title: title, Description: description}
		searchResults = append(searchResults, r)
	}

	return searchResults
}

func (yand *Yandex) SearchImage(query core.Query) ([]core.SearchResult, error) {
	logrus.Tracef("Start Yandex image search, query: %+v", query)

	allResults := []core.SearchResult{}
	searchPage := 0

	url, err := BuildImageURL(query, searchPage)
	if err != nil {
		return nil, err
	}

	page := yand.Navigate(url)

	// Get all search results in page
	searchRes, err := page.Timeout(yand.Timeout).Search("div.serp-item")
	if err != nil {
		defer page.Close()
		logrus.Errorf("Cannot parse search results: %s", err)
		return nil, core.ErrSearchTimeout
	}

	// Check why no results, maybe captcha?
	if searchRes == nil {
		defer page.Close()

		if yand.isNoResults(page) {
			logrus.Errorf("No results found")
		} else if yand.isCaptcha(page) {
			logrus.Errorf("Yandex captcha occurred during: %s", url)
			return nil, core.ErrCaptcha
		}
		return nil, err
	}

	elements, err := searchRes.All()
	if err != nil {
		logrus.Errorf("Cannot get all elements from search results: %s", err)
		return nil, err
	}

	r := yand.parseImageResults(elements, searchPage)
	allResults = append(allResults, r...)

	if !yand.Browser.LeavePageOpen {
		// Close tab before opening new one during the cycle
		err = page.Close()
		if err != nil {
			logrus.Error(err)
		}
	}

	return allResults, nil
}
