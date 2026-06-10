package google

// Selectors is the single source of truth for Google SERP CSS selectors.
// Both the browser parser (search.go, rod) and HTML parser (search_raw.go,
// goquery) reference these. When Google changes their DOM, edit here only.
var Selectors = struct {
	Captcha      string
	ResultStats  string
	CookieBtn    string
	Results      string
	ResultsBroad string
	Ad           string
	Link         string
	Title        string
	DescPrimary  string
	DescFallback string
	DescAny      string
	AnswerBox    string
	AnswerItem   string

	// Image search.
	ImageResults      string
	ImageLink         string
	ImageLinkFallback string
	ImageTitle        []string
}{
	Captcha:     "div[data-sitekey]",
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
