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
	ctx = core.PrepareEngineContext(ctx, query, baid.Name(), true)
	scoped := *baid
	scoped.logger = baid.logger.WithRequest(ctx)
	if scoped.Browser.WaitLoadTime == 0 || scoped.Browser.WaitLoadTime > 250*time.Millisecond {
		scoped.Browser.WaitLoadTime = 250 * time.Millisecond
	}
	baid = &scoped

	baid.logger.Debug("Starting search, query: %+v", query)
	defer func() {
		if recovered := recover(); recovered != nil {
			err = core.RecoverEnginePanicWithContext(ctx, baid.Name(), recovered, baid.logger)
			results = nil
		}
	}()

	// Build URL from query struct to open in browser
	url, err := BuildURL(query)
	if err != nil {
		return nil, err
	}

	page, err := baid.Navigate(ctx, url)
	if err != nil {
		return nil, err
	}
	defer core.DeferClosePage(ctx, page, &baid.Browser)()

	searchResults, err := baid.waitForParsedSearchResults(ctx, page, url)
	if err != nil {
		return nil, err
	}

	for i := range searchResults {
		if searchResults[i].AbsoluteRank > 0 {
			searchResults[i].AbsoluteRank += query.Start
		}
		if searchResults[i].Ad {
			continue
		}
		searchResults[i].Rank = query.Start + searchResults[i].Rank
	}
	return searchResults, nil
}

func (baid *Baidu) waitForParsedSearchResults(ctx context.Context, page *rod.Page, url string) ([]core.SearchResult, error) {
	timeout := baid.GetSelectorTimeout()
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	deadline := time.Now().Add(timeout)
	var sawResultContainer bool
	var lastErr error

	for {
		html, err := page.HTML()
		if err == nil {
			results, parseErr := ParseHTML(strings.NewReader(html))
			if parseErr == nil && len(results) > 0 {
				return results, nil
			}
			lastErr = parseErr
		} else {
			lastErr = err
		}

		if core.HasAnySelector(page, baiduResultSelectors()) {
			sawResultContainer = true
		}
		if blockErr := baid.classifyBlockPage(page, url); blockErr != nil {
			return nil, blockErr
		}
		if !time.Now().Before(deadline) {
			break
		}
		if err := core.SleepContext(ctx, 120*time.Millisecond); err != nil {
			return nil, err
		}
	}

	if sawResultContainer {
		if lastErr != nil {
			baid.logger.Debug("Baidu result containers found but HTML parsing failed: %v", lastErr)
		} else {
			baid.logger.Debug("Baidu result containers found but no parseable organic results")
		}
		return nil, core.ErrParser
	}
	return nil, nil
}

// SearchImage executes a Baidu image search and returns normalized image
// results. It may return core.ErrCaptcha or core.ErrSearchTimeout.
func (baid *Baidu) SearchImage(ctx context.Context, query core.Query) ([]core.SearchResult, error) {
	ctx = core.PrepareEngineContext(ctx, query, baid.Name(), true)
	scoped := *baid
	scoped.logger = baid.logger.WithRequest(ctx)
	baid = &scoped

	baid.logger.Debug("Starting image search, query: %+v", query)

	searchResults := []core.SearchResult{}
	searchPage := 0

	// fetchPage loads one image-results page and appends parsed results.
	// Returns (done, error): done=true ends the outer loop without error.
	fetchPage := func() (bool, error) {
		url, err := BuildImageURL(query, searchPage)
		if err != nil {
			return false, err
		}

		// First load often seeds anti-crawler cookies; results tend to become
		// available after one explicit reload.
		page, err := baid.Navigate(ctx, url)
		if err != nil {
			return false, err
		}
		defer core.DeferClosePage(ctx, page, &baid.Browser)()

		jsonWaitTimeout := baid.GetSelectorTimeout()
		if reloadErr := page.Reload(); reloadErr != nil {
			return false, core.ErrSearchTimeout
		}

		preElements, _, err := core.WaitForElements(ctx, page, Selectors.ImageJSONRoot, jsonWaitTimeout)
		if err != nil {
			if blockErr := baid.classifyBlockPage(page, url); blockErr != nil {
				return false, blockErr
			}
			baid.logger.Error("Cannot parse search results: %s", err)
			return false, core.ErrSearchTimeout
		}

		if len(preElements) == 0 {
			if blockErr := baid.classifyBlockPage(page, url); blockErr != nil {
				return false, blockErr
			}
			return true, nil
		}

		jsonText, err := preElements[0].Text()
		if err != nil {
			return false, err
		}

		var data imageDataJson

		// Fix broken JSON
		jsonText = strings.ReplaceAll(jsonText, `\'`, "'")
		matchNewlines, err := regexp.Compile(`[\r\n\t]`)
		if err != nil {
			return false, core.ErrParser
		}
		escapeNewlines := func(s string) string {
			return matchNewlines.ReplaceAllString(s, "\\n")
		}
		re, err := regexp.Compile(`"[^"\\]*(?:\\[\s\S][^"\\]*)*"`)
		if err != nil {
			return false, core.ErrParser
		}
		fixedJson := re.ReplaceAllStringFunc(jsonText, escapeNewlines)

		if err := json.Unmarshal([]byte(fixedJson), &data); err != nil {
			baid.logger.Error("Failed to unmarshal JSON: %v", err)
			return false, core.ErrParser
		}
		if len(data.Data) == 0 {
			return true, nil
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
				Ad: img.AdType != "0",
			}
			searchResults = append(searchResults, res)
			if query.Limit > 0 && len(searchResults) >= query.Limit {
				return true, nil
			}
		}

		return false, nil
	}

	for len(searchResults) < query.Limit {
		done, err := fetchPage()
		if err != nil {
			return nil, err
		}
		searchPage++
		if done {
			break
		}
	}

	deduped := core.DeduplicateResults(searchResults)
	if query.Limit > 0 && len(deduped) > query.Limit {
		deduped = deduped[:query.Limit]
	}
	return deduped, nil
}
