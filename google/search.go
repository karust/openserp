package google

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/karust/openserp/core"
)

// Google implements core.SearchEngine for Google SERP pages.
type Google struct {
	core.Browser
	core.SearchEngineOptions
	rgxpGetDigits *regexp.Regexp
	logger        *core.EngineLogger
}

// New creates a Google engine instance with browser/runtime options applied.
func New(browser core.Browser, opts core.SearchEngineOptions) *Google {
	gogl := Google{Browser: browser}
	opts.Init()
	gogl.SearchEngineOptions = opts
	gogl.logger = core.NewEngineLogger("Google")
	gogl.rgxpGetDigits = regexp.MustCompile(`\d`)
	return &gogl
}

// Name returns the stable engine identifier.
func (gogl *Google) Name() string {
	return "google"
}

func (gogl *Google) getTotalResults(page *rod.Page) (int, error) {
	if gogl.rgxpGetDigits == nil {
		return 0, core.ErrParser
	}

	// Stats div is absent on many locales; probe first so .Search doesn't block
	// the full selector timeout.
	if has, _, err := page.Has(Selectors.ResultStats); err != nil || !has {
		return 0, nil
	}

	resultsStats, err := page.Timeout(gogl.GetSelectorTimeout()).Search(Selectors.ResultStats)
	if err != nil {
		return 0, errors.New("Result stats not found: " + err.Error())
	}

	statsText, err := resultsStats.First.Text()
	if err != nil {
		return 0, errors.New("Cannot extract result stats text: " + err.Error())
	}

	if len(statsText) == 0 {
		return 0, nil
	}

	// Remove search time seconds info from the end
	if len(statsText) > 15 {
		statsText = statsText[:len(statsText)-15]
	}

	foundDigits := gogl.rgxpGetDigits.FindAllString(statsText, -1)
	totalNum := strings.Join(foundDigits, "")
	total, err := strconv.Atoi(totalNum)
	if err != nil {
		return 0, err
	}
	return total, nil
}

func (gogl *Google) solveCaptcha(page *rod.Page, sitekey, datas, proxyURL string) bool {
	gogl.logger.Debug("Solve captcha: sitekey=%s", sitekey)

	if gogl.CaptchaSolver == nil {
		gogl.logger.Error("Captcha solver is not configured")
		return false
	}
	if page == nil {
		gogl.logger.Error("Captcha page context is missing")
		return false
	}

	info, err := page.Info()
	if err != nil {
		gogl.logger.Error("Cannot read page info for captcha solve: %s", err)
		return false
	}

	resp, _, err := gogl.CaptchaSolver.SolveReCaptcha2(sitekey, info.URL, datas, proxyURL)
	if err != nil {
		gogl.logger.Error("Captcha solve failed: %s", err)
		return false
	}

	gogl.logger.Debug("Captcha response received")
	_, err = page.Eval(fmt.Sprintf(`;(() => { document.getElementById("g-recaptcha-response").innerHTML="%s"; submitCallback(); })();`, resp))
	if err != nil {
		gogl.logger.Error("Failed to set captcha response: %s", err)
		return false
	}

	return true
}

func (gogl *Google) checkCaptcha(page *rod.Page, queryProxyURL string) bool {
	has, _, _ := page.Has(Selectors.Captcha)
	if !has {
		return false
	}

	captchaDiv, err := page.Element(Selectors.Captcha)
	if err != nil {
		return true
	}

	sitekey, err := captchaDiv.Attribute("data-sitekey")
	if err != nil {
		gogl.logger.Error("Cannot get captcha sitekey: %s", err)
		return true
	}

	dataS, err := captchaDiv.Attribute("data-s")
	if err != nil {
		gogl.logger.Error("Cannot get captcha datas: %s", err)
		return true
	}

	if gogl.IsSolveCaptcha && gogl.CaptchaSolverEnabled {
		proxyURL := queryProxyURL
		if strings.TrimSpace(proxyURL) == "" {
			proxyURL = gogl.ProxyURL
		}
		return !gogl.solveCaptcha(page, *sitekey, *dataS, proxyURL)
	}
	return true
}

