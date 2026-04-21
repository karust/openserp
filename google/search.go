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
	"golang.org/x/time/rate"
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

// GetRateLimiter returns a limiter configured from SearchEngineOptions.
func (gogl *Google) GetRateLimiter() *rate.Limiter {
	ratelimit := rate.Every(gogl.GetRatelimit())
	return rate.NewLimiter(ratelimit, gogl.RateBurst)
}

func (gogl *Google) getTotalResults(page *rod.Page) (int, error) {
	if gogl.rgxpGetDigits == nil {
		return 0, core.ErrParser
	}

	resultsStats, err := page.Timeout(gogl.GetSelectorTimeout()).Search("div#result-stats")
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
	captchaDiv, err := page.Timeout(gogl.GetSelectorTimeout()).Search("div[data-sitekey]")
	if err != nil {
		return false
	}

	sitekey, err := captchaDiv.First.Attribute("data-sitekey")
	if err != nil {
		gogl.logger.Error("Cannot get captcha sitekey: %s", err)
		return false
	}

	dataS, err := captchaDiv.First.Attribute("data-s")
	if err != nil {
		gogl.logger.Error("Cannot get captcha datas: %s", err)
		return false
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

func (gogl *Google) acceptCookies(page *rod.Page) {
	diaglogBtns, err := page.Timeout(gogl.Timeout / 10).Search("div[role='dialog'][aria-modal] button")
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

// Search executes a Google web search and returns normalized search results.
// It may return core.ErrCaptcha or core.ErrSearchTimeout.
func (gogl *Google) Search(ctx context.Context, query core.Query) (results []core.SearchResult, err error) {
	ctx = core.EnsureContext(ctx)
	gogl.logger.Debug("Starting search, query: %+v", query)
	defer func() {
		if recovered := recover(); recovered != nil {
			err = core.RecoverEnginePanic(gogl.Name(), recovered, gogl.logger)
			results = nil
		}
	}()

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

	// Accept cookie consent to get google answers
	if query.Answers {
		gogl.acceptCookies(page)
	}

	// Find all results using stable attributes
	searchRes, err := page.Timeout(gogl.Timeout).Search("div[data-hveid][data-ved]")
	if err != nil {
		gogl.logger.Error("Cannot parse search results: %s", err)
		return nil, core.ErrParser
	}

	if searchRes == nil {
		return nil, nil
	}

	totalResults, err := gogl.getTotalResults(page)
	if err != nil {
		gogl.logger.Debug("Failed to get total results: %v", err)
	}
	gogl.logger.Info("Found %d total results", totalResults)

	searchResultElems, err := searchRes.All()
	if err != nil {
		return nil, err
	}

	rank := query.Start
	for _, resEl := range searchResultElems {
		srchRes := core.SearchResult{}

		describe, err := resEl.Describe(1, false)
		if err != nil {
			gogl.logger.Debug("Result describe failed: %s", err)
			if core.IsRodObjectNotFound(err) {
				break
			}
			continue
		}
		attrs := strings.Join(describe.Attributes, " ")

		if strings.Contains(attrs, "data-text-ad") {
			// 1. Parse ads

			srchRes.Ad = true

			// Get URL
			link, err := resEl.Element("a")
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
			srchRes.Description = strings.Join(textSliced[4:], "\n")
			rank += 1

		} else if query.Answers && strings.Contains(attrs, "data-ulkwtsb") && !strings.Contains(attrs, "data-ispaa") {
			// 2. Parse answer boxes
			answerEls, err := resEl.Page().Search("div[data-hveid][data-ulkwtsb] div[data-q]")
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
				//answ.Page().WaitRepaint()
			}
			if err := core.SleepContext(ctx, 2*time.Second); err != nil {
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
				link, err := answ.Element("a")
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
				srchRes.Description = strings.Join(answerText[1:len(answerText)-2], "\n")
				srchRes.Rank = -1 * (i + 1)
				searchResults = append(searchResults, srchRes)
			}
			continue
		} else if strings.Contains(attrs, "data-ved") {
			// Parse regular search results
			// Get title from h3
			titleTag, err := resEl.Element("h3")
			if err != nil {
				continue
			}
			srchRes.Title, _ = titleTag.Text()

			// Get URL from parent link of h3
			link, err := titleTag.Parent()
			if err == nil {
				isLink, matchErr := link.Matches("a")
				if matchErr != nil {
					gogl.logger.Debug("Failed to match link selector: %s", matchErr)
				}
				if isLink {
					href, _ := link.Property("href")
					srchRes.URL = href.String()
				}
			}

			// Skip if URL is empty
			if srchRes.URL == "" {
				continue
			}

			// Get description using multiple fallback strategies
			desc := ""
			if descTag, err := resEl.Element("div[data-sncf='1'] div"); err == nil {
				desc, _ = descTag.Text()
			} else if descTag, err := resEl.Element("div.VwiC3b"); err == nil {
				desc, _ = descTag.Text()
			} else {
				// Structural fallback
				parent, err := titleTag.Parent()
				if err == nil {
					parent, err = parent.Parent()
					if err == nil {
						parent, err = parent.Parent()
						if err == nil {
							if descTag, err := parent.Next(); err == nil {
								if descDiv, err := descTag.Element("div"); err == nil {
									desc, _ = descDiv.Text()
								}
							}
						}
					}
				}
			}
			srchRes.Description = desc

			rank += 1
			srchRes.Rank = rank
			searchResults = append(searchResults, srchRes)
			continue

		} else {
			//fmt.Println(i, attrs)
			continue
		}

		srchRes.Rank = rank
		searchResults = append(searchResults, srchRes)
	}

	return core.DeduplicateResults(searchResults), nil
}

// SearchImage executes a Google image search and returns normalized image
// results. It may return core.ErrCaptcha or core.ErrSearchTimeout.
func (gogl *Google) SearchImage(ctx context.Context, query core.Query) ([]core.SearchResult, error) {
	ctx = core.EnsureContext(ctx)
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

	for len(searchResultsMap) < query.Limit {
		if err := page.WaitLoad(); err != nil {
			gogl.logger.Error("Image page load wait failed: %s", err)
			return *core.ConvertSearchResultsMap(searchResultsMap), core.ErrSearchTimeout
		}
		if err := page.Mouse.Scroll(0, 1000000, 1); err != nil {
			gogl.logger.Error("Image page scroll failed: %s", err)
			return *core.ConvertSearchResultsMap(searchResultsMap), core.ErrSearchTimeout
		}
		if err := page.WaitLoad(); err != nil {
			gogl.logger.Error("Image results load wait failed: %s", err)
			return *core.ConvertSearchResultsMap(searchResultsMap), core.ErrSearchTimeout
		}

		results, err := page.Timeout(gogl.Timeout).Search("div[data-hveid][data-ved][jsaction]")
		if err != nil {
			gogl.logger.Error("Cannot parse search results: %s", err)
			return *core.ConvertSearchResultsMap(searchResultsMap), core.ErrSearchTimeout
		}

		// Check why no results
		if results == nil {
			if gogl.checkCaptcha(page, query.ProxyURL) {
				gogl.logger.Error("Captcha detected: %s", url)
				return *core.ConvertSearchResultsMap(searchResultsMap), core.ErrCaptcha
			}
			return *core.ConvertSearchResultsMap(searchResultsMap), err
		}

		resultElements, err := results.All()
		if err != nil {
			return *core.ConvertSearchResultsMap(searchResultsMap), err
		}

		if len(resultElements) < len(searchResultsMap) {
			continue
		}

		for i, r := range resultElements {
			// TODO: parse AF_initDataCallback to optimize instead of this?
			err := r.Click(proto.InputMouseButtonRight, 1)
			if err != nil {
				gogl.logger.Error("Click failed")
				continue
			}

			dataVed, err := r.Attribute("data-ved")
			if err != nil {
				gogl.logger.Error("Missing data-ved attribute")
				continue
			}

			// If already have image with this ID
			if _, ok := searchResultsMap[*dataVed]; ok {
				continue
			}

			// Get URLs
			link, err := r.Element("a:not([ping])")
			if err != nil {
				continue
			}

			linkText, err := link.Property("href")
			if err != nil {
				gogl.logger.Debug("Missing href")
				continue
			}

			imgSrc, err := parseSourceImageURL(linkText.String())
			if err != nil {
				gogl.logger.Error("Failed to parse image URL: %v", err)
				continue
			}

			// Get title
			titleTag, err := r.Element("h3")
			if err != nil {
				gogl.logger.Error("Missing h3 tag")
				continue
			}

			title, err := titleTag.Text()
			if err != nil {
				gogl.logger.Error("Failed to extract title")
				title = "No title"
			}

			gR := core.SearchResult{
				Rank:        i + 1,
				URL:         imgSrc.OriginalURL,
				Title:       title,
				Description: fmt.Sprintf("Height:%v, Width:%v, Source Page: %v", imgSrc.Height, imgSrc.Width, imgSrc.PageURL),
			}
			searchResultsMap[*dataVed] = gR

			if err := r.Remove(); err != nil {
				gogl.logger.Debug("Failed to remove parsed image element: %s", err)
			}
		}
	}

	return *core.ConvertSearchResultsMap(searchResultsMap), nil
}

func (gogl *Google) close(ctx context.Context, page *rod.Page) {
	if !gogl.Browser.LeavePageOpen {
		err := core.ClosePageWithTimeout(ctx, page, time.Second)
		if err != nil {
			gogl.logger.Debug("Page close error: %v", err)
		}
	}
}
