package baidu

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

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

// Baidu implements core.SearchEngine for Baidu SERP pages.
type Baidu struct {
	core.Browser
	core.SearchEngineOptions
	logger *core.EngineLogger
}

// New creates a Baidu engine instance with browser/runtime options applied.
func New(browser core.Browser, opts core.SearchEngineOptions) *Baidu {
	baid := Baidu{Browser: browser}
	opts.Init()
	baid.SearchEngineOptions = opts
	baid.logger = core.NewEngineLogger("Baidu")
	return &baid
}

// Name returns the stable engine identifier.
func (baid *Baidu) Name() string {
	return "baidu"
}

// GetRateLimiter returns a limiter configured from SearchEngineOptions.
func (baid *Baidu) GetRateLimiter() *rate.Limiter {
	ratelimit := rate.Every(baid.GetRatelimit())
	return rate.NewLimiter(ratelimit, baid.RateBurst)
}

func (baid *Baidu) isCaptcha(page *rod.Page) bool {
	has, _, _ := page.Has(Selectors.Captcha)
	return has
}

func (baid *Baidu) isTimeout(page *rod.Page) bool {
	has, _, _ := page.Has(Selectors.Timeout)
	return has
}

func (baid *Baidu) classifyBlockPage(page *rod.Page, url string) error {
	if baid.isCaptcha(page) {
		baid.logger.Error("Captcha detected: %s", url)
		return core.ErrCaptcha
	}
	if baid.isTimeout(page) {
		baid.logger.Error("Timeout occurred: %s", url)
		return core.ErrSearchTimeout
	}
	return nil
}

// Search executes a Baidu web search and returns normalized search results.
// It may return core.ErrCaptcha or core.ErrSearchTimeout.
func (baid *Baidu) Search(ctx context.Context, query core.Query) (results []core.SearchResult, err error) {
	ctx = core.WithEngine(core.EnsureContext(ctx), baid.Name())
	ctx = core.WithProfileRegion(ctx, query.LangCode)
	ctx = core.WithMinimalBrowserProfile(ctx)
	ctx = core.WithQueryHash(ctx, core.QueryHashFromQuery(query))
	scoped := *baid
	scoped.logger = baid.logger.WithRequest(ctx)
	baid = &scoped

	baid.logger.Debug("Starting search, query: %+v", query)
	defer func() {
		if recovered := recover(); recovered != nil {
			err = core.RecoverEnginePanicWithContext(ctx, baid.Name(), recovered, baid.logger)
			results = nil
		}
	}()

	searchResults := []core.SearchResult{}

	// Build URL from query struct to open in browser
	url, err := BuildURL(query)
	if err != nil {
		return nil, err
	}

	page, err := baid.Navigate(ctx, url)
	if err != nil {
		return nil, err
	}
	closePage := func() {
		if baid.Browser.LeavePageOpen {
			return
		}
		if closeErr := core.ClosePageWithTimeout(ctx, page, time.Second); closeErr != nil {
			baid.logger.Debug("Page close error: %v", closeErr)
		}
	}

	resultElements, _, err := core.WaitForElements(
		ctx,
		page,
		append([]string{Selectors.Results}, Selectors.ResultsAlt...),
		baid.GetSelectorTimeout(),
	)
	if err != nil {
		if blockErr := baid.classifyBlockPage(page, url); blockErr != nil {
			closePage()
			return nil, blockErr
		}
		closePage()
		baid.logger.Error("Cannot parse search results: %s", err)
		return nil, core.ErrSearchTimeout
	}

	if len(resultElements) == 0 {
		if blockErr := baid.classifyBlockPage(page, url); blockErr != nil {
			closePage()
			return nil, blockErr
		}
		closePage()
		return nil, nil
	}

	for i, r := range resultElements {
		// Get URL
		link, err := r.Element(Selectors.Link)
		if err != nil {
			// Element detached mid-iteration (page navigated/refreshed): the rest
			// of the slice is also stale, no point continuing.
			if core.IsRodObjectNotFound(err) {
				break
			}
			continue
		}
		linkText, err := link.Property("href")
		if err != nil {
			baid.logger.Debug("Missing href tag")
			continue
		}

		// Get title
		title, err := link.Text()
		if err != nil {
			baid.logger.Debug("Failed to extract title")
			title = "No title"
		}

		// Get description
		desc, err := r.Text()
		if err != nil {
			desc = ""
		}
		desc = strings.ReplaceAll(desc, title, "")

		gR := core.SearchResult{Rank: query.Start + i + 1, URL: linkText.String(), Title: title, Description: desc}
		searchResults = append(searchResults, gR)
	}

	closePage()

	return core.DeduplicateResults(searchResults), nil
}

