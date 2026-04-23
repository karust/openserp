package yandex

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/go-rod/rod"
	"github.com/karust/openserp/core"
	"golang.org/x/time/rate"
)

// ImageEntity contains one image record from Yandex image search state JSON.
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

// ImageData maps the subset of Yandex JSON state used by parser code.
type ImageData struct {
	InitalState struct {
		SerpList struct {
			Items struct {
				Entities map[string]ImageEntity `json:"entities"`
			} `json:"items"`
		} `json:"serpList"`
	} `json:"initialState"`
}

var sel = struct {
	Captcha    string
	NoResults  string
	Results    string
	ImageItems string
}{
	Captcha:    "div.CheckboxCaptcha",
	NoResults:  "div.EmptySearchResults",
	Results:    "li[data-fast], li.serp-item",
	ImageItems: "div[role='main'] div[data-state]",
}

// Yandex implements core.SearchEngine for Yandex SERP pages.
type Yandex struct {
	core.Browser
	core.SearchEngineOptions
	pageSleep time.Duration // Sleep between pages
	logger    *core.EngineLogger
}

// New creates a Yandex engine instance with browser/runtime options applied.
func New(browser core.Browser, opts core.SearchEngineOptions) *Yandex {
	yand := Yandex{Browser: browser}
	opts.Init()
	yand.SearchEngineOptions = opts
	yand.logger = core.NewEngineLogger("Yandex")

	yand.pageSleep = time.Second * 1
	return &yand
}

// Name returns the stable engine identifier.
func (yand *Yandex) Name() string {
	return "yandex"
}

// GetRateLimiter returns a limiter configured from SearchEngineOptions.
func (yand *Yandex) GetRateLimiter() *rate.Limiter {
	ratelimit := rate.Every(yand.GetRatelimit())
	return rate.NewLimiter(ratelimit, yand.RateBurst)
}

func (yand *Yandex) isCaptcha(page *rod.Page) bool {
	has, _, _ := page.Has(sel.Captcha)
	return has
}

func (yand *Yandex) isNoResults(page *rod.Page) bool {
	has, _, _ := page.Has(sel.NoResults)
	return has
}

func (yand *Yandex) parseResults(results rod.Elements, pageNum int) []core.SearchResult {
	searchResults := []core.SearchResult{}

	for i, r := range results {
		// Get URL
		link, err := r.Element("a")
		if err != nil {
			if core.IsRodObjectNotFound(err) {
				break
			}
			continue
		}
		linkText, err := link.Property("href")
		if err != nil {
			yand.logger.Debug("Missing href")
			continue
		}

		// Get title
		titleTag, err := link.Element("h2")
		if err != nil {
			yand.logger.Debug("Missing h2 title")
			continue
		}

		title, err := titleTag.Text()
		if err != nil {
			yand.logger.Debug("Failed to extract title")
			title = "No title"
		}

		// Get description
		descTag, err := r.Element(`span.OrganicTextContentSpan`)
		desc := ""
		if err != nil {
			yand.logger.Debug("No description")
		} else {
			desc, _ = descTag.Text()
		}

		r := core.SearchResult{Rank: (pageNum * 10) + (i + 1), URL: linkText.String(), Title: title, Description: desc}
		searchResults = append(searchResults, r)
	}

	return searchResults
}

