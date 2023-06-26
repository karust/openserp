package core

import (
	"testing"
)

var browser *Browser

func TestCreateBrowser(t *testing.T) {
	// if browser != nil && browser.IsInitialized() {
	// 	return
	// }

	var err error
	opts := BrowserOpts{IsHeadless: true, IsLeakless: false}
	browser, err = NewBrowser(opts)
	if err != nil {
		t.Fatalf("Error failed initializing browser: %s", err)
	}
}

// func TestCreateLeaklessBrowser(t *testing.T) {
// 	var err error
// 	opts := BrowserOpts{IsHeadless: true, IsLeakless: true}
// 	browser, err = NewBrowser(opts)
// 	if err != nil {
// 		t.Fatalf("Error failed initializing leakless browser: %s", err)
// 	}
// }

// Manually observe test results for now
func TestBot(t *testing.T) {

	var err error
	opts := BrowserOpts{IsHeadless: false, IsLeakless: true, LeavePageOpen: true}
	browser, err = NewBrowser(opts)
	if err != nil {
		t.Fatalf("Error failed initializing browser: %s", err)
	}

	page := browser.Navigate("https://bot.sannysoft.com")
	page.MustScreenshotFullPage("./test/screenshot_bot.png")

	page = browser.Navigate("https://www.whatismybrowser.com/")
	page.MustScreenshotFullPage("./test/screenshot_browser.png")

	page = browser.Navigate("https://abrahamjuliot.github.io/creepjs/")
	page.MustScreenshotFullPage("./test/screenshot_creep.png")

}
