package yandex

// Selectors is the single source of truth for Yandex SERP CSS selectors.
var Selectors = struct {
	Captcha    string
	NoResults  string
	Results    string
	Link       string
	Title      string
	Desc       string
	ImageItems string
}{
	Captcha:    "div.CheckboxCaptcha",
	NoResults:  "div.EmptySearchResults",
	Results:    "li[data-fast], li.serp-item",
	Link:       "a",
	Title:      "h2",
	Desc:       "span.OrganicTextContentSpan",
	ImageItems: "div[role='main'] div[data-state]",
}