func (gogl *Google) preparePage(page *rod.Page) {
	// Remove "similar queries" lists
	_, err := page.Eval(`() => {
		document.querySelectorAll("div[data-initq]").forEach((el) => el.remove())
	}`)
	if err != nil {
		gogl.logger.Debug("Page preparation skipped: %s", err)
	}
}

// waitAnswersExpanded polls until the first PAA entry has expanded to a
// title+body (≥2 text lines) or maxWait elapses, replacing a flat 2s sleep.
func (gogl *Google) waitAnswersExpanded(ctx context.Context, answers rod.Elements, maxWait time.Duration) error {
	if len(answers) == 0 || maxWait <= 0 {
		return nil
	}
	deadline := time.Now().Add(maxWait)
	for {
		text, err := answers[0].Text()
		if err == nil && len(strings.Split(text, "\n")) >= 2 {
			return nil
		}
		if !time.Now().Before(deadline) {
			return nil
		}
		if err := core.SleepContext(ctx, 100*time.Millisecond); err != nil {
			return err
		}
	}
}

func (gogl *Google) acceptCookies(page *rod.Page) {
	// Probe with Has first so a banner-less SERP doesn't block on .Search's full
	// timeout (AGENTS.md: use Has for existence).
	if has, _, err := page.Has(Selectors.CookieBtn); err != nil || !has {
		return
	}
	diaglogBtns, err := page.Timeout(gogl.Timeout / 10).Search(Selectors.CookieBtn)
	if err != nil {
		gogl.logger.Debug("Cookie consent not found: %s", err)
		return
	}
	btnElms, err := diaglogBtns.All()
	if err != nil {
		gogl.logger.Debug("Cannot get cookie consent buttons: %s", err)
		return
	}
	if len(btnElms) < 4 {
		gogl.logger.Debug("Cookie consent buttons unavailable")
		return
	}
	if err := btnElms[3].Click(proto.InputMouseButtonLeft, 1); err != nil {
		gogl.logger.Debug("Cookie consent click failed: %s", err)
	}

}

func googleElementHasAdMarker(el *rod.Element) bool {
	if el == nil {
		return false
	}
	matches, err := el.Matches(Selectors.Ad)
	if err == nil && matches {
		return true
	}
	child, err := el.Element(Selectors.Ad)
	return err == nil && child != nil
}

