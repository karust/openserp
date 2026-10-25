package bing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/karust/openserp/core"
)

// resultsPerPage is Bing's first= step between SERP pages.
const resultsPerPage = 10

// Bing implements core.SearchEngine for Bing SERP pages.
type Bing struct {
	core.Browser
	core.SearchEngineOptions
	logger *core.EngineLogger
}

// New creates a Bing engine instance with browser/runtime options applied.
func New(browser core.Browser, opts core.SearchEngineOptions) *Bing {
	bing := Bing{Browser: browser}
	opts.Init()
	bing.SearchEngineOptions = opts
	bing.logger = core.NewEngineLogger("Bing")
	return &bing
}

// Name returns the stable engine identifier.
func (bing *Bing) Name() string {
	return "bing"
}

// checkCaptcha classifies the live page with classifyBingDocument
// (parse_html.go), so /bing/search and /bing/parse agree.
func (bing *Bing) checkCaptcha(page *rod.Page) bool {
	if page == nil {
		return false
	}

	if info, err := page.Info(); err == nil {
		parsed, err := url.Parse(info.URL)
		if err == nil {
			path := strings.ToLower(parsed.Path)
			if strings.Contains(path, "turing") || strings.Contains(path, "captcha") {
				return true
			}
		}
	}
	return errors.Is(core.ClassifyFromPage(page, classifyBingDocument), core.ErrCaptcha)
}

// checkNoResults reports whether the page shows Bing's no-results message.
func (bing *Bing) checkNoResults(page *rod.Page) bool {
	return errors.Is(core.ClassifyFromPage(page, classifyBingDocument), core.ErrEmptyResult)
}

// loadInTab navigates the already open tab, keeping its Bing session. Navigate
// would open a fresh browser context instead.
func (bing *Bing) loadInTab(page *rod.Page, url string) error {
	ctx, cancel := context.WithTimeout(page.GetContext(), bing.Timeout)
	defer cancel()
	timed := page.Context(ctx)
	wait := timed.WaitNavigation(proto.PageLifecycleEventNameDOMContentLoaded)
	if err := timed.Navigate(url); err != nil {
		cancel()
		wait()
		return err
	}
	wait()
	return ctx.Err()
}

func (bing *Bing) acceptCookies(ctx context.Context, page *rod.Page) error {
	// Probe first so a banner-less SERP returns immediately instead of blocking
	// .Element for the full Timeout/10.
	if has, _, err := page.Has(Selectors.CookieBtn); err != nil || !has {
		return nil
	}
	consentBtn, err := page.Timeout(bing.Timeout / 10).Element(Selectors.CookieBtn)
	if err != nil {
		return nil
	}
	if err := consentBtn.Click(proto.InputMouseButtonLeft, 1); err != nil {
		bing.logger.Debug("Cookie consent click failed: %v", err)
	}

	return core.SleepContext(ctx, 500*time.Millisecond)
}

func bingElementMatches(el *rod.Element, selector string) bool {
	if el == nil {
		return false
	}
	matches, err := el.Matches(selector)
	return err == nil && matches
}

func (bing *Bing) parseResultElement(el *rod.Element, isAd bool, rank *core.RankState) (core.SearchResult, bool) {
	titleSelector := Selectors.Title
	if isAd {
		titleSelector = Selectors.AdTitle
	}

	titleElem, err := el.Element(titleSelector)
	if err != nil {
		bing.logger.Debug("Missing title")
		return core.SearchResult{}, false
	}
	href, err := titleElem.Property("href")
	if err != nil {
		bing.logger.Debug("Missing URL")
		return core.SearchResult{}, false
	}

	title := core.ElementAttribute(titleElem, "aria-label", "title")
	if title == "" {
		title = core.ElementText(titleElem)
	}
	if title == "" {
		title = core.FirstNonEmptyText(el, Selectors.TitleFallbacks...)
	}
	if title == "" {
		title = core.FirstNonEmptyAttribute(el, "aria-label", Selectors.TitleFallbacks...)
	}

	desc := core.FirstNonEmptyText(el, Selectors.DescPrimary, Selectors.DescFallback, Selectors.DescAny)
	if desc == "" {
		fullText, _ := el.Text()
		desc = core.NormalizeWhitespace(strings.Replace(fullText, title, "", 1))
	}

	return assembleBingRow(href.String(), title, desc, core.FirstNonEmptyText(el, Selectors.Cite), isAd, rank)
}

