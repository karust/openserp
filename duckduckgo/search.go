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

// captchaBodyText is matched against the raw page HTML because DDG returns
// a plain-text 202 rate-limit response rather than a structured captcha page.
const captchaBodyText = "bots user"

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
	return strings.Contains(html, captchaBodyText)
}

func (ddg *DuckDuckGo) isNoResults(page *rod.Page) bool {
	has, _, _ := page.Has(Selectors.NoResults)
	return has
}

func (ddg *DuckDuckGo) parseResults(results rod.Elements, pageNum int) []core.SearchResult {
	searchResults := []core.SearchResult{}

	for i, r := range results {
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
		for _, selector := range Selectors.Title {
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
		for _, selector := range Selectors.Desc {
			descTag, err := r.Element(selector)
			if err == nil {
				desc, _ = descTag.Text()
				break
			}
		}

		// Check if it's an ad
		isAd := false
		for _, selector := range Selectors.AdBadge {
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

		for _, selector := range Selectors.Results {
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

	for _, selector := range Selectors.ImageResult {
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

		// Get title - try multiple selectors based on the HTML structure
		var titleTag *rod.Element
		var titleErr error

		for _, selector := range Selectors.ImageTitle {
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
