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
	// DescAlt are additional description containers tried when Desc misses.
	// Baidu varies abstract markup across feature blocks (info cards, news rows).
	DescAlt []string
}{
	Captcha:       "div.passMod_dialog-wrapper",
	Timeout:       "button.timeout-button",
	Results:       "#content_left div.result.c-container",
	ResultsAlt:    []string{"#content_left div.result-op.c-container", "div.c-container.new-pmd"},
	ImageJSONRoot: []string{"body > pre", "pre"},
	Link:          "a",
	Desc:          "div.c-abstract",
	DescAlt:       []string{".content-right_8Zs40", ".summary-gap_3Jb4I"},
}
