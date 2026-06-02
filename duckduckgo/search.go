package duckduckgo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/karust/openserp/core"
)

// DuckDuckGo implements core.SearchEngine for DuckDuckGo SERP pages.
type DuckDuckGo struct {
	core.Browser
	core.SearchEngineOptions
	pageSleep time.Duration // Sleep between pages
	logger    *core.EngineLogger
}

// New creates a DuckDuckGo engine instance with browser/runtime options applied.
func New(browser core.Browser, opts core.SearchEngineOptions) *DuckDuckGo {
	ddg := DuckDuckGo{Browser: browser}
	opts.Init()
	ddg.SearchEngineOptions = opts
	ddg.logger = core.NewEngineLogger("DuckDuckGo")

	ddg.pageSleep = time.Second * 1
	return &ddg
}

// Name returns the stable engine identifier.
func (ddg *DuckDuckGo) Name() string {
	return "duckduckgo"
}

func (ddg *DuckDuckGo) isCaptcha(page *rod.Page) bool {
	if core.HasAnySelector(page, Selectors.CaptchaSelectors) {
		return true
	}

	if info, err := page.Info(); err == nil {
		url := strings.ToLower(info.URL)
		if strings.Contains(url, "anomaly") || strings.Contains(url, "captcha") || strings.Contains(url, "challenge") {
			return true
		}
	}

	htmlTimeout := ddg.GetSelectorTimeout() / 2
	if htmlTimeout <= 0 || htmlTimeout > 1500*time.Millisecond {
		htmlTimeout = 1500 * time.Millisecond
	}
	html, err := page.Timeout(htmlTimeout).HTML()
	if err != nil {
		return false
	}
	html = strings.ToLower(html)
	for _, marker := range Selectors.CaptchaMarkers {
		if strings.Contains(html, marker) {
			return true
		}
	}
	return false
}

func (ddg *DuckDuckGo) isNoResults(page *rod.Page) bool {
	return core.HasAnySelector(page, Selectors.NoResults)
}

func (ddg *DuckDuckGo) parseResults(results rod.Elements, pageNum int) []core.SearchResult {
	searchResults := []core.SearchResult{}
	organicRank := pageNum * 10
	adRank := 1
	absoluteRank := pageNum*10 + 1

	for _, r := range results {
		// Get URL - try multiple selectors
		var link *rod.Element
		var err error

		for _, selector := range Selectors.Link {
			link, err = r.Element(selector)
			if err == nil {
				break
			}
		}

		if err != nil {
			ddg.logger.Debug("Missing link")
			// If the result element itself was detached (page navigated mid-iteration),
			// the rest of the slice is also stale and further work is wasted.
			if core.IsRodObjectNotFound(err) {
				break
			}
			continue
		}

		linkText, err := link.Property("href")
		if err != nil {
			ddg.logger.Debug("Missing href")
			continue
		}

		// Validate that we have a proper URL
		hrefStr := linkText.String()
		if hrefStr == "" || hrefStr == "#" || hrefStr == "javascript:void(0)" {
			ddg.logger.Debug("Invalid href: %s", hrefStr)
			continue
		}

		title := core.FirstNonEmptyText(r, Selectors.Title...)
		if title == "" {
			title = "No title"
		}

		desc := core.FirstNonEmptyText(r, Selectors.Desc...)

		// Check if it's an ad
		isAd := duckduckgoElementHasAdMarker(r)
		resultRank := 0
		if isAd {
			resultRank = adRank
			adRank++
		} else {
			organicRank++
			resultRank = organicRank
		}

		result := core.SearchResult{
			Rank:         resultRank,
			AbsoluteRank: absoluteRank,
			URL:          hrefStr,
			Title:        title,
			Description:  desc,
			Ad:           isAd,
		}
		searchResults = append(searchResults, result)
		absoluteRank++
	}

	return searchResults
}

func duckduckgoElementHasAdMarker(el *rod.Element) bool {
	if el == nil {
		return false
	}
	for _, selector := range Selectors.AdBadge {
		matches, err := el.Matches(selector)
		if err == nil && matches {
			return true
		}
		if adIndicator, err := el.Element(selector); err == nil && adIndicator != nil {
			return true
		}
	}
	return false
}

