package bing

// Selectors is the single source of truth for Bing SERP CSS selectors.
var Selectors = struct {
	Captcha      []string
	CookieBtn    string
	Results      string
	Ads          string
	ImageResults string
	Title        string
	DescPrimary  string
	DescFallback string
	DescLast     string
	AdTitle      string
	AdDesc       string
}{
	Captcha:      []string{"div.captcha", "div.captcha_header"},
	CookieBtn:    "button#bnp_btn_accept",
	Results:      "li.b_algo",
	Ads:          "li.b_ad",
	ImageResults: "div.iuscp, div.isv",
	Title:        "a",
	DescPrimary:  "div.b_caption p",
	DescFallback: "div.b_caption div",
	DescLast:     "p",
	AdTitle:      "h2 a",
	AdDesc:       "p",
}
