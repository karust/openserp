package yandex

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/go-rod/rod"
	"github.com/karust/openserp/core"
	"github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
)

type ImageEntity struct {
	ID        string `json:"id"`
	Rank      int    `json:"pos"`
	Width     int    `json:"origWidth"`
	Height    int    `json:"origHeight"`
	Title     string `json:"alt"`
	OrigURL   string `json:"origUrl"`
	ThumbURL  string `json:"image"`
	Freshness string `json:"freshnessCounter"`
	IsGIF     bool   `json:"gifLabel"`
}

type ImageData struct {
	InitalState struct {
		SerpList struct {
			Items struct {
				Entities map[string]ImageEntity `json:"entities"`
			} `json:"items"`
		} `json:"serpList"`
	} `json:"initialState"`
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
	return err == nil
}

// Check if nothig is found
func (yand *Yandex) isNoResults(page *rod.Page) bool {
	_, err := page.Timeout(yand.GetSelectorTimeout()).Search("div.Correction.SearchCorrection")
	return err == nil
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

func (yand *Yandex) SearchImage(query core.Query) ([]core.SearchResult, error) {
	logrus.Tracef("Start Yandex image search, query: %+v", query)

	searchResults := []core.SearchResult{}

	searchPage := 0
	for len(searchResults) < query.Limit {
		url, err := BuildImageURL(query, searchPage)
		if err != nil {
			return nil, err
		}
		searchPage += 1

		page := yand.Navigate(url)

		if !yand.LeavePageOpen {
			defer page.Close()
		}

		//page.Keyboard.Press(input.End)
		//page.WaitLoad()
		//time.Sleep(time.Duration(time.Second * 2))

		results, err := page.Timeout(yand.Timeout).Search("div[role='main'] div[data-state]")
		if err != nil {
			logrus.Errorf("Cannot find search results: %s", err)
		}

		// Check why no results
		if results == nil {
			if yand.isCaptcha(page) {
				logrus.Errorf("Yandex captcha occurred during: %s", url)
				return searchResults, core.ErrCaptcha
			} else if yand.isNoResults(page) {
				logrus.Errorf("No results found")
			}
			return searchResults, core.ErrSearchTimeout
		}

		data, err := results.First.Attribute("data-state")
		if err != nil {
			return nil, err
		}

		var imgData ImageData
		if err := json.Unmarshal([]byte(*data), &imgData); err != nil {
			return nil, err
		}

		for id := range imgData.InitalState.SerpList.Items.Entities {
			img := imgData.InitalState.SerpList.Items.Entities[id]
			res := core.SearchResult{
				Rank:        img.Rank + 1,
				URL:         img.OrigURL,
				Title:       img.Title,
				Description: fmt.Sprintf("%dx%d, freshness:%s, thumb_url:%s", img.Height, img.Width, img.Freshness, img.ThumbURL),
			}

			searchResults = append(searchResults, res)
		}

		if !yand.LeavePageOpen {
			page.Close()
		}
	}

	sort.Slice(searchResults, func(i, j int) bool {
		return searchResults[i].Rank < searchResults[j].Rank
	})

	return searchResults, nil
}
