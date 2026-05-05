package baidu

// Selectors is the single source of truth for Baidu SERP CSS selectors.
var Selectors = struct {
	Captcha string
	Timeout string
	Results string
	Link    string
	Desc    string
}{
	Captcha: "div.passMod_dialog-wrapper",
	Timeout: "button.timeout-button",
	Results: "div.c-container.new-pmd",
	Link:    "a",
	Desc:    "div.c-abstract",
}
