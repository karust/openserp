package google

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/karust/openserp/core"
	"github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
)

type Google struct {
	core.Browser
	core.SearchEngineOptions
	findNumRgxp *regexp.Regexp
}

func New(browser core.Browser, opts core.SearchEngineOptions) *Google {
	gogl := Google{Browser: browser}
	opts.Init()
	gogl.SearchEngineOptions = opts

	gogl.findNumRgxp = regexp.MustCompile("\\d")
	return &gogl
}

func (gogl *Google) Name() string {
	return "google"
}

func (gogl *Google) GetRateLimiter() *rate.Limiter {
	ratelimit := rate.Every(gogl.GetRatelimit())
	return rate.NewLimiter(ratelimit, gogl.RateBurst)
}

func (gogl *Google) findTotalResults(page *rod.Page) (int, error) {
	resultsStats, err := page.Timeout(gogl.GetSelectorTimeout()).Search("div#result-stats")
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

func (gogl *Google) isCaptcha(page *rod.Page) bool {
	_, err := page.Timeout(gogl.GetSelectorTimeout()).Search("form#captcha-form")
	if err != nil {
		return false
	}
	return true
}

func (gogl *Google) preparePage(page *rod.Page) {
	// Remove "similar queries" lists
	page.Eval(";(() => { document.querySelectorAll(`div[data-initq]`).forEach( el => el.remove());  })();")
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
	gogl.preparePage(page)

	results, err := page.Timeout(gogl.Timeout).Search("div[data-hveid][data-ved][lang], div[data-surl][jsaction]")
	if err != nil {
		defer page.Close()
		logrus.Errorf("Cannot parse search results: %s", err)
		return nil, core.ErrSearchTimeout
	}

	// Check why no results, maybe captcha?
	if results == nil {
		defer page.Close()

		if gogl.isCaptcha(page) {
			logrus.Errorf("Google captcha occurred during: %s", url)
			return nil, core.ErrCaptcha
		}
		return nil, err
	}

	totalResults, err := gogl.findTotalResults(page)
	if err != nil {
		logrus.Errorf("Error capturing total results: %v", err)
	}
	logrus.Infof("%d total results found", totalResults)

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
		titleTag, err := link.Element("h3")
		if err != nil {
			logrus.Error("No `h3` tag found")
			continue
		}

		title, err := titleTag.Text()
		if err != nil {
			logrus.Error("Cannot extract text from title")
			title = "No title"
		}

		// Get description
		// doesn't catch all
		descTag, err := r.Element(`div[data-sncf~="1"]`)
		desc := ""
		if err != nil {
			logrus.Trace(`No description 'div[data-sncf~="1"]' tag found`)
		} else {
			desc = descTag.MustText()
		}

		gR := core.SearchResult{Rank: i + 1, URL: linkText.String(), Title: title, Description: desc}
		searchResults = append(searchResults, gR)
	}

	if !gogl.Browser.LeavePageOpen {
		err = page.Close()
		if err != nil {
			logrus.Error(err)
		}
	}

	return searchResults, nil
}

func (gogl *Google) SearchImage(query core.Query) ([]core.SearchResult, error) {
	logrus.Tracef("Start Google Image search, query: %+v", query)

	searchResultsMap := map[string]core.SearchResult{}
	url, err := BuildImageURL(query)
	if err != nil {
		return nil, err
	}

	page := gogl.Navigate(url)
	if !gogl.LeavePageOpen {
		defer page.Close()
	}

	//// TODO: Case with cookie accept (appears with VPN)
	// if page.MustInfo().URL != url {
	// 	results, _ := page.Search("button[aria-label][jsaction]")
	// 	if results != nil {
	// 		//buttons, _ := results.All()
	// 		//buttons[1].Click(proto.InputMouseButtonLeft, 1)
	// 	}
	// }

	for len(searchResultsMap) < query.Limit {
		page.WaitLoad()
		page.Mouse.Scroll(0, 1000000, 1)
		page.WaitLoad()

		results, err := page.Timeout(gogl.Timeout).Search("div[data-hveid][data-ved][jsaction]")
		if err != nil {
			logrus.Errorf("Cannot parse search results: %s", err)
			return *core.ConvertSearchResultsMap(searchResultsMap), core.ErrSearchTimeout
		}

		// Check why no results
		if results == nil {
			if gogl.isCaptcha(page) {
				logrus.Errorf("Google captcha occurred during: %s", url)
				return *core.ConvertSearchResultsMap(searchResultsMap), core.ErrCaptcha
			}
			return *core.ConvertSearchResultsMap(searchResultsMap), err
		}

		resultElements, err := results.All()
		if err != nil {
			return *core.ConvertSearchResultsMap(searchResultsMap), err
		}

		if len(resultElements) < len(searchResultsMap) {
			continue
		}

		for i, r := range resultElements {
			// TODO: parse AF_initDataCallback to optimize instead of this?
			err := r.Click(proto.InputMouseButtonRight, 1)
			if err != nil {
				logrus.Error("Error clicking")
				continue
			}

			dataID, err := r.Attribute("data-id")
			if err != nil {
				continue
			}

			// If already have image with this ID
			if _, ok := searchResultsMap[*dataID]; ok {
				continue
			}

			// Get URLs
			link, err := r.Element("a[tabindex][role]")
			if err != nil {
				continue
			}

			linkText, err := link.Property("href")
			if err != nil {
				logrus.Error("No `href` tag found")
			}

			imgSrc, err := parseSourceImageURL(linkText.String())
			if err != nil {
				logrus.Errorf("Cannot parse image href: %v", err)
				continue
			}

			// Get title
			titleTag, err := r.Element("h3")
			if err != nil {
				logrus.Error("No `h3` tag found")
				continue
			}

			title, err := titleTag.Text()
			if err != nil {
				logrus.Error("Cannot extract text from title")
				title = "No title"
			}

			gR := core.SearchResult{
				Rank:        i + 1,
				URL:         imgSrc.OriginalURL,
				Title:       title,
				Description: fmt.Sprintf("Height:%v, Width:%v, Source Page: %v", imgSrc.Height, imgSrc.Width, imgSrc.PageURL),
			}
			searchResultsMap[*dataID] = gR

			r.Remove()
		}

		if !gogl.LeavePageOpen {
			page.Close()
		}
	}

	return *core.ConvertSearchResultsMap(searchResultsMap), nil
}
