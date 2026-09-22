package google

// Selectors is the single source of truth for Google SERP CSS selectors.
// Both the browser parser (search.go, rod) and HTML parser (search_raw.go,
// goquery) reference these. When Google changes their DOM, edit here only.
var Selectors = struct {
	Captcha        string
	CaptchaPage    string
	CaptchaMarkers []string
	SoftBlock      string
	NoResults      string
	ResultStats    string
	CookieBtn      string
	Results        string
	ResultsBroad   string
	Ad             string
	Link           string
	Title          string
	Cite           string
	SourceName     string
	DescPrimary    string
	DescFallback   string
	DescAny        string
	AnswerBox      string
	AnswerItem     string

	// Image search.
	ImageResults      string
	ImageLink         string
	ImageLinkFallback string
	ImageTitle        []string
}{
	Captcha:     "[data-sitekey]",
	CaptchaPage: "form#captcha-form, form[action*='/sorry/'], body[onload*='captcha'], [data-sitekey], .g-recaptcha, script[src*='recaptcha']",
	// CaptchaMarkers is the page-text fallback for captcha variants whose
	// markup doesn't match CaptchaPage.
	CaptchaMarkers: []string{
		"detected unusual traffic",
		"unusual traffic from your computer network",
		"before you continue",
		"not a robot",
		"solve the captcha",
	},
	SoftBlock:   "noscript",
	NoResults:   "#botstuff, #topstuff, .mnr-c",
	ResultStats: "div#result-stats",
	CookieBtn:   "div[role='dialog'][aria-modal] button",
	// Results targets the canonical organic result block. div.tF2Cxc is the
	// stable per-result wrapper; the :not(:has(div.tF2Cxc)) guard keeps only the
	// innermost block so nested knowledge-panel cards (which reuse tF2Cxc) don't
	// double-count or concatenate sibling titles. ResultsBroad is the legacy
	// attribute selector, kept as a fallback for SERP layouts without tF2Cxc.
	Results:      "div.tF2Cxc:not(:has(div.tF2Cxc))",
	ResultsBroad: "div[data-hveid][data-ved]",
	Ad:           "div[data-text-ad], [data-text-ad]",
	Link:         "a",
	Title:        "h3",
	// Cite and SourceName are the visible attribution Google renders for every
	// result: the breadcrumb ("https://www.pcmag.com › ... › VPN") and the site
	// name beside the favicon ("PCMag", "Reddit · r/VPN"). Google now hands
	// automated clients encrypted link wrappers instead of hrefs, but it cannot
	// stop showing the user which site a result came from — so these are what a
	// result's domain and identity are derived from when the href is opaque.
	// Between them they covered every result on the SERPs we sampled; cite alone
	// covered ~78% (Reddit and video blocks put comment counts in cite instead).
	Cite:         "cite",
	SourceName:   "span.VuuXrf",
	DescPrimary:  "div[data-sncf='1'] div",
	DescFallback: "div.VwiC3b",
	DescAny:      "div",
	AnswerBox:    "div[data-hveid][data-ulkwtsb] div[data-q]",
	AnswerItem:   "a",

	// ImageResults selects each image cell in the image SERP grid.
	ImageResults: "div[data-hveid][data-ved][jsaction]",
	// ImageLink: the canonical href of an image cell. The :not([ping])
	// variant excludes Google's click-tracking hops; the imgres fallback is
	// only present after the cell has been right-clicked to materialize.
	ImageLink:         "a:not([ping])",
	ImageLinkFallback: "a[href*='imgres']",
	// ImageTitle selectors are tried in order to recover a human-readable title.
	ImageTitle: []string{"h3", "a"},
}

func searchResultSelectors() []string {
	return []string{Selectors.Results, Selectors.ResultsBroad}
}