// SearchImage executes a Baidu image search and returns normalized image
// results. It may return core.ErrCaptcha or core.ErrSearchTimeout.
func (baid *Baidu) SearchImage(ctx context.Context, query core.Query) ([]core.SearchResult, error) {
	ctx = core.WithEngine(core.EnsureContext(ctx), baid.Name())
	ctx = core.WithProfileRegion(ctx, query.LangCode)
	ctx = core.WithMinimalBrowserProfile(ctx)
	ctx = core.WithQueryHash(ctx, core.QueryHashFromQuery(query))
	scoped := *baid
	scoped.logger = baid.logger.WithRequest(ctx)
	baid = &scoped

	baid.logger.Debug("Starting image search, query: %+v", query)

	searchResults := []core.SearchResult{}
	searchPage := 0

	for len(searchResults) < query.Limit {
		url, err := BuildImageURL(query, searchPage)
		if err != nil {
			return nil, err
		}

		// First load often seeds anti-crawler cookies; results tend to become
		// available after one explicit reload.
		page, err := baid.Navigate(ctx, url)
		if err != nil {
			return nil, err
		}
		closePage := func() {
			if baid.Browser.LeavePageOpen {
				return
			}
			if closeErr := core.ClosePageWithTimeout(ctx, page, time.Second); closeErr != nil {
				baid.logger.Debug("Page close error: %v", closeErr)
			}
		}

		jsonWaitTimeout := baid.GetSelectorTimeout()
		if reloadErr := page.Reload(); reloadErr != nil {
			closePage()
			return nil, core.ErrSearchTimeout
		}

		preElements, _, err := core.WaitForElements(ctx, page, Selectors.ImageJSONRoot, jsonWaitTimeout)
		if err != nil {
			if blockErr := baid.classifyBlockPage(page, url); blockErr != nil {
				closePage()
				return nil, blockErr
			}
			closePage()
			baid.logger.Error("Cannot parse search results: %s", err)
			return nil, core.ErrSearchTimeout
		}

		if len(preElements) == 0 {
			if blockErr := baid.classifyBlockPage(page, url); blockErr != nil {
				closePage()
				return nil, blockErr
			}
			closePage()
			return nil, nil
		}

		jsonText, err := preElements[0].Text()
		if err != nil {
			closePage()
			return nil, err
		}

		var data imageDataJson

		// Fix broken JSON
		jsonText = strings.ReplaceAll(jsonText, `\'`, "'")
		matchNewlines, err := regexp.Compile(`[\r\n\t]`)
		if err != nil {
			closePage()
			return nil, core.ErrParser
		}
		escapeNewlines := func(s string) string {
			return matchNewlines.ReplaceAllString(s, "\\n")
		}
		re, err := regexp.Compile(`"[^"\\]*(?:\\[\s\S][^"\\]*)*"`)
		if err != nil {
			closePage()
			return nil, core.ErrParser
		}
		fixedJson := re.ReplaceAllStringFunc(jsonText, escapeNewlines)

		err = json.Unmarshal([]byte(fixedJson), &data)
		if err != nil {
			closePage()
			baid.logger.Error("Failed to unmarshal JSON: %v", err)
			return nil, core.ErrParser
		}
		if len(data.Data) == 0 {
			closePage()
			break
		}

		for i, img := range data.Data {
			if len(img.URL) == 0 {
				continue
			}
			res := core.SearchResult{
				Rank:  (searchPage * 30) + (i + 1),
				URL:   img.URL[0].Original,
				Title: img.Title,
				Description: fmt.Sprintf(
					"Source Page: %s, thumb_url:%s, %dx%d, date:%v, type:%v, copyright:%v",
					img.URL[0].SourcePage,
					img.ThumbURL,
					img.Width,
					img.Height,
					img.PictureDate,
					img.Type,
					img.IsCopyright,
				),
				Ad: func() bool {
					if img.AdType != "0" {
						return true
					} else {
						return false
					}
				}(),
			}
			searchResults = append(searchResults, res)
			if query.Limit > 0 && len(searchResults) >= query.Limit {
				break
			}
		}

		searchPage += 1

		closePage()
	}

	deduped := core.DeduplicateResults(searchResults)
	if query.Limit > 0 && len(deduped) > query.Limit {
		deduped = deduped[:query.Limit]
	}
	return deduped, nil
}
