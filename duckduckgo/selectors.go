package duckduckgo

// Selectors is the single source of truth for DuckDuckGo SERP CSS selectors.
//
// Order matters: WaitForElements / parseResults try selectors in declared order
// and stop on the first hit. Put the most specific / current variants first;
// keep older fallbacks last to absorb DOM rewrites without losing coverage.
var Selectors = struct {
	NoResults        []string
	CaptchaSelectors []string
	CaptchaMarkers   []string
	Results          []string
	Title            []string
	Desc             []string
	Link             []string
	AdBadge          []string
	ImageResult      []string
	ImageImg         []string
	ImageTitle       []string
	ImageLink        []string
}{
	NoResults: []string{
		"div[class*='no-results']",
		"[data-testid='no-results']",
		"div[data-result='no-results']",
	},
	// CaptchaSelectors detect DDG's anomaly/challenge interstitials via DOM.
	CaptchaSelectors: []string{
		"form[action*='anomaly']",
		"input[name='challenge']",
		"div[id*='anomaly']",
		"div[class*='captcha']",
	},
	// CaptchaMarkers are case-insensitive substrings used for full-HTML scan
	// fallback when the selectors above don't match. DDG sometimes returns a
	// plain-text 202 rate-limit page rather than a structured form, so the
	// HTML scan is the only reliable signal in those cases.
	CaptchaMarkers: []string{
		"bots user",
		"bots use duckduckgo too",
		"human verification",
		"unusual traffic",
		"anomaly",
	},
	// Results selectors target the canonical result card. The data-testid cards
	// are the innermost result element; selecting them avoids double-counting the
	// li[data-layout] wrapper that encloses each one (which previously inflated
	// ranks to 1,3,5,...). li[data-layout] is kept only as a fallback for older
	// markup that lacks data-testid. wikinlp/about layouts are deliberately
	// excluded here — they are instant-answer modules surfaced as serp_features.
	Results: []string{
		"article[data-testid='result'], article[data-testid='ad'], div[data-testid='result'], div[data-testid='ad']",
		"article[data-testid='result']",
		"article[data-testid='ad']",
		"div[data-testid='result']",
		"div[data-testid='ad']",
		"li[data-layout='organic'], li[data-layout='ad']",
		"li[data-layout='organic']",
		"li[data-layout='ad']",
		"div.result",
	},
	Title: []string{
		"h2",
		".result__title",
		".result__a",
	},
	Desc: []string{
		"div[data-result='snippet']",
		".result__snippet",
		".result__body",
	},
	Link: []string{
		"a[data-testid='result-title-a']",
		"a.result__a",
		"h2 a",
		"h3 a",
	},
	AdBadge: []string{
		"article[data-testid='ad']",
		"li[data-layout='ad']",
		"div[data-testid='ad']",
		"[data-testid='ad-badge']",
		".ad-badge",
		".result--ad",
	},
	ImageResult: []string{
		"figure[data-testid='image-result']",
		"figure",
	},
	ImageImg: []string{
		"img",
	},
	ImageTitle: []string{
		"figcaption a p span",
		"figcaption span",
		"figcaption p span",
		"h3",
		"span",
		"p",
	},
	ImageLink: []string{
		"figcaption a",
		"a",
	},
}
