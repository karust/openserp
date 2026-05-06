package bing

// Selectors is the single source of truth for Bing SERP CSS selectors.
var Selectors = struct {
	Captcha        []string
	CookieBtn      string
	Results        string
	Ads            string
	ImageResults   string
	Title          string
	TitleFallbacks []string
	DescPrimary    string
	DescFallback   string
	AdTitle        string
}{
	Captcha:      []string{"div.captcha", "div.captcha_header"},
	CookieBtn:    "button#bnp_btn_accept",
	Results:      "li.b_algo",
	Ads:          "li.b_ad",
	ImageResults: "a.iusc, div.iuscp, div.isv",
	Title:        "h2 a",
	// TitleFallbacks are tried by FirstNonEmptyText when the primary Title
	// selector matches but yields empty text (Bing occasionally renders an
	// empty <h2><a/></h2> while the visible label sits in aria-label or h2).
	TitleFallbacks: []string{"h2", "a[aria-label]"},
	DescPrimary:    "div.b_caption p",
	DescFallback:   "div.b_caption div",
	AdTitle:        "h2 a",
}