// Search executes a Google web search and returns normalized search results.
// It may return core.ErrCaptcha or core.ErrSearchTimeout.
func (gogl *Google) Search(ctx context.Context, query core.Query) (results []core.SearchResult, err error) {
	ctx = core.PrepareEngineContext(ctx, query, gogl.Name(), true)
	scoped := *gogl
	scoped.logger = gogl.logger.WithRequest(ctx)
	gogl = &scoped

	gogl.logger.Debug("Starting search, query: %+v", query)
	searchResults := []core.SearchResult{}

	// Build URL from query struct to open in browser
	url, err := BuildURL(query)
	if err != nil {
		return nil, err
	}
	page, err := gogl.Navigate(ctx, url)
	if err != nil {
		return nil, err
	}

	defer gogl.close(ctx, page)
	gogl.preparePage(page)

	// Check first if there captcha
	if gogl.checkCaptcha(page, query.ProxyURL) {
		gogl.logger.Error("Captcha detected: %s", url)
		return nil, core.ErrCaptcha
	}

	// Accept cookie consent so Google renders its SERP feature modules
	if query.Features {
		gogl.acceptCookies(page)
	}

	// Wait for the canonical organic wrapper first, then Google's broader
	// data-hveid/data-ved layout. Headless and headful Chrome can receive
	// different SERP markup for the same query.
	searchResultElems, matchedSelector, err := core.WaitForElements(ctx, page, searchResultSelectors(), gogl.GetSelectorTimeout())
	if err != nil {
		if gogl.checkCaptcha(page, query.ProxyURL) {
			gogl.logger.Error("Captcha detected: %s", url)
			return nil, core.ErrCaptcha
		}
		if core.IsContextDone(err) {
			return nil, err
		}
		// Keep empty-SERP behavior for selector timeout only.
		if errors.Is(err, core.ErrSearchTimeout) {
			return nil, nil
		}
		return nil, core.ErrSearchTimeout
	}
	gogl.logger.Debug("Search result selector matched: %s (%d elements)", matchedSelector, len(searchResultElems))

	totalResults, err := gogl.getTotalResults(page)
	if err != nil {
		gogl.logger.Debug("Failed to get total results: %v", err)
	}
	gogl.logger.Info("Found %d total results", totalResults)

	rank := core.NewRankStateAt(query.Start, query.Start+1)
	// When matched by the canonical organic selector (div.tF2Cxc) every element
	// is already an organic result, but the wrapper itself often lacks data-ved
	// (it sits on the outer .g/data-hveid container). Only require data-ved when
	// we fell back to the broad attribute selector, which also matches non-result
	// blocks (knowledge panels, nav) that must be filtered out.
	matchedOrganic := matchedSelector == Selectors.Results
	for _, resEl := range searchResultElems {
		srchRes := core.SearchResult{}

		isAd := googleElementHasAdMarker(resEl)
		isAnswerBox := query.Features && core.HasAttribute(resEl, "data-ulkwtsb") && !core.HasAttribute(resEl, "data-ispaa")
		isResultCandidate := matchedOrganic || core.HasAttribute(resEl, "data-ved")

		if isAd {
			// 1. Parse ads

			srchRes.Ad = true

			// Get URL
			link, err := resEl.Element(Selectors.Link)
			if err != nil {
				gogl.logger.Debug("Missing link")
				continue
			}
			if err := link.MoveMouseOut(); err != nil {
				gogl.logger.Debug("Move mouse out failed: %s", err)
			}

			href, err := link.Property("href")
			if err != nil {
				gogl.logger.Debug("Missing href")
				continue
			}
			srchRes.URL = href.String()

			// Get title
			titleText, err := link.Text()
			if err != nil {
				gogl.logger.Debug("Missing ad title text")
				continue
			}
			srchRes.Title = titleText

			// Get description
			text, err := resEl.Text()
			if err != nil {
				gogl.logger.Debug("Missing ad description text")
				continue
			}
			textSliced := strings.Split(text, "\n")
			if len(textSliced) > 4 {
				srchRes.Description = strings.Join(textSliced[4:], "\n")
			} else {
				srchRes.Description = strings.TrimSpace(text)
			}
			srchRes.Rank, srchRes.AbsoluteRank = rank.Next(true)
			searchResults = append(searchResults, srchRes)

		} else if isAnswerBox {
			// 2. Parse answer boxes
			answerEls, err := resEl.Page().Search(Selectors.AnswerBox)
			if err != nil {
				gogl.logger.Debug("Answer parsing failed: %s", err.Error())
				continue
			}

			answers, err := answerEls.All()
			if err != nil {
				gogl.logger.Debug("Answer parsing failed: %s", err.Error())
				continue
			}

			gogl.logger.Info("Found %d answers", len(answers))

			// Unvail answer contents
			for _, answ := range answers {
				if err := answ.Click(proto.InputMouseButtonLeft, 1); err != nil {
					gogl.logger.Debug("Answer expand click failed: %s", err)
					continue
				}
				if err := answ.Focus(); err != nil {
					gogl.logger.Debug("Answer focus failed: %s", err)
				}

			}
			// Poll for expansion (usually 200-400ms) instead of a flat 2s sleep.
			if err := gogl.waitAnswersExpanded(ctx, answers, 2*time.Second); err != nil {
				return nil, err
			}

			for i, answ := range answers {
				answerRawText, err := answ.Text()
				if err != nil {
					gogl.logger.Debug("Missing answer text")
					continue
				}
				answerText := strings.Split(answerRawText, "\n")
				if len(answerText) < 2 {
					gogl.logger.Debug("Short answer text: %s", answerText)
					continue
				}

				// Get URL
				link, err := answ.Element(Selectors.AnswerItem)
				if err != nil {
					gogl.logger.Debug("Missing answer link")
					continue
				}
				if err := link.MoveMouseOut(); err != nil {
					gogl.logger.Debug("Move mouse out failed: %s", err)
				}

				href, err := link.Property("href")
				if err != nil {
					gogl.logger.Debug("Missing answer href")
					continue
				}
				srchRes.URL = href.String()
				srchRes.Title = answerText[0]
				// answerText is [title, body..., source, meta]; drop the trailing
				// two metadata lines, but never slice past the title — a short
				// answer (len 2) would otherwise produce answerText[1:0] and panic.
				descEnd := max(len(answerText)-2, 1)
				srchRes.Description = strings.Join(answerText[1:descEnd], "\n")
				srchRes.Rank = -1 * (i + 1)
				srchRes.Type = core.ResultTypePeopleAlsoAsk
				searchResults = append(searchResults, srchRes)
			}
			continue
		} else if isResultCandidate {
			// Parse regular search results
			// Get title from h3
			titleTag, err := resEl.Element(Selectors.Title)
			if err != nil {
				continue
			}
			srchRes.Title, _ = titleTag.Text()

			// Get URL from the nearest link around the title.
			link := core.ClosestMatching(titleTag, Selectors.Link, 3)
			if link != nil {
				href, _ := link.Property("href")
				srchRes.URL = href.String()
			}

			// Skip if URL is empty
			if srchRes.URL == "" {
				continue
			}

			// Get description using multiple fallback strategies
			desc := ""
			if descTag, err := resEl.Element(Selectors.DescPrimary); err == nil {
				desc, _ = descTag.Text()
			} else if descTag, err := resEl.Element(Selectors.DescFallback); err == nil {
				desc, _ = descTag.Text()
			} else {
				// Structural fallback: the description lives in the sibling block
				// after the title's great-grandparent wrapper.
				anchor := titleTag
				for i := 0; i < 3 && anchor != nil; i++ {
					if anchor, err = anchor.Parent(); err != nil {
						anchor = nil
					}
				}
				if anchor != nil {
					if sib, err := anchor.Next(); err == nil {
						if descDiv, err := sib.Element(Selectors.DescAny); err == nil {
							desc, _ = descDiv.Text()
						}
					}
				}
			}
			srchRes.Description = desc

			srchRes.Rank, srchRes.AbsoluteRank = rank.Next(false)
			searchResults = append(searchResults, srchRes)
			continue

		} else {
			continue
		}
	}

	deduped := core.DeduplicateResults(searchResults)
	if len(deduped) == 0 {
		if gogl.checkCaptcha(page, query.ProxyURL) {
			return nil, core.ErrCaptcha
		}
		// Result candidates were found by Selectors.Results but none parsed
		// into usable rows: treat as a genuine no-results SERP rather than a
		// timeout, so callers don't retry pointlessly.
		if len(searchResultElems) == 0 {
			return nil, nil
		}
		return nil, core.ErrSearchTimeout
	}
	if query.Features {
		deduped = core.AttachFeaturesToFirstResult(deduped, extractGoogleFeaturesFromPage(ctx, page))
	}
	return deduped, nil
}

