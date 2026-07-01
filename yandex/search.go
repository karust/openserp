package yandex

import (
	"context"
	"encoding/json"
	"errors"
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

// classifyPage runs the same captcha/no-results rules the raw HTML path uses
// (classifyYandexDocument), against a snapshot of the live page, so both
// paths can't drift.
func (yand *Yandex) classifyPage(page *rod.Page) error {
	return core.ClassifyFromPage(page, classifyYandexDocument)
}

func (yand *Yandex) parseResults(results rod.Elements, pageNum int) []core.SearchResult {
	searchResults := []core.SearchResult{}
	rank := core.NewRankState(pageNum)

	for _, r := range results {
		titleTag, err := r.Element(Selectors.Title)
		if err != nil {
			yand.logger.Debug("Missing h2 title")
			continue
		}
		href, ok := yand.elementHref(r, titleTag)
		if !ok {
			continue
		}
		title := core.ElementText(titleTag)
		desc := core.FirstNonEmptyText(r, Selectors.Desc, Selectors.DescFallback)
		isAd := yandexElementHasAdMarker(r) || yandexURLLooksAd(href)

		if res, ok := assembleYandexRow(href, title, desc, isAd, rank); ok {
			searchResults = append(searchResults, res)
		}
	}

	return searchResults
}

// elementHref resolves a result row's link href in the rod path: the canonical
// organic-title link, then the <a> wrapping the title, then any <a> in the
// block. ok=false (with a debug log) when no usable anchor is found.
func (yand *Yandex) elementHref(r, titleTag *rod.Element) (string, bool) {
	link, err := r.Element(Selectors.LinkPrimary)
	if err != nil {
		if closest := core.ClosestMatching(titleTag, Selectors.Link, 4); closest != nil {
			link, err = closest, nil
		}
	}
	if err != nil {
		link, err = r.Element(Selectors.Link)
		if err != nil {
			yand.logger.Debug("Missing link")
			return "", false
		}
	}
	linkText, err := link.Property("href")
	if err != nil {
		yand.logger.Debug("Missing href")
		return "", false
	}
	return linkText.String(), true
}

func yandexElementHasAdMarker(el *rod.Element) bool {
	if el == nil {
		return false
	}
	for _, selector := range Selectors.AdMarkers {
		if matches, err := el.Matches(selector); err == nil && matches {
			return true
		}
		if child, err := el.Element(selector); err == nil && child != nil {
			return true
		}
	}
	return false
}

// Yandex hydrates the results list progressively, so the first parse can be
// short. After the list selector appears, re-poll briefly until we have the
// requested number of organic results or the grace period elapses.
const (
	resultHydrationGrace = 2 * time.Second
	resultPollInterval   = 120 * time.Millisecond
)

func (yand *Yandex) waitForParsedResults(ctx context.Context, page *rod.Page, pageNum, wantOrganic int) ([]core.SearchResult, error) {
	elements, _, err := core.WaitForElements(ctx, page, []string{Selectors.Results}, yand.GetSelectorTimeout())
	if err != nil {
		return nil, err
	}

	results := yand.parseResults(elements, pageNum)
	if wantOrganic <= 0 {
		return results, nil
	}

	deadline := time.Now().Add(resultHydrationGrace)
	for core.CountOrganicResults(results) < wantOrganic && time.Now().Before(deadline) {
		if err := core.SleepContext(ctx, resultPollInterval); err != nil {
			return results, err
		}
		nextElements, eerr := page.Elements(Selectors.Results)
		if eerr != nil || len(nextElements) <= len(elements) {
			continue
		}
		elements = nextElements
		results = yand.parseResults(nextElements, pageNum)
	}

	return results, nil
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
	if query.Start < 0 {
		return nil, fmt.Errorf("incorrect start provided")
	}

	allResults := []core.SearchResult{}
	var pageFeatures []core.SerpFeature
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

		wantOrganic := query.Limit
		if wantOrganic <= 0 || wantOrganic > pageSize {
			wantOrganic = pageSize
		}
		if searchPage == startPage && skipOnFirstPage > 0 {
			wantOrganic += skipOnFirstPage
		}
		r, err := yand.waitForParsedResults(ctx, page, searchPage, wantOrganic)
		if err != nil {
			switch pageErr := yand.classifyPage(page); {
			case errors.Is(pageErr, core.ErrCaptcha):
				yand.logger.Error("Captcha detected: %s", url)
				return false, core.ErrCaptcha
			case errors.Is(pageErr, core.ErrEmptyResult):
				yand.logger.Warn("No results found")
				return true, nil
			default:
				yand.logger.Error("Cannot parse search results: %s", err)
				return false, core.ErrSearchTimeout
			}
		}

		if searchPage == startPage && skipOnFirstPage > 0 {
			r = skipOrganicResults(r, skipOnFirstPage)
		}
		if query.Features && searchPage == startPage {
			pageFeatures = extractYandexFeaturesFromPage(page)
		}
		allResults = append(allResults, r...)
		return false, nil
	}

	for core.ShouldFetchResultPage(core.CountOrganicResults(allResults), query.Limit, searchPage-startPage) {
		done, err := fetchPage()
		if err != nil {
			// Yandex commonly challenges rapid pagination, so a later page can
			// be blocked after earlier pages already succeeded. Don't discard
			// what we have: return the collected results and only surface the
			// error when the first page itself yielded nothing.
			if core.CountOrganicResults(allResults) == 0 {
				return nil, err
			}
			yand.logger.Warn("Pagination stopped after page %d (%s); returning %d collected results", searchPage, err, len(allResults))
			break
		}
		searchPage++
		if done || !core.ShouldFetchResultPage(core.CountOrganicResults(allResults), query.Limit, searchPage-startPage) {
			break
		}
		if err := core.SleepContext(ctx, yand.pageSleep); err != nil {
			return nil, err
		}
	}

	yand.logger.Info("Search completed: %d results", len(allResults))
	limited := core.LimitOrganicResults(core.DeduplicateResults(allResults), query.Limit)
	return core.AttachFeaturesToFirstResult(limited, pageFeatures), nil
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
			switch pageErr := yand.classifyPage(page); {
			case errors.Is(pageErr, core.ErrCaptcha):
				yand.logger.Error("Captcha detected: %s", url)
				return false, core.ErrCaptcha
			case errors.Is(pageErr, core.ErrEmptyResult):
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
			if query.Limit > 0 && len(searchResults) >= query.Limit {
				return true, nil
			}
		}
		return false, nil
	}

	for core.ShouldFetchResultPage(len(searchResults), query.Limit, searchPage) {
		done, err := fetchPage()
		if err != nil {
			return searchResults, err
		}
		if done || !core.ShouldFetchResultPage(len(searchResults), query.Limit, searchPage) {
			break
		}
	}

	sort.Slice(searchResults, func(i, j int) bool {
		return searchResults[i].Rank < searchResults[j].Rank
	})

	return core.DeduplicateResults(searchResults), nil
}
