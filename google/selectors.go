package google

// Selectors is the single source of truth for Google SERP CSS selectors.
// Both the browser parser (search.go, rod) and HTML parser (search_raw.go,
// goquery) reference these. When Google changes their DOM, edit here only.
var Selectors = struct {
	Captcha      string
	ResultStats  string
	CookieBtn    string
	Results      string
	Title        string
	DescPrimary  string
	DescFallback string
	AnswerBox    string
	AnswerItem   string
}{
	Captcha:      "div[data-sitekey]",
	ResultStats:  "div#result-stats",
	CookieBtn:    "div[role='dialog'][aria-modal] button",
	Results:      "div[data-hveid][data-ved]",
	Title:        "h3",
	DescPrimary:  "div[data-sncf='1'] div",
	DescFallback: "div.VwiC3b",
	AnswerBox:    "div[data-hveid][data-ulkwtsb] div[data-q]",
	AnswerItem:   "a",
}
