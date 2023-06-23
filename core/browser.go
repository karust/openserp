package core

import (
	"time"

	"github.com/corpix/uarand"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/devices"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/stealth"
	"github.com/sirupsen/logrus"
)

type BrowserOpts struct {
	IsHeadless   bool          // Use browser interface
	IsLeakless   bool          // Force to kill browser
	Timeout      time.Duration // Timeout
	LanguageCode string
	WaitRequests bool // Wait requests to complete after navigation
}

func (o *BrowserOpts) Check() {
	if o.Timeout == 0 {
		o.Timeout = time.Second * 30
	}

	if o.LanguageCode == "" {
		o.LanguageCode = "en"
	}
}

type Browser struct {
	BrowserOpts
	browserAddr string
	browser     *rod.Browser
}

func NewBrowser(opts BrowserOpts) (*Browser, error) {
	opts.Check()
	logrus.Debugf("Browser options: %+v", opts)

	path, has := launcher.LookPath()
	logrus.Debug("Browser found: ", has)

	var err error
	b := Browser{BrowserOpts: opts}
	b.browserAddr, err = launcher.New().Bin(path).Leakless(opts.IsLeakless).Headless(opts.IsHeadless).Launch()

	return &b, err
}

// Check whether browser instance is already created
func (b *Browser) IsInitialized() bool {
	if b.browserAddr != "" {
		return true
	} else {
		return false
	}
}

// Open URL
func (b *Browser) Navigate(URL string) *rod.Page {
	logrus.Debug("Navigate to: ", URL)

	b.browser = rod.New().ControlURL(b.browserAddr)
	b.browser.MustConnect()
	b.browser.SetCookies(nil)

	page := stealth.MustPage(b.browser)
	wait := page.MustWaitRequestIdle()
	page.Navigate(URL)
	wait()

	page.MustEmulate(devices.Device{
		UserAgent:      uarand.GetRandom(),
		AcceptLanguage: b.LanguageCode,
	})
	return page
}

func (b *Browser) Close() error {
	return b.browser.Close()
}
