package google

import (
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

type Google struct {
	core.Browser
	core.SearchEngineOptions
	rgxpGetDigits *regexp.Regexp
	logger        *core.EngineLogger
}

func New(browser core.Browser, opts core.SearchEngineOptions) *Google {
	gogl := Google{Browser: browser}
	opts.Init()
	gogl.SearchEngineOptions = opts
	gogl.logger = core.NewEngineLogger("Google")
	gogl.rgxpGetDigits = regexp.MustCompile(`\d`)
	return &gogl
}

func (gogl *Google) Name() string {
	return "google"
}

func (gogl *Google) GetRateLimiter() *rate.Limiter {
	ratelimit := rate.Every(gogl.GetRatelimit())
	return rate.NewLimiter(ratelimit, gogl.RateBurst)
}

func (gogl *Google) getTotalResults(page *rod.Page) (int, error) {
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

func (gogl *Google) solveCaptcha(page *rod.Page, sitekey, datas string) bool {
	gogl.logger.Debug("Solve captcha: sitekey=%s", sitekey)

	resp, _, err := gogl.CaptchaSolver.SolveReCaptcha2(sitekey, page.MustInfo().URL, datas)
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

func (gogl *Google) checkCaptcha(page *rod.Page) bool {
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

	if gogl.IsSolveCaptcha {
		return !gogl.solveCaptcha(page, *sitekey, *dataS)
	}
	return true
}

func (gogl *Google) preparePage(page *rod.Page) {
	// Remove "similar queries" lists
	_, err := page.Eval(";(() => { document.querySelectorAll(`div[data-initq]`).forEach( el => el.remove());  })();")
	if err != nil {
		gogl.logger.Error("Page preparation failed: %s", err)
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
	btnElms[3].Click(proto.InputMouseButtonLeft, 1)

}

func (gogl *Google) Search(query core.Query) ([]core.SearchResult, error) {
	gogl.logger.Debug("Starting search, query: %+v", query)

	searchResults := []core.SearchResult{}

	// Build URL from query struct to open in browser
	url, err := BuildURL(query)
	if err != nil {
		return nil, err
	}
	page, err := gogl.Navigate(url)
	if err != nil {
		return nil, err
	}

	defer gogl.close(page)
	gogl.preparePage(page)

	// Check first if there captcha
	if gogl.checkCaptcha(page) {
		gogl.logger.Error("Captcha detected: %s", url)
		return nil, core.ErrCaptcha
	}

	// Accept cookie consent to get google answers
	if query.Answers {
		gogl.acceptCookies(page)
	}

	// Find all results using stable attributes
	results, err := page.Timeout(gogl.Timeout).Search("div[data-hveid][data-ved]")
	if err != nil {
		gogl.logger.Error("Cannot parse search results: %s", err)
		return nil, core.ErrSearchTimeout
	}

	if results == nil {
		return nil, nil
	}

	totalResults, err := gogl.getTotalResults(page)
	if err != nil {
		gogl.logger.Debug("Failed to get total results: %v", err)
	}
	gogl.logger.Info("Found %d total results", totalResults)

	searchResultElems, err := results.All()
	if err != nil {
		return nil, err
	}

	rank := 0
	for _, resEl := range searchResultElems {
		srchRes := core.SearchResult{}

		attrs := strings.Join(resEl.MustDescribe().Attributes, " ")

		if strings.Contains(attrs, "data-text-ad") {
			// 1. Parse ads

			srchRes.Ad = true

			// Get URL
			link, err := resEl.Element("a")
			if err != nil {
				gogl.logger.Debug("Missing link")
				continue
			}
			link.MoveMouseOut()

			href, err := link.Property("href")
			if err != nil {
				gogl.logger.Debug("Missing href")
				continue
			}
			srchRes.URL = href.String()

			// Get title
			srchRes.Title = link.MustText()

			// Get description
			text := resEl.MustText()
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
				answ.Click(proto.InputMouseButtonLeft, 1)
				answ.Focus()
				//answ.Page().WaitRepaint()
			}
			time.Sleep(time.Millisecond * 2000)

			for i, answ := range answers {
				answerText := strings.Split(answ.MustText(), "\n")
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
				link.MoveMouseOut()

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
			if err == nil && link.MustMatches("a") {
				href, _ := link.Property("href")
				srchRes.URL = href.String()
			}

			// Skip if URL is empty
			if srchRes.URL == "" {
				continue
			}

			// Get description using multiple fallback strategies
			desc := ""
			if descTag, err := resEl.Element("div[data-sncf='1'] div"); err == nil {
				desc = descTag.MustText()
			} else if descTag, err := resEl.Element("div.VwiC3b"); err == nil {
				desc = descTag.MustText()
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
									desc = descDiv.MustText()
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

func (gogl *Google) SearchImage(query core.Query) ([]core.SearchResult, error) {
	gogl.logger.Debug("Starting image search, query: %+v", query)

	searchResultsMap := map[string]core.SearchResult{}
	url, err := BuildImageURL(query)
	if err != nil {
		return nil, err
	}

	page, err := gogl.Navigate(url)
	if err != nil {
		return nil, err
	}

	defer gogl.close(page)

	for len(searchResultsMap) < query.Limit {
		page.WaitLoad()
		page.Mouse.Scroll(0, 1000000, 1)
		page.WaitLoad()

		results, err := page.Timeout(gogl.Timeout).Search("div[data-hveid][data-ved][jsaction]")
		if err != nil {
			gogl.logger.Error("Cannot parse search results: %s", err)
			return *core.ConvertSearchResultsMap(searchResultsMap), core.ErrSearchTimeout
		}

		// Check why no results
		if results == nil {
			if gogl.checkCaptcha(page) {
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
				gogl.logger.Error("Missing href")
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

			r.Remove()
		}
	}

	return *core.ConvertSearchResultsMap(searchResultsMap), nil
}

func (gogl *Google) close(page *rod.Page) {
	if !gogl.Browser.LeavePageOpen {
		err := page.Close()
		if err != nil {
			gogl.logger.Debug("Page close error: %v", err)
		}
	}
}
