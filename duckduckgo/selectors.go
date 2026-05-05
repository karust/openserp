package duckduckgo

// Selectors is the single source of truth for DuckDuckGo SERP CSS selectors.
var Selectors = struct {
	NoResults   string
	Results     []string
	Title       []string
	Desc        []string
	Link        []string
	AdBadge     []string
	ImageResult []string
	ImageImg    []string
	ImageTitle  []string
	ImageLink   []string
}{
	NoResults: "div[class*='no-results']",
	Results: []string{
		"article[data-testid='result']",
		"div.result",
		"div[data-testid='result']",
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
		"[data-testid='ad-badge']",
		".ad-badge",
		".result--ad",
	},
	ImageResult: []string{
		"figure",
	},
	ImageImg: []string{
		"img",
		"img[src*='duckduckgo.com']",
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
