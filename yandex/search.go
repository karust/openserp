package yandex

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/go-rod/rod"
	"github.com/karust/openserp/core"
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

func (yand *Yandex) isCaptcha(page *rod.Page) bool {
	has, _, _ := page.Has(Selectors.Captcha)
	return has
}

func (yand *Yandex) isNoResults(page *rod.Page) bool {
	has, _, _ := page.Has(Selectors.NoResults)
	return has
}

func (yand *Yandex) parseResults(results rod.Elements, pageNum int) []core.SearchResult {
	searchResults := []core.SearchResult{}

	for i, r := range results {
		// Get URL
		link, err := r.Element(Selectors.Link)
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
		titleTag, err := link.Element(Selectors.Title)
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
		descTag, err := r.Element(Selectors.Desc)
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

func (yand *Yandex) parseImageEntities(items rod.Elements) map[string]ImageEntity {
	entities := make(map[string]ImageEntity)
	for _, item := range items {
		state, err := item.Attribute("data-state")
		if err != nil || state == nil || *state == "" {
			continue
		}

		var imgData ImageData
		if err := json.Unmarshal([]byte(*state), &imgData); err != nil {
			continue
		}

		for id, entity := range imgData.InitalState.SerpList.Items.Entities {
			entities[id] = entity
		}
	}
	return entities
}

// Search executes a Yandex web search and returns normalized search results.
// It may return core.ErrCaptcha or core.ErrSearchTimeout.
func (yand *Yandex) Search(ctx context.Context, query core.Query) (results []core.SearchResult, err error) {
	ctx = core.PrepareEngineContext(ctx, query, yand.Name(), false)
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

	// fetchPage loads one SERP page and appends parsed results.
	// Returns (done, error): done=true ends the outer loop without error.
	fetchPage := func() (bool, error) {
		url, err := BuildURL(query, searchPage)
		if err != nil {
			return false, err
		}

		page, err := yand.Navigate(ctx, url)
		if err != nil {
			return false, err
		}
		defer core.DeferClosePage(ctx, page, &yand.Browser)()

		elements, _, err := core.WaitForElements(ctx, page, []string{Selectors.Results}, yand.GetSelectorTimeout())
		if err != nil {
			if yand.isCaptcha(page) {
				yand.logger.Error("Captcha detected: %s", url)
				return false, core.ErrCaptcha
			}
			if yand.isNoResults(page) {
				yand.logger.Warn("No results found")
				return true, nil
			}
			yand.logger.Error("Cannot parse search results: %s", err)
			return false, core.ErrSearchTimeout
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
		return false, nil
	}

	for len(allResults) < query.Limit {
		done, err := fetchPage()
		if err != nil {
			return nil, err
		}
		searchPage++
		if done {
			break
		}
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
	ctx = core.PrepareEngineContext(ctx, query, yand.Name(), false)
	scoped := *yand
	scoped.logger = yand.logger.WithRequest(ctx)
	yand = &scoped

	yand.logger.Debug("Starting image search, query: %+v", query)

	searchResults := []core.SearchResult{}
	allowPagination := query.Limit > 30

	searchPage := 0
	// fetchPage loads one image page and appends parsed results.
	// Returns (done, error): done=true ends the outer loop.
	fetchPage := func() (bool, error) {
		url, err := BuildImageURL(query, searchPage)
		if err != nil {
			return false, err
		}
		searchPage += 1

		page, err := yand.Navigate(ctx, url)
		if err != nil {
			return false, err
		}
		defer core.DeferClosePage(ctx, page, &yand.Browser)()

		results, _, err := core.WaitForElements(
			ctx,
			page,
			append([]string{Selectors.ImageItems}, Selectors.ImageItemsAlt...),
			yand.GetSelectorTimeout(),
		)
		if err != nil {
			if allStateNodes, allErr := page.Elements(Selectors.ImageStateAll); allErr == nil && len(allStateNodes) > 0 {
				results = allStateNodes
				err = nil
			}
		}
		if err != nil {
			if yand.isCaptcha(page) {
				yand.logger.Error("Captcha detected: %s", url)
				return false, core.ErrCaptcha
			}
			if yand.isNoResults(page) {
				yand.logger.Warn("No results found")
			}
			return false, core.ErrSearchTimeout
		}

		pageEntities := yand.parseImageEntities(results)
		if len(pageEntities) == 0 {
			allStateNodes, allErr := page.Elements(Selectors.ImageStateAll)
			if allErr == nil && len(allStateNodes) > 0 {
				pageEntities = yand.parseImageEntities(allStateNodes)
			}
		}
		if len(pageEntities) == 0 {
			return true, nil
		}

		for id := range pageEntities {
			img := pageEntities[id]
			res := core.SearchResult{
				Rank:        img.Rank + 1,
				URL:         img.OrigURL,
				Title:       img.Title,
				Description: fmt.Sprintf("%dx%d, freshness:%s, thumb_url:%s", img.Width, img.Height, img.Freshness, img.ThumbURL),
			}

			searchResults = append(searchResults, res)
		}
		if len(searchResults) >= query.Limit {
			return true, nil
		}
		if searchPage == 1 && !allowPagination {
			return true, nil
		}
		return false, nil
	}

	for len(searchResults) < query.Limit {
		done, err := fetchPage()
		if err != nil {
			return searchResults, err
		}
		if done {
			break
		}
	}

	sort.Slice(searchResults, func(i, j int) bool {
		return searchResults[i].Rank < searchResults[j].Rank
	})

	return core.DeduplicateResults(searchResults), nil
}
