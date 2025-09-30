package baidu

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/go-rod/rod"
	"github.com/karust/openserp/core"
	"golang.org/x/time/rate"
)

type imageDataJson struct {
	Query        string `json:"queryExt"`
	TotalResults int    `json:"displayNum"`

	Data []struct {
		Title       string `json:"fromPageTitle"`
		PictureDate string `json:"bdImgnewsDate"`
		ThumbURL    string `json:"thumbURL"`
		Type        string
		Height      int
		Width       int
		IsCopyright int
		AdType      string `json:"adType"`
		URL         []struct {
			SourcePage string `json:"FromURL"`
			Original   string `json:"ObjURL"`
		} `json:"replaceUrl"`
	}
}

type Baidu struct {
	core.Browser
	core.SearchEngineOptions
	logger *core.EngineLogger
}

func New(browser core.Browser, opts core.SearchEngineOptions) *Baidu {
	baid := Baidu{Browser: browser}
	opts.Init()
	baid.SearchEngineOptions = opts
	baid.logger = core.NewEngineLogger("Baidu")
	return &baid
}

func (baid *Baidu) Name() string {
	return "baidu"
}

func (baid *Baidu) GetRateLimiter() *rate.Limiter {
	ratelimit := rate.Every(baid.GetRatelimit())
	return rate.NewLimiter(ratelimit, baid.RateBurst)
}

func (baid *Baidu) isCaptcha(page *rod.Page) bool {
	_, err := page.Timeout(baid.GetSelectorTimeout()).Search("div.passMod_dialog-body")
	return err == nil
}

func (baid *Baidu) isTimeout(page *rod.Page) bool {
	_, err := page.Timeout(baid.GetSelectorTimeout()).Search("button.timeout-button")
	return err == nil
}

func (baid *Baidu) Search(query core.Query) ([]core.SearchResult, error) {
	baid.logger.Debug("Starting search, query: %+v", query)

	searchResults := []core.SearchResult{}

	// Build URL from query struct to open in browser
	url, err := BuildURL(query)
	if err != nil {
		return nil, err
	}

	page, err := baid.Navigate(url)
	if err != nil {
		return nil, err
	}

	results, err := page.Timeout(baid.Timeout).Search("div.c-container.new-pmd")
	if err != nil {
		defer page.Close()
		baid.logger.Error("Cannot parse search results: %s", err)
		return nil, core.ErrSearchTimeout
	}

	// Check why no results, maybe captcha?
	if results == nil {
		defer page.Close()

		if baid.isCaptcha(page) {
			baid.logger.Error("Captcha detected: %s", url)
			return nil, core.ErrCaptcha
		} else if baid.isTimeout(page) {
			baid.logger.Error("Timeout occurred: %s", url)
			return nil, core.ErrCaptcha
		}
		return nil, nil
	}

	resultElements, err := results.All()
	if err != nil {
		return nil, err
	}

	for i, r := range resultElements {
		// Get URL
		link, err := r.Element("a")
		if err != nil {
			continue
		}
		linkText, err := link.Property("href")
		if err != nil {
			baid.logger.Error("Missing href tag")
		}

		// Get title
		title, err := link.Text()
		if err != nil {
			baid.logger.Error("Failed to extract title")
			title = "No title"
		}

		// Get description
		desc, err := r.Text()
		if err != nil {
			desc = ""
		}
		desc = strings.ReplaceAll(desc, title, "")

		gR := core.SearchResult{Rank: i + 1, URL: linkText.String(), Title: title, Description: desc}
		searchResults = append(searchResults, gR)
	}

	if !baid.Browser.LeavePageOpen {
		err = page.Close()
		if err != nil {
			baid.logger.Error("Page close error: %v", err)
		}
	}

	return core.DeduplicateResults(searchResults), nil
}

func (baid *Baidu) SearchImage(query core.Query) ([]core.SearchResult, error) {
	baid.logger.Debug("Starting image search, query: %+v", query)

	searchResults := []core.SearchResult{}
	searchPage := 0

	for len(searchResults) < query.Limit {
		url, err := BuildImageURL(query, searchPage)
		if err != nil {
			return nil, err
		}

		// Get anti-crawler cookies first, then reload page
		page, err := baid.Navigate(url)
		if err != nil {
			return nil, err
		}

		if !baid.Browser.LeavePageOpen {
			defer page.Close()
		}
		page.Reload()
		page.WaitLoad()

		result, err := page.Timeout(baid.Timeout).Search("body > pre")
		if err != nil {
			defer page.Close()
			baid.logger.Error("Cannot parse search results: %s", err)
			return nil, core.ErrSearchTimeout
		}

		// Check why no results, maybe captcha?
		if result == nil {
			defer page.Close()

			if baid.isCaptcha(page) {
				baid.logger.Error("Captcha detected: %s", url)
				return nil, core.ErrCaptcha
			} else if baid.isTimeout(page) {
				baid.logger.Error("Timeout occurred: %s", url)
				return nil, core.ErrCaptcha
			}
			return nil, nil
		}

		jsonText, err := result.First.Text()
		if err != nil {
			return nil, err
		}

		var data imageDataJson

		// Fix broken JSON
		jsonText = strings.ReplaceAll(jsonText, `\'`, "'")
		matchNewlines := regexp.MustCompile(`[\r\n\t]`)
		escapeNewlines := func(s string) string {
			return matchNewlines.ReplaceAllString(s, "\\n")
		}
		re := regexp.MustCompile(`"[^"\\]*(?:\\[\s\S][^"\\]*)*"`)
		fixedJson := re.ReplaceAllStringFunc(jsonText, escapeNewlines)

		err = json.Unmarshal([]byte(fixedJson), &data)
		if err != nil {
			baid.logger.Error("Failed to unmarshal JSON: %v", err)
			return nil, err
		}

		for i, img := range data.Data {
			if len(img.URL) == 0 {
				continue
			}
			res := core.SearchResult{
				Rank:        (searchPage * 30) + (i + 1),
				URL:         img.URL[0].Original,
				Title:       img.Title,
				Description: fmt.Sprintf("%v,%v,%vx%x,copyright:%v", img.PictureDate, img.Type, img.Height, img.Width, img.IsCopyright),
				Ad: func() bool {
					if img.AdType != "0" {
						return true
					} else {
						return false
					}
				}(),
			}
			searchResults = append(searchResults, res)
		}

		searchPage += 1

		if !baid.Browser.LeavePageOpen {
			page.Close()
		}
	}

	return core.DeduplicateResults(searchResults), nil
}
