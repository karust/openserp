package baidu

// Selectors is the single source of truth for Baidu SERP CSS selectors.
var Selectors = struct {
	Captcha       string
	Timeout       string
	Results       string
	ResultsAlt    []string
	ImageJSONRoot []string
	Link          string
	Desc          string
}{
	Captcha:       "div.passMod_dialog-wrapper",
	Timeout:       "button.timeout-button",
	Results:       "div.c-container.new-pmd",
	ResultsAlt:    []string{"#content_left div.result.c-container", "#content_left div.result-op.c-container"},
	ImageJSONRoot: []string{"body > pre", "pre"},
	Link:          "a",
	Desc:          "div.c-abstract",
}
