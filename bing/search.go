package bing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/karust/openserp/core"
	"golang.org/x/time/rate"
)

var sel = struct {
	Captcha      []string
	CookieBtn    string
	Results      string
	Ads          string
	ImageResults string
}{
	Captcha:      []string{"div.captcha", "div.captcha_header"},
	CookieBtn:    "button#bnp_btn_accept",
	Results:      "li.b_algo",
	Ads:          "li.b_ad",
	ImageResults: "div.iuscp, div.isv",
}

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

// GetRateLimiter returns a limiter configured from SearchEngineOptions.
func (bing *Bing) GetRateLimiter() *rate.Limiter {
	ratelimit := rate.Every(bing.GetRatelimit())
	return rate.NewLimiter(ratelimit, bing.RateBurst)
}

func (bing *Bing) getTotalResults(page *rod.Page) (int, error) {
	results, err := page.Timeout(bing.GetSelectorTimeout()).Elements(sel.Results)
	if err != nil {
		return 0, errors.New("Cannot find result elements: " + err.Error())
	}
	return len(results), nil
}

func (bing *Bing) checkCaptcha(page *rod.Page) bool {
	if page == nil {
		return false
	}

	if info, err := page.Info(); err == nil {
		url := strings.ToLower(info.URL)
		if strings.Contains(url, "turing") || strings.Contains(url, "captcha") {
			return true
		}
	}

	for _, selector := range sel.Captcha {
		has, _, _ := page.Has(selector)
		if has {
			bing.logger.Debug("Captcha detected: %s", selector)
			return true
		}
	}

	return false
}

func (bing *Bing) acceptCookies(ctx context.Context, page *rod.Page) error {
	consentBtn, err := page.Timeout(bing.Timeout / 10).Element(sel.CookieBtn)
	if err != nil {
		return nil
	}
	if err := consentBtn.Click(proto.InputMouseButtonLeft, 1); err != nil {
		bing.logger.Debug("Cookie consent click failed: %v", err)
	}

	return core.SleepContext(ctx, 500*time.Millisecond)
}

