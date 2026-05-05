package ecosia

// Selectors is the single source of truth for Ecosia SERP CSS selectors.
var Selectors = struct {
	Mainline    string
	Result      string
	Ad          string
	ResultLink  string
	Title       string
	Desc        string
	ImageResult string
	ImageLink   string
	ImageSource string
	ImageDims   string
}{
	Mainline:    "[data-test-id='mainline']",
	Result:      "[data-test-id='mainline-result-web']",
	Ad:          "[data-test-id='mainline-result-ad']",
	ResultLink:  "[data-test-id='result-link']",
	Title:       "[data-test-id='result-title']",
	Desc:        "[data-test-id='result-description']",
	ImageResult: "[data-test-id='images-result']",
	ImageLink:   "[data-test-id='image-result-link']",
	ImageSource: "[data-test-id='image-result-source']",
	ImageDims:   "[data-test-id='image-result-dimensions']",
}