// Search executes a DuckDuckGo web search and returns normalized search
// results. It may return core.ErrCaptcha or core.ErrSearchTimeout.
func (ddg *DuckDuckGo) Search(ctx context.Context, query core.Query) (results []core.SearchResult, err error) {
	ctx = core.PrepareEngineContext(ctx, query, ddg.Name(), false)
	scoped := *ddg
	scoped.logger = ddg.logger.WithRequest(ctx)
	ddg = &scoped

	ddg.logger.Debug("Starting search, query: %+v", query)
	defer func() {
		if recovered := recover(); recovered != nil {
			err = core.RecoverEnginePanicWithContext(ctx, ddg.Name(), recovered, ddg.logger)
			results = nil
		}
	}()

	allResults := []core.SearchResult{}
	var pageFeatures []core.SerpFeature
	searchPage := 0

	// fetchPage loads one SERP page and appends parsed results.
	// Returns (done, error): done=true ends the outer loop without error.
	fetchPage := func() (bool, error) {
		url, err := BuildURL(query, searchPage)
		if err != nil {
			return false, err
		}

		page, err := ddg.Navigate(ctx, url)
		if err != nil {
			return false, err
		}
		defer core.DeferClosePage(ctx, page, &ddg.Browser)()

		elements, selector, err := core.WaitForElements(ctx, page, Selectors.Results, ddg.GetSelectorTimeout())
		if err != nil {
			if ddg.isNoResults(page) {
				ddg.logger.Warn("No results found")
				return true, nil
			}
			if ddg.isCaptcha(page) {
				ddg.logger.Error("Captcha detected: %s", url)
				return false, core.ErrCaptcha
			}
			ddg.logger.Error("Cannot parse search results: %s", err)
			return false, core.ErrSearchTimeout
		}
		ddg.logger.Debug("Found results with selector: %s", selector)

		r := ddg.parseResults(elements, searchPage)
		if len(r) == 0 {
			ddg.logger.Debug("No valid results found on page %d", searchPage)
			return false, core.ErrSearchTimeout
		}

		if query.Features && searchPage == 0 {
			pageFeatures = extractDDGFeaturesFromPage(page)
		}
		allResults = append(allResults, r...)
		return false, nil
	}

	for core.ShouldFetchResultPage(core.CountOrganicResults(allResults), query.Limit, searchPage) {
		done, err := fetchPage()
		if err != nil {
			return nil, err
		}
		searchPage++
		if done || !core.ShouldFetchResultPage(core.CountOrganicResults(allResults), query.Limit, searchPage) {
			break
		}
		if err := core.SleepContext(ctx, ddg.pageSleep); err != nil {
			return nil, err
		}
	}

	// Deduplicate results
	deduped := core.DeduplicateResults(allResults)

	// Trim to exact limit if necessary
	deduped = core.LimitOrganicResults(deduped, query.Limit)

	ddg.logger.Info("Search completed: %d results", len(deduped))
	return core.AttachFeaturesToFirstResult(deduped, pageFeatures), nil
}

// SearchImage executes a DuckDuckGo image search and returns normalized image
// results. It may return core.ErrCaptcha or core.ErrSearchTimeout.
func (ddg *DuckDuckGo) SearchImage(ctx context.Context, query core.Query) ([]core.SearchResult, error) {
	ctx = core.PrepareEngineContext(ctx, query, ddg.Name(), false)
	scoped := *ddg
	scoped.logger = ddg.logger.WithRequest(ctx)
	ddg = &scoped

	ddg.logger.Debug("Starting image search, query: %+v", query)

	searchResults := []core.SearchResult{}

	url, err := BuildImageURL(query)
	if err != nil {
		return nil, err
	}

	page, err := ddg.Navigate(ctx, url)
	if err != nil {
		return nil, err
	}

	defer core.DeferClosePage(ctx, page, &ddg.Browser)()

	elements, selector, err := core.WaitForElements(ctx, page, Selectors.ImageResult, ddg.GetSelectorTimeout())
	if err != nil {
		if ddg.isCaptcha(page) {
			ddg.logger.Error("Captcha detected: %s", url)
			return searchResults, core.ErrCaptcha
		} else if ddg.isNoResults(page) {
			ddg.logger.Warn("No image results found")
		}
		return searchResults, core.ErrSearchTimeout
	}
	ddg.logger.Debug("Found image results with selector: %s", selector)

	ddg.logger.Info("Found %d image elements", len(elements))

	for i, r := range elements {
		// Get image URL - try multiple selectors
		var imgTag *rod.Element
		var imgErr error

		for _, selector := range Selectors.ImageImg {
			imgTag, imgErr = r.Element(selector)
			if imgErr == nil {
				break
			}
		}

		if imgErr != nil {
			ddg.logger.Debug("Missing img tag for element %d", i)
			continue
		}

		imgSrc, err := imgTag.Property("src")
		if err != nil {
			ddg.logger.Debug("Missing src property for image %d", i)
			continue
		}

		title := core.FirstNonEmptyText(r, Selectors.ImageTitle...)
		if title == "" {
			title = "No title"
		}

		// Get source page URL - try multiple selectors
		var linkTag *rod.Element
		var linkErr error

		for _, selector := range Selectors.ImageLink {
			linkTag, linkErr = r.Element(selector)
			if linkErr == nil {
				break
			}
		}

		sourceURL := ""
		if linkTag != nil {
			href, err := linkTag.Property("href")
			if err == nil {
				sourceURL = href.String()
			}
		}

		result := core.SearchResult{
			Rank:        i + 1,
			URL:         imgSrc.String(),
			Title:       title,
			Description: fmt.Sprintf("Source: %s", sourceURL),
		}

		searchResults = append(searchResults, result)
	}

	ddg.logger.Info("Parsed %d image results", len(searchResults))
	return core.DeduplicateResults(searchResults), nil
}
