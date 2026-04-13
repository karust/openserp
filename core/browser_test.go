//go:build integration
// +build integration

package core

import (
	"os"
	"strings"
	"testing"

	"github.com/karust/openserp/testutil"
)

var browser *Browser

func TestCreateBrowser(t *testing.T) {
	testutil.RequireIntegration(t)

	var err error
	opts := BrowserOpts{IsHeadless: true, IsLeakless: false}
	browser, err = NewBrowser(opts)
	if err != nil {
		t.Fatalf("Error failed initializing browser: %s", err)
	}
}

// Manually observe test results for now.
func TestBot(t *testing.T) {
	testutil.RequireIntegration(t)
	if strings.TrimSpace(os.Getenv("OPENSERP_BOT_TESTS")) != "1" {
		t.Skip("set OPENSERP_BOT_TESTS=1 to run manual anti-bot screenshot integration test")
	}

	var err error
	opts := BrowserOpts{IsHeadless: false, IsLeakless: false, LeavePageOpen: true}
	browser, err = NewBrowser(opts)
	if err != nil {
		t.Fatalf("Error failed initializing browser: %s", err)
	}

	page, _ := browser.Navigate("https://bot.sannysoft.com")
	page.MustScreenshotFullPage("./test/screenshot_bot.png")

	page, _ = browser.Navigate("https://www.whatismybrowser.com/")
	page.MustScreenshotFullPage("./test/screenshot_browser.png")

	page, _ = browser.Navigate("https://abrahamjuliot.github.io/creepjs/")
	page.MustScreenshotFullPage("./test/screenshot_creep.png")
}
