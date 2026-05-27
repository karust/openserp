package baidu

// Selectors is the single source of truth for Baidu SERP CSS selectors.
var Selectors = struct {
	Captcha       string
	Timeout       string
	Results       string
	ResultsAlt    []string
	AdMarkers     []string
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
	AdMarkers:     []string{"[data-tuiguang]", "[data-click*='tuiguang']", ".ec-tuiguang", ".c-icon-bear-p"},
	ImageJSONRoot: []string{"body > pre", "pre"},
	Link:          "a",
	Desc:          "div.c-abstract",
	// DescAlt matches Baidu's hashed abstract containers by class *prefix*
	// ([class*='summary-gap_']) rather than a frozen hash suffix
	// (.summary-gap_3Jb4I): Baidu rotates the trailing hash per build (the same
	// page already carries summary-gap_3Jb4I and summary-gap_68jXq), and the old
	// content-right_8Zs40 suffix no longer appears at all. These two prefixes are
	// specific enough to use as substrings. text_ is NOT: it is Baidu's generic
	// text-styling class reused on dozens of nodes, so the baike abstract body is
	// pinned to its exact .text_2NOr6 hash and tried last.
	DescAlt: []string{"[class*='content-right_']", "[class*='summary-gap_']", "div.text_2NOr6"},
}
