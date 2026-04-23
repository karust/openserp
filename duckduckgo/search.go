package duckduckgo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/karust/openserp/core"
	"golang.org/x/time/rate"
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

// GetRateLimiter returns a limiter configured from SearchEngineOptions.
func (ddg *DuckDuckGo) GetRateLimiter() *rate.Limiter {
	ratelimit := rate.Every(ddg.GetRatelimit())
	return rate.NewLimiter(ratelimit, ddg.RateBurst)
}

func (ddg *DuckDuckGo) isCaptcha(page *rod.Page) bool {
	html, err := page.Timeout(ddg.GetSelectorTimeout()).HTML()
	if err != nil {
		return false
	}
	return strings.Contains(html, "bots user")
}

// Check if no results are found
func (ddg *DuckDuckGo) isNoResults(page *rod.Page) bool {
	_, err := page.Timeout(ddg.GetSelectorTimeout()).Search("div[class*='no-results']")
	return err == nil
}

func (ddg *DuckDuckGo) parseResults(results rod.Elements, pageNum int) []core.SearchResult {
	searchResults := []core.SearchResult{}

	for i, r := range results {
		// Get URL - try multiple selectors
		var link *rod.Element
		var err error

		linkSelectors := []string{
			"a[data-testid='result-title-a']",
			"a.result__a",
			"a.result__url",
			"h2 a",
			"h3 a",
			"a[href]",
			"a",
		}

		for _, selector := range linkSelectors {
			link, err = r.Element(selector)
			if err == nil {
				break
			}
		}

		if err != nil {
			ddg.logger.Debug("Missing link")
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

		// Get title - try multiple selectors
		var titleTag *rod.Element
		titleSelectors := []string{
			"h2",
			".result__title",
			".result__a",
			"span",
			"div",
		}

		for _, selector := range titleSelectors {
			titleTag, err = r.Element(selector)
			if err == nil {
				break
			}
		}

		title := "No title"
		if titleTag != nil {
			title, _ = titleTag.Text()
		}

		// Get description - try multiple selectors
		desc := ""
		descSelectors := []string{
			"div[data-result='snippet']",
			".result__snippet",
			".result__body",
			"span[class*='snippet']",
			"div[class*='snippet']",
			"p",
		}

		for _, selector := range descSelectors {
			descTag, err := r.Element(selector)
			if err == nil {
				desc, _ = descTag.Text()
				break
			}
		}

		// Check if it's an ad
		isAd := false
		adSelectors := []string{
			"[data-testid='ad-badge']",
			".ad-badge",
			".result--ad",
		}

		for _, selector := range adSelectors {
			adIndicator, err := r.Element(selector)
			if err == nil && adIndicator != nil {
				isAd = true
				break
			}
		}

		result := core.SearchResult{
			Rank:        (pageNum * 10) + (i + 1),
			URL:         hrefStr,
			Title:       title,
			Description: desc,
			Ad:          isAd,
		}
		searchResults = append(searchResults, result)
	}

	return searchResults
}

// Search executes a DuckDuckGo web search and returns normalized search
// results. It may return core.ErrCaptcha or core.ErrSearchTimeout.
func (ddg *DuckDuckGo) Search(ctx context.Context, query core.Query) (results []core.SearchResult, err error) {
	ctx = core.WithEngine(core.EnsureContext(ctx), ddg.Name())
	ctx = core.WithProfileRegion(ctx, query.LangCode)
	ctx = core.WithQueryHash(ctx, core.QueryHashFromQuery(query))
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
	searchPage := 0

	for len(allResults) < query.Limit {
		url, err := BuildURL(query, searchPage)
		if err != nil {
			return nil, err
		}

		page, err := ddg.Navigate(ctx, url)
		if err != nil {
			return nil, err
		}
		closePage := func() {
			if ddg.Browser.LeavePageOpen {
				return
			}
			if closeErr := core.ClosePageWithTimeout(ctx, page, time.Second); closeErr != nil {
				ddg.logger.Debug("Page close error: %v", closeErr)
			}
		}

		// Get all search results in page - try multiple selectors
		var searchRes *rod.SearchResult
		var searchErr error

		// Try different selectors for DuckDuckGo results
		selectors := []string{
			"article[data-testid='result']",
			"div[data-testid='result']",
			"div.result",
			"div.web-result",
			".result",
			"[data-testid='result']",
		}

		for _, selector := range selectors {
			searchRes, searchErr = page.Timeout(ddg.GetSelectorTimeout()).Search(selector)
			if searchErr == nil && searchRes != nil {
				ddg.logger.Debug("Found results with selector: %s", selector)
				break
			}
		}

		if searchErr != nil {
			closePage()
			ddg.logger.Error("Cannot parse search results: %s", searchErr)
			return nil, core.ErrParser
		}

		// Check why no results, maybe captcha?
		if searchRes == nil {
			closePage()

			if ddg.isNoResults(page) {
				ddg.logger.Warn("No results found")
			} else if ddg.isCaptcha(page) {
				ddg.logger.Error("Captcha detected: %s", url)
				return nil, core.ErrCaptcha
			}
			break
		}

		elements, err := searchRes.All()
		if err != nil {
			ddg.logger.Error("Cannot get search elements: %s", err)
			closePage()
			break
		}

		r := ddg.parseResults(elements, searchPage)

		if len(r) == 0 {
			ddg.logger.Debug("No valid results found on page %d", searchPage)
			closePage()
			return nil, core.ErrParser
		}

		allResults = append(allResults, r...)
		searchPage++

		// Close tab before opening new one during the cycle
		closePage()

		// Break if we've reached or exceeded the limit
		if len(allResults) >= query.Limit {
			break
		}

		if err := core.SleepContext(ctx, ddg.pageSleep); err != nil {
			return nil, err
		}
	}

	// Deduplicate results
	deduped := core.DeduplicateResults(allResults)

	// Trim to exact limit if necessary
	if len(deduped) > query.Limit {
		deduped = deduped[:query.Limit]
	}

	ddg.logger.Info("Search completed: %d results", len(deduped))
	return deduped, nil
}

// SearchImage executes a DuckDuckGo image search and returns normalized image
// results. It may return core.ErrCaptcha or core.ErrSearchTimeout.
func (ddg *DuckDuckGo) SearchImage(ctx context.Context, query core.Query) ([]core.SearchResult, error) {
	ctx = core.WithEngine(core.EnsureContext(ctx), ddg.Name())
	ctx = core.WithProfileRegion(ctx, query.LangCode)
	ctx = core.WithQueryHash(ctx, core.QueryHashFromQuery(query))
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

	if !ddg.Browser.LeavePageOpen {
		defer func() {
			if closeErr := core.ClosePageWithTimeout(ctx, page, time.Second); closeErr != nil {
				ddg.logger.Debug("Page close error: %v", closeErr)
			}
		}()
	}

	// Wait for page to load
	if err := page.WaitLoad(); err != nil {
		ddg.logger.Error("Wait load failed: %s", err)
		return searchResults, core.ErrSearchTimeout
	}
	if err := core.SleepContext(ctx, 2*time.Second); err != nil {
		return searchResults, err
	}

	// Try multiple selectors for DuckDuckGo image results
	var searchRes *rod.SearchResult
	var searchErr error

	selectors := []string{
		"figure",
		// "figure.nsogf_Hpj9UUxfhcwQd5",
		// "div[data-testid='result']",
		// "div.tile--img",
		// "div.tile.tile--img",
		// "div.js-images-show-more",
		// "div.img-result",
	}

	ddg.logger.Debug("Trying selectors: %v", selectors)

	for _, selector := range selectors {
		searchRes, searchErr = page.Timeout(ddg.GetSelectorTimeout()).Search(selector)
		if searchErr == nil && searchRes != nil {
			ddg.logger.Debug("Found image results with selector: %s", selector)
			break
		} else {
			ddg.logger.Debug("Selector '%s' not found: %v", selector, searchErr)
		}
	}

	if searchErr != nil {
		ddg.logger.Error("Cannot find image results: %s", searchErr)
		return searchResults, core.ErrSearchTimeout
	}

	// Check why no results
	if searchRes == nil {
		if ddg.isCaptcha(page) {
			ddg.logger.Error("Captcha detected: %s", url)
			return searchResults, core.ErrCaptcha
		} else if ddg.isNoResults(page) {
			ddg.logger.Warn("No image results found")
		}
		return searchResults, core.ErrSearchTimeout
	}

	elements, err := searchRes.All()
	if err != nil {
		ddg.logger.Error("Cannot get search elements: %s", err)
		return searchResults, err
	}

	ddg.logger.Info("Found %d image elements", len(elements))

	for i, r := range elements {
		// Get image URL - try multiple selectors
		var imgTag *rod.Element
		var imgErr error

		imgSelectors := []string{
			"img",
			"div.SZ76bwIlqO8BBoqOLqYV img",
			"img[src*='duckduckgo.com']",
		}

		for _, selector := range imgSelectors {
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

		// Get title - try multiple selectors based on the HTML structure
		var titleTag *rod.Element
		var titleErr error

		titleSelectors := []string{
			"figcaption a p span",
			"figcaption span",
			"figcaption p span",
			"span.EKtkFWMYpwzMKOYr0GYm",
			"h3",
			"span",
			"p",
		}

		for _, selector := range titleSelectors {
			titleTag, titleErr = r.Element(selector)
			if titleErr == nil {
				break
			}
		}

		title := "No title"
		if titleTag != nil {
			title, _ = titleTag.Text()
		}

		// Get source page URL - try multiple selectors
		var linkTag *rod.Element
		var linkErr error

		linkSelectors := []string{
			"figcaption a",
			"a",
		}

		for _, selector := range linkSelectors {
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