// Search executes a Yandex web search and returns normalized search results.
// It may return core.ErrCaptcha or core.ErrSearchTimeout.
func (yand *Yandex) Search(ctx context.Context, query core.Query) (results []core.SearchResult, err error) {
	ctx = core.WithEngine(core.EnsureContext(ctx), yand.Name())
	ctx = core.WithProfileRegion(ctx, query.LangCode)
	ctx = core.WithQueryHash(ctx, core.QueryHashFromQuery(query))
	scoped := *yand
	scoped.logger = yand.logger.WithRequest(ctx)
	yand = &scoped

	yand.logger.Debug("Starting search, query: %+v", query)
	defer func() {
		if recovered := recover(); recovered != nil {
			err = core.RecoverEnginePanicWithContext(ctx, yand.Name(), recovered, yand.logger)
			results = nil
		}
	}()
	if query.Start < 0 {
		return nil, fmt.Errorf("incorrect start provided")
	}

	allResults := []core.SearchResult{}
	const pageSize = 10
	searchPage, skipOnFirstPage, err := core.ComputePagination(query.Start, pageSize)
	if err != nil {
		return nil, err
	}
	startPage := searchPage

	for len(allResults) < query.Limit {
		url, err := BuildURL(query, searchPage)
		if err != nil {
			return nil, err
		}

		page, err := yand.Navigate(ctx, url)
		if err != nil {
			return nil, err
		}
		closePage := func() {
			if yand.Browser.LeavePageOpen {
				return
			}
			if closeErr := core.ClosePageWithTimeout(ctx, page, time.Second); closeErr != nil {
				yand.logger.Debug("Page close error: %v", closeErr)
			}
		}

		// Get all search results in page
		searchRes, err := page.Timeout(yand.Timeout).Search(sel.Results)
		if err != nil {
			closePage()
			yand.logger.Error("Cannot parse search results: %s", err)
			return nil, core.ErrParser
		}

		// Check why no results, maybe captcha?
		if searchRes == nil {
			closePage()

			if yand.isNoResults(page) {
				yand.logger.Warn("No results found")
			} else if yand.isCaptcha(page) {
				yand.logger.Error("Captcha detected: %s", url)
				return nil, core.ErrCaptcha
			}
			break
		}

		elements, err := searchRes.All()
		if err != nil {
			yand.logger.Error("Cannot get search elements: %s", err)
			closePage()
			break
		}

		r := yand.parseResults(elements, searchPage)
		if searchPage == startPage && skipOnFirstPage > 0 {
			if skipOnFirstPage >= len(r) {
				r = []core.SearchResult{}
			} else {
				r = r[skipOnFirstPage:]
			}
		}
		allResults = append(allResults, r...)

		searchPage++

		// Close tab before opening new one during the cycle
		closePage()

		if err := core.SleepContext(ctx, yand.pageSleep); err != nil {
			return nil, err
		}
	}

	yand.logger.Info("Search completed: %d results", len(allResults))
	return core.DeduplicateResults(allResults), nil
}

// SearchImage executes a Yandex image search and returns normalized image
// results. It may return core.ErrCaptcha or core.ErrSearchTimeout.
func (yand *Yandex) SearchImage(ctx context.Context, query core.Query) ([]core.SearchResult, error) {
	ctx = core.WithEngine(core.EnsureContext(ctx), yand.Name())
	ctx = core.WithProfileRegion(ctx, query.LangCode)
	ctx = core.WithQueryHash(ctx, core.QueryHashFromQuery(query))
	scoped := *yand
	scoped.logger = yand.logger.WithRequest(ctx)
	yand = &scoped

	yand.logger.Debug("Starting image search, query: %+v", query)

	searchResults := []core.SearchResult{}

	searchPage := 0
	for len(searchResults) < query.Limit {
		url, err := BuildImageURL(query, searchPage)
		if err != nil {
			return nil, err
		}
		searchPage += 1

		page, err := yand.Navigate(ctx, url)
		if err != nil {
			return nil, err
		}
		closePage := func() {
			if yand.Browser.LeavePageOpen {
				return
			}
			if closeErr := core.ClosePageWithTimeout(ctx, page, time.Second); closeErr != nil {
				yand.logger.Debug("Page close error: %v", closeErr)
			}
		}

		//page.Keyboard.Press(input.End)
		//page.WaitLoad()
		//time.Sleep(time.Duration(time.Second * 2))

		results, err := page.Timeout(yand.Timeout).Search(sel.ImageItems)
		if err != nil {
			closePage()
			yand.logger.Error("Cannot find search results: %s", err)
			return searchResults, core.ErrParser
		}

		// Check why no results
		if results == nil {
			closePage()
			if yand.isCaptcha(page) {
				yand.logger.Error("Captcha detected: %s", url)
				return searchResults, core.ErrCaptcha
			} else if yand.isNoResults(page) {
				yand.logger.Warn("No results found")
			}
			return searchResults, core.ErrSearchTimeout
		}

		data, err := results.First.Attribute("data-state")
		if err != nil {
			closePage()
			return nil, err
		}

		var imgData ImageData
		if err := json.Unmarshal([]byte(*data), &imgData); err != nil {
			closePage()
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

		closePage()
	}

	sort.Slice(searchResults, func(i, j int) bool {
		return searchResults[i].Rank < searchResults[j].Rank
	})

	return core.DeduplicateResults(searchResults), nil
}