// SearchImage executes a Google image search and returns normalized image
// results. It may return core.ErrCaptcha or core.ErrSearchTimeout.
func (gogl *Google) SearchImage(ctx context.Context, query core.Query) ([]core.SearchResult, error) {
	ctx = core.PrepareEngineContext(ctx, query, gogl.Name(), true)
	scoped := *gogl
	scoped.logger = gogl.logger.WithRequest(ctx)
	gogl = &scoped

	gogl.logger.Debug("Starting image search, query: %+v", query)

	searchResultsMap := map[string]core.SearchResult{}
	url, err := BuildImageURL(query)
	if err != nil {
		return nil, err
	}

	page, err := gogl.Navigate(ctx, url)
	if err != nil {
		return nil, err
	}

	defer gogl.close(ctx, page)

	// Hard cap on outer scroll/parse passes. Google's image grid is virtualized
	// and infinite-scroll: WaitLoad after scroll never settles and individual
	// cells can hang on right-click. Cap iterations as a last-resort guard.
	const maxImagePasses = 20
	stagnant := 0
	for pass := 0; pass < maxImagePasses && core.ShouldFetchResultPage(len(searchResultsMap), query.Limit, pass); pass++ {
		if err := ctx.Err(); err != nil {
			return *core.ConvertSearchResultsMap(searchResultsMap), err
		}
		if _, err := page.Eval(`() => window.scrollBy(0, 1000000)`); err != nil {
			gogl.logger.Debug("Image page scroll failed: %s", err)
		}
		if err := core.SleepContext(ctx, 600*time.Millisecond); err != nil {
			return *core.ConvertSearchResultsMap(searchResultsMap), err
		}

		resultElements, _, err := core.WaitForElements(ctx, page, []string{Selectors.ImageResults}, gogl.GetSelectorTimeout())
		if err != nil {
			if gogl.checkCaptcha(page, query.ProxyURL) {
				gogl.logger.Error("Captcha detected: %s", url)
				return *core.ConvertSearchResultsMap(searchResultsMap), core.ErrCaptcha
			}
			break
		}

		before := len(searchResultsMap)
		for _, r := range resultElements {
			if err := ctx.Err(); err != nil {
				return *core.ConvertSearchResultsMap(searchResultsMap), err
			}
			gogl.parseImageCell(r, searchResultsMap)
			// Always remove the cell so the next outer iteration only sees
			// freshly-scrolled elements. Without this we re-iterate the same
			// already-parsed cells and the loop scales O(n²) — or worse,
			// hangs entirely when right-click on a stale node never returns.
			if err := r.Remove(); err != nil {
				gogl.logger.Debug("Failed to remove parsed image element: %s", err)
			}
			if query.Limit > 0 && len(searchResultsMap) >= query.Limit {
				break
			}
		}

		if len(searchResultsMap) == before {
			stagnant++
			if stagnant >= 2 {
				break
			}
		} else {
			stagnant = 0
		}
	}

	return *core.ConvertSearchResultsMap(searchResultsMap), nil
}

