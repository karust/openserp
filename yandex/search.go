package yandex

import (
	"errors"
	"fmt"
	"time"

	"github.com/go-rod/rod"
	"github.com/karust/googler/core"
	"github.com/sirupsen/logrus"
)

type Yandex struct {
	core.Browser
	checkTimeout time.Duration // Timeout for secondary elements check
	pagesSleep   time.Duration // Sleep between pages
}

func New(browser core.Browser) *Yandex {
	yand := Yandex{Browser: browser}
	yand.checkTimeout = time.Second * 2
	yand.pagesSleep = time.Second * 1
	return &yand
}

func (yand *Yandex) Name() string {
	return "yandex"
}

func (yand *Yandex) isCaptcha(page *rod.Page) bool {
	_, err := page.Timeout(yand.checkTimeout).Search("form#checkbox-captcha-form")
	if err != nil {
		return false
	}
	return true
}

func (yand *Yandex) isNoResults(page *rod.Page) bool {
	noResFound := false

	_, err := page.Timeout(yand.checkTimeout).Search("div.EmptySearchResults-Title")
	fmt.Println(err)
	if err == nil {
		noResFound = true
	}

	_, err = page.Timeout(yand.checkTimeout).Search("div>div.RequestMeta-Message")
	fmt.Println(err)
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

		// Get title
		titleTag, err := link.Element("h2")
		if err != nil {
			logrus.Error("No title tag found")
			continue
		}

		title, err := titleTag.Text()
		if err != nil {
			title = "No title"
		}

		// Get description
		descTag, err := r.Element(`span.OrganicTextContentSpan`)
		desc := "No description found"
		if err == nil {
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
		defer page.Close()

		searchRes, _ := page.Timeout(yand.Timeout).Search("li.serp-item")
		if searchRes != nil {
			elements, _ := searchRes.All()
			r := yand.parseResults(elements, searchPage)
			allResults = append(allResults, r...)
		}

		if searchRes == nil {
			if yand.isNoResults(page) {
				return allResults, nil
			} else if yand.isCaptcha(page) {
				logrus.Error(errors.New("Yandex captcha occured during: " + url))
				return allResults, nil
			}
			break
		}

		searchPage++

		err = page.Close()
		if err != nil {
			logrus.Error(err)
		}

		time.Sleep(yand.pagesSleep)
	}

	return allResults, nil
}
