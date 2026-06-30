package bing

// Selectors is the single source of truth for Bing SERP CSS selectors.
var Selectors = struct {
	Captcha          []string
	CaptchaMarkers   []string
	NoResultsMarkers []string
	CookieBtn        string
	ResultItems      string
	Results          string
	Ads              string
	ImageResults     string
	Title            string
	TitleFallbacks   []string
	DescPrimary      string
	DescFallback     string
	DescAny          string
	AdTitle          string
}{
	Captcha: []string{"div.captcha", "div.captcha_header"},
	// CaptchaMarkers/NoResultsMarkers are checked against lowercased page text
	// as a fallback when the CSS selectors above don't match a challenge page.
	CaptchaMarkers: []string{
		"verify that you are not a robot",
		"enter the characters you see",
	},
	NoResultsMarkers: []string{
		"there are no results for",
		"no results found for",
	},
	CookieBtn: "button#bnp_btn_accept",
	// ResultItems matches the main-column children only, so carousels and
	// "related searches" cards that reuse b_algo-style markup are excluded.
	ResultItems:  "#b_results > li.b_algo, #b_results > li.b_ad",
	Results:      "li.b_algo",
	Ads:          "li.b_ad",
	ImageResults: "a.iusc, div.iuscp, div.isv",
	Title:        "h2 a",
	// TitleFallbacks are tried when the primary Title selector matches but
	// yields empty text (Bing occasionally renders an empty <h2><a/></h2>
	// while the visible label sits in aria-label or h2).
	TitleFallbacks: []string{"h2", "a[aria-label]"},
	DescPrimary:    "div.b_caption p",
	DescFallback:   "div.b_caption div",
	DescAny:        "p",
	AdTitle:        "h2 a",
}