// parseImageCell extracts one image result from a Google image grid cell and
// stores it in dst keyed by the cell's data-ved. Returns silently on any
// failure — the surrounding loop calls r.Remove() unconditionally so a
// problem cell can't stall the outer scroll loop.
func (gogl *Google) parseImageCell(r *rod.Element, dst map[string]core.SearchResult) {
	// Right-clicking a Google image cell forces it to materialize its
	// `imgres` link (the grid is virtualized; href is absent until the
	// cell is interacted with).
	if err := r.Click(proto.InputMouseButtonRight, 1); err != nil {
		gogl.logger.Debug("Right-click on image cell failed: %s", err)
		return
	}

	dataVed, err := r.Attribute("data-ved")
	if err != nil || dataVed == nil || strings.TrimSpace(*dataVed) == "" {
		gogl.logger.Debug("Missing data-ved attribute")
		return
	}
	resultKey := *dataVed
	if _, ok := dst[resultKey]; ok {
		return
	}

	link, err := r.Element(Selectors.ImageLink)
	if err != nil {
		link, err = r.Element(Selectors.ImageLinkFallback)
		if err != nil {
			return
		}
	}

	linkText, err := link.Property("href")
	if err != nil {
		gogl.logger.Debug("Missing href")
		return
	}

	imgSrc, err := parseSourceImageURL(linkText.String())
	if err != nil {
		gogl.logger.Error("Failed to parse image URL: %v", err)
		return
	}
	if strings.TrimSpace(imgSrc.OriginalURL) == "" {
		return
	}

	title := core.FirstNonEmptyText(r, Selectors.ImageTitle...)
	if title == "" {
		title = core.FirstNonEmptyAttribute(r, "alt", "img")
	}
	if title == "" {
		title = core.FirstNonEmptyAttribute(r, "aria-label", "a")
	}
	if title == "" {
		title = "No title"
	}

	dst[resultKey] = core.SearchResult{
		Rank:        len(dst) + 1,
		URL:         imgSrc.OriginalURL,
		Title:       title,
		Description: fmt.Sprintf("Height:%v, Width:%v, Source Page: %v", imgSrc.Height, imgSrc.Width, imgSrc.PageURL),
	}
}

func (gogl *Google) close(ctx context.Context, page *rod.Page) {
	core.DeferClosePage(ctx, page, &gogl.Browser)()
}
