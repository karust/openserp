// Package ecosia implements an Ecosia SERP scraper (web + image search).
//
// Ecosia (https://www.ecosia.org/) is a Berlin-based search engine that
// proxies results from Bing, Google, and its own EUSP/Staan index
// (https://staan.ai/) which underlying provider serves a query depends on
// market and device.
package ecosia

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/karust/openserp/core"
)

// ecosiaPageSize is the organic-results-per-page count on Ecosia's web SERP.
// Image search uses the same ?p=N param shape but its per-page count varies,
// so SearchImage paginates by Limit alone.
const ecosiaPageSize = 10

// startPage translates q.Start (a 0-based result offset, Google/Bing
// convention) into Ecosia's 0-based page index and the rank of its first
// result. Off-grid offsets round down to a page boundary since Ecosia
// exposes no per-result offset param.
func startPage(start int) (pageNum, startRank int, err error) {
	if start < 0 {
		return 0, 0, errors.New("incorrect start provided")
	}
	pageNum = start / ecosiaPageSize
	startRank = pageNum*ecosiaPageSize + 1
	return pageNum, startRank, nil
}

// Cloudflare interstitial markers. URL/title are CF defaults.
const (
	cfURLPath    = "cdn-cgi"
	cfPageTitle  = "just a moment"
	cfBodyMarker = "not a bot"
)

// Ecosia implements core.SearchEngine for Ecosia SERP pages. Additional
// documentation at https://support.ecosia.org/article/447-search-features.
type Ecosia struct {
	core.Browser
	core.SearchEngineOptions
	pageSleep time.Duration // Sleep between pages
	logger    *core.EngineLogger
}

// New creates an Ecosia engine instance with browser/runtime options applied.
func New(browser core.Browser, opts core.SearchEngineOptions) *Ecosia {
	e := Ecosia{Browser: browser}
	opts.Init()
	e.SearchEngineOptions = opts
	e.logger = core.NewEngineLogger("Ecosia")
	e.pageSleep = time.Second
	return &e
}

// Name returns the stable engine identifier.
func (e *Ecosia) Name() string { return "ecosia" }

// isCaptcha reports whether the current page is a Cloudflare interstitial.
// Checks the URL, title, then body text (cheapest first).
func (e *Ecosia) isCaptcha(page *rod.Page) bool {
	info, err := page.Info()
	if err == nil {
		if strings.Contains(strings.ToLower(info.URL), cfURLPath) {
			return true
		}
		if strings.Contains(strings.ToLower(info.Title), cfPageTitle) {
			return true
		}
	}
	html, err := page.Timeout(e.GetSelectorTimeout()).HTML()
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(html), cfBodyMarker)
}

func (e *Ecosia) parseResult(elem *rod.Element, rank int, ad bool) (core.SearchResult, bool) {
	link, err := elem.Element(Selectors.ResultLink)
	if err != nil {
		// Fall back to the first anchor when the test-id selector is absent.
		link, err = elem.Element("a[href]")
		if err != nil {
			return core.SearchResult{}, false
		}
	}
	href, err := link.Property("href")
	if err != nil {
		return core.SearchResult{}, false
	}
	hrefStr := strings.TrimSpace(href.String())
	if hrefStr == "" || strings.HasPrefix(hrefStr, "javascript:") {
		return core.SearchResult{}, false
	}

	title := ""
	if t, err := elem.Element(Selectors.Title); err == nil {
		title, _ = t.Text()
	} else if t, err := elem.Element("h2, h3"); err == nil {
		title, _ = t.Text()
	}

	desc := ""
	if d, err := elem.Element(Selectors.Desc); err == nil {
		desc, _ = d.Text()
	}

	return core.SearchResult{
		Rank:        rank,
		URL:         hrefStr,
		Title:       strings.TrimSpace(title),
		Description: strings.TrimSpace(desc),
		Ad:          ad,
	}, true
}

// Search executes an Ecosia web search and returns normalized search results.
// It may return core.ErrCaptcha or core.ErrSearchTimeout.
func (e *Ecosia) Search(ctx context.Context, query core.Query) (results []core.SearchResult, err error) {
	ctx = core.PrepareEngineContext(ctx, query, e.Name(), false)
	scoped := *e
	scoped.logger = e.logger.WithRequest(ctx)
	e = &scoped

	e.logger.Debug("Starting search, query: %+v", query)
	defer func() {
		if recovered := recover(); recovered != nil {
			err = core.RecoverEnginePanicWithContext(ctx, e.Name(), recovered, e.logger)
			results = nil
		}
	}()

	// nextRank counts up across pages for organic results; nextAdRank counts
	// up within sponsored results so ad rank stays separate from SEO rank.
	all := []core.SearchResult{}
	pageNum, nextRank, err := startPage(query.Start)
	if err != nil {
		return nil, err
	}
	nextAdRank := 1
	// fetchPage loads one SERP page and appends parsed results.
	// Returns (done, error): done=true ends the outer loop without error.
	fetchPage := func() (bool, error) {
		u, err := BuildURL(query, pageNum)
		if err != nil {
			return false, err
		}

		page, err := e.Navigate(ctx, u)
		if err != nil {
			return false, err
		}
		defer core.DeferClosePage(ctx, page, &e.Browser)()

		if err := page.WaitLoad(); err != nil {
			e.logger.Error("Page load wait failed: %s", err)
			return false, core.ErrSearchTimeout
		}

		if _, err := page.Timeout(e.GetSelectorTimeout()).Element(Selectors.Mainline); err != nil {
			if e.isCaptcha(page) {
				e.logger.Error("Captcha detected: %s", u)
				return false, core.ErrCaptcha
			}
			e.logger.Warn("Mainline not found on page %d", pageNum)
			return true, nil
		}

		organic, _ := page.Elements(Selectors.Result)
		ads, _ := page.Elements(Selectors.Ad)
		if len(organic) == 0 && len(ads) == 0 {
			// Empty mainline = zero-result query or end of pagination, not
			// a parser failure; don't trip the retry path.
			e.logger.Debug("No results on page %d", pageNum)
			return true, nil
		}

		for _, r := range organic {
			if res, ok := e.parseResult(r, nextRank, false); ok {
				all = append(all, res)
				nextRank++
			}
		}
		for _, r := range ads {
			if res, ok := e.parseResult(r, nextAdRank, true); ok {
				all = append(all, res)
				nextAdRank++
			}
		}
		return false, nil
	}

	for query.Limit <= 0 || core.CountOrganicResults(all) < query.Limit {
		done, err := fetchPage()
		if err != nil {
			return nil, err
		}
		pageNum++
		if done {
			break
		}
		if query.Limit > 0 && core.CountOrganicResults(all) >= query.Limit {
			break
		}
		if err := core.SleepContext(ctx, e.pageSleep); err != nil {
			return nil, err
		}
	}

	setSeparatedAdAbsoluteRanks(all, query.Start)
	deduped := core.DeduplicateResults(all)
	if query.Limit > 0 {
		deduped = core.LimitOrganicResults(deduped, query.Limit)
	}
	e.logger.Info("Search completed: %d results", len(deduped))
	return deduped, nil
}