// Search executes a Bing web search and returns normalized search results.
// It may return core.ErrCaptcha or core.ErrSearchTimeout.
func (bing *Bing) Search(ctx context.Context, query core.Query) (results []core.SearchResult, err error) {
	ctx = core.PrepareEngineContext(ctx, query, bing.Name())
	scoped := *bing
	scoped.logger = bing.logger.WithRequest(ctx)
	bing = &scoped

	bing.logger.Debug("Starting search, query: %+v", query)

	url, err := BuildURL(query)
	if err != nil {
		return nil, err
	}

	// A fresh session ignores first= and gets junk for date filters, so open
	// the plain query first and load the rest in the same tab.
	landing := sessionURL(url)
	page, err := bing.Navigate(ctx, landing)
	if err != nil {
		return nil, err
	}
	defer core.DeferClosePage(ctx, page, &bing.Browser)()

	if bing.checkCaptcha(page) {
		bing.logger.Error("Captcha detected: %s", landing)
		return nil, core.ErrCaptcha
	}

	if err := bing.acceptCookies(ctx, page); err != nil {
		return nil, err
	}

	searchResults := []core.SearchResult{}
	var features []core.SerpFeature
	rank := core.NewRankStateAt(query.Start, query.Start+1)
	pageQuery := query
	for pagesFetched := 0; core.ShouldFetchResultPage(core.CountOrganicResults(searchResults), query.Limit, pagesFetched); pagesFetched++ {
		if pagesFetched > 0 {
			pageQuery.Start += resultsPerPage
			if url, err = BuildURL(pageQuery); err != nil {
				return nil, err
			}
		}
		pageResults, err := bing.parsePage(ctx, page, url, url != landing, rank)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			// Keep earlier pages if a later one gets blocked.
			if len(searchResults) == 0 {
				return nil, err
			}
			bing.logger.Warn("Pagination stopped at %s (%s); returning %d results", url, err, len(searchResults))
			break
		}
		if len(pageResults) == 0 {
			break
		}
		if query.Features && pagesFetched == 0 {
			features = extractBingFeaturesFromPage(ctx, page)
		}
		previousCount := core.CountOrganicResults(searchResults)
		searchResults = core.DeduplicateResults(append(searchResults, pageResults...))
		if core.CountOrganicResults(searchResults) == previousCount {
			break
		}
	}

	if len(searchResults) == 0 {
		return searchResults, nil
	}
	deduped := core.LimitOrganicResults(searchResults, query.Limit)
	return core.AttachFeaturesToFirstResult(deduped, features), nil
}

// parsePage optionally loads url in the session tab, then waits for result rows
// and parses them. An empty slice means Bing has no (more) results.
func (bing *Bing) parsePage(ctx context.Context, page *rod.Page, url string, load bool, rank *core.RankState) ([]core.SearchResult, error) {
	if load {
		if err := bing.loadInTab(page, url); err != nil {
			return nil, err
		}
	}
	resultElements, _, err := core.WaitForElements(ctx, page, []string{Selectors.ResultItems, Selectors.Results}, bing.GetSelectorTimeout())
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// Re-check captcha/no-results on timeout - Bing interstitials and
		// no-results pages can both render after WaitLoad.
		if bing.checkCaptcha(page) {
			bing.logger.Error("Captcha detected: %s", url)
			return nil, core.ErrCaptcha
		}
		if bing.checkNoResults(page) {
			return []core.SearchResult{}, nil
		}
		bing.logger.Error("Cannot parse organic results: %s", err)
		return nil, core.ErrSearchTimeout
	}
	bing.logger.Info("Found %d result containers", len(resultElements))

	results := []core.SearchResult{}
	for _, result := range resultElements {
		isAd := bingElementMatches(result, Selectors.Ads)
		isOrganic := bingElementMatches(result, Selectors.Results)
		if !isAd && !isOrganic {
			continue
		}
		if srchRes, ok := bing.parseResultElement(result, isAd, rank); ok {
			results = append(results, srchRes)
		}
	}
	return results, nil
}

