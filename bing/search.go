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
	"github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
)

type Bing struct {
	core.Browser
	core.SearchEngineOptions
}

func New(browser core.Browser, opts core.SearchEngineOptions) *Bing {
	bing := Bing{Browser: browser}
	opts.Init()
	bing.SearchEngineOptions = opts
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
	captcha, err := page.Timeout(bing.GetSelectorTimeout() / 2).Element("div#bxc")
	if err != nil {
		return false
	}
	return captcha != nil
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
	if page != nil {
		page.Close()
	}
}

func (bing *Bing) Search(query core.Query) ([]core.SearchResult, error) {
	logrus.Tracef("Start Bing search, query: %+v", query)

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

	if bing.checkCaptcha(page) {
		logrus.Errorf("Bing captcha occurred during: %s", url)
		return nil, core.ErrCaptcha
	}

	bing.acceptCookies(page)
	page.WaitLoad()

	organicElements, err := page.Timeout(bing.Timeout).Elements("li.b_algo")
	if err != nil {
		logrus.Errorf("Cannot parse organic results: %s", err)
		return nil, core.ErrSearchTimeout
	}

	adElements, err := page.Timeout(bing.Timeout).Elements("li.b_ad")
	if err != nil {
		logrus.Debug("No ad results found or error parsing ads")
	}

	totalResults, err := bing.getTotalResults(page)
	if err != nil {
		logrus.Errorf("Error capturing total results: %v", err)
	}
	logrus.Infof("%d SERP results found (%d ads)", totalResults, len(adElements))

	rank := 0
	for _, result := range organicElements {
		srchRes := core.SearchResult{}

		titleElem, err := result.Element("a")
		if err != nil {
			logrus.Debug("No title found for result")
			continue
		}
		srchRes.Title, _ = titleElem.Text()

		href, err := titleElem.Property("href")
		if err != nil {
			logrus.Debug("No URL found for result")
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
			logrus.Debug("No title found for ad")
			continue
		}
		srchRes.Title, _ = titleElem.Text()

		href, err := titleElem.Property("href")
		if err != nil {
			logrus.Debug("No URL found for ad")
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
	logrus.Tracef("Start Bing image search, query: %+v", query)

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

	// Check for captcha
	if bing.checkCaptcha(page) {
		logrus.Errorf("Bing captcha occurred during image search: %s", url)
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
		logrus.Errorf("Cannot parse image results: %s", err)
		return nil, core.ErrSearchTimeout
	}

	if len(imageContainers) == 0 {
		return nil, errors.New("no image results found")
	}

	logrus.Infof("Found %d image result elements", len(imageContainers))

	rank := 0
	for _, c := range imageContainers {
		srchRes := core.SearchResult{}

		// Get the <a> element inside the div
		linkElem, err := c.Element("a")
		if err != nil {
			logrus.Debug("No <a> element found in image container")
			continue
		}

		// Extract image metadata from m attribute (contains JSON)
		mAttr, err := linkElem.Attribute("m")
		if err != nil || mAttr == nil {
			logrus.Debug("No m attribute found in image element or attribute is nil")
			continue
		}

		// Ensure we have valid JSON data to unmarshal
		jsonData := []byte(*mAttr)
		if len(jsonData) == 0 {
			logrus.Debug("Empty JSON data in m attribute")
			continue
		}

		// Initialize imgData properly
		var imgData BingImageData
		err = json.Unmarshal(jsonData, &imgData)
		if err != nil {
			logrus.Debugf("Failed to parse image JSON data: %s", err)
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
