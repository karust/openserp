package yandex

// Selectors is the single source of truth for Yandex SERP CSS selectors.
var Selectors = struct {
	Captcha   string
	NoResults string
	Results   string
	AdMarkers []string
	// LinkPrimary is preferred over a generic <a>; falls back to title.Closest("a")
	// then the first <a> in the result block when absent.
	LinkPrimary   string
	Link          string
	Title         string
	Desc          string
	DescFallback  string
	ImageItems    string
	ImageItemsAlt []string
	ImageStateAll string
}{
	Captcha:   "div.CheckboxCaptcha",
	NoResults: "div.EmptySearchResults",
	Results:   "li[data-fast], li.serp-item",
	AdMarkers: []string{
		"[data-fast-name='direct']",
		"[data-fast-name='serp-adv']",
		"[data-bem*='serp-adv']",
		".serp-adv-item",
		".serp-adv__found",
		"[aria-label='Реклама']",
		"[title='Реклама']",
	},
	LinkPrimary:   "a.OrganicTitle-Link",
	Link:          "a",
	Title:         "h2",
	Desc:          "span.OrganicTextContentSpan",
	DescFallback:  "div.OrganicText",
	ImageItems:    "div[role='main'] div[data-state]",
	ImageItemsAlt: []string{"div[data-state*='serpList']"},
	ImageStateAll: "div[data-state]",
}
