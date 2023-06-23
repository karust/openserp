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