// parseImageResult extracts a single image card into a SearchResult,
// returning (_, false) if the card lacks a usable image URL.
func (e *Ecosia) parseImageResult(el *rod.Element, rank int) (core.SearchResult, bool) {
	link, err := el.Element(Selectors.ImageLink)
	if err != nil {
		return core.SearchResult{}, false
	}
	href, err := link.Property("href")
	if err != nil {
		return core.SearchResult{}, false
	}
	imgURL := strings.TrimSpace(href.String())
	if imgURL == "" {
		return core.SearchResult{}, false
	}

	title := ""
	if img, err := link.Element("img"); err == nil {
		if alt, err := img.Attribute("alt"); err == nil && alt != nil {
			title = strings.TrimSpace(*alt)
		}
	}

	source := ""
	if s, err := el.Element(Selectors.ImageSource); err == nil {
		source, _ = s.Text()
		source = strings.TrimSpace(source)
	}
	dims := ""
	if d, err := el.Element(Selectors.ImageDims); err == nil {
		dims, _ = d.Text()
		dims = strings.TrimSpace(dims)
	}

	desc := source
	if dims != "" {
		if source != "" {
			desc = fmt.Sprintf("%s (%s)", source, dims)
		} else {
			desc = dims
		}
	}

	return core.SearchResult{
		Rank:        rank,
		URL:         imgURL,
		Title:       title,
		Description: desc,
	}, true
}

// SearchImage executes an Ecosia image search and returns normalized image
// results. It may return core.ErrCaptcha or core.ErrSearchTimeout.
// query.Start is ignored: per-page card count varies, so callers should
// drive depth through query.Limit alone.
func (e *Ecosia) SearchImage(ctx context.Context, query core.Query) (results []core.SearchResult, err error) {
	ctx = core.PrepareEngineContext(ctx, query, e.Name(), false)
	scoped := *e
	scoped.logger = e.logger.WithRequest(ctx)
	e = &scoped

	e.logger.Debug("Starting image search, query: %+v", query)
	defer func() {
		if recovered := recover(); recovered != nil {
			err = core.RecoverEnginePanicWithContext(ctx, e.Name(), recovered, e.logger)
			results = nil
		}
	}()

	out := []core.SearchResult{}
	pageNum := 0
	nextRank := 1
	// fetchPage loads one image page and appends parsed results.
	// Returns (done, error): done=true ends the outer loop without error.
	fetchPage := func() (bool, error) {
		u, err := BuildImageURL(query, pageNum)
		if err != nil {
			return false, err
		}

		page, err := e.Navigate(ctx, u)
		if err != nil {
			return false, err
		}
		defer core.DeferClosePage(ctx, page, &e.Browser)()

		if err := page.WaitLoad(); err != nil {
			e.logger.Error("Page load wait failed: %s", err)
			return false, core.ErrSearchTimeout
		}

		if _, err := page.Timeout(e.GetSelectorTimeout()).Element(Selectors.ImageResult); err != nil {
			if e.isCaptcha(page) {
				e.logger.Error("Captcha detected: %s", u)
				return false, core.ErrCaptcha
			}
			e.logger.Debug("No image results on page %d", pageNum)
			return true, nil
		}

		elements, err := page.Elements(Selectors.ImageResult)
		if err != nil {
			e.logger.Error("Cannot collect image results: %s", err)
			return false, core.ErrParser
		}

		for _, el := range elements {
			if res, ok := e.parseImageResult(el, nextRank); ok {
				out = append(out, res)
				nextRank++
			}
			if query.Limit > 0 && len(out) >= query.Limit {
				break
			}
		}
		return false, nil
	}

	for query.Limit <= 0 || len(out) < query.Limit {
		done, err := fetchPage()
		if err != nil {
			return nil, err
		}
		pageNum++
		if done {
			break
		}
		if query.Limit > 0 && len(out) >= query.Limit {
			break
		}
		if err := core.SleepContext(ctx, e.pageSleep); err != nil {
			return nil, err
		}
	}

	deduped := core.DeduplicateResults(out)
	if query.Limit > 0 && len(deduped) > query.Limit {
		deduped = deduped[:query.Limit]
	}
	e.logger.Info("Image search completed: %d results", len(deduped))
	return deduped, nil
}