// Search executes a Bing web search and returns normalized search results.
// It may return core.ErrCaptcha or core.ErrSearchTimeout.
func (bing *Bing) Search(ctx context.Context, query core.Query) (results []core.SearchResult, err error) {
	ctx = core.WithEngine(core.EnsureContext(ctx), bing.Name())
	ctx = core.WithProfileRegion(ctx, query.LangCode)
	ctx = core.WithQueryHash(ctx, core.QueryHashFromQuery(query))
	scoped := *bing
	scoped.logger = bing.logger.WithRequest(ctx)
	bing = &scoped

	bing.logger.Debug("Starting search, query: %+v", query)
	defer func() {
		if recovered := recover(); recovered != nil {
			err = core.RecoverEnginePanicWithContext(ctx, bing.Name(), recovered, bing.logger)
			results = nil
		}
	}()

	searchResults := []core.SearchResult{}

	url, err := BuildURL(query)
	if err != nil {
		return nil, err
	}

	page, err := bing.Navigate(ctx, url)
	if err != nil {
		return nil, err
	}
	defer func() {
		if bing.Browser.LeavePageOpen {
			return
		}
		if closeErr := core.ClosePageWithTimeout(ctx, page, time.Second); closeErr != nil {
			bing.logger.Debug("Page close error: %v", closeErr)
		}
	}()

	if err := page.WaitLoad(); err != nil {
		bing.logger.Error("Initial page load wait failed: %s", err)
		return nil, core.ErrSearchTimeout
	}

	if bing.checkCaptcha(page) {
		bing.logger.Error("Captcha detected: %s", url)
		return nil, core.ErrCaptcha
	}

	if err := bing.acceptCookies(ctx, page); err != nil {
		return nil, err
	}
	if err := page.WaitLoad(); err != nil {
		bing.logger.Error("Post-consent page load wait failed: %s", err)
		return nil, core.ErrSearchTimeout
	}

	organicElements, err := page.Timeout(bing.Timeout).Elements(sel.Results)
	if err != nil {
		bing.logger.Error("Cannot parse organic results: %s", err)
		return nil, core.ErrParser
	}

	adElements, err := page.Timeout(bing.Timeout).Elements(sel.Ads)
	if err != nil {
		bing.logger.Debug("No ads found")
	}

	totalResults, err := bing.getTotalResults(page)
	if err != nil {
		bing.logger.Debug("Failed to get total results: %v", err)
	}
	bing.logger.Info("Found %d results (%d ads)", totalResults, len(adElements))

	rank := query.Start
	for _, result := range organicElements {
		srchRes := core.SearchResult{}

		titleElem, err := result.Element("a")
		if err != nil {
			bing.logger.Debug("Missing title")
			continue
		}
		srchRes.Title, _ = titleElem.Text()

		href, err := titleElem.Property("href")
		if err != nil {
			bing.logger.Debug("Missing URL")
			continue
		}
		srchRes.URL = href.String()

		var desc string
		if descElem, err := result.Element("div.b_caption p"); err == nil {
			desc, _ = descElem.Text()
		} else if descElem, err := result.Element("div.b_caption div"); err == nil {
			desc, _ = descElem.Text()
		} else if descElem, err := result.Element("p"); err == nil {
			desc, _ = descElem.Text()
		} else {
			fullText, _ := result.Text()
			desc = strings.TrimSpace(strings.Replace(fullText, srchRes.Title, "", 1))
		}
		srchRes.Description = desc

		rank++
		srchRes.Rank = rank
		srchRes.Ad = false

		searchResults = append(searchResults, srchRes)
	}

	for _, adResult := range adElements {
		srchRes := core.SearchResult{Ad: true}

		titleElem, err := adResult.Element("h2 a")
		if err != nil {
			bing.logger.Debug("Ad missing title")
			continue
		}
		srchRes.Title, _ = titleElem.Text()

		href, err := titleElem.Property("href")
		if err != nil {
			bing.logger.Debug("Ad missing URL")
			continue
		}
		srchRes.URL = href.String()

		if descElem, err := adResult.Element("p"); err == nil {
			srchRes.Description, _ = descElem.Text()
		}

		// Mark ads with negative rank
		srchRes.Rank = -1
		searchResults = append(searchResults, srchRes)
	}

	// Deduplicate results
	deduped := core.DeduplicateResults(searchResults)

	// Trim to exact limit if necessary (only organic results, not ads)
	if query.Limit > 0 {
		organicResults := []core.SearchResult{}
		adResults := []core.SearchResult{}

		for _, result := range deduped {
			if result.Ad {
				adResults = append(adResults, result)
			} else {
				organicResults = append(organicResults, result)
			}
		}

		// Trim organic results to limit
		if len(organicResults) > query.Limit {
			organicResults = organicResults[:query.Limit]
		}

		// Combine back: organic results + ads
		deduped = append(organicResults, adResults...)
	}

	return deduped, nil
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

// SearchImage executes a Bing image search and returns normalized image
// results. It may return core.ErrCaptcha or core.ErrSearchTimeout.
func (bing *Bing) SearchImage(ctx context.Context, query core.Query) ([]core.SearchResult, error) {
	ctx = core.WithEngine(core.EnsureContext(ctx), bing.Name())
	ctx = core.WithProfileRegion(ctx, query.LangCode)
	ctx = core.WithQueryHash(ctx, core.QueryHashFromQuery(query))
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
	defer func() {
		if bing.Browser.LeavePageOpen {
			return
		}
		if closeErr := core.ClosePageWithTimeout(ctx, page, time.Second); closeErr != nil {
			bing.logger.Debug("Page close error: %v", closeErr)
		}
	}()

	if err := page.WaitLoad(); err != nil {
		bing.logger.Error("Initial image page load wait failed: %s", err)
		return nil, core.ErrSearchTimeout
	}

	// Check for captcha
	if bing.checkCaptcha(page) {
		bing.logger.Error("Captcha detected during image search: %s", url)
		return nil, core.ErrCaptcha
	}

	// Accept cookies if present
	if err := bing.acceptCookies(ctx, page); err != nil {
		return nil, err
	}

	// Wait for image results to load
	if err := page.WaitLoad(); err != nil {
		bing.logger.Error("Image results load wait failed: %s", err)
		return nil, core.ErrSearchTimeout
	}
	if err := core.SleepContext(ctx, 2*time.Second); err != nil {
		return nil, err
	}

	// Find all image result containers using CSS selector
	imageContainers, err := page.Timeout(bing.Timeout).Elements(sel.ImageResults)
	if err != nil {
		bing.logger.Error("Cannot parse image results: %s", err)
		return nil, core.ErrSearchTimeout
	}

	if len(imageContainers) == 0 {
		return nil, errors.New("no image results found")
	}

	bing.logger.Info("Found %d image elements", len(imageContainers))

	rank := 0
	for _, c := range imageContainers {
		srchRes := core.SearchResult{}

		// Get the <a> element inside the div
		linkElem, err := c.Element("a")
		if err != nil {
			bing.logger.Debug("Missing <a> element")
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

		rank++
		srchRes.Rank = rank

		searchResults = append(searchResults, srchRes)
	}

	return searchResults, nil
}
