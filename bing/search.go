package bing

import (
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

type Bing struct {
	core.Browser
	core.SearchEngineOptions
	logger *core.EngineLogger
}

func New(browser core.Browser, opts core.SearchEngineOptions) *Bing {
	bing := Bing{Browser: browser}
	opts.Init()
	bing.SearchEngineOptions = opts
	bing.logger = core.NewEngineLogger("Bing")
	return &bing
}

func (bing *Bing) Name() string {
	return "bing"
}

func (bing *Bing) GetRateLimiter() *rate.Limiter {
	ratelimit := rate.Every(bing.GetRatelimit())
	return rate.NewLimiter(ratelimit, bing.RateBurst)
}

func (bing *Bing) getTotalResults(page *rod.Page) (int, error) {
	results, err := page.Timeout(bing.GetSelectorTimeout()).Elements("li.b_algo")
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

	timeout := bing.GetSelectorTimeout() / 2
	if timeout <= 0 {
		timeout = time.Second * 2
	}

	selectors := []string{
		"div.captcha",
		"div.captcha_header",
	}

	for _, selector := range selectors {
		has, err, _ := page.Timeout(timeout).Has(selector)
		if err == nil && has {
			bing.logger.Debug("Captcha detected: %s", selector)
			return true
		}
	}

	return false
}

func (bing *Bing) acceptCookies(page *rod.Page) {
	consentBtn, err := page.Timeout(bing.Timeout / 10).Element("button#bnp_btn_accept")
	if err != nil {
		return
	}
	consentBtn.Click(proto.InputMouseButtonLeft, 1)
	time.Sleep(time.Millisecond * 500)
}

func (bing *Bing) close(page *rod.Page) {
	if !bing.Browser.LeavePageOpen {
		if page != nil {
			err := page.Close()
			if err != nil {
				bing.logger.Debug("Page close error: %v", err)
			}
		}
	}
}

func (bing *Bing) Search(query core.Query) ([]core.SearchResult, error) {
	bing.logger.Debug("Starting search, query: %+v", query)

	searchResults := []core.SearchResult{}

	url, err := BuildURL(query)
	if err != nil {
		return nil, err
	}

	page, err := bing.Navigate(url)
	if err != nil {
		return nil, err
	}
	defer bing.close(page)

	page.WaitLoad()

	if bing.checkCaptcha(page) {
		bing.logger.Error("Captcha detected: %s", url)
		return nil, core.ErrCaptcha
	}

	bing.acceptCookies(page)
	page.WaitLoad()

	organicElements, err := page.Timeout(bing.Timeout).Elements("li.b_algo")
	if err != nil {
		bing.logger.Error("Cannot parse organic results: %s", err)
		return nil, core.ErrSearchTimeout
	}

	adElements, err := page.Timeout(bing.Timeout).Elements("li.b_ad")
	if err != nil {
		bing.logger.Debug("No ads found")
	}

	totalResults, err := bing.getTotalResults(page)
	if err != nil {
		bing.logger.Debug("Failed to get total results: %v", err)
	}
	bing.logger.Info("Found %d results (%d ads)", totalResults, len(adElements))

	rank := 0
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

	return core.DeduplicateResults(searchResults), nil
}

// BingImageData represents the JSON structure in the m attribute of image elements
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

// SearchImage performs Bing image search and returns results
func (bing *Bing) SearchImage(query core.Query) ([]core.SearchResult, error) {
	bing.logger.Debug("Starting image search, query: %+v", query)

	searchResults := []core.SearchResult{}

	// Build Bing image search URL
	url, err := BuildImageURL(query)
	if err != nil {
		return nil, err
	}

	page, err := bing.Navigate(url)
	if err != nil {
		return nil, err
	}
	defer bing.close(page)

	page.WaitLoad()

	// Check for captcha
	if bing.checkCaptcha(page) {
		bing.logger.Error("Captcha detected during image search: %s", url)
		return nil, core.ErrCaptcha
	}

	// Accept cookies if present
	bing.acceptCookies(page)

	// Wait for image results to load
	page.WaitLoad()
	time.Sleep(time.Second * 2)

	// Find all image result containers using CSS selector
	imageContainers, err := page.Timeout(bing.Timeout).Elements("div.iuscp, div.isv")
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
		srchRes.Description = imgData.Desc

		// Add dimensions to description if available
		if imgData.W > 0 && imgData.H > 0 {
			srchRes.Description += fmt.Sprintf(" (%dx%d)", imgData.W, imgData.H)
		}

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