// BingImageData represents metadata encoded in the image result `m` attribute.
type BingImageData struct {
	T      string `json:"t"`      // Title
	Desc   string `json:"desc"`   // Description
	IMGURL string `json:"imgurl"` // Original image URL
	W      int    `json:"w"`      // Width
	H      int    `json:"h"`      // Height
	PURL   string `json:"purl"`   // Page URL
	TURL   string `json:"turl"`   // Thumbnail URL
	MURL   string `json:"murl"`   // Image URL
}

func resolveImageLinkElement(container *rod.Element) (*rod.Element, error) {
	if container == nil {
		return nil, errors.New("nil image container")
	}
	if core.HasAttribute(container, "m") {
		return container, nil
	}
	return container.Element("a")
}

// SearchImage executes a Bing image search and returns normalized image
// results. It may return core.ErrCaptcha or core.ErrSearchTimeout.
func (bing *Bing) SearchImage(ctx context.Context, query core.Query) ([]core.SearchResult, error) {
	ctx = core.PrepareEngineContext(ctx, query, bing.Name())
	scoped := *bing
	scoped.logger = bing.logger.WithRequest(ctx)
	bing = &scoped

	bing.logger.Debug("Starting image search, query: %+v", query)

	searchResults := []core.SearchResult{}

	// Build Bing image search URL
	url, err := BuildImageURL(query)
	if err != nil {
		return nil, err
	}

	page, err := bing.Navigate(ctx, url)
	if err != nil {
		return nil, err
	}
	defer core.DeferClosePage(ctx, page, &bing.Browser)()

	// Check for captcha
	if bing.checkCaptcha(page) {
		bing.logger.Error("Captcha detected during image search: %s", url)
		return nil, core.ErrCaptcha
	}

	// Accept cookies if present
	if err := bing.acceptCookies(ctx, page); err != nil {
		return nil, err
	}

	imageContainers, _, err := core.WaitForElements(
		ctx,
		page,
		[]string{Selectors.ImageResults},
		bing.GetSelectorTimeout(),
	)
	if err != nil {
		if bing.checkCaptcha(page) {
			return nil, core.ErrCaptcha
		}
		bing.logger.Error("Cannot parse image results: %s", err)
		return nil, core.ErrSearchTimeout
	}

	if len(imageContainers) == 0 {
		return nil, errors.New("no image results found")
	}

	bing.logger.Info("Found %d image elements", len(imageContainers))

	rank := 0
	// The selector list matches a tile's div and the a.iusc inside it, and both
	// resolve to the same anchor, so every tile arrives twice.
	seen := make(map[string]struct{}, len(imageContainers))
	for _, c := range imageContainers {
		srchRes := core.SearchResult{}

		linkElem, err := resolveImageLinkElement(c)
		if err != nil {
			bing.logger.Debug("Missing image link element")
			continue
		}

		// Extract image metadata from m attribute (contains JSON)
		mAttr, err := linkElem.Attribute("m")
		if err != nil || mAttr == nil {
			bing.logger.Debug("Missing m attribute")
			continue
		}

		// Ensure we have valid JSON data to unmarshal
		jsonData := []byte(*mAttr)
		if len(jsonData) == 0 {
			bing.logger.Debug("Empty JSON data")
			continue
		}

		// Initialize imgData properly
		var imgData BingImageData
		err = json.Unmarshal(jsonData, &imgData)
		if err != nil {
			bing.logger.Debug("Failed to parse JSON: %s", err)
			continue
		}

		// Extract information from the parsed data
		srchRes.Title = imgData.T
		srchRes.URL = imgData.IMGURL
		srchRes.Description = fmt.Sprintf(
			"%s Source Page: %s, thumb_url:%s, %dx%d",
			imgData.Desc,
			imgData.PURL,
			imgData.TURL,
			imgData.W,
			imgData.H,
		)

		// Get the page URL
		if imgData.MURL != "" {
			srchRes.URL = imgData.MURL
		}
		if _, dup := seen[srchRes.URL]; dup {
			continue
		}
		seen[srchRes.URL] = struct{}{}

		rank++
		srchRes.Rank = rank

		searchResults = append(searchResults, srchRes)
		if query.Limit > 0 && len(searchResults) >= query.Limit {
			break
		}
	}

	return searchResults, nil
}
