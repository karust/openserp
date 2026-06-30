package ecosia

// Selectors is the single source of truth for Ecosia SERP CSS selectors.
var Selectors = struct {
	Captcha     string
	NoResults   string
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
	// Captcha matches Ecosia's Cloudflare Turnstile interstitial. The hidden
	// cf-turnstile-response input is present on every challenge page and never
	// on a real SERP, so it's a precise marker for the raw (browserless) path.
	Captcha:     "input[name='cf-turnstile-response']",
	NoResults:   "[data-test-id='web-no-results']",
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
